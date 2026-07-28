package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"

	"github.com/DharmilG/go-commerce/internal/auth"
	"github.com/DharmilG/go-commerce/internal/cache"
	"github.com/DharmilG/go-commerce/internal/cart"
	"github.com/DharmilG/go-commerce/internal/config"
	"github.com/DharmilG/go-commerce/internal/httpx"
	"github.com/DharmilG/go-commerce/internal/order"
	"github.com/DharmilG/go-commerce/internal/postgres"
	"github.com/DharmilG/go-commerce/internal/product"
	"github.com/DharmilG/go-commerce/internal/worker"
	"github.com/DharmilG/go-commerce/migrations"
)

// App owns the server, dependencies, and readiness state.
type App struct {
	cfg    config.Config
	logger *slog.Logger
	db     *pgxpool.Pool
	cache  *redis.Client
	server *http.Server
	ready  atomic.Bool

	auth         *auth.Service
	authHandler  *auth.Handler
	product      *product.Handler
	cart         *cart.Handler
	order        *order.Handler
	orderService *order.Service
	workers      *worker.Pool

	closeOnce sync.Once
}

// New wires the dependencies required by the API.
func New(ctx context.Context, cfg config.Config, logger *slog.Logger) (*App, error) {
	if cfg.AutoMigrate {
		if err := postgres.Migrate(cfg.DatabaseURL, migrations.FS); err != nil {
			return nil, fmt.Errorf("run migrations: %w", err)
		}
	}

	db, err := postgres.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		return nil, err
	}

	rdb, err := cache.Open(ctx, cfg)
	if err != nil {
		db.Close()
		return nil, err
	}

	tokens := auth.NewTokenManager(cfg.JWTSecret, cfg.JWTIssuer, cfg.JWTAudience, cfg.JWTTTL)
	authService := auth.NewService(db, tokens, cfg.BcryptCost, logger)

	productStore := product.NewStore(db)
	productHandler := product.NewHandler(productStore)

	cartStore := cart.NewStore(rdb, cfg.RedisPrefix, cfg.CartTTL, cfg.MaxCartItems, cfg.MaxItemQuantity)
	cartService := cart.NewService(cartStore, productStore, cfg)
	cartHandler := cart.NewHandler(cartService)

	workers := worker.NewPool(cfg.WorkerCount, cfg.WorkerQueue, worker.NewLogNotifier(logger), logger)
	orderService := order.NewService(db, cartService, cfg, logger, workers)
	orderHandler := order.NewHandler(orderService)

	a := &App{
		cfg:          cfg,
		logger:       logger,
		db:           db,
		cache:        rdb,
		auth:         authService,
		authHandler:  auth.NewHandler(authService),
		product:      productHandler,
		cart:         cartHandler,
		order:        orderHandler,
		orderService: orderService,
		workers:      workers,
	}

	a.server = &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           a.routes(),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    1 << 20,
		ErrorLog:          slog.NewLogLogger(logger.Handler(), slog.LevelError),
	}
	a.ready.Store(true)

	if removed, err := orderService.CleanupExpired(ctx); err != nil {
		logger.WarnContext(ctx, "initial idempotency cleanup failed", "error", err)
	} else if removed > 0 {
		logger.InfoContext(ctx, "removed expired idempotency keys", "count", removed)
	}
	go orderService.CleanupLoop(ctx, time.Hour)

	return a, nil
}

// Handler exposes the HTTP handler for tests and for main.
func (a *App) Handler() http.Handler {
	return a.server.Handler
}

// Run starts the HTTP server and blocks until the context is canceled or the
// listener fails. Shutdown is deliberately ordered: stop accepting requests,
// wait for active handlers, drain order events, then close the pools.
func (a *App) Run(ctx context.Context) error {
	errCh := make(chan error, 1)
	go func() {
		a.logger.Info("http server listening", "addr", a.cfg.HTTPAddr)
		errCh <- a.server.ListenAndServe()
	}()

	var runErr error
	select {
	case <-ctx.Done():
		a.logger.Info("shutdown signal received")
	case err := <-errCh:
		if !errors.Is(err, http.ErrServerClosed) {
			runErr = fmt.Errorf("http server: %w", err)
		}
	}

	a.ready.Store(false)

	shutdownCtx, cancel := context.WithTimeout(context.Background(), a.cfg.ShutdownTimeout)
	defer cancel()

	if err := a.server.Shutdown(shutdownCtx); err != nil {
		a.logger.Error("graceful HTTP shutdown failed", "error", err)
		_ = a.server.Close()
		if runErr == nil {
			runErr = fmt.Errorf("shutdown http server: %w", err)
		}
	}

	a.closeResources(shutdownCtx)

	a.logger.Info("shutdown complete")
	return runErr
}

// Close releases resources when New succeeded but Run was never called.
func (a *App) Close() {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	a.closeResources(ctx)
}

func (a *App) closeResources(ctx context.Context) {
	a.closeOnce.Do(func() {
		if a.workers != nil {
			if err := a.workers.Shutdown(ctx); err != nil {
				a.logger.Warn("order worker shutdown incomplete", "error", err)
			}
		}
		if a.db != nil {
			a.db.Close()
		}
		if a.cache != nil {
			if err := a.cache.Close(); err != nil {
				a.logger.Warn("close redis client", "error", err)
			}
		}
	})
}

func (a *App) routes() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /healthz", a.handleLive)
	mux.HandleFunc("GET /readyz", a.handleReady)

	mux.HandleFunc("POST /api/v1/auth/register", a.authHandler.Register)
	mux.HandleFunc("POST /api/v1/auth/login", a.authHandler.Login)
	mux.Handle("GET /api/v1/auth/me", a.protected(a.authHandler.Me))

	mux.HandleFunc("GET /api/v1/products", a.product.List)
	mux.HandleFunc("GET /api/v1/products/{id}", a.product.Get)

	mux.Handle("GET /api/v1/cart", a.protected(a.cart.Get))
	mux.Handle("POST /api/v1/cart/items", a.protected(a.cart.Add))
	mux.Handle("PUT /api/v1/cart/items/{productID}", a.protected(a.cart.Set))
	mux.Handle("DELETE /api/v1/cart/items/{productID}", a.protected(a.cart.Remove))
	mux.Handle("DELETE /api/v1/cart", a.protected(a.cart.Clear))

	mux.Handle("POST /api/v1/orders", a.protected(a.order.Place))
	mux.Handle("GET /api/v1/orders", a.protected(a.order.List))
	mux.Handle("GET /api/v1/orders/{id}", a.protected(a.order.Get))

	mux.Handle("POST /api/v1/admin/products", a.admin(a.product.Create))
	mux.Handle("PATCH /api/v1/admin/products/{id}", a.admin(a.product.Update))

	return httpx.Chain(
		mux,
		httpx.RequestID,
		httpx.Logger(a.logger),
		httpx.Recoverer(a.logger),
		httpx.SecurityHeaders,
	)
}

func (a *App) protected(next http.HandlerFunc) http.Handler {
	return a.auth.Middleware(next)
}

func (a *App) admin(next http.HandlerFunc) http.Handler {
	return a.auth.Middleware(auth.RequireAdmin(next))
}

func (a *App) handleLive(w http.ResponseWriter, _ *http.Request) {
	httpx.JSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (a *App) handleReady(w http.ResponseWriter, r *http.Request) {
	if !a.ready.Load() {
		httpx.JSON(w, http.StatusServiceUnavailable, map[string]any{
			"status": "shutting_down",
			"checks": map[string]string{},
		})
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()

	checks := map[string]string{
		"postgres": "ok",
		"redis":    "ok",
	}
	status := http.StatusOK
	overall := "ready"

	if err := a.db.Ping(ctx); err != nil {
		checks["postgres"] = "unavailable"
		status = http.StatusServiceUnavailable
		overall = "not_ready"
	}
	if err := a.cache.Ping(ctx).Err(); err != nil {
		checks["redis"] = "unavailable"
		status = http.StatusServiceUnavailable
		overall = "not_ready"
	}

	httpx.JSON(w, status, map[string]any{
		"status": overall,
		"checks": checks,
	})
}

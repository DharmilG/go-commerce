package integration_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"

	"github.com/DharmilG/go-commerce/internal/app"
	"github.com/DharmilG/go-commerce/internal/auth"
	"github.com/DharmilG/go-commerce/internal/config"
	"github.com/DharmilG/go-commerce/internal/postgres"
	"github.com/DharmilG/go-commerce/migrations"
)

type testSuite struct {
	cfg         config.Config
	application *app.App
	server      *httptest.Server
	client      *http.Client
	db          *pgxpool.Pool
	redis       *redis.Client
	adminToken  string
}

var suite *testSuite

func TestMain(m *testing.M) {
	databaseURL := strings.TrimSpace(os.Getenv("TEST_DATABASE_URL"))
	if databaseURL == "" {
		fmt.Println("integration tests skipped: set TEST_DATABASE_URL to a disposable PostgreSQL database")
		os.Exit(0)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	if err := ensureTestDatabase(ctx, databaseURL); err != nil {
		fmt.Fprintf(os.Stderr, "prepare test database: %v\n", err)
		os.Exit(1)
	}
	if err := postgres.Migrate(databaseURL, migrations.FS); err != nil {
		fmt.Fprintf(os.Stderr, "migrate test database: %v\n", err)
		os.Exit(1)
	}

	cfg := testConfig(databaseURL)
	logger := slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelError}))

	appCtx, appCancel := context.WithCancel(context.Background())
	application, err := app.New(appCtx, cfg, logger)
	if err != nil {
		fmt.Fprintf(os.Stderr, "start test app: %v\n", err)
		os.Exit(1)
	}

	db, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		fmt.Fprintf(os.Stderr, "open verification pool: %v\n", err)
		os.Exit(1)
	}

	redisDB, err := strconv.Atoi(envOr("TEST_REDIS_DB", "15"))
	if err != nil || redisDB == 0 {
		fmt.Fprintln(os.Stderr, "TEST_REDIS_DB must be a non-zero integer (tests refuse to flush the development Redis DB)")
		os.Exit(1)
	}
	rdb := redis.NewClient(&redis.Options{
		Addr: envOr("TEST_REDIS_ADDR", "localhost:6379"),
		DB:   redisDB,
	})
	if err := rdb.Ping(ctx).Err(); err != nil {
		fmt.Fprintf(os.Stderr, "ping test Redis: %v\n", err)
		os.Exit(1)
	}
	if err := rdb.FlushDB(ctx).Err(); err != nil {
		fmt.Fprintf(os.Stderr, "flush test Redis: %v\n", err)
		os.Exit(1)
	}

	server := httptest.NewServer(application.Handler())
	suite = &testSuite{
		cfg:         cfg,
		application: application,
		server:      server,
		client:      &http.Client{Timeout: 15 * time.Second},
		db:          db,
		redis:       rdb,
	}

	// Build one admin token for authorization tests.
	suite.adminToken = seedAdmin(ctx, databaseURL, cfg)

	code := m.Run()

	server.Close()
	appCancel()
	application.Close()
	db.Close()
	_ = rdb.Close()
	os.Exit(code)
}

func testConfig(databaseURL string) config.Config {
	redisDB, _ := strconv.Atoi(envOr("TEST_REDIS_DB", "15"))
	return config.Config{
		AppEnv:          "test",
		HTTPAddr:        "127.0.0.1:0",
		LogLevel:        slog.LevelError,
		LogFormat:       "text",
		DatabaseURL:     databaseURL,
		RedisAddr:       envOr("TEST_REDIS_ADDR", "localhost:6379"),
		RedisDB:         redisDB,
		RedisPrefix:     "go-commerce-test:",
		JWTSecret:       strings.Repeat("test-secret-", 4),
		JWTIssuer:       "go-commerce-test",
		JWTAudience:     "go-commerce-api-test",
		JWTTTL:          time.Hour,
		CartTTL:         10 * time.Minute,
		IdempotencyTTL:  24 * time.Hour,
		WorkerCount:     2,
		WorkerQueue:     32,
		ShutdownTimeout: 5 * time.Second,
		AutoMigrate:     true,
		BcryptCost:      4,
		Currency:        "USD",
		MaxCartItems:    10,
		MaxItemQuantity: 10,
	}
}

func ensureTestDatabase(ctx context.Context, databaseURL string) error {
	parsed, err := url.Parse(databaseURL)
	if err != nil {
		return fmt.Errorf("parse TEST_DATABASE_URL: %w", err)
	}
	databaseName := strings.TrimPrefix(parsed.Path, "/")
	if databaseName == "" || databaseName == "postgres" {
		return fmt.Errorf("TEST_DATABASE_URL must point at a disposable database, not %q", databaseName)
	}

	parsed.Path = "/postgres"
	admin, err := pgxpool.New(ctx, parsed.String())
	if err != nil {
		return fmt.Errorf("connect maintenance database: %w", err)
	}
	defer admin.Close()

	var exists bool
	if err := admin.QueryRow(ctx, "SELECT EXISTS (SELECT 1 FROM pg_database WHERE datname = $1)", databaseName).Scan(&exists); err != nil {
		return fmt.Errorf("check test database: %w", err)
	}
	if !exists {
		quoted := `"` + strings.ReplaceAll(databaseName, `"`, `""`) + `"`
		if _, err := admin.Exec(ctx, "CREATE DATABASE "+quoted); err != nil {
			return fmt.Errorf("create test database %s: %w", databaseName, err)
		}
	}
	return nil
}

func seedAdmin(ctx context.Context, databaseURL string, cfg config.Config) string {
	hash, err := auth.HashPassword("admin-password-123", cfg.BcryptCost)
	if err != nil {
		panic(err)
	}
	db, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		panic(err)
	}
	defer db.Close()

	email := "admin-" + uuid.NewString() + "@example.com"
	if _, err := db.Exec(ctx,
		`INSERT INTO users (id, email, password_hash, name, role) VALUES ($1, $2, $3, $4, 'admin')`,
		uuid.NewString(), email, hash, "Integration Admin",
	); err != nil {
		panic(err)
	}

	// Login through the real HTTP handler so the token exercises the same path.
	body := map[string]string{"email": email, "password": "admin-password-123"}
	status, payload, _, err := rawRequest("POST", "/api/v1/auth/login", body, "", nil)
	if err != nil || status != http.StatusOK {
		panic(fmt.Sprintf("admin login status=%d error=%v body=%s", status, err, payload))
	}
	var session auth.Session
	if err := json.Unmarshal(payload, &session); err != nil {
		panic(err)
	}
	return session.AccessToken
}

func reset(t *testing.T) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if _, err := suite.db.Exec(ctx,
		`TRUNCATE TABLE idempotency_keys, order_items, orders, products, users RESTART IDENTITY CASCADE`,
	); err != nil {
		t.Fatalf("reset database: %v", err)
	}
	if err := suite.redis.FlushDB(ctx).Err(); err != nil {
		t.Fatalf("reset redis: %v", err)
	}

	// The admin token is created outside the reset boundary, so recreate the
	// admin after each truncate.
	hash, err := auth.HashPassword("admin-password-123", suite.cfg.BcryptCost)
	if err != nil {
		t.Fatalf("hash admin password: %v", err)
	}
	email := "admin-" + uuid.NewString() + "@example.com"
	if _, err := suite.db.Exec(ctx,
		`INSERT INTO users (id, email, password_hash, name, role) VALUES ($1, $2, $3, $4, 'admin')`,
		uuid.NewString(), email, hash, "Integration Admin",
	); err != nil {
		t.Fatalf("recreate admin: %v", err)
	}
	status, payload, _, err := rawRequest("POST", "/api/v1/auth/login", map[string]string{
		"email": email, "password": "admin-password-123",
	}, "", nil)
	if err != nil || status != http.StatusOK {
		t.Fatalf("admin login after reset: status=%d err=%v body=%s", status, err, payload)
	}
	var session auth.Session
	if err := json.Unmarshal(payload, &session); err != nil {
		t.Fatalf("decode admin session: %v", err)
	}
	suite.adminToken = session.AccessToken
}

func rawRequest(method, path string, body any, token string, headers map[string]string) (int, []byte, http.Header, error) {
	var reader io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return 0, nil, nil, err
		}
		reader = bytes.NewReader(encoded)
	}

	req, err := http.NewRequest(method, suite.server.URL+path, reader)
	if err != nil {
		return 0, nil, nil, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	for key, value := range headers {
		req.Header.Set(key, value)
	}

	resp, err := suite.client.Do(req)
	if err != nil {
		return 0, nil, nil, err
	}
	defer resp.Body.Close()
	payload, err := io.ReadAll(resp.Body)
	if err != nil {
		return resp.StatusCode, nil, resp.Header, err
	}
	return resp.StatusCode, payload, resp.Header, nil
}

func request(t *testing.T, method, path string, body any, token string, headers map[string]string) (int, []byte, http.Header) {
	t.Helper()
	status, payload, responseHeaders, err := rawRequest(method, path, body, token, headers)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	return status, payload, responseHeaders
}

func decode[T any](t *testing.T, payload []byte) T {
	t.Helper()
	var value T
	if err := json.Unmarshal(payload, &value); err != nil {
		t.Fatalf("decode %s: %v", payload, err)
	}
	return value
}

func assertStatus(t *testing.T, got, want int, payload []byte) {
	t.Helper()
	if got != want {
		t.Fatalf("status = %d, want %d; body=%s", got, want, payload)
	}
}

func assertErrorCode(t *testing.T, payload []byte, want string) {
	t.Helper()
	var envelope struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(payload, &envelope); err != nil {
		t.Fatalf("decode error envelope: %v; body=%s", err, payload)
	}
	if envelope.Error.Code != want {
		t.Fatalf("error code = %q, want %q; body=%s", envelope.Error.Code, want, payload)
	}
}

func register(t *testing.T, email string) auth.Session {
	t.Helper()
	if email == "" {
		email = "user-" + uuid.NewString() + "@example.com"
	}
	status, payload, _ := request(t, http.MethodPost, "/api/v1/auth/register", map[string]string{
		"name":     "Integration User",
		"email":    email,
		"password": "password-123",
	}, "", nil)
	assertStatus(t, status, http.StatusCreated, payload)
	return decode[auth.Session](t, payload)
}

func login(t *testing.T, email, password string) auth.Session {
	t.Helper()
	status, payload, _ := request(t, http.MethodPost, "/api/v1/auth/login", map[string]string{
		"email": email, "password": password,
	}, "", nil)
	assertStatus(t, status, http.StatusOK, payload)
	return decode[auth.Session](t, payload)
}

func insertProduct(t *testing.T, sku string, priceCents int64, stock int) uuid.UUID {
	t.Helper()
	if sku == "" {
		sku = "SKU-" + uuid.NewString()[:8]
	}
	id := uuid.New()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := suite.db.Exec(ctx,
		`INSERT INTO products (id, sku, name, description, price_cents, stock, category, is_active)
		 VALUES ($1, $2, $3, '', $4, $5, 'Test', true)`,
		id.String(), sku, "Product "+sku, priceCents, stock,
	); err != nil {
		t.Fatalf("insert product: %v", err)
	}
	return id
}

func addToCart(t *testing.T, token string, productID uuid.UUID, quantity int) {
	t.Helper()
	status, payload, _ := request(t, http.MethodPost, "/api/v1/cart/items", map[string]any{
		"product_id": productID, "quantity": quantity,
	}, token, nil)
	assertStatus(t, status, http.StatusOK, payload)
}

func placeOrder(t *testing.T, token, key string, address map[string]string, headers map[string]string) (int, []byte, http.Header) {
	t.Helper()
	if address == nil {
		address = defaultAddress()
	}
	merged := map[string]string{}
	for k, v := range headers {
		merged[k] = v
	}
	if key != "" {
		merged["Idempotency-Key"] = key
	}
	return request(t, http.MethodPost, "/api/v1/orders", map[string]any{
		"shipping_address": address,
	}, token, merged)
}

func defaultAddress() map[string]string {
	return map[string]string{
		"full_name":   "Integration User",
		"line1":       "1 Test Street",
		"city":        "Ahmedabad",
		"state":       "Gujarat",
		"postal_code": "380001",
		"country":     "IN",
	}
}

func insertAdmin(t *testing.T, ctx context.Context) string {
	t.Helper()
	email := "admin-" + uuid.NewString() + "@example.com"
	hash, err := auth.HashPassword("admin-password-123", suite.cfg.BcryptCost)
	if err != nil {
		t.Fatalf("hash admin password: %v", err)
	}
	if _, err := suite.db.Exec(ctx,
		`INSERT INTO users (id, email, password_hash, name, role) VALUES ($1, $2, $3, $4, 'admin')`,
		uuid.NewString(), email, hash, "Integration Admin",
	); err != nil {
		t.Fatalf("insert admin: %v", err)
	}
	return email
}

func envOr(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}

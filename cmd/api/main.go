package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/DharmilG/go-commerce/internal/app"
	"github.com/DharmilG/go-commerce/internal/config"
	"github.com/DharmilG/go-commerce/internal/logging"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	logger := logging.New(cfg)
	slog.SetDefault(logger)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	application, err := app.New(ctx, cfg, logger)
	if err != nil {
		return fmt.Errorf("start app: %w", err)
	}
	defer application.Close()

	logger.Info("starting go-commerce API",
		"addr", cfg.HTTPAddr,
		"app_env", cfg.AppEnv,
	)

	return application.Run(ctx)
}

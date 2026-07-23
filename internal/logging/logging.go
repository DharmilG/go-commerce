package logging

import (
	"io"
	"log/slog"
	"os"

	"github.com/DharmilG/go-commerce/internal/config"
)

// New builds the application's structured logger.
func New(cfg config.Config) *slog.Logger {
	return NewWithWriter(cfg, os.Stdout)
}

// NewWithWriter is useful in tests that need to inspect output.
func NewWithWriter(cfg config.Config, writer io.Writer) *slog.Logger {
	opts := &slog.HandlerOptions{Level: cfg.LogLevel}

	var handler slog.Handler
	if cfg.LogFormat == "text" {
		handler = slog.NewTextHandler(writer, opts)
	} else {
		handler = slog.NewJSONHandler(writer, opts)
	}

	return slog.New(handler).With(
		"service", "go-commerce",
		"env", cfg.AppEnv,
	)
}

package config

import (
	"log/slog"
	"strings"
	"testing"
	"time"
)

func validEnv() map[string]string {
	return map[string]string{
		"DATABASE_URL": "postgres://postgres:postgres@localhost:5434/go_commerce?sslmode=disable",
		"JWT_SECRET":   strings.Repeat("s", 32),
	}
}

func lookupFrom(values map[string]string) func(string) (string, bool) {
	return func(key string) (string, bool) {
		value, ok := values[key]
		return value, ok
	}
}

func TestParseAppliesDefaults(t *testing.T) {
	cfg, err := parse(lookupFrom(validEnv()))
	if err != nil {
		t.Fatalf("parse() error = %v", err)
	}

	if cfg.HTTPAddr != ":8080" {
		t.Fatalf("HTTPAddr = %q, want :8080", cfg.HTTPAddr)
	}
	if cfg.CartTTL != 7*24*time.Hour {
		t.Fatalf("CartTTL = %v, want 168h", cfg.CartTTL)
	}
	if cfg.WorkerCount != 4 {
		t.Fatalf("WorkerCount = %d, want 4", cfg.WorkerCount)
	}
	if cfg.LogLevel != slog.LevelInfo {
		t.Fatalf("LogLevel = %v, want info", cfg.LogLevel)
	}
}

func TestParseRejectsMissingRequiredSettings(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(map[string]string)
		want   string
	}{
		{
			name: "missing database URL",
			mutate: func(values map[string]string) {
				delete(values, "DATABASE_URL")
			},
			want: "DATABASE_URL",
		},
		{
			name: "short JWT secret",
			mutate: func(values map[string]string) {
				values["JWT_SECRET"] = "too-short"
			},
			want: "JWT_SECRET",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			values := validEnv()
			tt.mutate(values)

			_, err := parse(lookupFrom(values))
			if err == nil {
				t.Fatal("parse() error = nil, want an error")
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error = %q, want it to mention %q", err, tt.want)
			}
		})
	}
}

func TestParseRejectsInvalidValues(t *testing.T) {
	tests := []struct {
		name  string
		key   string
		value string
		want  string
	}{
		{name: "bad duration", key: "CART_TTL", value: "seven days", want: "CART_TTL"},
		{name: "bad worker count", key: "WORKER_COUNT", value: "many", want: "WORKER_COUNT"},
		{name: "bad log format", key: "LOG_FORMAT", value: "xml", want: "LOG_FORMAT"},
		{name: "bad bcrypt cost", key: "BCRYPT_COST", value: "99", want: "BCRYPT_COST"},
		{name: "bad currency", key: "CURRENCY", value: "US", want: "CURRENCY"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			values := validEnv()
			values[tt.key] = tt.value

			_, err := parse(lookupFrom(values))
			if err == nil {
				t.Fatal("parse() error = nil, want an error")
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error = %q, want it to mention %q", err, tt.want)
			}
		})
	}
}

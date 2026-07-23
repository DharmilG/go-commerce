package config

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/joho/godotenv"
)

// Config contains every runtime setting the API needs.
type Config struct {
	AppEnv      string
	HTTPAddr    string
	LogLevel    slog.Level
	LogFormat   string
	DatabaseURL string

	RedisAddr     string
	RedisPassword string
	RedisDB       int
	RedisPrefix   string

	JWTSecret   string
	JWTIssuer   string
	JWTAudience string
	JWTTTL      time.Duration

	CartTTL         time.Duration
	IdempotencyTTL  time.Duration
	WorkerCount     int
	WorkerQueue     int
	ShutdownTimeout time.Duration
	AutoMigrate     bool

	BcryptCost      int
	Currency        string
	MaxCartItems    int
	MaxItemQuantity int
}

// Load reads .env when present, then validates the process environment.
func Load() (Config, error) {
	_ = godotenv.Load()
	return parse(os.LookupEnv)
}

func parse(lookup func(string) (string, bool)) (Config, error) {
	cfg := Config{
		AppEnv:          env(lookup, "APP_ENV", "development"),
		HTTPAddr:        env(lookup, "HTTP_ADDR", ":8080"),
		LogFormat:       strings.ToLower(env(lookup, "LOG_FORMAT", "json")),
		RedisAddr:       env(lookup, "REDIS_ADDR", "localhost:6379"),
		RedisPassword:   env(lookup, "REDIS_PASSWORD", ""),
		RedisPrefix:     env(lookup, "REDIS_PREFIX", "go-commerce:"),
		JWTIssuer:       env(lookup, "JWT_ISSUER", "go-commerce"),
		JWTAudience:     env(lookup, "JWT_AUDIENCE", "go-commerce-api"),
		Currency:        strings.ToUpper(env(lookup, "CURRENCY", "USD")),
		MaxCartItems:    50,
		MaxItemQuantity: 99,
		AutoMigrate:     true,
	}

	var problems []error

	cfg.DatabaseURL = env(lookup, "DATABASE_URL", "")
	if cfg.DatabaseURL == "" {
		problems = append(problems, errors.New("DATABASE_URL is required"))
	}

	cfg.JWTSecret = env(lookup, "JWT_SECRET", "")
	if len(cfg.JWTSecret) < 32 {
		problems = append(problems, errors.New("JWT_SECRET must be at least 32 characters"))
	}

	cfg.RedisDB = intValue(lookup, "REDIS_DB", 0, &problems)
	cfg.WorkerCount = intValue(lookup, "WORKER_COUNT", 4, &problems)
	cfg.WorkerQueue = intValue(lookup, "WORKER_QUEUE_SIZE", 256, &problems)
	cfg.BcryptCost = intValue(lookup, "BCRYPT_COST", 12, &problems)
	cfg.MaxCartItems = intValue(lookup, "MAX_CART_ITEMS", 50, &problems)
	cfg.MaxItemQuantity = intValue(lookup, "MAX_ITEM_QUANTITY", 99, &problems)

	cfg.JWTTTL = durationValue(lookup, "JWT_TTL", 24*time.Hour, &problems)
	cfg.CartTTL = durationValue(lookup, "CART_TTL", 7*24*time.Hour, &problems)
	cfg.IdempotencyTTL = durationValue(lookup, "IDEMPOTENCY_TTL", 7*24*time.Hour, &problems)
	cfg.ShutdownTimeout = durationValue(lookup, "SHUTDOWN_TIMEOUT", 15*time.Second, &problems)

	level := strings.ToLower(env(lookup, "LOG_LEVEL", "info"))
	switch level {
	case "debug":
		cfg.LogLevel = slog.LevelDebug
	case "info":
		cfg.LogLevel = slog.LevelInfo
	case "warn":
		cfg.LogLevel = slog.LevelWarn
	case "error":
		cfg.LogLevel = slog.LevelError
	default:
		problems = append(problems, fmt.Errorf("LOG_LEVEL must be debug, info, warn, or error, got %q", level))
	}

	if cfg.LogFormat != "json" && cfg.LogFormat != "text" {
		problems = append(problems, fmt.Errorf("LOG_FORMAT must be json or text, got %q", cfg.LogFormat))
	}
	if cfg.RedisDB < 0 {
		problems = append(problems, errors.New("REDIS_DB cannot be negative"))
	}
	if cfg.WorkerCount < 1 {
		problems = append(problems, errors.New("WORKER_COUNT must be at least 1"))
	}
	if cfg.WorkerQueue < 1 {
		problems = append(problems, errors.New("WORKER_QUEUE_SIZE must be at least 1"))
	}
	if cfg.BcryptCost < 4 || cfg.BcryptCost > 31 {
		problems = append(problems, errors.New("BCRYPT_COST must be between 4 and 31"))
	}
	if cfg.MaxCartItems < 1 {
		problems = append(problems, errors.New("MAX_CART_ITEMS must be at least 1"))
	}
	if cfg.MaxItemQuantity < 1 {
		problems = append(problems, errors.New("MAX_ITEM_QUANTITY must be at least 1"))
	}
	if len(cfg.Currency) != 3 {
		problems = append(problems, errors.New("CURRENCY must be a three-letter code"))
	}
	if cfg.JWTTTL <= 0 {
		problems = append(problems, errors.New("JWT_TTL must be positive"))
	}
	if cfg.CartTTL <= 0 {
		problems = append(problems, errors.New("CART_TTL must be positive"))
	}
	if cfg.IdempotencyTTL <= 0 {
		problems = append(problems, errors.New("IDEMPOTENCY_TTL must be positive"))
	}
	if cfg.ShutdownTimeout <= 0 {
		problems = append(problems, errors.New("SHUTDOWN_TIMEOUT must be positive"))
	}

	cfg.AutoMigrate = boolValue(lookup, "AUTO_MIGRATE", true, &problems)

	if len(problems) > 0 {
		return Config{}, errors.Join(problems...)
	}
	return cfg, nil
}

func env(lookup func(string) (string, bool), key, fallback string) string {
	if value, ok := lookup(key); ok && strings.TrimSpace(value) != "" {
		return strings.TrimSpace(value)
	}
	return fallback
}

func intValue(lookup func(string) (string, bool), key string, fallback int, problems *[]error) int {
	raw := env(lookup, key, "")
	if raw == "" {
		return fallback
	}
	value, err := strconv.Atoi(raw)
	if err != nil {
		*problems = append(*problems, fmt.Errorf("%s must be an integer: %w", key, err))
		return fallback
	}
	return value
}

func durationValue(lookup func(string) (string, bool), key string, fallback time.Duration, problems *[]error) time.Duration {
	raw := env(lookup, key, "")
	if raw == "" {
		return fallback
	}
	value, err := time.ParseDuration(raw)
	if err != nil {
		*problems = append(*problems, fmt.Errorf("%s must be a Go duration such as 15m or 24h: %w", key, err))
		return fallback
	}
	return value
}

func boolValue(lookup func(string) (string, bool), key string, fallback bool, problems *[]error) bool {
	raw := env(lookup, key, "")
	if raw == "" {
		return fallback
	}
	value, err := strconv.ParseBool(raw)
	if err != nil {
		*problems = append(*problems, fmt.Errorf("%s must be true or false: %w", key, err))
		return fallback
	}
	return value
}

package cache

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/DharmilG/go-commerce/internal/config"
)

// Open creates a Redis client and verifies connectivity.
func Open(ctx context.Context, cfg config.Config) (*redis.Client, error) {
	client := redis.NewClient(&redis.Options{
		Addr:         cfg.RedisAddr,
		Password:     cfg.RedisPassword,
		DB:           cfg.RedisDB,
		DialTimeout:  5 * time.Second,
		ReadTimeout:  3 * time.Second,
		WriteTimeout: 3 * time.Second,
		PoolSize:     20,
	})

	pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := client.Ping(pingCtx).Err(); err != nil {
		_ = client.Close()
		return nil, fmt.Errorf("ping redis: %w", err)
	}

	return client, nil
}

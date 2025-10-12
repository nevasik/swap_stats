package redis

import (
	"context"
	"dexcelerate/internal/config"
	"fmt"

	goredis "github.com/redis/go-redis/v9"
	"gitlab.com/nevasik7/alerting/logger"
)

type Client struct {
	Log logger.Logger
	*goredis.Client
}

func New(ctx context.Context, lg logger.Logger, cfg *config.RedisConfig) (*Client, error) {
	rdb := goredis.NewClient(&goredis.Options{
		Addr:         cfg.Addr,
		Username:     cfg.Username,
		Password:     cfg.Password,
		DB:           cfg.DB,
		DialTimeout:  cfg.DialTimeout,
		ReadTimeout:  cfg.ReadTimeout,
		WriteTimeout: cfg.WriteTimeout,
	})

	if err := rdb.Ping(ctx).Err(); err != nil {
		return nil, fmt.Errorf("failed to ping redis: %w", err)
	}

	return &Client{Log: lg, Client: rdb}, nil
}

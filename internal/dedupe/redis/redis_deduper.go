package redis

import (
	"context"
	"dexcelerate/internal/config"
	rdb "dexcelerate/internal/stores/redis"
	"errors"
	"fmt"
	"time"

	"gitlab.com/nevasik7/alerting/logger"
)

type Deduplicator interface {
	IsDuplicate(ctx context.Context, eventID string) (bool, error)
	MarkSeen(ctx context.Context, eventID string) error
	Health(ctx context.Context) error
}

var Deduper = (*RedisDedupe)(nil)

type RedisDedupe struct {
	log    logger.Logger
	rdb    *rdb.Client
	ttl    time.Duration
	prefix string
	bloom  *Bloom // optional
}

// Cluster dedupe for Redis SETNX + TTL
// prefix example "swapstats:dedupe:"
func NewRedisDeduper(log logger.Logger, cfg *config.DedupeConfig, rdb *rdb.Client, bloom *Bloom) (*RedisDedupe, error) {
	if cfg == nil {
		return nil, errors.New("config is required to the redis deduper")
	}
	if rdb == nil {
		return nil, errors.New("redis client is required to the redis deduper")
	}

	prefix := cfg.Prefix
	if prefix == "" {
		prefix = "dedupe:"
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := rdb.Ping(ctx).Err(); err != nil {
		return nil, err
	}

	return &RedisDedupe{
		log:    log,
		rdb:    rdb,
		ttl:    cfg.TTL,
		prefix: prefix,
		bloom:  bloom,
	}, nil
}

func (d *RedisDedupe) MarkSeen(ctx context.Context, eventID string) error {
	// if exists bloom and asked "see" -> duplicate(economy SETNX)
	if d.bloom != nil {
		if exists, err := d.bloom.Exists(ctx, eventID); err == nil && exists {
			return nil
		}
		// if bloom said "not see" -> continue(SETNX)
	}

	key := d.prefix + eventID
	ok, err := d.rdb.SetNX(ctx, key, 1, d.ttl).Result()
	if err != nil {
		d.log.Errorf("Redis SetNX error=%v", err)
		return fmt.Errorf("redis SetNX error=%v", err)
	}

	seen := !ok                  // ok=true -> new ID("not see"); ok=false -> "see"
	if !seen && d.bloom != nil { // if success new item and bloom not nil - add there
		if _, err = d.bloom.Add(ctx, eventID); err != nil {
			d.log.Errorf("Failed to add bloom id %s, err=%v", eventID, err)
		}
	}

	return nil
}

func (d *RedisDedupe) IsDuplicate(ctx context.Context, eventID string) (bool, error) {
	return false, nil
}

func (d *RedisDedupe) Health(ctx context.Context) error {
	if err := d.rdb.Ping(ctx).Err(); err != nil {
		d.log.Errorf("Redis connection error: %v", err)
	}
	return nil
}

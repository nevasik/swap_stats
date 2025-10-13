package redis

import (
	"context"
	"dexcelerate/internal/config"
	rdb "dexcelerate/internal/stores/redis"
	"fmt"
	"time"

	"gitlab.com/nevasik7/alerting/logger"
)

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
		return nil, fmt.Errorf("config is required to the redis deduper")
	}
	if rdb == nil {
		return nil, fmt.Errorf("redis client is required to the redis deduper")
	}

	prefix := cfg.Prefix
	if prefix == "" {
		prefix = "dedupe:"
	}

	return &RedisDedupe{
		log:    log,
		rdb:    rdb,
		ttl:    cfg.TTL,
		prefix: prefix,
		bloom:  bloom,
	}, nil
}

func (d *RedisDedupe) Seen(ctx context.Context, id string) (bool, error) {
	// if exists bloom and asked "see" -> duplicate(economy SETNX)
	if d.bloom != nil {
		if exists, err := d.bloom.Exists(ctx, id); err == nil && exists {
			return true, nil
		}
		// if bloom said "not see" -> continue(SETNX)
	}

	key := d.prefix + id
	ok, err := d.rdb.SetNX(ctx, key, 1, d.ttl).Result()
	if err != nil {
		d.log.Errorf("Redis SetNX error=%v", err)
		return false, fmt.Errorf("redis SetNX error=%v", err)
	}

	seen := !ok                  // ok=true -> new ID("not see"); ok=false -> "see"
	if !seen && d.bloom != nil { // if success new item and bloom not nil - add there
		if _, err = d.bloom.Add(ctx, id); err != nil {
			d.log.Errorf("Failed to add bloom id %s, err=%v", id, err)
		}
	}

	return seen, nil
}

package redis

import (
	"context"
	"dexcelerate/internal/config"
	rdb "dexcelerate/internal/stores/redis"
	"errors"
	"fmt"
)

/*
The Bloom prefilter is a low-cost probabilistic "seen/not seen" filter before accessing Redis SETNX
It reduces Redis QPS when dealing with a large influx of duplicates:
	- if the filter says "definitely not seen," we go to Redis;
	- if it says "most likely seen," we can skip Redis and immediately return the double (with a very low probability of false positives, as defined by error_rate)
*/

type Bloom struct {
	rdb      *rdb.Client
	Key      string
	Capacity int64
	ErrRate  float64
}

func NewBloom(cfg *config.BloomConfig, rdb *rdb.Client) (*Bloom, error) {
	if cfg == nil {
		return nil, errors.New("bloom config is required to the bloom")
	}
	if rdb == nil {
		return nil, errors.New("redis client is required to the bloom")
	}

	key := cfg.Key
	if key == "" {
		key = "dedupe:bf:events"
	}

	capacity := cfg.Capacity
	if capacity <= 0 {
		capacity = 1_000_000
	}

	errRate := cfg.ErrRate
	if errRate <= 0 {
		errRate = 0.001 // 1%
	}

	return &Bloom{
		//logger:   log,
		rdb:      rdb,
		Key:      key,
		Capacity: capacity,
		ErrRate:  errRate,
	}, nil
}

// Create filter if not exists. Repeated calls are safe
func (b *Bloom) Ensure(ctx context.Context) error {
	exists, err := b.rdb.Exists(ctx, b.Key).Result()
	if err != nil {
		return fmt.Errorf("failed to check if redis exists to the bloom, error: %w", err)
	}
	if exists > 0 {
		return nil // exists
	}

	// try added
	res := b.rdb.Do(ctx, "BF.RESERVE", b.Key, b.ErrRate, b.Capacity)
	if res.Err() != nil {
		return fmt.Errorf("BF.RESERVE failed: %w", res.Err()) // if module not load -> err unknown command 'BF.RESERVE'
	}

	return nil
}

// Added item to the filter. Return true if not exists definitely
func (b *Bloom) Add(ctx context.Context, item string) (bool, error) {
	res := b.rdb.Do(ctx, "BF.ADD", b.Key, item)
	if err := res.Err(); err != nil {
		return false, fmt.Errorf("failed to add item to bloom: %w", err)
	}

	// BF.ADD -> 1 -> item not add; 0 -> item exists(not definitely)
	v, err := res.Int()
	return v == 1, err
}

// Check exists, true -> item "probably" exists
func (b *Bloom) Exists(ctx context.Context, item string) (bool, error) {
	res := b.rdb.Do(ctx, "BF.EXISTS", b.Key, item)
	if err := res.Err(); err != nil {
		return false, fmt.Errorf("failed to check if item exists to bloom: %w", err)
	}
	v, err := res.Int()
	return v == 1, err
}

// Get key filter
func (b *Bloom) GetKey() string {
	return b.Key
}

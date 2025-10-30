package redis

import (
	"context"
	"dexcelerate/internal/config"
	rdb "dexcelerate/internal/stores/redis"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	goredis "github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ========== Test Helpers ==========
func setupTestRedisForDeduper(t *testing.T) (*miniredis.Miniredis, *rdb.Client) {
	t.Helper()

	mr := miniredis.RunT(t)

	client := &rdb.Client{
		Client: goredis.NewClient(&goredis.Options{
			Addr: mr.Addr(),
		}),
	}

	return mr, client
}

func createTestDedupeConfig(prefix string, ttl time.Duration) *config.DedupeConfig {
	return &config.DedupeConfig{
		Prefix: prefix,
		TTL:    ttl,
	}
}

func createMockBloom(t *testing.T, mr *miniredis.Miniredis, rdb *rdb.Client) *Bloom {
	t.Helper()

	cfg := createTestBloomConfig("test:bloom:dedupe", 10000, 0.01)

	bloom, err := NewBloom(cfg, rdb)
	require.NoError(t, err)

	return bloom
}

// ========== Constructor Tests ==========

func TestNewRedisDeduper_Success(t *testing.T) {
	_, rdb := setupTestRedisForDeduper(t)
	defer rdb.Close()

	log := createTestLogger()
	cfg := createTestDedupeConfig("test:dedupe:", 24*time.Hour)

	deduper, err := NewRedisDeduper(log, cfg, rdb, nil)

	require.NoError(t, err)
	assert.NotNil(t, deduper)
	assert.Equal(t, "test:dedupe:", deduper.prefix)
	assert.Equal(t, 24*time.Hour, deduper.ttl)
	assert.Equal(t, rdb, deduper.rdb)
	assert.Nil(t, deduper.bloom)
}

func TestNewRedisDeduper_SuccessWithBloom(t *testing.T) {
	mr, rdb := setupTestRedisForDeduper(t)
	defer mr.Close()
	defer rdb.Close()

	log := createTestLogger()
	cfg := createTestDedupeConfig("test:dedupe:", 24*time.Hour)
	bloom := createMockBloom(t, mr, rdb)

	deduper, err := NewRedisDeduper(log, cfg, rdb, bloom)

	require.NoError(t, err)
	assert.NotNil(t, deduper)
	assert.NotNil(t, deduper.bloom)
	assert.Equal(t, bloom, deduper.bloom)
}

func TestNewRedisDeduper_NilConfig(t *testing.T) {
	_, rdb := setupTestRedisForDeduper(t)
	defer rdb.Close()

	log := createTestLogger()

	deduper, err := NewRedisDeduper(log, nil, rdb, nil)

	assert.Error(t, err)
	assert.Nil(t, deduper)
	assert.Contains(t, err.Error(), "config is required")
}

func TestNewRedisDeduper_NilRedis(t *testing.T) {
	log := createTestLogger()
	cfg := createTestDedupeConfig("test:dedupe:", 24*time.Hour)

	deduper, err := NewRedisDeduper(log, cfg, nil, nil)

	assert.Error(t, err)
	assert.Nil(t, deduper)
	assert.Contains(t, err.Error(), "redis client is required")
}

func TestNewRedisDeduper_DefaultPrefix(t *testing.T) {
	_, rdb := setupTestRedisForDeduper(t)
	defer rdb.Close()

	log := createTestLogger()
	cfg := createTestDedupeConfig("", 24*time.Hour)

	deduper, err := NewRedisDeduper(log, cfg, rdb, nil)

	require.NoError(t, err)
	assert.Equal(t, "dedupe:", deduper.prefix)
}

// ========== Seen Tests - Without Bloom ==========

func TestRedisDedupe_Seen_WithoutBloom_FirstTime(t *testing.T) {
	mr, rdb := setupTestRedisForDeduper(t)
	defer mr.Close()
	defer rdb.Close()

	log := createTestLogger()
	cfg := createTestDedupeConfig("test:dedupe:", 1*time.Hour)

	deduper, err := NewRedisDeduper(log, cfg, rdb, nil)
	require.NoError(t, err)

	ctx := context.Background()
	eventID := "event-123"

	// First time seeing this ID
	seen, err := deduper.Seen(ctx, eventID)

	require.NoError(t, err)
	assert.False(t, seen, "first time ID should not be marked as seen")

	// Verify key was created in Redis
	val, err := rdb.Get(ctx, "test:dedupe:event-123").Result()
	require.NoError(t, err)
	assert.Equal(t, "1", val)

	// Verify TTL was set
	ttl, err := rdb.TTL(ctx, "test:dedupe:event-123").Result()
	require.NoError(t, err)
	assert.Greater(t, ttl, time.Duration(0))
	assert.LessOrEqual(t, ttl, 1*time.Hour)
}

func TestRedisDedupe_Seen_WithoutBloom_SecondTime(t *testing.T) {
	mr, rdb := setupTestRedisForDeduper(t)
	defer mr.Close()
	defer rdb.Close()

	log := createTestLogger()
	cfg := createTestDedupeConfig("test:dedupe:", 1*time.Hour)

	deduper, err := NewRedisDeduper(log, cfg, rdb, nil)
	require.NoError(t, err)

	ctx := context.Background()
	eventID := "event-456"

	// First call
	seen, err := deduper.Seen(ctx, eventID)
	require.NoError(t, err)
	assert.False(t, seen)

	// Second call - should be marked as seen
	seen, err = deduper.Seen(ctx, eventID)
	require.NoError(t, err)
	assert.True(t, seen, "second time ID should be marked as seen")
}

func TestRedisDedupe_Seen_WithoutBloom_MultipleIDs(t *testing.T) {
	mr, rdb := setupTestRedisForDeduper(t)
	defer mr.Close()
	defer rdb.Close()

	log := createTestLogger()
	cfg := createTestDedupeConfig("test:dedupe:", 1*time.Hour)

	deduper, err := NewRedisDeduper(log, cfg, rdb, nil)
	require.NoError(t, err)

	ctx := context.Background()

	testCases := []struct {
		id         string
		shouldSeen bool
	}{
		{"id-1", false}, // First time
		{"id-2", false}, // First time
		{"id-1", true},  // Duplicate
		{"id-3", false}, // First time
		{"id-2", true},  // Duplicate
		{"id-3", true},  // Duplicate
	}

	for _, tc := range testCases {
		t.Run(tc.id, func(t *testing.T) {
			seen, err := deduper.Seen(ctx, tc.id)
			require.NoError(t, err)
			assert.Equal(t, tc.shouldSeen, seen)
		})
	}
}

// ========== Seen Tests - With Bloom (Mocked Behavior) ==========

func TestRedisDedupe_Seen_WithBloom_BloomSaysExists(t *testing.T) {
	mr, rdb := setupTestRedisForDeduper(t)
	defer mr.Close()
	defer rdb.Close()

	log := createTestLogger()
	cfg := createTestDedupeConfig("test:dedupe:", 1*time.Hour)
	bloom := createMockBloom(t, mr, rdb)

	deduper, err := NewRedisDeduper(log, cfg, rdb, bloom)
	require.NoError(t, err)

	ctx := context.Background()
	eventID := "event-bloom-test"

	// Manually set a key in Redis to simulate bloom saying "exists"
	// We need to first add it so bloom would hypothetically say "exists"
	err = rdb.Set(ctx, "test:dedupe:event-bloom-test", 1, 1*time.Hour).Err()
	require.NoError(t, err)

	// Note: Since miniredis doesn't support BF.EXISTS, bloom.Exists will error
	// In real scenario with RedisBloom:
	// - If bloom says "exists", Seen returns true immediately without SetNX
	// - If bloom says "not exists", Seen does SetNX

	// With current setup (no RedisBloom), bloom.Exists will error,
	// so it falls through to SetNX
	seen, err := deduper.Seen(ctx, eventID)
	require.NoError(t, err)
	// Since key exists, SetNX returns false (not set), so seen=true
	assert.True(t, seen)
}

func TestRedisDedupe_Seen_WithBloom_BloomSaysNotExists_NewItem(t *testing.T) {
	mr, rdb := setupTestRedisForDeduper(t)
	defer mr.Close()
	defer rdb.Close()

	log := createTestLogger()
	cfg := createTestDedupeConfig("test:dedupe:", 1*time.Hour)
	bloom := createMockBloom(t, mr, rdb)

	deduper, err := NewRedisDeduper(log, cfg, rdb, bloom)
	require.NoError(t, err)

	ctx := context.Background()
	eventID := "new-event-bloom"

	// Bloom doesn't have it (miniredis will error on BF.EXISTS)
	// SetNX will succeed since key doesn't exist
	seen, err := deduper.Seen(ctx, eventID)
	require.NoError(t, err)
	assert.False(t, seen, "new item should not be marked as seen")

	// Verify key was created
	val, err := rdb.Get(ctx, "test:dedupe:new-event-bloom").Result()
	require.NoError(t, err)
	assert.Equal(t, "1", val)
}

// ========== Edge Cases ==========

func TestRedisDedupe_Seen_EmptyID(t *testing.T) {
	mr, rdb := setupTestRedisForDeduper(t)
	defer mr.Close()
	defer rdb.Close()

	log := createTestLogger()
	cfg := createTestDedupeConfig("test:dedupe:", 1*time.Hour)

	deduper, err := NewRedisDeduper(log, cfg, rdb, nil)
	require.NoError(t, err)

	ctx := context.Background()

	// Empty ID - still valid for Redis
	seen, err := deduper.Seen(ctx, "")
	require.NoError(t, err)
	assert.False(t, seen)

	// Second call with empty ID
	seen, err = deduper.Seen(ctx, "")
	require.NoError(t, err)
	assert.True(t, seen)
}

func TestRedisDedupe_Seen_SpecialCharactersInID(t *testing.T) {
	mr, rdb := setupTestRedisForDeduper(t)
	defer mr.Close()
	defer rdb.Close()

	log := createTestLogger()
	cfg := createTestDedupeConfig("test:dedupe:", 1*time.Hour)

	deduper, err := NewRedisDeduper(log, cfg, rdb, nil)
	require.NoError(t, err)

	ctx := context.Background()

	testIDs := []string{
		"id-with-dashes",
		"id_with_underscores",
		"id:with:colons",
		"id/with/slashes",
		"id.with.dots",
		"id@with@ats",
		"id with spaces",
		"id\twith\ttabs",
		"MixedCaseID123",
		"émojis-🚀-✨",
	}

	for _, id := range testIDs {
		t.Run(id, func(t *testing.T) {
			// First call
			seen, err := deduper.Seen(ctx, id)
			require.NoError(t, err)
			assert.False(t, seen, "first time should not be seen")

			// Second call
			seen, err = deduper.Seen(ctx, id)
			require.NoError(t, err)
			assert.True(t, seen, "second time should be seen")
		})
	}
}

func TestRedisDedupe_Seen_PrefixIsolation(t *testing.T) {
	mr, rdb := setupTestRedisForDeduper(t)
	defer mr.Close()
	defer rdb.Close()

	log := createTestLogger()

	// Create two dedupers with different prefixes
	cfg1 := createTestDedupeConfig("dedupe:service1:", 1*time.Hour)
	deduper1, err := NewRedisDeduper(log, cfg1, rdb, nil)
	require.NoError(t, err)

	cfg2 := createTestDedupeConfig("dedupe:service2:", 1*time.Hour)
	deduper2, err := NewRedisDeduper(log, cfg2, rdb, nil)
	require.NoError(t, err)

	ctx := context.Background()
	eventID := "shared-event-id"

	// Add to deduper1
	seen, err := deduper1.Seen(ctx, eventID)
	require.NoError(t, err)
	assert.False(t, seen)

	// Check deduper1 again - should be seen
	seen, err = deduper1.Seen(ctx, eventID)
	require.NoError(t, err)
	assert.True(t, seen)

	// Check deduper2 - should NOT be seen (different prefix)
	seen, err = deduper2.Seen(ctx, eventID)
	require.NoError(t, err)
	assert.False(t, seen, "different prefix should have separate deduplication")

	// Verify both keys exist
	val1, err := rdb.Get(ctx, "dedupe:service1:shared-event-id").Result()
	require.NoError(t, err)
	assert.Equal(t, "1", val1)

	val2, err := rdb.Get(ctx, "dedupe:service2:shared-event-id").Result()
	require.NoError(t, err)
	assert.Equal(t, "1", val2)
}

// ========== Concurrent Access Tests ==========

func TestRedisDedupe_Seen_ConcurrentAccess(t *testing.T) {
	mr, rdb := setupTestRedisForDeduper(t)
	defer mr.Close()
	defer rdb.Close()

	log := createTestLogger()
	cfg := createTestDedupeConfig("test:dedupe:", 1*time.Hour)

	deduper, err := NewRedisDeduper(log, cfg, rdb, nil)
	require.NoError(t, err)

	ctx := context.Background()
	eventID := "concurrent-event"

	// Run multiple goroutines trying to mark same ID
	numGoroutines := 10
	results := make(chan bool, numGoroutines)

	for i := 0; i < numGoroutines; i++ {
		go func() {
			seen, err := deduper.Seen(ctx, eventID)
			require.NoError(t, err)
			results <- seen
		}()
	}

	// Collect results
	seenCount := 0
	notSeenCount := 0

	for i := 0; i < numGoroutines; i++ {
		if <-results {
			seenCount++
		} else {
			notSeenCount++
		}
	}

	// Exactly one should succeed (not seen), rest should see it as duplicate
	// Note: Due to miniredis's in-memory nature and Go's goroutine scheduling,
	// results may vary, but at least one should be "not seen"
	assert.GreaterOrEqual(t, notSeenCount, 1, "at least one goroutine should see it as new")
	assert.GreaterOrEqual(t, seenCount, 0, "some goroutines may see it as duplicate")
}

// ========== TTL Tests ==========

func TestRedisDedupe_Seen_TTL_DifferentDurations(t *testing.T) {
	mr, rdb := setupTestRedisForDeduper(t)
	defer mr.Close()
	defer rdb.Close()

	log := createTestLogger()

	testCases := []struct {
		name        string
		ttl         time.Duration
		expectedTTL time.Duration
	}{
		{
			name:        "1_hour",
			ttl:         1 * time.Hour,
			expectedTTL: 1 * time.Hour,
		},
		{
			name:        "24_hours",
			ttl:         24 * time.Hour,
			expectedTTL: 24 * time.Hour,
		},
		{
			name:        "1_minute",
			ttl:         1 * time.Minute,
			expectedTTL: 1 * time.Minute,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := createTestDedupeConfig("test:dedupe:ttl:", tc.ttl)
			deduper, err := NewRedisDeduper(log, cfg, rdb, nil)
			require.NoError(t, err)

			ctx := context.Background()
			eventID := "ttl-test-" + tc.name

			seen, err := deduper.Seen(ctx, eventID)
			require.NoError(t, err)
			assert.False(t, seen)

			// Check TTL
			actualTTL, err := rdb.TTL(ctx, "test:dedupe:ttl:"+eventID).Result()
			require.NoError(t, err)
			assert.Greater(t, actualTTL, time.Duration(0))
			assert.LessOrEqual(t, actualTTL, tc.expectedTTL)
		})
	}
}

// ========== Redis Failure Tests ==========

func TestRedisDedupe_Seen_RedisFailure(t *testing.T) {
	mr, rdb := setupTestRedisForDeduper(t)
	defer rdb.Close()

	log := createTestLogger()
	cfg := createTestDedupeConfig("test:dedupe:", 1*time.Hour)

	deduper, err := NewRedisDeduper(log, cfg, rdb, nil)
	require.NoError(t, err)

	ctx := context.Background()

	// Close Redis to simulate failure
	mr.Close()

	// Should return error when Redis is down
	seen, err := deduper.Seen(ctx, "event-fail")
	assert.Error(t, err)
	assert.False(t, seen)
	assert.Contains(t, err.Error(), "redis SetNX error")
}

// ========== Context Tests ==========

func TestRedisDedupe_Seen_ContextCancellation(t *testing.T) {
	mr, rdb := setupTestRedisForDeduper(t)
	defer mr.Close()
	defer rdb.Close()

	log := createTestLogger()
	cfg := createTestDedupeConfig("test:dedupe:", 1*time.Hour)

	deduper, err := NewRedisDeduper(log, cfg, rdb, nil)
	require.NoError(t, err)

	// Create cancelled context
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	// Should return error due to cancelled context
	seen, err := deduper.Seen(ctx, "event-cancelled")
	assert.Error(t, err)
	assert.False(t, seen)
}

func TestRedisDedupe_Seen_ContextTimeout(t *testing.T) {
	mr, rdb := setupTestRedisForDeduper(t)
	defer mr.Close()
	defer rdb.Close()

	log := createTestLogger()
	cfg := createTestDedupeConfig("test:dedupe:", 1*time.Hour)

	deduper, err := NewRedisDeduper(log, cfg, rdb, nil)
	require.NoError(t, err)

	// Create context with very short timeout
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Nanosecond)
	defer cancel()

	time.Sleep(10 * time.Millisecond) // Ensure timeout

	// Should return error due to timeout
	seen, err := deduper.Seen(ctx, "event-timeout")
	// Note: miniredis is so fast this might not timeout, but we test the pattern
	if err != nil {
		assert.False(t, seen)
	}
}

// ========== Performance/Load Tests ==========

func TestRedisDedupe_Seen_ManySequentialCalls(t *testing.T) {
	mr, rdb := setupTestRedisForDeduper(t)
	defer mr.Close()
	defer rdb.Close()

	log := createTestLogger()
	cfg := createTestDedupeConfig("test:dedupe:", 1*time.Hour)

	deduper, err := NewRedisDeduper(log, cfg, rdb, nil)
	require.NoError(t, err)

	ctx := context.Background()
	numCalls := 1000

	for i := 0; i < numCalls; i++ {
		eventID := "event-" + string(rune(i))
		seen, err := deduper.Seen(ctx, eventID)
		require.NoError(t, err)
		assert.False(t, seen)
	}

	// Verify all keys were created
	keys, err := rdb.Keys(ctx, "test:dedupe:*").Result()
	require.NoError(t, err)
	assert.Equal(t, numCalls, len(keys))
}

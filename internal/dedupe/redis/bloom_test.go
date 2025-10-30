package redis

import (
	"context"
	"dexcelerate/internal/config"
	rdb "dexcelerate/internal/stores/redis"
	"testing"

	"github.com/alicebob/miniredis/v2"
	goredis "github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ========== Test Helpers ==========

func setupTestRedisForBloom(t *testing.T) (*miniredis.Miniredis, *rdb.Client) {
	t.Helper()

	mr := miniredis.RunT(t)

	client := &rdb.Client{
		Client: goredis.NewClient(&goredis.Options{
			Addr: mr.Addr(),
		}),
	}

	return mr, client
}

func createTestBloomConfig(key string, capacity int64, errRate float64) *config.BloomConfig {
	return &config.BloomConfig{
		Enabled:  true,
		Key:      key,
		Capacity: capacity,
		ErrRate:  errRate,
	}
}

// ========== Constructor Tests ==========

func TestNewBloom_Success(t *testing.T) {
	_, rdb := setupTestRedisForBloom(t)
	defer rdb.Close()

	cfg := createTestBloomConfig("test:bloom:key", 100000, 0.01)

	bloom, err := NewBloom(cfg, rdb)

	require.NoError(t, err)
	assert.NotNil(t, bloom)
	assert.Equal(t, "test:bloom:key", bloom.Key)
	assert.Equal(t, int64(100000), bloom.Capacity)
	assert.Equal(t, 0.01, bloom.ErrRate)
	assert.Equal(t, rdb, bloom.rdb)
}

func TestNewBloom_NilConfig(t *testing.T) {
	_, rdb := setupTestRedisForBloom(t)
	defer rdb.Close()

	bloom, err := NewBloom(nil, rdb)

	assert.Error(t, err)
	assert.Nil(t, bloom)
	assert.Contains(t, err.Error(), "bloom config is required")
}

func TestNewBloom_NilRedis(t *testing.T) {
	cfg := createTestBloomConfig("test:key", 100000, 0.01)

	bloom, err := NewBloom(cfg, nil)

	assert.Error(t, err)
	assert.Nil(t, bloom)
	assert.Contains(t, err.Error(), "redis client is required")
}

func TestNewBloom_DefaultValues(t *testing.T) {
	_, rdb := setupTestRedisForBloom(t)
	defer rdb.Close()

	testCases := []struct {
		name             string
		inputKey         string
		inputCapacity    int64
		inputErrRate     float64
		expectedKey      string
		expectedCapacity int64
		expectedErrRate  float64
	}{
		{
			name:             "empty_key_uses_default",
			inputKey:         "",
			inputCapacity:    100000,
			inputErrRate:     0.01,
			expectedKey:      "dedupe:bf:events",
			expectedCapacity: 100000,
			expectedErrRate:  0.01,
		},
		{
			name:             "zero_capacity_uses_default",
			inputKey:         "custom:key",
			inputCapacity:    0,
			inputErrRate:     0.01,
			expectedKey:      "custom:key",
			expectedCapacity: 1_000_000,
			expectedErrRate:  0.01,
		},
		{
			name:             "negative_capacity_uses_default",
			inputKey:         "custom:key",
			inputCapacity:    -100,
			inputErrRate:     0.01,
			expectedKey:      "custom:key",
			expectedCapacity: 1_000_000,
			expectedErrRate:  0.01,
		},
		{
			name:             "zero_errrate_uses_default",
			inputKey:         "custom:key",
			inputCapacity:    100000,
			inputErrRate:     0,
			expectedKey:      "custom:key",
			expectedCapacity: 100000,
			expectedErrRate:  0.001,
		},
		{
			name:             "negative_errrate_uses_default",
			inputKey:         "custom:key",
			inputCapacity:    100000,
			inputErrRate:     -0.5,
			expectedKey:      "custom:key",
			expectedCapacity: 100000,
			expectedErrRate:  0.001,
		},
		{
			name:             "all_empty_use_defaults",
			inputKey:         "",
			inputCapacity:    0,
			inputErrRate:     0,
			expectedKey:      "dedupe:bf:events",
			expectedCapacity: 1_000_000,
			expectedErrRate:  0.001,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := createTestBloomConfig(tc.inputKey, tc.inputCapacity, tc.inputErrRate)

			bloom, err := NewBloom(cfg, rdb)

			require.NoError(t, err)
			assert.Equal(t, tc.expectedKey, bloom.Key)
			assert.Equal(t, tc.expectedCapacity, bloom.Capacity)
			assert.Equal(t, tc.expectedErrRate, bloom.ErrRate)
		})
	}
}

// ========== Ensure Tests ==========

func TestBloom_Ensure_CreatesFilterWhenNotExists(t *testing.T) {
	mr, rdb := setupTestRedisForBloom(t)
	defer mr.Close()
	defer rdb.Close()

	cfg := createTestBloomConfig("test:bloom:ensure", 10000, 0.01)

	bloom, err := NewBloom(cfg, rdb)
	require.NoError(t, err)

	ctx := context.Background()

	// Simulate BF.RESERVE success
	// Note: miniredis doesn't support RedisBloom modules, so this will fail
	// We're testing the logic flow
	err = bloom.Ensure(ctx)

	// Since miniredis doesn't support BF.RESERVE, we expect an error
	// In real Redis with RedisBloom module, this would succeed
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "BF.RESERVE failed")
}

func TestBloom_Ensure_IdempotentWhenFilterExists(t *testing.T) {
	mr, rdb := setupTestRedisForBloom(t)
	defer mr.Close()
	defer rdb.Close()

	cfg := createTestBloomConfig("test:bloom:exists", 10000, 0.01)

	bloom, err := NewBloom(cfg, rdb)
	require.NoError(t, err)

	ctx := context.Background()

	// Manually set a key to simulate existing filter
	err = rdb.Set(ctx, bloom.Key, "exists", 0).Err()
	require.NoError(t, err)

	// Now Ensure should detect it exists and return nil without creating
	err = bloom.Ensure(ctx)
	assert.NoError(t, err)
}

// ========== Add Tests ==========

func TestBloom_Add(t *testing.T) {
	mr, rdb := setupTestRedisForBloom(t)
	defer mr.Close()
	defer rdb.Close()

	cfg := createTestBloomConfig("test:bloom:add", 10000, 0.01)

	bloom, err := NewBloom(cfg, rdb)
	require.NoError(t, err)

	ctx := context.Background()

	// Since miniredis doesn't support BF.ADD, we expect an error
	added, err := bloom.Add(ctx, "test-item-123")

	assert.Error(t, err)
	assert.False(t, added)
	assert.Contains(t, err.Error(), "failed to add item to bloom")
}

// ========== Exists Tests ==========

func TestBloom_Exists(t *testing.T) {
	mr, rdb := setupTestRedisForBloom(t)
	defer mr.Close()
	defer rdb.Close()

	cfg := createTestBloomConfig("test:bloom:exists", 10000, 0.01)

	bloom, err := NewBloom(cfg, rdb)
	require.NoError(t, err)

	ctx := context.Background()

	// Since miniredis doesn't support BF.EXISTS, we expect an error
	exists, err := bloom.Exists(ctx, "test-item-123")

	assert.Error(t, err)
	assert.False(t, exists)
	assert.Contains(t, err.Error(), "failed to check if item exists to bloom")
}

// ========== GetKey Tests ==========

func TestBloom_GetKey(t *testing.T) {
	_, rdb := setupTestRedisForBloom(t)
	defer rdb.Close()

	testCases := []struct {
		name        string
		key         string
		expectedKey string
	}{
		{
			name:        "custom_key",
			key:         "custom:bloom:key",
			expectedKey: "custom:bloom:key",
		},
		{
			name:        "default_key",
			key:         "",
			expectedKey: "dedupe:bf:events",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := createTestBloomConfig(tc.key, 10000, 0.01)
			bloom, err := NewBloom(cfg, rdb)
			require.NoError(t, err)

			key := bloom.GetKey()
			assert.Equal(t, tc.expectedKey, key)
		})
	}
}

// ========== Integration-Style Tests (Documentation) ==========

// Note: The tests above will fail for Add/Exists/Ensure with actual BF commands
// because miniredis doesn't support RedisBloom module commands.
// For proper integration testing with Bloom filters, you would need:
// 1. Real Redis instance with RedisBloom module loaded, OR
// 2. Docker-based test with Redis + RedisBloom, OR
// 3. Mock the redis Do() method to return expected responses

// Here's an example of what the test would look like with real Redis+RedisBloom:
/*
func TestBloom_Integration_AddAndExists(t *testing.T) {
	// This test requires Redis with RedisBloom module
	// Skip if REDIS_URL is not set
	redisURL := os.Getenv("REDIS_URL")
	if redisURL == "" {
		t.Skip("Skipping integration test: REDIS_URL not set")
	}

	client := goredis.NewClient(&goredis.Options{Addr: redisURL})
	defer client.Close()

	rdb := &rdb.Client{Client: client}
	log := createTestLogger()
	cfg := createTestBloomConfig("test:bloom:integration", 10000, 0.01)

	bloom, err := NewBloom(log, cfg, rdb)
	require.NoError(t, err)

	ctx := context.Background()

	// Ensure filter exists
	err = bloom.Ensure(ctx)
	require.NoError(t, err)

	// Add item
	added, err := bloom.Add(ctx, "item-1")
	require.NoError(t, err)
	assert.True(t, added) // First add returns true

	// Check exists
	exists, err := bloom.Exists(ctx, "item-1")
	require.NoError(t, err)
	assert.True(t, exists)

	// Check non-existent item
	exists, err = bloom.Exists(ctx, "item-non-existent")
	require.NoError(t, err)
	assert.False(t, exists)

	// Add same item again
	added, err = bloom.Add(ctx, "item-1")
	require.NoError(t, err)
	assert.False(t, added) // Already exists, returns false

	// Cleanup
	err = client.Del(ctx, bloom.Key).Err()
	require.NoError(t, err)
}
*/

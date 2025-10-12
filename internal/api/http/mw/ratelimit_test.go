package mw

import (
	"crypto/rand"
	"crypto/rsa"
	"dexcelerate/internal/config"
	"dexcelerate/internal/security"
	"dexcelerate/internal/stores/redis"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	miniredis "github.com/alicebob/miniredis/v2"
	goredis "github.com/redis/go-redis/v9"
)

// ========== Test Helpers ==========

func setupTestRedis(t *testing.T) (*miniredis.Miniredis, *redis.Client) {
	t.Helper()

	mr := miniredis.RunT(t)

	client := &redis.Client{
		Client: goredis.NewClient(&goredis.Options{
			Addr: mr.Addr(),
		}),
	}

	return mr, client
}

func createTestRateLimitConfig() *config.RateLimitConfig {
	return &config.RateLimitConfig{
		ByIP: config.RateBucket{
			RefillPerSec: 10,
			Burst:        20,
			TTL:          2 * time.Minute,
		},
		ByJWT: config.RateBucket{
			RefillPerSec: 50,
			Burst:        100,
			TTL:          2 * time.Minute,
		},
		TrustedProxiesList: []string{},
	}
}

func generateTestKeysForRL(t *testing.T) (*rsa.PrivateKey, *rsa.PublicKey) {
	t.Helper()
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	return privateKey, &privateKey.PublicKey
}

func createTestTokenForRL(t *testing.T, privateKey *rsa.PrivateKey, sub string) string {
	t.Helper()

	now := time.Now()
	claims := jwt.RegisteredClaims{
		Subject:   sub,
		Audience:  jwt.ClaimStrings{"test-aud"},
		Issuer:    "test-iss",
		ExpiresAt: jwt.NewNumericDate(now.Add(1 * time.Hour)),
		IssuedAt:  jwt.NewNumericDate(now),
	}

	token := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	tokenString, err := token.SignedString(privateKey)
	require.NoError(t, err)
	return tokenString
}

// ========== Constructor Tests ==========

func TestNewRateLimit(t *testing.T) {
	_, rdb := setupTestRedis(t)
	cfg := createTestRateLimitConfig()

	t.Run("panic_when_config_is_nil", func(t *testing.T) {
		assert.Panics(t, func() {
			NewRateLimit(nil, rdb, nil)
		})
	})

	t.Run("panic_when_redis_is_nil", func(t *testing.T) {
		assert.Panics(t, func() {
			NewRateLimit(cfg, nil, nil)
		})
	})

	t.Run("successful_creation_without_verifier", func(t *testing.T) {
		middleware := NewRateLimit(cfg, rdb, nil)
		assert.NotNil(t, middleware)
		assert.Equal(t, cfg, middleware.Cfg)
		assert.Equal(t, rdb, middleware.Rdb)
		assert.Nil(t, middleware.Verifier)
	})

	t.Run("successful_creation_with_verifier", func(t *testing.T) {
		_, pubKey := generateTestKeysForRL(t)
		verifier := &security.RS256Verifier{PubKey: pubKey}

		middleware := NewRateLimit(cfg, rdb, verifier)
		assert.NotNil(t, middleware)
		assert.Equal(t, verifier, middleware.Verifier)
	})

	t.Run("sets_default_ttl_when_zero", func(t *testing.T) {
		cfgNoTTL := &config.RateLimitConfig{
			ByIP: config.RateBucket{
				RefillPerSec: 10,
				Burst:        20,
				TTL:          0, // Zero TTL
			},
			ByJWT: config.RateBucket{
				RefillPerSec: 50,
				Burst:        100,
				TTL:          0, // Zero TTL
			},
		}

		middleware := NewRateLimit(cfgNoTTL, rdb, nil)
		assert.Equal(t, 2*time.Minute, middleware.Cfg.ByIP.TTL)
		assert.Equal(t, 2*time.Minute, middleware.Cfg.ByJWT.TTL)
	})
}

// ========== Handler Tests - IP Rate Limiting ==========

func TestRateLimitMiddleware_Handler_IPLimit(t *testing.T) {
	mr, rdb := setupTestRedis(t)
	defer mr.Close()

	cfg := &config.RateLimitConfig{
		ByIP: config.RateBucket{
			RefillPerSec: 2, // 2 requests per second
			Burst:        3, // Max 3 requests
			TTL:          1 * time.Minute,
		},
		ByJWT: config.RateBucket{
			RefillPerSec: 100,
			Burst:        100,
			TTL:          1 * time.Minute,
		},
	}

	middleware := NewRateLimit(cfg, rdb, nil)

	nextHandlerCalls := 0
	nextHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		nextHandlerCalls++
		w.WriteHeader(http.StatusOK)
	})

	handler := middleware.Handler(nextHandler)

	// First 3 requests should pass (burst = 3)
	for i := 1; i <= 3; i++ {
		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		req.RemoteAddr = "192.168.1.100:12345"
		rec := httptest.NewRecorder()

		handler.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusOK, rec.Code, "request %d should pass", i)
		assert.Contains(t, rec.Header().Get("X-RateLimit-Limit-IP"), "3")
		assert.NotEmpty(t, rec.Header().Get("X-RateLimit-Remaining-IP"))
	}

	assert.Equal(t, 3, nextHandlerCalls)

	// 4th request should be rate limited
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.RemoteAddr = "192.168.1.100:12345"
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusTooManyRequests, rec.Code)
	assert.Contains(t, rec.Body.String(), "rate limit exceeded")
	assert.NotEmpty(t, rec.Header().Get("Retry-After"))
	assert.Equal(t, 3, nextHandlerCalls, "next handler should not be called")
}

func TestRateLimitMiddleware_Handler_DifferentIPsIndependent(t *testing.T) {
	mr, rdb := setupTestRedis(t)
	defer mr.Close()

	cfg := &config.RateLimitConfig{
		ByIP: config.RateBucket{
			RefillPerSec: 1,
			Burst:        1, // Only 1 request allowed
			TTL:          1 * time.Minute,
		},
		ByJWT: config.RateBucket{
			RefillPerSec: 100,
			Burst:        100,
			TTL:          1 * time.Minute,
		},
	}

	middleware := NewRateLimit(cfg, rdb, nil)

	nextHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	handler := middleware.Handler(nextHandler)

	// Request from IP1 should pass
	req1 := httptest.NewRequest(http.MethodGet, "/test", nil)
	req1.RemoteAddr = "192.168.1.1:12345"
	rec1 := httptest.NewRecorder()
	handler.ServeHTTP(rec1, req1)
	assert.Equal(t, http.StatusOK, rec1.Code)

	// Request from IP2 should also pass (different IP)
	req2 := httptest.NewRequest(http.MethodGet, "/test", nil)
	req2.RemoteAddr = "192.168.1.2:12345"
	rec2 := httptest.NewRecorder()
	handler.ServeHTTP(rec2, req2)
	assert.Equal(t, http.StatusOK, rec2.Code)

	// Second request from IP1 should be limited
	req3 := httptest.NewRequest(http.MethodGet, "/test", nil)
	req3.RemoteAddr = "192.168.1.1:12345"
	rec3 := httptest.NewRecorder()
	handler.ServeHTTP(rec3, req3)
	assert.Equal(t, http.StatusTooManyRequests, rec3.Code)
}

// ========== Handler Tests - JWT Rate Limiting ==========

func TestRateLimitMiddleware_Handler_JWTLimit(t *testing.T) {
	mr, rdb := setupTestRedis(t)
	defer mr.Close()

	privKey, pubKey := generateTestKeysForRL(t)
	verifier := &security.RS256Verifier{
		PubKey: pubKey,
		Aud:    "test-aud",
		Iss:    "test-iss",
	}

	cfg := &config.RateLimitConfig{
		ByIP: config.RateBucket{
			RefillPerSec: 100, // High limit so IP doesn't block
			Burst:        100,
			TTL:          1 * time.Minute,
		},
		ByJWT: config.RateBucket{
			RefillPerSec: 1,
			Burst:        2, // Only 2 requests allowed per user
			TTL:          1 * time.Minute,
		},
	}

	middleware := NewRateLimit(cfg, rdb, verifier)

	nextHandlerCalls := 0
	nextHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		nextHandlerCalls++
		w.WriteHeader(http.StatusOK)
	})

	handler := middleware.Handler(nextHandler)

	token := createTestTokenForRL(t, privKey, "user123")

	// First 2 requests should pass
	for i := 1; i <= 2; i++ {
		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		req.RemoteAddr = "192.168.1.100:12345"
		req.Header.Set("Authorization", "Bearer "+token)
		rec := httptest.NewRecorder()

		handler.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusOK, rec.Code, "request %d should pass", i)
		assert.Contains(t, rec.Header().Get("X-RateLimit-Limit-JWT"), "2")
	}

	assert.Equal(t, 2, nextHandlerCalls)

	// 3rd request should be rate limited
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.RemoteAddr = "192.168.1.100:12345"
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusTooManyRequests, rec.Code)
	assert.Contains(t, rec.Body.String(), "rate limit exceeded")
	assert.Equal(t, 2, nextHandlerCalls)
}

func TestRateLimitMiddleware_Handler_DifferentUsersIndependent(t *testing.T) {
	mr, rdb := setupTestRedis(t)
	defer mr.Close()

	privKey, pubKey := generateTestKeysForRL(t)
	verifier := &security.RS256Verifier{
		PubKey: pubKey,
		Aud:    "test-aud",
		Iss:    "test-iss",
	}

	cfg := &config.RateLimitConfig{
		ByIP: config.RateBucket{
			RefillPerSec: 100,
			Burst:        100,
			TTL:          1 * time.Minute,
		},
		ByJWT: config.RateBucket{
			RefillPerSec: 1,
			Burst:        1, // Only 1 request per user
			TTL:          1 * time.Minute,
		},
	}

	middleware := NewRateLimit(cfg, rdb, verifier)

	nextHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	handler := middleware.Handler(nextHandler)

	// User1 first request should pass
	token1 := createTestTokenForRL(t, privKey, "user1")
	req1 := httptest.NewRequest(http.MethodGet, "/test", nil)
	req1.RemoteAddr = "192.168.1.100:12345"
	req1.Header.Set("Authorization", "Bearer "+token1)
	rec1 := httptest.NewRecorder()
	handler.ServeHTTP(rec1, req1)
	assert.Equal(t, http.StatusOK, rec1.Code)

	// User2 first request should also pass (different user)
	token2 := createTestTokenForRL(t, privKey, "user2")
	req2 := httptest.NewRequest(http.MethodGet, "/test", nil)
	req2.RemoteAddr = "192.168.1.100:12345"
	req2.Header.Set("Authorization", "Bearer "+token2)
	rec2 := httptest.NewRecorder()
	handler.ServeHTTP(rec2, req2)
	assert.Equal(t, http.StatusOK, rec2.Code)

	// User1 second request should be limited
	req3 := httptest.NewRequest(http.MethodGet, "/test", nil)
	req3.RemoteAddr = "192.168.1.100:12345"
	req3.Header.Set("Authorization", "Bearer "+token1)
	rec3 := httptest.NewRecorder()
	handler.ServeHTTP(rec3, req3)
	assert.Equal(t, http.StatusTooManyRequests, rec3.Code)
}

// ========== Handler Tests - Headers ==========

func TestRateLimitMiddleware_Handler_ResponseHeaders(t *testing.T) {
	mr, rdb := setupTestRedis(t)
	defer mr.Close()

	cfg := createTestRateLimitConfig()
	middleware := NewRateLimit(cfg, rdb, nil)

	nextHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	handler := middleware.Handler(nextHandler)

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.RemoteAddr = "192.168.1.100:12345"
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	// Check that rate limit headers are present
	assert.NotEmpty(t, rec.Header().Get("X-RateLimit-Limit-IP"))
	assert.NotEmpty(t, rec.Header().Get("X-RateLimit-Remaining-IP"))

	// JWT headers should not be present if no token provided
	assert.Empty(t, rec.Header().Get("X-RateLimit-Limit-JWT"))
	assert.Empty(t, rec.Header().Get("X-RateLimit-Remaining-JWT"))
}

func TestRateLimitMiddleware_Handler_ResponseHeadersWithJWT(t *testing.T) {
	mr, rdb := setupTestRedis(t)
	defer mr.Close()

	privKey, pubKey := generateTestKeysForRL(t)
	verifier := &security.RS256Verifier{
		PubKey: pubKey,
		Aud:    "test-aud",
		Iss:    "test-iss",
	}

	cfg := createTestRateLimitConfig()
	middleware := NewRateLimit(cfg, rdb, verifier)

	nextHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	handler := middleware.Handler(nextHandler)

	token := createTestTokenForRL(t, privKey, "user123")

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.RemoteAddr = "192.168.1.100:12345"
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	// Both IP and JWT headers should be present
	assert.NotEmpty(t, rec.Header().Get("X-RateLimit-Limit-IP"))
	assert.NotEmpty(t, rec.Header().Get("X-RateLimit-Remaining-IP"))
	assert.NotEmpty(t, rec.Header().Get("X-RateLimit-Limit-JWT"))
	assert.NotEmpty(t, rec.Header().Get("X-RateLimit-Remaining-JWT"))
}

// ========== calculateRetryAfter Tests ==========

func TestCalculateRetryAfter(t *testing.T) {
	_, rdb := setupTestRedis(t)

	testCases := []struct {
		name             string
		ipRefillPerSec   int
		jwtRefillPerSec  int
		okIP             bool
		okJWT            bool
		expectedMinRetry int
	}{
		{
			name:             "both_limits_exceeded_ip_slower",
			ipRefillPerSec:   10, // 0.1s per token
			jwtRefillPerSec:  20, // 0.05s per token
			okIP:             false,
			okJWT:            false,
			expectedMinRetry: 1, // Should use IP's slower rate
		},
		{
			name:             "both_limits_exceeded_jwt_slower",
			ipRefillPerSec:   20, // 0.05s per token
			jwtRefillPerSec:  10, // 0.1s per token
			okIP:             false,
			okJWT:            false,
			expectedMinRetry: 1,
		},
		{
			name:             "only_ip_exceeded",
			ipRefillPerSec:   50, // 0.02s per token
			jwtRefillPerSec:  100,
			okIP:             false,
			okJWT:            true,
			expectedMinRetry: 1,
		},
		{
			name:             "only_jwt_exceeded",
			ipRefillPerSec:   100,
			jwtRefillPerSec:  50,
			okIP:             true,
			okJWT:            false,
			expectedMinRetry: 1,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := &config.RateLimitConfig{
				ByIP: config.RateBucket{
					RefillPerSec: tc.ipRefillPerSec,
					Burst:        10,
					TTL:          1 * time.Minute,
				},
				ByJWT: config.RateBucket{
					RefillPerSec: tc.jwtRefillPerSec,
					Burst:        10,
					TTL:          1 * time.Minute,
				},
			}

			middleware := NewRateLimit(cfg, rdb, nil)
			retryAfter := middleware.calculateRetryAfter(tc.okIP, tc.okJWT)

			assert.GreaterOrEqual(t, retryAfter, tc.expectedMinRetry)
		})
	}
}

// ========== IP Extraction Tests ==========

func TestExtractClientIP(t *testing.T) {
	testCases := []struct {
		name           string
		remoteAddr     string
		headers        map[string]string
		trustedProxies []string
		expectedIP     string
	}{
		{
			name:       "simple_remote_addr",
			remoteAddr: "192.168.1.100:12345",
			expectedIP: "192.168.1.100",
		},
		{
			name:       "x_forwarded_for_single_ip",
			remoteAddr: "10.0.0.1:12345",
			headers: map[string]string{
				"X-Forwarded-For": "203.0.113.1",
			},
			expectedIP: "203.0.113.1",
		},
		{
			name:       "x_forwarded_for_multiple_ips",
			remoteAddr: "10.0.0.1:12345",
			headers: map[string]string{
				"X-Forwarded-For": "203.0.113.1, 203.0.113.2, 203.0.113.3",
			},
			expectedIP: "203.0.113.1",
		},
		{
			name:       "x_real_ip",
			remoteAddr: "10.0.0.1:12345",
			headers: map[string]string{
				"X-Real-IP": "203.0.113.50",
			},
			expectedIP: "203.0.113.50",
		},
		{
			name:       "x_forwarded_for_with_private_ips",
			remoteAddr: "10.0.0.1:12345",
			headers: map[string]string{
				"X-Forwarded-For": "192.168.1.1, 10.0.0.2",
			},
			expectedIP: "192.168.1.1",
		},
		{
			name:       "remote_addr_without_port",
			remoteAddr: "192.168.1.100",
			expectedIP: "192.168.1.100",
		},
		{
			name:       "invalid_remote_addr",
			remoteAddr: "invalid",
			expectedIP: "unknown",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/test", nil)
			req.RemoteAddr = tc.remoteAddr

			for key, value := range tc.headers {
				req.Header.Set(key, value)
			}

			ip := extractClientIP(req, tc.trustedProxies)
			assert.Equal(t, tc.expectedIP, ip)
		})
	}
}

func TestIsTrusted(t *testing.T) {
	testCases := []struct {
		name     string
		ip       string
		trusted  []string
		expected bool
	}{
		{
			name:     "ip_in_trusted_list",
			ip:       "192.168.1.1",
			trusted:  []string{"192.168.1.1", "10.0.0.1"},
			expected: true,
		},
		{
			name:     "ip_not_in_trusted_list",
			ip:       "203.0.113.1",
			trusted:  []string{"192.168.1.1", "10.0.0.1"},
			expected: false,
		},
		{
			name:     "ip_in_cidr_range",
			ip:       "192.168.1.50",
			trusted:  []string{"192.168.1.0/24"},
			expected: true,
		},
		{
			name:     "ip_not_in_cidr_range",
			ip:       "192.168.2.50",
			trusted:  []string{"192.168.1.0/24"},
			expected: false,
		},
		{
			name:     "multiple_cidr_ranges",
			ip:       "10.0.0.50",
			trusted:  []string{"192.168.0.0/16", "10.0.0.0/8"},
			expected: true,
		},
		{
			name:     "empty_trusted_list",
			ip:       "192.168.1.1",
			trusted:  []string{},
			expected: false,
		},
		{
			name:     "invalid_ip",
			ip:       "invalid",
			trusted:  []string{"192.168.1.0/24"},
			expected: false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := isTrusted(tc.ip, tc.trusted)
			assert.Equal(t, tc.expected, result)
		})
	}
}

func TestIsPublicIP(t *testing.T) {
	testCases := []struct {
		name     string
		ip       string
		expected bool
	}{
		{
			name:     "public_ipv4",
			ip:       "8.8.8.8",
			expected: true,
		},
		{
			name:     "public_ipv4_2",
			ip:       "1.1.1.1",
			expected: true,
		},
		{
			name:     "private_10",
			ip:       "10.0.0.1",
			expected: false,
		},
		{
			name:     "private_192_168",
			ip:       "192.168.1.1",
			expected: false,
		},
		{
			name:     "private_172_16",
			ip:       "172.16.0.1",
			expected: false,
		},
		{
			name:     "loopback",
			ip:       "127.0.0.1",
			expected: false,
		},
		{
			name:     "link_local",
			ip:       "169.254.1.1",
			expected: false,
		},
		{
			name:     "invalid_ip",
			ip:       "invalid",
			expected: false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := isPublicIP(tc.ip)
			assert.Equal(t, tc.expected, result)
		})
	}
}

func TestParseXFF(t *testing.T) {
	testCases := []struct {
		name     string
		xff      string
		expected []string
	}{
		{
			name:     "single_ip",
			xff:      "192.168.1.1",
			expected: []string{"192.168.1.1"},
		},
		{
			name:     "multiple_ips",
			xff:      "192.168.1.1, 10.0.0.1, 203.0.113.1",
			expected: []string{"192.168.1.1", "10.0.0.1", "203.0.113.1"},
		},
		{
			name:     "with_spaces",
			xff:      "  192.168.1.1  ,  10.0.0.1  ",
			expected: []string{"192.168.1.1", "10.0.0.1"},
		},
		{
			name:     "with_invalid_ip",
			xff:      "192.168.1.1, invalid, 10.0.0.1",
			expected: []string{"192.168.1.1", "10.0.0.1"},
		},
		{
			name:     "empty_string",
			xff:      "",
			expected: []string{},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := parseXFF(tc.xff)
			assert.Equal(t, tc.expected, result)
		})
	}
}

func TestRemoteAddrIP(t *testing.T) {
	testCases := []struct {
		name       string
		remoteAddr string
		expected   string
	}{
		{
			name:       "with_port",
			remoteAddr: "192.168.1.1:12345",
			expected:   "192.168.1.1",
		},
		{
			name:       "without_port",
			remoteAddr: "192.168.1.1",
			expected:   "192.168.1.1",
		},
		{
			name:       "with_spaces",
			remoteAddr: "  192.168.1.1:12345  ",
			expected:   "192.168.1.1",
		},
		{
			name:       "ipv6_with_port",
			remoteAddr: "[2001:db8::1]:8080",
			expected:   "2001:db8::1",
		},
		{
			name:       "invalid",
			remoteAddr: "invalid",
			expected:   "unknown",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := remoteAddrIP(tc.remoteAddr)
			assert.Equal(t, tc.expected, result)
		})
	}
}

// ========== Integration Tests ==========

func TestRateLimitMiddleware_Integration_BothLimitsApply(t *testing.T) {
	mr, rdb := setupTestRedis(t)
	defer mr.Close()

	privKey, pubKey := generateTestKeysForRL(t)
	verifier := &security.RS256Verifier{
		PubKey: pubKey,
		Aud:    "test-aud",
		Iss:    "test-iss",
	}

	// IP limit is more restrictive than JWT limit
	cfg := &config.RateLimitConfig{
		ByIP: config.RateBucket{
			RefillPerSec: 1,
			Burst:        1, // Only 1 request per IP
			TTL:          1 * time.Minute,
		},
		ByJWT: config.RateBucket{
			RefillPerSec: 100,
			Burst:        100, // High limit
			TTL:          1 * time.Minute,
		},
	}

	middleware := NewRateLimit(cfg, rdb, verifier)

	nextHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	handler := middleware.Handler(nextHandler)

	token := createTestTokenForRL(t, privKey, "user123")

	// First request from IP should pass
	req1 := httptest.NewRequest(http.MethodGet, "/test", nil)
	req1.RemoteAddr = "192.168.1.100:12345"
	req1.Header.Set("Authorization", "Bearer "+token)
	rec1 := httptest.NewRecorder()
	handler.ServeHTTP(rec1, req1)
	assert.Equal(t, http.StatusOK, rec1.Code)

	// Second request from same IP should be blocked even with valid JWT
	req2 := httptest.NewRequest(http.MethodGet, "/test", nil)
	req2.RemoteAddr = "192.168.1.100:12345"
	req2.Header.Set("Authorization", "Bearer "+token)
	rec2 := httptest.NewRecorder()
	handler.ServeHTTP(rec2, req2)
	assert.Equal(t, http.StatusTooManyRequests, rec2.Code)
}

func TestRateLimitMiddleware_Integration_RedisFailure(t *testing.T) {
	mr, rdb := setupTestRedis(t)
	cfg := createTestRateLimitConfig()
	middleware := NewRateLimit(cfg, rdb, nil)

	nextHandlerCalled := false
	nextHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		nextHandlerCalled = true
		w.WriteHeader(http.StatusOK)
	})

	handler := middleware.Handler(nextHandler)

	// Close redis to simulate failure
	mr.Close()

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.RemoteAddr = "192.168.1.100:12345"
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	// When Redis fails, middleware should allow request (fail-open)
	assert.True(t, nextHandlerCalled, "should allow request when Redis fails")
	assert.Equal(t, http.StatusOK, rec.Code)
}

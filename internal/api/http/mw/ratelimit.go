package mw

import (
	"context"
	"dexcelerate/internal/security"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/redis/go-redis/v9"
)

// Config for 2 bucket
type RateBucket struct {
	RefillPerSec int           // how many tokens are add every second
	Burst        int           // max len bucket
	TTL          time.Duration // how long should you keep a key if it isn't use
}

type RateLimitConfig struct {
	ByJWT    RateBucket
	ByIP     RateBucket
	Verifier *security.RS256Verifier // not necessarily
}

type RateLimitMiddleware struct {
	rdb *redis.Client
	cfg RateLimitConfig
}

func NewRateLimit(rdb *redis.Client, cfg RateLimitConfig) *RateLimitMiddleware {
	// sane defaults
	if cfg.ByJWT.TTL == 0 {
		cfg.ByJWT.TTL = 2 * time.Minute
	}
	if cfg.ByIP.TTL == 0 {
		cfg.ByIP.TTL = 2 * time.Minute
	}
	return &RateLimitMiddleware{rdb: rdb, cfg: cfg}
}

func (m *RateLimitMiddleware) Handler(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()

		now := time.Now()

		// by ip
		ip := clientIP(r)
		if ip == "" {
			ip = "unknown"
		}

		ipKey := "rl:ip:" + ip
		okIP, _ := m.allow(ctx, ipKey, now, m.cfg.ByIP)

		// by JWT if exists/valid
		okJWT := true

		sub := subjectFromContext(r)
		if sub == "" && m.cfg.Verifier != nil {
			// try to parse ourselves
			if cl, err := m.cfg.Verifier.VerifyBearer(r.Header.Get("Authorization")); err == nil {
				if rc, ok := cl.(*jwt.RegisteredClaims); ok && rc.Subject != "" {
					sub = rc.Subject
				}
			}
		}
		if sub != "" {
			jwtKey := "rl:jwt:" + sub
			okJWT, _ = m.allow(ctx, jwtKey, now, m.cfg.ByJWT)
		}

		if !(okIP && okJWT) {
			w.Header().Set("Retry-After", "1")
			http.Error(w, "rate limit exceeded", http.StatusTooManyRequests)
			return
		}

		next.ServeHTTP(w, r)
	})
}

func subjectFromContext(r *http.Request) string {
	if v := r.Context().Value(claimsCtxKey{}); v != nil {
		if s, ok := v.(string); ok {
			return s
		}
	}

	return ""
}

// --- redis token-bucket (Lua) for atomic and one query ---
var luaTokenBucket = redis.NewScript(`
-- KEYS[1] = key
-- ARGV[1] = now_ms
-- ARGV[2] = refill_per_sec (integer)
-- ARGV[3] = burst (integer)
-- ARGV[4] = ttl_seconds
local key   = KEYS[1]
local now   = tonumber(ARGV[1])
local rate  = tonumber(ARGV[2])
local burst = tonumber(ARGV[3])
local ttl   = tonumber(ARGV[4])

-- read state
local last_ms = tonumber(redis.call('HGET', key, 'ts') or now)
local tokens  = tonumber(redis.call('HGET', key, 'tok') or burst)

-- replenish
if now > last_ms then
  local delta = (now - last_ms) / 1000.0
  tokens = math.min(burst, tokens + (delta * rate))
end

local allowed = 0
if tokens >= 1 then
  tokens = tokens - 1
  allowed = 1
end

redis.call('HSET', key, 'tok', tokens, 'ts', now)
redis.call('EXPIRE', key, ttl)

return {allowed, tokens}
`)

func clientIP(r *http.Request) string {
	// return user IP among the proxy IPs
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		parts := strings.Split(xff, ",")
		return strings.TrimSpace(parts[0])
	}

	if xrip := r.Header.Get("X-Real-IP"); xrip != "" {
		return xrip
	}

	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

func (m *RateLimitMiddleware) allow(ctx context.Context, key string, now time.Time, b RateBucket) (bool, float64) {
	ttl := int(b.TTL.Seconds())
	if ttl <= 0 {
		ttl = 120
	}

	res, err := luaTokenBucket.Run(ctx, m.rdb, []string{key},
		now.UnixMilli(),
		b.RefillPerSec,
		b.Burst,
		ttl,
	).Result()
	if err != nil { // if failure then don't crash
		return true, 0
	}

	arr := res.([]any)
	if len(arr) == 0 {
		return false, 0
	}

	allowed := arr[0].(int64) == 1
	tokenLeft, _ := arr[1].(float64)

	return allowed, tokenLeft
}

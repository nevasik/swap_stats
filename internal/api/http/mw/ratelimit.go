package mw

import (
	"context"
	"dexcelerate/internal/config"
	"dexcelerate/internal/security"
	"dexcelerate/internal/stores/redis"
	"fmt"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	goredis "github.com/redis/go-redis/v9"
)

type RateLimitMiddleware struct {
	Cfg      *config.RateLimitConfig
	Rdb      *redis.Client
	Verifier *security.RS256Verifier
}

func NewRateLimit(cfg *config.RateLimitConfig, rdb *redis.Client, verifier *security.RS256Verifier) *RateLimitMiddleware {
	if cfg == nil {
		panic("rate limit config cannot be nil")
	}
	if rdb == nil {
		panic("redis client cannot be nil")
	}

	// sane defaults
	if cfg.ByJWT.TTL == 0 {
		cfg.ByJWT.TTL = 2 * time.Minute
	}
	if cfg.ByIP.TTL == 0 {
		cfg.ByIP.TTL = 2 * time.Minute
	}
	return &RateLimitMiddleware{
		Rdb:      rdb,
		Cfg:      cfg,
		Verifier: verifier,
	}
}

func (m *RateLimitMiddleware) Handler(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()

		now := time.Now()

		// by ip
		ip := extractClientIP(r, m.Cfg.TrustedProxiesList)
		if ip == "" {
			ip = "unknown"
		}

		ipKey := "rl:ip:" + ip
		okIP, tokensIP := m.allow(ctx, ipKey, now, &m.Cfg.ByIP)

		// by JWT if exists/valid
		okJWT := true
		var tokensJWT float64 = -1 // -1 -> what JWT don't valid

		sub := subjectFromContext(r)
		if sub == "" && m.Verifier != nil {
			// try to parse ourselves
			if cl, err := m.Verifier.VerifyBearer(r.Header.Get("Authorization")); err == nil {
				if rc, ok := cl.(*jwt.RegisteredClaims); ok && rc.Subject != "" {
					sub = rc.Subject
				}
			}
		}
		if sub != "" {
			jwtKey := "rl:jwt:" + sub
			okJWT, tokensJWT = m.allow(ctx, jwtKey, now, &m.Cfg.ByJWT)
		}

		// add headers with info by limit
		w.Header().Set("X-RateLimit-Limit-IP", fmt.Sprintf("%d", m.Cfg.ByIP.Burst)) // max burst for IP
		w.Header().Set("X-RateLimit-Remaining-IP", fmt.Sprintf("%.0f", tokensIP))   // remaining tokens for IP
		if tokensJWT >= 0 {
			w.Header().Set("X-RateLimit-Limit-JWT", fmt.Sprintf("%d", m.Cfg.ByJWT.Burst)) // max burst for JWT
			w.Header().Set("X-RateLimit-Remaining-JWT", fmt.Sprintf("%.0f", tokensJWT))   // remaining tokens for JWT
		}

		if !(okIP && okJWT) {
			retryAfter := m.calculateRetryAfter(okIP, okJWT)
			w.Header().Set("Retry-After", fmt.Sprintf("%d", retryAfter))

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
var luaTokenBucket = goredis.NewScript(`
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

// calculate timeout wait for next attempt based on refill rate this bucket exceeded limit
func (m *RateLimitMiddleware) calculateRetryAfter(okIP, okJWT bool) int {
	// if two limit exceeded, take max time
	if !okIP && !okJWT {
		ipRetry := 1.0 / float64(m.Cfg.ByIP.RefillPerSec)
		jwtRetry := 1.0 / float64(m.Cfg.ByJWT.RefillPerSec)
		if ipRetry > jwtRetry {
			return int(ipRetry) + 1
		}
		return int(jwtRetry) + 1
	}

	// only the ip limit
	if !okIP {
		retrySeconds := 1.0 / float64(m.Cfg.ByIP.RefillPerSec)
		if retrySeconds < 1 {
			return 1
		}
		return int(retrySeconds) + 1
	}

	// only the jwt limit
	if !okJWT {
		retrySeconds := 1.0 / float64(m.Cfg.ByJWT.RefillPerSec)
		if retrySeconds < 1 {
			return 1
		}
		return int(retrySeconds) + 1
	}

	return 1
}

func (m *RateLimitMiddleware) allow(ctx context.Context, key string, now time.Time, b *config.RateBucket) (bool, float64) {
	ttl := int(b.TTL.Seconds())
	if ttl <= 0 {
		ttl = 120
	}

	res, err := luaTokenBucket.Run(ctx, m.Rdb, []string{key},
		now.UnixMilli(),
		b.RefillPerSec,
		b.Burst,
		ttl,
	).Result()
	if err != nil { // if failure then don't crash
		return true, 0
	}

	arr := res.([]any)

	allowed := false
	if v, ok := arr[0].(int64); ok && v == 1 {
		allowed = true
	}

	var tokenLeft float64
	switch v := arr[1].(type) {
	case float64:
		tokenLeft = v
	case int64:
		tokenLeft = float64(v)
	default:
		tokenLeft = 0
	}
	return allowed, tokenLeft
}

// extract real IP client with take into the proxy
// trustedProxies - allowed(white list) proxy, if nil -> allowed all true
func extractClientIP(r *http.Request, trustedProxies []string) string {
	remoteIP := remoteAddrIP(r.RemoteAddr)

	if trustedProxies != nil && !isTrusted(remoteIP, trustedProxies) {
		return remoteIP
	}

	// X-Forwarded-For
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		ips := parseXFF(xff)

		if len(ips) > 0 {
			if trustedProxies == nil {
				// allowed all
				for _, ip := range ips {
					if isPublicIP(ip) {
						return ip
					}
				}
				return ips[0]
			}

			// allowed
			i := len(ips) - 1
			for ; i >= 0; i-- {
				if !isTrusted(ips[i], trustedProxies) {
					break
				}
			}

			if i >= 0 {
				if isPublicIP(ips[i]) {
					return ips[i]
				}

				for j := i; j >= 0; j-- {
					if isPublicIP(ips[j]) {
						return ips[j]
					}
				}
				return ips[i]
			}
			return remoteIP
		}
	}

	if xrip := r.Header.Get("X-Real-IP"); xrip != "" {
		xrip = strings.TrimSpace(xrip)
		if ip := net.ParseIP(xrip); ip != nil {
			return xrip
		}
	}

	// fallback by RemoteAddr
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		if ip := net.ParseIP(r.RemoteAddr); ip != nil {
			return r.RemoteAddr
		}
		return "unknown"
	}

	return host
}

// check, included in this ip to list trusted (support IP and CIDR)
func isTrusted(ipStr string, trusted []string) bool {
	ip := net.ParseIP(ipStr)
	if ip == nil {
		return false
	}
	for _, t := range trusted {
		t = strings.TrimSpace(t)
		if t == "" {
			continue
		}
		// CIDR
		if _, cidr, err := net.ParseCIDR(t); err == nil && cidr != nil {
			if cidr.Contains(ip) {
				return true
			}
			continue
		}
		// alone IP
		if tip := net.ParseIP(t); tip != nil && tip.Equal(ip) {
			return true
		}
	}
	return false
}

func remoteAddrIP(remoteAddr string) string {
	host, _, err := net.SplitHostPort(strings.TrimSpace(remoteAddr))
	if err != nil {
		// бывает уже просто IP без порта
		host = strings.TrimSpace(remoteAddr)
	}
	if ip := net.ParseIP(host); ip != nil {
		return ip.String()
	}
	return "unknown"
}

func parseXFF(xff string) []string {
	raw := strings.Split(xff, ",")
	out := make([]string, 0, len(raw))
	for _, p := range raw {
		ipStr := strings.TrimSpace(p)
		if ip := net.ParseIP(ipStr); ip != nil {
			out = append(out, ip.String())
		}
	}
	return out
}

func isPublicIP(ipStr string) bool {
	ip := net.ParseIP(ipStr)
	if ip == nil {
		return false
	}

	// loopback
	if ip.IsLoopback() {
		return false
	}

	// link-local (IPv4 169.254/16, IPv6 fe80::/10)
	if ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() {
		return false
	}

	// private range IPv4
	privateCIDRs := []string{
		"10.0.0.0/8",
		"172.16.0.0/12",
		"192.168.0.0/16",
		"127.0.0.0/8",
		"169.254.0.0/16",
	}
	for _, c := range privateCIDRs {
		_, n, _ := net.ParseCIDR(c)
		if n.Contains(ip) {
			return false
		}
	}

	// unique local IPv6 fc00::/7
	if ip.To4() == nil {
		_, ula, _ := net.ParseCIDR("fc00::/7")
		if ula.Contains(ip) {
			return false
		}
	}
	return true
}

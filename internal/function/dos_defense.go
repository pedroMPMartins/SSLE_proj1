package function

import (
	"fmt"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

// ExtractClientIP extracts the client IP address from standard proxy headers
// (X-Forwarded-For, X-Real-IP) or falls back to RemoteAddr.
func ExtractClientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		parts := strings.Split(xff, ",")
		return strings.TrimSpace(parts[0])
	}
	if xri := r.Header.Get("X-Real-IP"); xri != "" {
		return strings.TrimSpace(xri)
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// TokenBucket represents a token-bucket state for a single client IP.
type TokenBucket struct {
	tokens         float64
	capacity       float64
	refillRate     float64 // tokens per second
	lastRefillTime time.Time
}

func (tb *TokenBucket) allow() bool {
	now := time.Now()
	elapsed := now.Sub(tb.lastRefillTime).Seconds()
	tb.lastRefillTime = now

	tb.tokens += elapsed * tb.refillRate
	if tb.tokens > tb.capacity {
		tb.tokens = tb.capacity
	}

	if tb.tokens >= 1.0 {
		tb.tokens -= 1.0
		return true
	}
	return false
}

// RateLimiter manages token-bucket limiters per client IP address.
type RateLimiter struct {
	mu         sync.Mutex
	rps        float64
	capacity   float64
	clients    map[string]*TokenBucket
	lastPruned time.Time
}

// NewRateLimiter creates a new thread-safe IP-based RateLimiter.
func NewRateLimiter(rps, capacity float64) *RateLimiter {
	if rps <= 0 {
		rps = 20.0
	}
	if capacity <= 0 {
		capacity = rps * 3
		if capacity < 15.0 {
			capacity = 15.0
		}
	}
	return &RateLimiter{
		rps:        rps,
		capacity:   capacity,
		clients:    make(map[string]*TokenBucket),
		lastPruned: time.Now(),
	}
}

// Allow checks if the request from remoteAddr/ip is permitted under the rate limit.
func (rl *RateLimiter) Allow(remoteAddr string) bool {
	if rl == nil {
		return true
	}
	host, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		host = remoteAddr
	}

	rl.mu.Lock()
	defer rl.mu.Unlock()

	// Periodic pruning to avoid unbounded memory growth
	if time.Since(rl.lastPruned) > 5*time.Minute || len(rl.clients) > 5000 {
		cutoff := time.Now().Add(-10 * time.Minute)
		for k, v := range rl.clients {
			if v.lastRefillTime.Before(cutoff) {
				delete(rl.clients, k)
			}
		}
		rl.lastPruned = time.Now()
	}

	bucket, exists := rl.clients[host]
	if !exists {
		bucket = &TokenBucket{
			tokens:         rl.capacity, // Start full to allow initial burst
			capacity:       rl.capacity,
			refillRate:     rl.rps,
			lastRefillTime: time.Now(),
		}
		rl.clients[host] = bucket
	}

	return bucket.allow()
}

// DoSProtectionConfig defines the operational parameters for DoS and package dropping defense.
type DoSProtectionConfig struct {
	EnableRateLimit       bool
	RateLimitRPS          float64
	EnablePayloadClamping bool
	MaxBodyBytes          int64
	RateLimiter           *RateLimiter
}

// LoadDoSConfigFromEnv creates a DoSProtectionConfig populated from environment variables.
func LoadDoSConfigFromEnv() DoSProtectionConfig {
	enableRateLimit := parseBoolEnv("ENABLE_RATE_LIMIT", false)
	rps := parseFloatEnv("RATE_LIMIT_RPS", 20.0)
	maxBytes := parseInt64Env("MAX_PAYLOAD_BYTES", 1048576) // Default 1MB

	return DoSProtectionConfig{
		EnableRateLimit:       enableRateLimit,
		RateLimitRPS:          rps,
		EnablePayloadClamping: true,
		MaxBodyBytes:          maxBytes,
		RateLimiter:           NewRateLimiter(rps, 0), // Uses automatic burst capacity
	}
}

// Helper environment parsers
func parseBoolEnv(key string, fallback bool) bool {
	val := os.Getenv(key)
	if val == "" {
		if os.Getenv("ENABLE_DEFENSES") == "true" {
			return true
		}
		return fallback
	}
	return val == "true" || val == "1"
}

func parseFloatEnv(key string, fallback float64) float64 {
	val := os.Getenv(key)
	if val == "" {
		return fallback
	}
	f, err := strconv.ParseFloat(val, 64)
	if err != nil {
		return fallback
	}
	return f
}

func parseInt64Env(key string, fallback int64) int64 {
	val := os.Getenv(key)
	if val == "" {
		return fallback
	}
	n, err := strconv.ParseInt(val, 10, 64)
	if err != nil {
		return fallback
	}
	return n
}

// DoSProtectionMiddleware returns an http.Handler that wraps next with:
// 1. Shellshock exploit inspection (returning 403 Forbidden before rate limiting)
// 2. Token-bucket rate limiting per client IP (returning 429 Too Many Requests)
// 3. Body payload size clamping to prevent memory exhaustion (returning 400 Bad Request if exceeded)
func DoSProtectionMiddleware(next http.Handler, cfg DoSProtectionConfig) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 1. Security Exploit Inspection (Shellshock CVE-2014-6271)
		// Exploit signatures must always be blocked with 403 and never masked by rate limiting
		if strings.Contains(r.UserAgent(), "() {") || strings.Contains(r.URL.Path, "() {") {
			RespondJSON(w, http.StatusForbidden, map[string]string{
				"error": "Malicious payload detected: Shellshock attack blocked by application security filter",
			})
			return
		}

		// 2. Rate Limiting Check
		if cfg.EnableRateLimit && cfg.RateLimiter != nil {
			clientIP := ExtractClientIP(r)
			if !cfg.RateLimiter.Allow(clientIP) {
				w.Header().Set("Retry-After", "1")
				RespondJSON(w, http.StatusTooManyRequests, map[string]string{
					"error": "Rate limit exceeded. Please throttle requests.",
				})
				return
			}
		}

		// 3. Payload Clamping Check (Drop oversized packages)
		if cfg.EnablePayloadClamping && cfg.MaxBodyBytes > 0 {
			if r.ContentLength > 0 && r.ContentLength > cfg.MaxBodyBytes {
				RespondJSON(w, http.StatusBadRequest, map[string]string{
					"error": fmt.Sprintf("Payload exceeds maximum allowed size of %d bytes", cfg.MaxBodyBytes),
				})
				return
			}
			if r.Body != nil && r.Body != http.NoBody {
				r.Body = http.MaxBytesReader(w, r.Body, cfg.MaxBodyBytes)
			}
		}

		next.ServeHTTP(w, r)
	})
}

// NewHardenedServer returns an *http.Server configured with robust timeouts
// against Slowloris and connection-flooding DoS attacks.
func NewHardenedServer(addr string, handler http.Handler) *http.Server {
	return &http.Server{
		Addr:              addr,
		Handler:           handler,
		ReadHeaderTimeout: 3 * time.Second,
		ReadTimeout:       5 * time.Second,
		WriteTimeout:      10 * time.Second,
		IdleTimeout:       15 * time.Second,
		MaxHeaderBytes:    1 << 20, // 1MB header limit
	}
}

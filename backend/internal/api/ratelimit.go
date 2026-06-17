package api

import (
	"fmt"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

// rateLimiter implements a simple in-memory token-bucket rate limiter keyed by IP.
type rateLimiter struct {
	mu      sync.Mutex
	buckets map[string]*bucket
	rate    int           // max requests per window
	window  time.Duration // window duration
}

type bucket struct {
	tokens    int
	lastReset time.Time
}

func newRateLimiter(rate int, window time.Duration) *rateLimiter {
	return &rateLimiter{
		buckets: make(map[string]*bucket),
		rate:    rate,
		window:  window,
	}
}

func (rl *rateLimiter) allow(key string) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := time.Now()
	b, ok := rl.buckets[key]
	if !ok || now.Sub(b.lastReset) >= rl.window {
		rl.buckets[key] = &bucket{tokens: rl.rate - 1, lastReset: now}
		return true
	}
	if b.tokens <= 0 {
		return false
	}
	b.tokens--
	return true
}

func (rl *rateLimiter) retryAfter(key string) int {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	b, ok := rl.buckets[key]
	if !ok {
		return 0
	}
	elapsed := time.Since(b.lastReset)
	remaining := rl.window - elapsed
	if remaining <= 0 {
		return 0
	}
	return int(remaining.Seconds()) + 1
}

// withRateLimit wraps a handler with rate limiting.
func withRateLimit(rl *rateLimiter, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ip := extractClientIP(r)
		if !rl.allow(ip) {
			retry := rl.retryAfter(ip)
			w.Header().Set("Retry-After", fmt.Sprintf("%d", retry))
			writeError(w, http.StatusTooManyRequests, "RATE_LIMITED", "too many requests, try again later")
			return
		}
		next(w, r)
	}
}

func extractClientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		parts := strings.Split(xff, ",")
		if ip := strings.TrimSpace(parts[0]); ip != "" {
			return ip
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

package redirectsrv

import (
	"net"
	"net/http"
	"sync"
	"time"
)

// TokenBucket is a simple token bucket rate limiter for a single client.
type TokenBucket struct {
	capacity  float64
	fillRate  float64
	tokens    float64
	lastFill  time.Time
	mu        sync.Mutex
}

// NewTokenBucket creates a new token bucket with the given capacity and fill rate.
// capacity is the maximum number of tokens.
// fillRate is the number of tokens added per second.
func NewTokenBucket(capacity float64, fillRate float64) *TokenBucket {
	return &TokenBucket{
		capacity: capacity,
		fillRate: fillRate,
		tokens:   capacity,
		lastFill: time.Now(),
	}
}

// Allow returns true if a token is available, false otherwise.
func (tb *TokenBucket) Allow() bool {
	tb.mu.Lock()
	defer tb.mu.Unlock()

	now := time.Now()
	elapsed := now.Sub(tb.lastFill).Seconds()
	tokensToAdd := elapsed * tb.fillRate
	tb.tokens = min(tb.capacity, tb.tokens+tokensToAdd)
	tb.lastFill = now

	if tb.tokens >= 1 {
		tb.tokens--
		return true
	}
	return false
}

func min(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}

// RateLimiter is an in-memory rate limiter that uses token buckets per client IP.
type RateLimiter struct {
	buckets   map[string]*TokenBucket
	capacity  float64
	fillRate  float64
	mu        sync.RWMutex
	cleanupTk *time.Ticker
	done      chan struct{}
}

// NewRateLimiter creates a new rate limiter with the given capacity and fill rate.
// capacity is the maximum number of requests per client.
// fillRate is the number of tokens (requests) refilled per second.
func NewRateLimiter(capacity float64, fillRate float64) *RateLimiter {
	rl := &RateLimiter{
		buckets:   make(map[string]*TokenBucket),
		capacity:  capacity,
		fillRate:  fillRate,
		cleanupTk: time.NewTicker(1 * time.Minute),
		done:      make(chan struct{}),
	}
	go rl.cleanupRoutine()
	return rl
}

// Allow checks if the client IP is rate limited. Returns true if the request is allowed.
func (rl *RateLimiter) Allow(clientIP string) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	tb, ok := rl.buckets[clientIP]
	if !ok {
		tb = NewTokenBucket(rl.capacity, rl.fillRate)
		rl.buckets[clientIP] = tb
	}

	return tb.Allow()
}

// cleanupRoutine periodically removes empty buckets to prevent memory leaks.
func (rl *RateLimiter) cleanupRoutine() {
	for {
		select {
		case <-rl.cleanupTk.C:
			rl.mu.Lock()
			now := time.Now()
			for ip, tb := range rl.buckets {
				tb.mu.Lock()
				// Remove bucket if it hasn't been used in the last 10 minutes and is full
				if now.Sub(tb.lastFill) > 10*time.Minute && tb.tokens >= tb.capacity {
					delete(rl.buckets, ip)
				}
				tb.mu.Unlock()
			}
			rl.mu.Unlock()
		case <-rl.done:
			return
		}
	}
}

// Close stops the cleanup routine.
func (rl *RateLimiter) Close() {
	rl.cleanupTk.Stop()
	close(rl.done)
}

// getClientIP extracts the client IP from the request.
func getClientIP(r *http.Request) string {
	// Check X-Forwarded-For header first (for proxied requests)
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		if ip, _, err := net.SplitHostPort(xff); err == nil {
			return ip
		}
		return xff
	}

	// Fall back to RemoteAddr
	if ip, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		return ip
	}
	return r.RemoteAddr
}

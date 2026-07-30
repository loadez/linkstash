package redirectsrv

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestTokenBucket_Allow(t *testing.T) {
	tests := []struct {
		name     string
		capacity float64
		fillRate float64
		requests int
		expected []bool
	}{
		{
			name:     "allow burst within capacity",
			capacity: 3,
			fillRate: 1,
			requests: 3,
			expected: []bool{true, true, true},
		},
		{
			name:     "deny beyond capacity",
			capacity: 2,
			fillRate: 1,
			requests: 3,
			expected: []bool{true, true, false},
		},
		{
			name:     "single request allowed",
			capacity: 1,
			fillRate: 1,
			requests: 1,
			expected: []bool{true},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tb := NewTokenBucket(tt.capacity, tt.fillRate)
			for i, shouldAllow := range tt.expected {
				allowed := tb.Allow()
				if allowed != shouldAllow {
					t.Errorf("request %d: got %v, want %v", i+1, allowed, shouldAllow)
				}
			}
		})
	}
}

func TestTokenBucket_Refill(t *testing.T) {
	// Create a bucket with capacity 2, but fill at 1 token per 100ms
	tb := NewTokenBucket(2, 10) // 10 tokens per second = 1 per 100ms

	// Consume both tokens
	if !tb.Allow() {
		t.Fatal("expected first request to be allowed")
	}
	if !tb.Allow() {
		t.Fatal("expected second request to be allowed")
	}

	// Third request should be denied
	if tb.Allow() {
		t.Error("expected third request to be denied")
	}

	// Wait for one token to refill
	time.Sleep(110 * time.Millisecond)

	// Fourth request should be allowed
	if !tb.Allow() {
		t.Error("expected fourth request to be allowed after refill")
	}

	// Fifth request should be denied (only one token refilled)
	if tb.Allow() {
		t.Error("expected fifth request to be denied")
	}
}

func TestRateLimiter_PerIPIsolation(t *testing.T) {
	rl := NewRateLimiter(2, 100)
	defer rl.Close()

	// IP 1 uses up its capacity
	if !rl.Allow("192.168.1.1") {
		t.Fatal("expected first request from IP1 to be allowed")
	}
	if !rl.Allow("192.168.1.1") {
		t.Fatal("expected second request from IP1 to be allowed")
	}

	// IP 1 should be rate limited
	if rl.Allow("192.168.1.1") {
		t.Error("expected third request from IP1 to be denied")
	}

	// IP 2 should not be rate limited (different IP)
	if !rl.Allow("192.168.1.2") {
		t.Error("expected first request from IP2 to be allowed")
	}
	if !rl.Allow("192.168.1.2") {
		t.Error("expected second request from IP2 to be allowed")
	}

	// IP 2 should be rate limited now
	if rl.Allow("192.168.1.2") {
		t.Error("expected third request from IP2 to be denied")
	}

	// IP 1 should still be rate limited
	if rl.Allow("192.168.1.1") {
		t.Error("expected IP1 to still be rate limited")
	}
}

func TestRateLimitMiddleware_Returns429(t *testing.T) {
	rl := NewRateLimiter(2, 100)
	defer rl.Close()

	// Create a simple handler
	handler := rateLimitMiddleware(rl)(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})

	// Create test requests from the same IP
	clientIP := "192.168.1.1"

	// First request should succeed
	req := httptest.NewRequest("GET", "/", nil)
	req.RemoteAddr = clientIP + ":12345"
	w := httptest.NewRecorder()
	handler(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("first request: got %d, want %d", w.Code, http.StatusOK)
	}

	// Second request should succeed
	req = httptest.NewRequest("GET", "/", nil)
	req.RemoteAddr = clientIP + ":12346"
	w = httptest.NewRecorder()
	handler(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("second request: got %d, want %d", w.Code, http.StatusOK)
	}

	// Third request should be rate limited (429)
	req = httptest.NewRequest("GET", "/", nil)
	req.RemoteAddr = clientIP + ":12347"
	w = httptest.NewRecorder()
	handler(w, req)
	if w.Code != http.StatusTooManyRequests {
		t.Errorf("third request: got %d, want %d", w.Code, http.StatusTooManyRequests)
	}
}

func TestRateLimitMiddleware_DifferentIPSucceeds(t *testing.T) {
	rl := NewRateLimiter(2, 100)
	defer rl.Close()

	handler := rateLimitMiddleware(rl)(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})

	// IP1 uses up quota
	for i := 0; i < 2; i++ {
		req := httptest.NewRequest("GET", "/", nil)
		req.RemoteAddr = fmt.Sprintf("192.168.1.1:%d", 12345+i)
		w := httptest.NewRecorder()
		handler(w, req)
		if w.Code != http.StatusOK {
			t.Errorf("IP1 request %d: got %d, want %d", i+1, w.Code, http.StatusOK)
		}
	}

	// IP1 third request should fail
	req := httptest.NewRequest("GET", "/", nil)
	req.RemoteAddr = "192.168.1.1:12347"
	w := httptest.NewRecorder()
	handler(w, req)
	if w.Code != http.StatusTooManyRequests {
		t.Errorf("IP1 third request: got %d, want %d", w.Code, http.StatusTooManyRequests)
	}

	// IP2 should still be able to make requests
	req = httptest.NewRequest("GET", "/", nil)
	req.RemoteAddr = "192.168.1.2:12345"
	w = httptest.NewRecorder()
	handler(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("IP2 first request: got %d, want %d", w.Code, http.StatusOK)
	}
}

func TestGetClientIP_RemoteAddr(t *testing.T) {
	req := httptest.NewRequest("GET", "/", nil)
	req.RemoteAddr = "192.168.1.1:12345"

	ip := getClientIP(req)
	if ip != "192.168.1.1" {
		t.Errorf("got %s, want 192.168.1.1", ip)
	}
}

func TestGetClientIP_XForwardedFor(t *testing.T) {
	req := httptest.NewRequest("GET", "/", nil)
	req.RemoteAddr = "127.0.0.1:12345"
	req.Header.Set("X-Forwarded-For", "192.168.1.100:12345")

	ip := getClientIP(req)
	if ip != "192.168.1.100" {
		t.Errorf("got %s, want 192.168.1.100", ip)
	}
}

func TestGetClientIP_XForwardedForNoPort(t *testing.T) {
	req := httptest.NewRequest("GET", "/", nil)
	req.RemoteAddr = "127.0.0.1:12345"
	req.Header.Set("X-Forwarded-For", "192.168.1.100")

	ip := getClientIP(req)
	if ip != "192.168.1.100" {
		t.Errorf("got %s, want 192.168.1.100", ip)
	}
}

// Package redirectsrv builds the HTTP handler for the redirector service.
package redirectsrv

import (
	"errors"
	"log"
	"net/http"
	"strings"

	"github.com/loadez/linkstash/internal/store"
)

// NewHandler returns the redirector service's http.Handler backed by s.
// The handler includes per-IP rate limiting.
func NewHandler(s *store.Store) http.Handler {
	// Create a rate limiter: 100 requests per second capacity, 100 requests/second refill rate
	// This allows bursts of 100 requests, with refill at 100 req/sec
	rl := NewRateLimiter(100, 100)

	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", healthzHandler)
	mux.HandleFunc("/", rateLimitMiddleware(rl)(redirectHandler(s)))
	return mux
}

// rateLimitMiddleware returns a middleware that applies rate limiting based on client IP.
func rateLimitMiddleware(rl *RateLimiter) func(http.HandlerFunc) http.HandlerFunc {
	return func(next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			clientIP := getClientIP(r)
			if !rl.Allow(clientIP) {
				http.Error(w, "too many requests", http.StatusTooManyRequests)
				return
			}
			next(w, r)
		}
	}
}

func healthzHandler(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ok"))
}

func redirectHandler(s *store.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		code := strings.TrimPrefix(r.URL.Path, "/")
		if code == "" {
			http.NotFound(w, r)
			return
		}

		link, err := s.GetLink(r.Context(), code)
		if err != nil {
			if errors.Is(err, store.ErrNotFound) {
				http.NotFound(w, r)
				return
			}
			log.Printf("redirector: get link: %v", err)
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}

		if err := s.RecordClick(r.Context(), code); err != nil {
			// Don't fail the redirect over a click-tracking error.
			log.Printf("redirector: record click: %v", err)
		}

		http.Redirect(w, r, link.TargetURL, http.StatusFound)
	}
}

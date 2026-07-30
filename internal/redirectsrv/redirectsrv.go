// Package redirectsrv builds the HTTP handler for the redirector service.
package redirectsrv

import (
	"errors"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/loadez/linkstash/internal/store"
)

// NewHandler returns the redirector service's http.Handler backed by s.
func NewHandler(s *store.Store) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", healthzHandler)
	mux.HandleFunc("/", redirectHandler(s))
	return mux
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

		// Check if link has expired
		if link.ExpiresAt.Valid && link.ExpiresAt.Time.Before(time.Now()) {
			w.WriteHeader(http.StatusGone)
			return
		}

		if err := s.RecordClick(r.Context(), code); err != nil {
			// Don't fail the redirect over a click-tracking error.
			log.Printf("redirector: record click: %v", err)
		}

		http.Redirect(w, r, link.TargetURL, http.StatusFound)
	}
}

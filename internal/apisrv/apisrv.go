// Package apisrv builds the HTTP handler for the api service, kept separate
// from cmd/api so it can be exercised directly in tests (including e2e).
package apisrv

import (
	"encoding/json"
	"log"
	"net/http"
	"net/url"
	"strings"

	"github.com/loadez/linkstash/internal/models"
	"github.com/loadez/linkstash/internal/store"
)

type createLinkRequest struct {
	TargetURL string `json:"target_url"`
	Code      string `json:"code,omitempty"`
}

// NewHandler returns the api service's http.Handler backed by s.
func NewHandler(s *store.Store) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", healthzHandler)
	mux.HandleFunc("/links", linksHandler(s))
	return mux
}

func healthzHandler(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ok"))
}

func linksHandler(s *store.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodPost:
			createLink(s, w, r)
		case http.MethodGet:
			listLinks(s, w, r)
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	}
}

func createLink(s *store.Store, w http.ResponseWriter, r *http.Request) {
	var req createLinkRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid json body", http.StatusBadRequest)
		return
	}

	req.TargetURL = strings.TrimSpace(req.TargetURL)
	if req.TargetURL == "" {
		http.Error(w, "target_url is required", http.StatusBadRequest)
		return
	}
	u, err := url.ParseRequestURI(req.TargetURL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") {
		http.Error(w, "target_url must be a valid http(s) URL", http.StatusBadRequest)
		return
	}

	link, err := s.CreateLink(r.Context(), req.Code, req.TargetURL)
	if err != nil {
		if isUniqueViolation(err) {
			http.Error(w, "code already in use", http.StatusConflict)
			return
		}
		log.Printf("api: create link: %v", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(link)
}

func listLinks(s *store.Store, w http.ResponseWriter, r *http.Request) {
	links, err := s.ListLinks(r.Context())
	if err != nil {
		log.Printf("api: list links: %v", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	if links == nil {
		links = []models.Link{} // never render `null` for an empty list
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(links)
}

func isUniqueViolation(err error) bool {
	// lib/pq wraps unique_violation as pq.Error with Code 23505; string match
	// keeps this file free of an extra import for a one-line check.
	return err != nil && strings.Contains(err.Error(), "23505")
}

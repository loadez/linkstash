// Package apisrv builds the HTTP handler for the api service, kept separate
// from cmd/api so it can be exercised directly in tests (including e2e).
package apisrv

import (
	"database/sql"
	"encoding/json"
	"log"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/loadez/linkstash/internal/models"
	"github.com/loadez/linkstash/internal/store"
)

type createLinkRequest struct {
	TargetURL string     `json:"target_url"`
	Code      string     `json:"code,omitempty"`
	ExpiresAt *time.Time `json:"expires_at,omitempty"`
}

// NewHandler returns the api service's http.Handler backed by s.
func NewHandler(s *store.Store) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", healthzHandler)
	mux.HandleFunc("/links", linksHandler(s))
	mux.HandleFunc("/links/", linksDetailHandler(s))
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

func linksDetailHandler(s *store.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		code := strings.TrimPrefix(r.URL.Path, "/links/")
		if code == "" {
			http.Error(w, "code is required", http.StatusBadRequest)
			return
		}
		switch r.Method {
		case http.MethodDelete:
			deleteLink(s, w, r, code)
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

	if err := validateAlias(req.Code); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	expiresAt := sql.NullTime{}
	if req.ExpiresAt != nil {
		expiresAt = sql.NullTime{Time: *req.ExpiresAt, Valid: true}
	}
	link, err := s.CreateLink(r.Context(), req.Code, req.TargetURL, expiresAt)
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

func deleteLink(s *store.Store, w http.ResponseWriter, r *http.Request, code string) {
	err := s.DeleteLink(r.Context(), code)
	if err != nil {
		if err == store.ErrNotFound {
			http.Error(w, "link not found", http.StatusNotFound)
			return
		}
		log.Printf("api: delete link: %v", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func isUniqueViolation(err error) bool {
	// lib/pq wraps unique_violation as pq.Error with Code 23505; string match
	// keeps this file free of an extra import for a one-line check.
	return err != nil && strings.Contains(err.Error(), "23505")
}

// validateAlias checks that a custom alias code is valid.
// If code is empty, it's allowed (will be auto-generated).
// If provided, it must match [a-zA-Z0-9_-]{3,32} and not be a reserved word.
func validateAlias(code string) error {
	if code == "" {
		return nil // empty code is allowed, will be auto-generated
	}

	// Check reserved words
	reserved := map[string]bool{
		"healthz": true,
		"links":   true,
	}
	if reserved[code] {
		return &aliasError{msg: "code is reserved"}
	}

	// Check format: [a-zA-Z0-9_-]{3,32}
	if !isValidAlias(code) {
		return &aliasError{msg: "code must be 3-32 characters matching [a-zA-Z0-9_-]"}
	}

	return nil
}

func isValidAlias(code string) bool {
	if len(code) < 3 || len(code) > 32 {
		return false
	}
	re := regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)
	return re.MatchString(code)
}

type aliasError struct {
	msg string
}

func (e *aliasError) Error() string {
	return e.msg
}

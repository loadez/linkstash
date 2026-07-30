package apisrv_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	_ "github.com/lib/pq"

	"github.com/loadez/linkstash/internal/apisrv"
	"github.com/loadez/linkstash/internal/config"
	"github.com/loadez/linkstash/internal/models"
	"github.com/loadez/linkstash/internal/store"
)

// These cases are all rejected before the handler ever touches the store, so
// a nil *store.Store is safe here and keeps this test DB-free.

func TestCreateLinkRejectsInvalidJSON(t *testing.T) {
	h := apisrv.NewHandler(nil)
	req := httptest.NewRequest(http.MethodPost, "/links", strings.NewReader(`not json`))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestCreateLinkRejectsEmptyTargetURL(t *testing.T) {
	h := apisrv.NewHandler(nil)
	req := httptest.NewRequest(http.MethodPost, "/links", strings.NewReader(`{"target_url":""}`))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestCreateLinkRejectsNonHTTPScheme(t *testing.T) {
	h := apisrv.NewHandler(nil)
	req := httptest.NewRequest(http.MethodPost, "/links", strings.NewReader(`{"target_url":"ftp://example.com/x"}`))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestLinksHandlerRejectsUnsupportedMethod(t *testing.T) {
	h := apisrv.NewHandler(nil)
	req := httptest.NewRequest(http.MethodDelete, "/links", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d", rec.Code)
	}
}

func TestHealthz(t *testing.T) {
	h := apisrv.NewHandler(nil)
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
}

// Helper functions for database tests.

func newTestStore(t *testing.T) *store.Store {
	t.Helper()

	dsn := config.DatabaseURL()
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Skipf("apisrv: cannot open db handle: %v", err)
	}
	if err := db.Ping(); err != nil {
		db.Close()
		t.Skipf("apisrv: postgres not reachable at %s: %v", dsn, err)
	}

	schema, err := os.ReadFile(migrationPath(t))
	if err != nil {
		db.Close()
		t.Fatalf("apisrv: read migration: %v", err)
	}
	if _, err := db.Exec(string(schema)); err != nil {
		db.Close()
		t.Fatalf("apisrv: apply migration: %v", err)
	}
	if _, err := db.Exec(`TRUNCATE clicks, links RESTART IDENTITY CASCADE`); err != nil {
		db.Close()
		t.Fatalf("apisrv: truncate: %v", err)
	}
	db.Close()

	s, err := store.Open(dsn)
	if err != nil {
		t.Fatalf("apisrv: open store: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func migrationPath(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(1)
	if !ok {
		t.Fatal("apisrv: cannot determine caller for migration path")
	}
	return filepath.Join(filepath.Dir(thisFile), "..", "..", "migrations", "0001_init.sql")
}

// TestListLinksPagination tests the acceptance criteria: create 60 links,
// GET /links?limit=10 returns exactly 10, newest first.
func TestListLinksPagination(t *testing.T) {
	s := newTestStore(t)
	h := apisrv.NewHandler(s)
	ctx := context.Background()

	// Create 60 links
	var lastCode string
	for i := 0; i < 60; i++ {
		link, err := s.CreateLink(ctx, "", "https://example.com/test")
		if err != nil {
			t.Fatalf("CreateLink: %v", err)
		}
		if i == 59 {
			lastCode = link.Code
		}
	}

	// Test GET /links?limit=10
	req := httptest.NewRequest(http.MethodGet, "/links?limit=10", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var links []models.Link
	if err := json.NewDecoder(rec.Body).Decode(&links); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if len(links) != 10 {
		t.Fatalf("expected 10 links, got %d", len(links))
	}

	// Verify newest first (last created should be first)
	if links[0].Code != lastCode {
		t.Fatalf("expected first link to be newest (code %s), got %s", lastCode, links[0].Code)
	}

	// Test default limit (50)
	req = httptest.NewRequest(http.MethodGet, "/links", nil)
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var linksDefault []models.Link
	if err := json.NewDecoder(rec.Body).Decode(&linksDefault); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if len(linksDefault) != 50 {
		t.Fatalf("expected 50 links with default limit, got %d", len(linksDefault))
	}

	// Test offset
	req = httptest.NewRequest(http.MethodGet, "/links?limit=10&offset=10", nil)
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var linksOffset []models.Link
	if err := json.NewDecoder(rec.Body).Decode(&linksOffset); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if len(linksOffset) != 10 {
		t.Fatalf("expected 10 links on second page, got %d", len(linksOffset))
	}

	// Second page should not contain the first link from the first page
	for _, link := range linksOffset {
		if link.Code == links[0].Code {
			t.Fatalf("first link from page 1 should not appear in page 2")
		}
	}
}

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
	"github.com/loadez/linkstash/internal/store"
)

// These cases are all rejected before the handler ever touches the store, so
// a nil *store.Store is safe here and keeps this test DB-free.

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
		t.Fatalf("apisrv: open: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func migrationPath(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("apisrv: cannot determine caller for migration path")
	}
	return filepath.Join(filepath.Dir(thisFile), "..", "..", "migrations", "0001_init.sql")
}

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

func TestGetStatsReturnsClickCount(t *testing.T) {
	s := newTestStore(t)
	h := apisrv.NewHandler(s)

	link, err := s.CreateLink(context.Background(), "testcode", "https://example.com/test")
	if err != nil {
		t.Fatalf("CreateLink: %v", err)
	}

	// Record and process some clicks
	ctx := context.Background()
	for i := 0; i < 5; i++ {
		if err := s.RecordClick(ctx, "testcode"); err != nil {
			t.Fatalf("RecordClick: %v", err)
		}
	}
	if _, err := s.ProcessClicks(ctx); err != nil {
		t.Fatalf("ProcessClicks: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/links/testcode/stats", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var resp map[string]interface{}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if resp["code"] != "testcode" {
		t.Fatalf("expected code 'testcode', got %v", resp["code"])
	}

	if int(resp["click_count"].(float64)) != 5 {
		t.Fatalf("expected click_count 5, got %v", resp["click_count"])
	}

	if resp["created_at"] == nil {
		t.Fatal("expected created_at in response")
	}

	// Verify created_at matches what was returned from CreateLink
	if resp["created_at"] != link.CreatedAt.Format("2006-01-02T15:04:05Z07:00") {
		t.Fatalf("created_at mismatch: expected %s, got %v",
			link.CreatedAt.Format("2006-01-02T15:04:05Z07:00"), resp["created_at"])
	}
}

func TestGetStatsReturnsNotFoundForUnknownCode(t *testing.T) {
	s := newTestStore(t)
	h := apisrv.NewHandler(s)

	req := httptest.NewRequest(http.MethodGet, "/links/doesnotexist/stats", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
}

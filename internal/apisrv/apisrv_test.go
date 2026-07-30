package apisrv_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/loadez/linkstash/internal/apisrv"
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


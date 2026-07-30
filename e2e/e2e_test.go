// Package e2e exercises the api and redirector handlers together against a
// real Postgres, without spawning separate processes: create a link through
// the api handler, then follow the short code through the redirector
// handler and confirm we land on the target URL with a click recorded.
//
// Requires a reachable Postgres (DATABASE_URL or the local-dev default);
// skips if none is available. CI always has one via sem-service.
package e2e

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
	"github.com/loadez/linkstash/internal/redirectsrv"
	"github.com/loadez/linkstash/internal/store"
)

func TestCreateLinkAndFollowRedirect(t *testing.T) {
	dsn := config.DatabaseURL()

	setup, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Skipf("e2e: cannot open db handle: %v", err)
	}
	if err := setup.Ping(); err != nil {
		setup.Close()
		t.Skipf("e2e: postgres not reachable at %s: %v", dsn, err)
	}
	schema, err := os.ReadFile(migrationPath(t))
	if err != nil {
		setup.Close()
		t.Fatalf("e2e: read migration: %v", err)
	}
	if _, err := setup.Exec(string(schema)); err != nil {
		setup.Close()
		t.Fatalf("e2e: apply migration: %v", err)
	}
	if _, err := setup.Exec(`TRUNCATE clicks, links RESTART IDENTITY CASCADE`); err != nil {
		setup.Close()
		t.Fatalf("e2e: truncate: %v", err)
	}
	setup.Close()

	s, err := store.Open(dsn)
	if err != nil {
		t.Fatalf("e2e: open store: %v", err)
	}
	defer s.Close()

	api := httptest.NewServer(apisrv.NewHandler(s))
	defer api.Close()
	redirector := httptest.NewServer(redirectsrv.NewHandler(s))
	defer redirector.Close()

	// 1. Create a link via the api service.
	body := strings.NewReader(`{"target_url":"https://example.com/e2e-target"}`)
	resp, err := http.Post(api.URL+"/links", "application/json", body)
	if err != nil {
		t.Fatalf("POST /links: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("POST /links: expected 201, got %d", resp.StatusCode)
	}
	var link models.Link
	if err := json.NewDecoder(resp.Body).Decode(&link); err != nil {
		t.Fatalf("decode created link: %v", err)
	}
	if link.Code == "" {
		t.Fatal("expected a generated code")
	}

	// 2. Follow the short code through the redirector, without
	// auto-following, so we can assert the redirect target directly.
	client := &http.Client{
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	redirResp, err := client.Get(redirector.URL + "/" + link.Code)
	if err != nil {
		t.Fatalf("GET /%s: %v", link.Code, err)
	}
	defer redirResp.Body.Close()

	if redirResp.StatusCode != http.StatusFound {
		t.Fatalf("expected 302, got %d", redirResp.StatusCode)
	}
	if got := redirResp.Header.Get("Location"); got != "https://example.com/e2e-target" {
		t.Fatalf("expected redirect to target URL, got %q", got)
	}

	// 3. The click was recorded (raw, unprocessed) for the worker to fold in.
	var clickCount int
	if err := s2Query(dsn, `SELECT count(*) FROM clicks WHERE code = $1`, link.Code, &clickCount); err != nil {
		t.Fatalf("count clicks: %v", err)
	}
	if clickCount != 1 {
		t.Fatalf("expected 1 recorded click, got %d", clickCount)
	}
}

func TestDeleteLinkViaAPI(t *testing.T) {
	dsn := config.DatabaseURL()

	setup, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Skipf("e2e: cannot open db handle: %v", err)
	}
	if err := setup.Ping(); err != nil {
		setup.Close()
		t.Skipf("e2e: postgres not reachable at %s: %v", dsn, err)
	}
	schema, err := os.ReadFile(migrationPath(t))
	if err != nil {
		setup.Close()
		t.Fatalf("e2e: read migration: %v", err)
	}
	if _, err := setup.Exec(string(schema)); err != nil {
		setup.Close()
		t.Fatalf("e2e: apply migration: %v", err)
	}
	if _, err := setup.Exec(`TRUNCATE clicks, links RESTART IDENTITY CASCADE`); err != nil {
		setup.Close()
		t.Fatalf("e2e: truncate: %v", err)
	}
	setup.Close()

	s, err := store.Open(dsn)
	if err != nil {
		t.Fatalf("e2e: open store: %v", err)
	}
	defer s.Close()

	api := httptest.NewServer(apisrv.NewHandler(s))
	defer api.Close()
	redirector := httptest.NewServer(redirectsrv.NewHandler(s))
	defer redirector.Close()

	// 1. Create a link
	body := strings.NewReader(`{"target_url":"https://example.com/todelete"}`)
	resp, err := http.Post(api.URL+"/links", "application/json", body)
	if err != nil {
		t.Fatalf("POST /links: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("POST /links: expected 201, got %d", resp.StatusCode)
	}
	var link models.Link
	if err := json.NewDecoder(resp.Body).Decode(&link); err != nil {
		t.Fatalf("decode created link: %v", err)
	}

	// 2. Record a click on the link
	if err := s.RecordClick(context.Background(), link.Code); err != nil {
		t.Fatalf("RecordClick: %v", err)
	}

	// 3. Verify the link is in the list
	resp, err = http.Get(api.URL + "/links")
	if err != nil {
		t.Fatalf("GET /links: %v", err)
	}
	defer resp.Body.Close()
	var links []models.Link
	if err := json.NewDecoder(resp.Body).Decode(&links); err != nil {
		t.Fatalf("decode links: %v", err)
	}
	foundBefore := false
	for _, l := range links {
		if l.Code == link.Code {
			foundBefore = true
			break
		}
	}
	if !foundBefore {
		t.Fatal("expected link to be in list before delete")
	}

	// 4. Delete the link via DELETE /links/{code}
	client := &http.Client{}
	req, err := http.NewRequest(http.MethodDelete, api.URL+"/links/"+link.Code, nil)
	if err != nil {
		t.Fatalf("create DELETE request: %v", err)
	}
	resp, err = client.Do(req)
	if err != nil {
		t.Fatalf("DELETE /links/{code}: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", resp.StatusCode)
	}

	// 5. Verify the link is no longer in the list
	resp, err = http.Get(api.URL + "/links")
	if err != nil {
		t.Fatalf("GET /links (after delete): %v", err)
	}
	defer resp.Body.Close()
	links = []models.Link{}
	if err := json.NewDecoder(resp.Body).Decode(&links); err != nil {
		t.Fatalf("decode links: %v", err)
	}
	foundAfter := false
	for _, l := range links {
		if l.Code == link.Code {
			foundAfter = true
			break
		}
	}
	if foundAfter {
		t.Fatal("expected link to be removed from list after delete")
	}

	// 6. Verify the redirector returns 404 for the deleted link
	client = &http.Client{
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	redirResp, err := client.Get(redirector.URL + "/" + link.Code)
	if err != nil {
		t.Fatalf("GET /%s (redirector): %v", link.Code, err)
	}
	defer redirResp.Body.Close()
	if redirResp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404 on redirector after delete, got %d", redirResp.StatusCode)
	}
}

func s2Query(dsn, query string, arg any, dest *int) error {
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		return err
	}
	defer db.Close()
	return db.QueryRow(query, arg).Scan(dest)
}

func migrationPath(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("e2e: cannot determine caller for migration path")
	}
	return filepath.Join(filepath.Dir(thisFile), "..", "migrations", "0001_init.sql")
}

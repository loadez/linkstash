// Package e2e exercises the api and redirector handlers together against a
// real Postgres, without spawning separate processes: create a link through
// the api handler, then follow the short code through the redirector
// handler and confirm we land on the target URL with a click recorded.
//
// Requires a reachable Postgres (DATABASE_URL or the local-dev default);
// skips if none is available. CI always has one via sem-service.
package e2e

import (
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

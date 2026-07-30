package store_test

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	_ "github.com/lib/pq"

	"github.com/loadez/linkstash/internal/config"
	"github.com/loadez/linkstash/internal/store"
)

// newTestStore connects to the Postgres instance described by DATABASE_URL
// (or the local-dev default), applies the schema, truncates it, and returns
// a ready-to-use Store. Tests are skipped (not failed) when Postgres isn't
// reachable, so `go test ./...` still works on a laptop with no DB running;
// CI always has Postgres up via sem-service, so this runs for real there.
func newTestStore(t *testing.T) *store.Store {
	t.Helper()

	dsn := config.DatabaseURL()
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Skipf("store: cannot open db handle: %v", err)
	}
	if err := db.Ping(); err != nil {
		db.Close()
		t.Skipf("store: postgres not reachable at %s: %v", dsn, err)
	}

	schema, err := os.ReadFile(migrationPath(t))
	if err != nil {
		db.Close()
		t.Fatalf("store: read migration: %v", err)
	}
	if _, err := db.Exec(string(schema)); err != nil {
		db.Close()
		t.Fatalf("store: apply migration: %v", err)
	}
	if _, err := db.Exec(`TRUNCATE clicks, links RESTART IDENTITY CASCADE`); err != nil {
		db.Close()
		t.Fatalf("store: truncate: %v", err)
	}
	db.Close()

	s, err := store.Open(dsn)
	if err != nil {
		t.Fatalf("store: open: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func migrationPath(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("store: cannot determine caller for migration path")
	}
	return filepath.Join(filepath.Dir(thisFile), "..", "..", "migrations", "0001_init.sql")
}

func TestCreateAndGetLink(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	link, err := s.CreateLink(ctx, "", "https://example.com/a")
	if err != nil {
		t.Fatalf("CreateLink: %v", err)
	}
	if link.Code == "" {
		t.Fatal("expected a generated code")
	}
	if link.ClickCount != 0 {
		t.Fatalf("expected 0 clicks, got %d", link.ClickCount)
	}

	got, err := s.GetLink(ctx, link.Code)
	if err != nil {
		t.Fatalf("GetLink: %v", err)
	}
	if got.TargetURL != "https://example.com/a" {
		t.Fatalf("target url mismatch: %q", got.TargetURL)
	}
}

func TestCreateLinkWithExplicitCode(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	link, err := s.CreateLink(ctx, "custom", "https://example.com/b")
	if err != nil {
		t.Fatalf("CreateLink: %v", err)
	}
	if link.Code != "custom" {
		t.Fatalf("expected code 'custom', got %q", link.Code)
	}

	if _, err := s.CreateLink(ctx, "custom", "https://example.com/c"); err == nil {
		t.Fatal("expected error creating duplicate code")
	}
}

func TestCreateLinkRequiresTargetURL(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.CreateLink(context.Background(), "", ""); err == nil {
		t.Fatal("expected error for empty target_url")
	}
}

func TestGetLinkNotFound(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.GetLink(context.Background(), "doesnotexist"); err != store.ErrNotFound {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestListLinks(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	if _, err := s.CreateLink(ctx, "one", "https://example.com/1"); err != nil {
		t.Fatalf("CreateLink: %v", err)
	}
	if _, err := s.CreateLink(ctx, "two", "https://example.com/2"); err != nil {
		t.Fatalf("CreateLink: %v", err)
	}

	links, err := s.ListLinks(ctx, 0, 0)
	if err != nil {
		t.Fatalf("ListLinks: %v", err)
	}
	if len(links) != 2 {
		t.Fatalf("expected 2 links, got %d", len(links))
	}
}

func TestListLinksWithPagination(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	// Create 60 links
	var lastCode string
	for i := 0; i < 60; i++ {
		code := fmt.Sprintf("link%02d", i)
		url := fmt.Sprintf("https://example.com/%d", i)
		link, err := s.CreateLink(ctx, code, url)
		if err != nil {
			t.Fatalf("CreateLink: %v", err)
		}
		if i == 59 {
			lastCode = link.Code
		}
	}

	// Get first page with limit 10
	links, err := s.ListLinks(ctx, 10, 0)
	if err != nil {
		t.Fatalf("ListLinks: %v", err)
	}
	if len(links) != 10 {
		t.Fatalf("expected 10 links, got %d", len(links))
	}

	// Verify order is newest first (last created should be first)
	if links[0].Code != lastCode {
		t.Fatalf("expected first link to be newest (%s), got %s", lastCode, links[0].Code)
	}

	// Get second page with limit 10
	links2, err := s.ListLinks(ctx, 10, 10)
	if err != nil {
		t.Fatalf("ListLinks (page 2): %v", err)
	}
	if len(links2) != 10 {
		t.Fatalf("expected 10 links on page 2, got %d", len(links2))
	}

	// Default limit should be 50
	links3, err := s.ListLinks(ctx, 0, 0)
	if err != nil {
		t.Fatalf("ListLinks (default limit): %v", err)
	}
	if len(links3) != 50 {
		t.Fatalf("expected 50 links with default limit, got %d", len(links3))
	}
}

func TestRecordClickAndProcessClicks(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	link, err := s.CreateLink(ctx, "clicky", "https://example.com/clicky")
	if err != nil {
		t.Fatalf("CreateLink: %v", err)
	}

	for i := 0; i < 3; i++ {
		if err := s.RecordClick(ctx, link.Code); err != nil {
			t.Fatalf("RecordClick: %v", err)
		}
	}

	got, err := s.GetLink(ctx, link.Code)
	if err != nil {
		t.Fatalf("GetLink: %v", err)
	}
	if got.ClickCount != 0 {
		t.Fatalf("expected click_count to still be 0 before processing, got %d", got.ClickCount)
	}

	n, err := s.ProcessClicks(ctx)
	if err != nil {
		t.Fatalf("ProcessClicks: %v", err)
	}
	if n != 1 {
		t.Fatalf("expected 1 link updated, got %d", n)
	}

	got, err = s.GetLink(ctx, link.Code)
	if err != nil {
		t.Fatalf("GetLink: %v", err)
	}
	if got.ClickCount != 3 {
		t.Fatalf("expected click_count 3, got %d", got.ClickCount)
	}

	// Re-running with no new clicks should be a no-op.
	n, err = s.ProcessClicks(ctx)
	if err != nil {
		t.Fatalf("ProcessClicks (second run): %v", err)
	}
	if n != 0 {
		t.Fatalf("expected 0 links updated on second run, got %d", n)
	}
}

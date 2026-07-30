package store_test

import (
	"context"
	"database/sql"
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

	// Apply the second migration for expires_at column
	schema2Path := filepath.Join(filepath.Dir(migrationPath(t)), "0002_add_expires_at.sql")
	schema2, err := os.ReadFile(schema2Path)
	if err != nil {
		db.Close()
		t.Fatalf("store: read migration 0002: %v", err)
	}
	if _, err := db.Exec(string(schema2)); err != nil {
		db.Close()
		t.Fatalf("store: apply migration 0002: %v", err)
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

	link, err := s.CreateLink(ctx, "", "https://example.com/a", sql.NullTime{})
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

	link, err := s.CreateLink(ctx, "custom", "https://example.com/b", sql.NullTime{})
	if err != nil {
		t.Fatalf("CreateLink: %v", err)
	}
	if link.Code != "custom" {
		t.Fatalf("expected code 'custom', got %q", link.Code)
	}

	if _, err := s.CreateLink(ctx, "custom", "https://example.com/c", sql.NullTime{}); err == nil {
		t.Fatal("expected error creating duplicate code")
	}
}

func TestCreateLinkRequiresTargetURL(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.CreateLink(context.Background(), "", "", sql.NullTime{}); err == nil {
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

	if _, err := s.CreateLink(ctx, "one", "https://example.com/1", sql.NullTime{}); err != nil {
		t.Fatalf("CreateLink: %v", err)
	}
	if _, err := s.CreateLink(ctx, "two", "https://example.com/2", sql.NullTime{}); err != nil {
		t.Fatalf("CreateLink: %v", err)
	}

	links, err := s.ListLinks(ctx)
	if err != nil {
		t.Fatalf("ListLinks: %v", err)
	}
	if len(links) != 2 {
		t.Fatalf("expected 2 links, got %d", len(links))
	}
}

func TestRecordClickAndProcessClicks(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	link, err := s.CreateLink(ctx, "clicky", "https://example.com/clicky", sql.NullTime{})
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

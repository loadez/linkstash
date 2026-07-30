package config_test

import (
	"os"
	"testing"

	"github.com/loadez/linkstash/internal/config"
)

func TestDatabaseURLDefault(t *testing.T) {
	os.Unsetenv("DATABASE_URL")
	got := config.DatabaseURL()
	if got == "" {
		t.Fatal("expected a non-empty default DSN")
	}
}

func TestDatabaseURLFromEnv(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://custom/dsn")
	if got := config.DatabaseURL(); got != "postgres://custom/dsn" {
		t.Fatalf("expected env override, got %q", got)
	}
}

func TestAddrDefault(t *testing.T) {
	os.Unsetenv("PORT")
	if got := config.Addr("9999"); got != ":9999" {
		t.Fatalf("expected :9999, got %q", got)
	}
}

func TestAddrFromEnv(t *testing.T) {
	t.Setenv("PORT", "1234")
	if got := config.Addr("9999"); got != ":1234" {
		t.Fatalf("expected :1234, got %q", got)
	}
}

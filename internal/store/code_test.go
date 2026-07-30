package store_test

import (
	"testing"

	"github.com/loadez/linkstash/internal/store"
)

func TestGenerateCode(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 50; i++ {
		code, err := store.GenerateCode()
		if err != nil {
			t.Fatalf("GenerateCode: %v", err)
		}
		if len(code) != 7 {
			t.Fatalf("expected 7-char code, got %q (len %d)", code, len(code))
		}
		if seen[code] {
			t.Fatalf("GenerateCode produced a duplicate: %q", code)
		}
		seen[code] = true
	}
}

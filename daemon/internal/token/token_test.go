package token

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadOrCreateGeneratesThenReuses(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", "token")

	first, err := LoadOrCreate(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(first) != 64 { // 32 bytes hex-encoded
		t.Fatalf("token length = %d, want 64", len(first))
	}

	second, err := LoadOrCreate(path)
	if err != nil {
		t.Fatal(err)
	}
	if second != first {
		t.Fatalf("second LoadOrCreate returned a different token")
	}
}

func TestLoadOrCreateRegeneratesEmptyFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "token")
	// An interrupted first run can leave a 0-byte token file; it must not
	// brick startup forever.
	if err := os.WriteFile(path, []byte("  \n"), 0o600); err != nil {
		t.Fatal(err)
	}

	tok, err := LoadOrCreate(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(tok) != 64 {
		t.Fatalf("token length = %d, want 64", len(tok))
	}
}

func TestLoadMissingFileErrors(t *testing.T) {
	if _, err := Load(filepath.Join(t.TempDir(), "nope")); err == nil {
		t.Fatal("Load on missing file: want error, got nil")
	}
}

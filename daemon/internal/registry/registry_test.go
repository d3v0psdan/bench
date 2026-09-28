package registry

import (
	"os"
	"path/filepath"
	"testing"
)

func TestOpenCreatesSchema(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bench.db")

	r, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()

	for _, table := range []string{"sites", "services", "settings", "schema_migrations"} {
		var n int
		err := r.DB().QueryRow(
			`SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = ?`, table,
		).Scan(&n)
		if err != nil {
			t.Fatal(err)
		}
		if n != 1 {
			t.Errorf("table %q missing", table)
		}
	}
}

func TestOpenPathWithURISpecialChars(t *testing.T) {
	// SQLite treats the DSN as a URI: unescaped '%HH', '#', or '?' in the
	// bench home would silently open a database at the wrong location.
	dir := filepath.Join(t.TempDir(), "we#ird %25 dir")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "bench.db")

	r, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.DB().Exec(`INSERT INTO settings (key, value) VALUES ('k', 'v')`); err != nil {
		t.Fatal(err)
	}
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("database not created at the intended path: %v", err)
	}
}

func TestOpenIsIdempotent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bench.db")

	for i := 0; i < 2; i++ {
		r, err := Open(path)
		if err != nil {
			t.Fatalf("open #%d: %v", i+1, err)
		}
		if err := r.Close(); err != nil {
			t.Fatalf("close #%d: %v", i+1, err)
		}
	}
}

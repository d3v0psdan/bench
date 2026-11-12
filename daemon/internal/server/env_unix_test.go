//go:build !windows

package server

import (
	"io/fs"
	"os"
	"path/filepath"
	"testing"
)

// Not built on Windows: it has no Unix permission bits (os.Chmod only sets
// read-only), so the mode writeAtomic leaves can't be checked there.
func TestWriteAtomicKeepsAnExistingModeAndGivesANewFile0644(t *testing.T) {
	tests := []struct {
		name     string
		existing fs.FileMode // 0: no .env yet
		want     fs.FileMode
	}{
		{name: "a private .env stays private", existing: 0o600, want: 0o600},
		{name: "a group-readable .env keeps its bits", existing: 0o640, want: 0o640},
		{name: "a new .env is 0644", existing: 0, want: 0o644},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), ".env")
			if tt.existing != 0 {
				if err := os.WriteFile(path, []byte("OLD=1\n"), 0o600); err != nil {
					t.Fatal(err)
				}
				// Chmod, not WriteFile's mode, so the umask can't change it.
				if err := os.Chmod(path, tt.existing); err != nil {
					t.Fatal(err)
				}
			}
			if err := writeAtomic(path, []byte("NEW=1\n")); err != nil {
				t.Fatal(err)
			}
			fi, err := os.Stat(path)
			if err != nil {
				t.Fatal(err)
			}
			if got := fi.Mode().Perm(); got != tt.want {
				t.Errorf("mode after writeAtomic = %o, want %o", got, tt.want)
			}
		})
	}
}

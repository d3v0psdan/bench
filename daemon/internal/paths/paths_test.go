package paths

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRootHonorsBenchHome(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("BENCH_HOME", dir)

	root, err := Root()
	if err != nil {
		t.Fatal(err)
	}
	if root != dir {
		t.Fatalf("Root() = %q, want %q", root, dir)
	}
}

func TestRootDefaultsToHomeDotBench(t *testing.T) {
	t.Setenv("BENCH_HOME", "")
	// Setenv with "" still sets the var; unset it for the default path.
	os.Unsetenv("BENCH_HOME")

	root, err := Root()
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(root) != ".bench" {
		t.Fatalf("Root() = %q, want a .bench directory", root)
	}
}

func TestFilesLiveUnderRoot(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("BENCH_HOME", dir)

	for name, fn := range map[string]func() (string, error){
		"TokenFile": TokenFile,
		"DBFile":    DBFile,
		"AddrFile":  AddrFile,
		"LogFile":   LogFile,
	} {
		p, err := fn()
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if !strings.HasPrefix(p, dir) {
			t.Errorf("%s() = %q, not under root %q", name, p, dir)
		}
	}
}

func TestEnsureRootCreatesLogsDir(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "nested", "home")
	t.Setenv("BENCH_HOME", dir)

	root, err := EnsureRoot()
	if err != nil {
		t.Fatal(err)
	}
	if root != dir {
		t.Fatalf("EnsureRoot() = %q, want %q", root, dir)
	}
	if _, err := os.Stat(filepath.Join(dir, "logs")); err != nil {
		t.Fatalf("logs dir not created: %v", err)
	}
}

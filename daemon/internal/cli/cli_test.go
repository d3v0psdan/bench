package cli

import (
	"bytes"
	"io"
	"log/slog"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/d3v0psdan/bench/daemon/internal/api"
	"github.com/d3v0psdan/bench/daemon/internal/server"
)

// runBench executes the CLI with args against a fresh command tree and
// returns its combined output.
func runBench(t *testing.T, args ...string) (string, error) {
	t.Helper()
	buf := new(bytes.Buffer)
	cmd := New()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs(args)
	err := cmd.Execute()
	return buf.String(), err
}

func TestStatusNotRunning(t *testing.T) {
	t.Setenv("BENCH_HOME", t.TempDir())

	out, err := runBench(t, "status")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "not running") {
		t.Errorf("output = %q, want it to say not running", out)
	}
}

func TestStatusAgainstLiveDaemon(t *testing.T) {
	home := t.TempDir()
	t.Setenv("BENCH_HOME", home)

	srv := &server.Server{
		Version:   "1.2.3",
		Token:     "tok",
		StartedAt: time.Now(),
		Log:       slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	addr := strings.TrimPrefix(ts.URL, "http://")
	if err := os.WriteFile(filepath.Join(home, "benchd.addr"), []byte(addr+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, "token"), []byte("tok\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	out, err := runBench(t, "status")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "benchd running") || !strings.Contains(out, "v1.2.3") {
		t.Errorf("output = %q, want running status with version", out)
	}
}

func TestStatusStaleAddrFile(t *testing.T) {
	home := t.TempDir()
	t.Setenv("BENCH_HOME", home)

	// Addr file points at a port nothing listens on (crash leftover).
	if err := os.WriteFile(filepath.Join(home, "benchd.addr"), []byte("127.0.0.1:1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, "token"), []byte("tok\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	out, err := runBench(t, "status")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "not running") {
		t.Errorf("output = %q, want it to say not running", out)
	}
}

func TestPickDB(t *testing.T) {
	one := []api.Service{{Name: "cache", Service: "mailpit"}, {Name: "app-db", Service: "mysql"}}
	if s, err := pickDB(one, nil); err != nil || s.Name != "app-db" {
		t.Fatalf("single db: %+v, %v", s, err)
	}
	two := append(one, api.Service{Name: "pg", Service: "postgresql"})
	if _, err := pickDB(two, nil); err == nil || !strings.Contains(err.Error(), "app-db|pg") {
		t.Fatalf("ambiguous: want a list of names, got %v", err)
	}
	if s, err := pickDB(two, []string{"pg"}); err != nil || s.Name != "pg" {
		t.Fatalf("by name: %+v, %v", s, err)
	}
	if _, err := pickDB(two, []string{"cache"}); err == nil {
		t.Fatal("mail is not a database")
	}
}

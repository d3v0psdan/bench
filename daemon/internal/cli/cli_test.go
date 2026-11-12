package cli

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
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

func TestPathArgumentsReachTheDaemonAbsolute(t *testing.T) {
	home := t.TempDir()
	t.Setenv("BENCH_HOME", home)
	t.Chdir(t.TempDir())
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}

	// A real daemon for the event stream `bench new` opens first, with the
	// path-taking endpoints swapped for recorders.
	srv := &server.Server{Token: "tok", StartedAt: time.Now(), Heartbeat: time.Minute, Log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	sent := make(chan string, 1)
	record := func(field string, status int, reply string) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			path, _ := body[field].(string) // absent ("" by omitempty) reads as ""
			sent <- path
			w.WriteHeader(status)
			io.WriteString(w, reply)
		}
	}
	mux := http.NewServeMux()
	mux.Handle("/", srv.Handler())
	mux.HandleFunc("POST /api/sites/park", record("path", http.StatusOK, "[]"))
	mux.HandleFunc("POST /api/sites/unpark", record("path", http.StatusOK, "[]"))
	// Refused, so `bench new` stops instead of waiting for a task.
	mux.HandleFunc("POST /api/sites/new", record("dir", http.StatusBadRequest, "recorded"))
	ts := httptest.NewServer(mux)
	defer ts.Close()
	if err := os.WriteFile(filepath.Join(home, "benchd.addr"), []byte(strings.TrimPrefix(ts.URL, "http://")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, "token"), []byte("tok\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name string
		args []string
		want string
	}{
		{name: "park .", args: []string{"park", "."}, want: cwd},
		{name: "park a subfolder", args: []string{"park", "code"}, want: filepath.Join(cwd, "code")},
		{name: "park with no argument", args: []string{"park"}, want: cwd},
		{name: "unpark .", args: []string{"unpark", "."}, want: cwd},
		{name: "unpark with no argument", args: []string{"unpark"}, want: cwd},
		{name: "new --dir a subfolder", args: []string{"new", "shop", "--dir", "code"}, want: filepath.Join(cwd, "code")},
		{name: "new without --dir leaves the projects folder to the daemon", args: []string{"new", "shop"}, want: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out, _ := runBench(t, tt.args...)
			select {
			case got := <-sent:
				if got != tt.want {
					t.Errorf("daemon got %q, want %q", got, tt.want)
				}
			default:
				t.Fatalf("nothing reached the daemon; output: %s", out)
			}
		})
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

func TestSitesTableSaysNoneForASiteWithNoPHP(t *testing.T) {
	buf := new(bytes.Buffer)
	cmd := New()
	cmd.SetOut(buf)
	printSites(cmd, []api.Site{
		{URL: "https://app.test", Kind: "linked", Path: "/code/app"},
		{URL: "https://api.test", Kind: "proxy", ProxyTo: "127.0.0.1:3000"},
	})
	lines := strings.Split(buf.String(), "\n")
	if len(lines) < 3 || !strings.Contains(lines[1], " none ") || strings.Contains(lines[2], "none") {
		t.Fatalf("sites table:\n%s\nwant PHP none for app.test and blank for the proxy", buf.String())
	}
}

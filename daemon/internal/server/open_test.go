package server

import (
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/d3v0psdan/bench/daemon/internal/api"
	"github.com/d3v0psdan/bench/daemon/internal/registry"
	"github.com/d3v0psdan/bench/daemon/internal/sites"
)

func TestSiteDatabaseMatchesConnectionAndPort(t *testing.T) {
	list := []api.Service{
		{Name: "mysql", Service: "mysql", Port: 3307},
		{Name: "maria", Service: "mariadb", Port: 3306},
		{Name: "pg", Service: "postgresql", Port: 5432},
	}
	for _, c := range []struct {
		name string
		env  map[string]string
		want string // "" = a conflict error
	}{
		{"mysql on its port", map[string]string{"DB_CONNECTION": "mysql", "DB_PORT": "3307"}, "mysql"},
		{"mysql driver reaches mariadb", map[string]string{"DB_CONNECTION": "mysql", "DB_PORT": "3306"}, "maria"},
		{"pgsql default port", map[string]string{"DB_CONNECTION": "pgsql"}, "pg"},
		{"nothing on that port", map[string]string{"DB_CONNECTION": "mysql", "DB_PORT": "3399"}, ""},
		{"sqlite", map[string]string{"DB_CONNECTION": "sqlite"}, ""},
		{"remote host", map[string]string{"DB_CONNECTION": "mysql", "DB_HOST": "db.example.com", "DB_PORT": "3307"}, ""},
		{"no connection", map[string]string{}, ""},
	} {
		got, err := siteDatabase("demo.test", c.env, list)
		if c.want == "" {
			if !errors.As(err, new(errConflict)) {
				t.Errorf("%s: got %+v, %v; want a conflict error", c.name, got, err)
			}
			continue
		}
		if err != nil || got.Name != c.want {
			t.Errorf("%s: got %q, %v; want %q", c.name, got.Name, err, c.want)
		}
	}
}

func TestOpenDatabaseRefusesStoppedInstances(t *testing.T) {
	s := &Server{}
	err := s.openDatabase(httptest.NewRecorder(), api.Service{Name: "mysql", Service: "mysql", State: "stopped"})
	if !errors.As(err, new(errConflict)) {
		t.Fatalf("got %v, want a conflict: a stopped database must not launch a client", err)
	}
	err = s.openDatabase(httptest.NewRecorder(), api.Service{Name: "search", Service: "meilisearch", State: "running"})
	if !errors.As(err, new(errBadRequest)) {
		t.Fatalf("got %v, want a bad request for a non-database", err)
	}
}

func TestOpenSiteRoutes(t *testing.T) {
	reg, err := registry.Open(filepath.Join(t.TempDir(), "bench.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { reg.Close() })
	s := &Server{
		Token: testToken, StartedAt: time.Now(), Heartbeat: time.Minute,
		Log:   slog.New(slog.NewTextHandler(io.Discard, nil)),
		Sites: &sites.Manager{Reg: reg, Router: nopRouter{}, Log: slog.New(slog.NewTextHandler(io.Discard, nil))},
	}
	ts := httptest.NewServer(s.Handler())
	t.Cleanup(ts.Close)

	for _, c := range []struct {
		method, path, body string
		want               int
	}{
		{"POST", "/api/sites/nope/open", `{"in":"editor"}`, http.StatusNotFound},
		{"POST", "/api/settings", `{"key":"editor.default","value":"notepad"}`, http.StatusBadRequest},
		{"POST", "/api/settings", `{"key":"editor.default","value":"zed"}`, http.StatusOK},
		{"GET", "/api/editors", "", http.StatusOK},
	} {
		resp := authedJSON(t, c.method, ts.URL+c.path, c.body)
		resp.Body.Close()
		if resp.StatusCode != c.want {
			t.Errorf("%s %s %s: status %d, want %d", c.method, c.path, c.body, resp.StatusCode, c.want)
		}
	}
	if got, _ := reg.Setting("editor.default"); got != "zed" {
		t.Errorf("editor.default = %q, want zed", got)
	}
}

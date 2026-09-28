package server

import (
	"context"
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
	"github.com/d3v0psdan/bench/daemon/internal/dotenv"
	"github.com/d3v0psdan/bench/daemon/internal/registry"
	"github.com/d3v0psdan/bench/daemon/internal/sites"
)

// nopPools hands out a fake upstream for every PHP channel.
type nopPools struct{}

func (nopPools) Ensure(context.Context, string) (string, error) { return "127.0.0.1:9000", nil }

func TestEnvDryRunThenWrite(t *testing.T) {
	root := t.TempDir()
	reg, err := registry.Open(filepath.Join(root, "bench.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { reg.Close() })
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	siteMgr := &sites.Manager{Reg: reg, Router: nopRouter{}, Pools: nopPools{}, Log: log}
	project := filepath.Join(root, "shop")
	envPath := filepath.Join(project, ".env")
	if err := os.MkdirAll(project, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(envPath, []byte("APP_URL=http://localhost\nDB_CONNECTION=sqlite\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := siteMgr.Link(context.Background(), project, "shop"); err != nil {
		t.Fatal(err)
	}
	s := &Server{Token: testToken, StartedAt: time.Now(), Heartbeat: time.Minute, Log: log, Sites: siteMgr}
	ts := httptest.NewServer(s.Handler())
	t.Cleanup(ts.Close)

	post := func(body string) (int, api.EnvResult) {
		resp := authedJSON(t, "POST", ts.URL+"/api/sites/shop/env", body)
		defer resp.Body.Close()
		var res api.EnvResult
		_ = json.NewDecoder(resp.Body).Decode(&res)
		return resp.StatusCode, res
	}

	lines := `["APP_URL=https://shop.test","DB_CONNECTION=mysql","DB_PORT=3307"]`
	code, res := post(`{"lines":` + lines + `,"dry_run":true}`)
	if code != http.StatusOK || len(res.Changes) != 3 {
		t.Fatalf("dry run: %d %+v", code, res)
	}
	if b, _ := os.ReadFile(envPath); strings.Contains(string(b), "mysql") {
		t.Fatal("a dry run wrote the file")
	}

	if code, _ := post(`{"lines":` + lines + `}`); code != http.StatusOK {
		t.Fatalf("apply: %d", code)
	}
	b, _ := os.ReadFile(envPath)
	if !strings.Contains(string(b), dotenv.Marker+"\nDB_CONNECTION=mysql") || !strings.Contains(string(b), "DB_PORT=3307") {
		t.Fatalf(".env after apply:\n%s", b)
	}

	if code, _ := post(`{"lines":["db_port=1"]}`); code != http.StatusBadRequest {
		t.Errorf("a malformed line: %d, want 400", code)
	}
	if code, _ := post(`{"lines":[]}`); code != http.StatusOK {
		t.Errorf("nothing to set: %d, want 200", code)
	}

	icon := authedJSON(t, "GET", ts.URL+"/api/sites/shop/icon", "")
	icon.Body.Close()
	if icon.StatusCode != http.StatusNotFound {
		t.Errorf("no favicon yet: %d, want 404", icon.StatusCode)
	}
	if err := os.MkdirAll(filepath.Join(project, "public"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(project, "public", "favicon.svg"), []byte("<svg/>"), 0o644); err != nil {
		t.Fatal(err)
	}
	icon = authedJSON(t, "GET", ts.URL+"/api/sites/shop/icon", "")
	body, _ := io.ReadAll(icon.Body)
	icon.Body.Close()
	if icon.StatusCode != http.StatusOK || string(body) != "<svg/>" {
		t.Errorf("favicon: %d %q", icon.StatusCode, body)
	}
}

func TestEnvRefusesASymlinkedEnv(t *testing.T) {
	root := t.TempDir()
	reg, err := registry.Open(filepath.Join(root, "bench.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { reg.Close() })
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	siteMgr := &sites.Manager{Reg: reg, Router: nopRouter{}, Pools: nopPools{}, Log: log}
	project := filepath.Join(root, "shop")
	victim := filepath.Join(root, "other.env")
	if err := os.MkdirAll(project, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(victim, []byte("KEEP=1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(victim, filepath.Join(project, ".env")); err != nil {
		t.Skipf("symlinks unavailable here: %v", err)
	}
	if _, err := siteMgr.Link(context.Background(), project, "shop"); err != nil {
		t.Fatal(err)
	}
	s := &Server{Token: testToken, StartedAt: time.Now(), Heartbeat: time.Minute, Log: log, Sites: siteMgr}
	ts := httptest.NewServer(s.Handler())
	t.Cleanup(ts.Close)
	resp := authedJSON(t, "POST", ts.URL+"/api/sites/shop/env", `{"lines":["APP_URL=https://shop.test"]}`)
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("a symlinked .env: status %d, want 400", resp.StatusCode)
	}
	if b, _ := os.ReadFile(victim); string(b) != "KEEP=1\n" {
		t.Errorf("the link's target was written: %q", b)
	}
}

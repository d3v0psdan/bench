package server

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/d3v0psdan/bench/daemon/internal/binman"
	"github.com/d3v0psdan/bench/daemon/internal/caddy"
	"github.com/d3v0psdan/bench/daemon/internal/registry"
	"github.com/d3v0psdan/bench/daemon/internal/sites"
)

func TestUninstallPHPRefusesVersionsSitesNeed(t *testing.T) {
	reg, err := registry.Open(filepath.Join(t.TempDir(), "bench.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { reg.Close() })
	if err := reg.SetSetting("php.default", "8.4"); err != nil {
		t.Fatal(err)
	}
	if err := reg.UpsertSite(registry.Site{Name: "blog", Path: t.TempDir(), Kind: "linked", PHPVersion: "8.3"}); err != nil {
		t.Fatal(err)
	}
	php := func(channel string) binman.Build {
		return binman.Build{Name: "php", Version: channel + ".0", Channel: channel, OS: runtime.GOOS, Arch: runtime.GOARCH}
	}
	var removed []string
	s := &Server{
		Token:     testToken,
		StartedAt: time.Now(),
		Log:       slog.New(slog.NewTextHandler(io.Discard, nil)),
		Heartbeat: time.Minute,
		Binaries: &binman.Manager{Manifests: map[string]binman.Manifest{
			"php": {Schema: 1, Name: "php", Builds: []binman.Build{php("8.3"), php("8.4")}},
		}},
		Sites: &sites.Manager{Reg: reg},
		RemovePHP: func(channel string) error {
			removed = append(removed, channel)
			return nil
		},
	}
	ts := httptest.NewServer(s.Handler())
	t.Cleanup(ts.Close)

	del := func(path string) int {
		resp := authedJSON(t, "DELETE", ts.URL+path, "")
		resp.Body.Close()
		return resp.StatusCode
	}
	if got := del("/api/binaries/mysql/8.4"); got != http.StatusBadRequest {
		t.Errorf("uninstall mysql = %d, want 400", got)
	}
	if got := del("/api/binaries/php/8.4"); got != http.StatusConflict {
		t.Errorf("uninstall the default = %d, want 409", got)
	}
	if got := del("/api/binaries/php/8.4.0"); got != http.StatusConflict {
		t.Errorf("uninstall the default by full version = %d, want 409", got)
	}
	if got := del("/api/binaries/php/8.3.0"); got != http.StatusConflict {
		t.Errorf("uninstall a pinned version by full version = %d, want 409", got)
	}
	if got := del("/api/binaries/php/8.3"); got != http.StatusConflict {
		t.Errorf("uninstall a pinned version = %d, want 409", got)
	}
	if len(removed) != 0 {
		t.Fatalf("removed %v despite refusing", removed)
	}
	if err := reg.SetSitePHP("blog", ""); err != nil {
		t.Fatal(err)
	}
	if got := del("/api/binaries/php/8.3"); got != http.StatusNoContent {
		t.Fatalf("uninstall an unused version = %d, want 204", got)
	}
	if len(removed) != 1 || removed[0] != "8.3" {
		t.Fatalf("removed = %v, want [8.3]", removed)
	}

	// Rows written before versions were stored by channel hold full
	// versions; they still count.
	removed = nil
	if err := reg.SetSetting("php.default", "8.4.0"); err != nil {
		t.Fatal(err)
	}
	if err := reg.SetSitePHP("blog", "8.3.0"); err != nil {
		t.Fatal(err)
	}
	if got := del("/api/binaries/php/8.4"); got != http.StatusConflict {
		t.Errorf("uninstall a default stored as a full version = %d, want 409", got)
	}
	if got := del("/api/binaries/php/8.3"); got != http.StatusConflict {
		t.Errorf("uninstall a pin stored as a full version = %d, want 409", got)
	}
	if len(removed) != 0 {
		t.Fatalf("removed %v despite full-version default and pin", removed)
	}
}

func TestPHPVersionsAreStoredByChannel(t *testing.T) {
	reg, err := registry.Open(filepath.Join(t.TempDir(), "bench.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { reg.Close() })
	build := binman.Build{Name: "php", Version: "8.4.12", Channel: "8.4", OS: runtime.GOOS, Arch: runtime.GOARCH}
	s := &Server{
		Token:     testToken,
		StartedAt: time.Now(),
		Log:       slog.New(slog.NewTextHandler(io.Discard, nil)),
		Heartbeat: time.Minute,
		Binaries:  &binman.Manager{Manifests: map[string]binman.Manifest{"php": {Schema: 1, Name: "php", Builds: []binman.Build{build}}}},
		Sites:     &sites.Manager{Reg: reg, Router: nopRouter{}, Log: slog.New(slog.NewTextHandler(io.Discard, nil))},
	}
	ts := httptest.NewServer(s.Handler())
	t.Cleanup(ts.Close)

	resp := authedJSON(t, "POST", ts.URL+"/api/settings", `{"key":"php.default","value":"8.4.12"}`)
	resp.Body.Close()
	if got, _ := reg.Setting("php.default"); got != "8.4" {
		t.Fatalf("php.default stored as %q, want the channel 8.4", got)
	}
}

// nopRouter accepts every Caddy config.
type nopRouter struct{}

func (nopRouter) Ensure(context.Context) error              { return nil }
func (nopRouter) Apply(context.Context, []caddy.Site) error { return nil }

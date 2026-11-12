package server

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"runtime"
	"strings"
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
		Sites: &sites.Manager{Reg: reg, InstalledPHP: installedPHP("8.3", "8.4")},
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
		Sites: &sites.Manager{
			Reg: reg, Router: nopRouter{}, Log: slog.New(slog.NewTextHandler(io.Discard, nil)),
			InstalledPHP: installedPHP("8.4"),
		},
	}
	ts := httptest.NewServer(s.Handler())
	t.Cleanup(ts.Close)

	resp := authedJSON(t, "POST", ts.URL+"/api/settings", `{"key":"php.default","value":"8.4.12"}`)
	resp.Body.Close()
	if got, _ := reg.Setting("php.default"); got != "8.4" {
		t.Fatalf("php.default stored as %q, want the channel 8.4", got)
	}
}

// phpServer serves a catalog of PHP channels with installed ones on disk
// and php.default saved as savedDefault.
func phpServer(t *testing.T, catalog, installed []string, savedDefault string) (*httptest.Server, *registry.Registry, *[]string) {
	t.Helper()
	reg, err := registry.Open(filepath.Join(t.TempDir(), "bench.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { reg.Close() })
	if err := reg.SetSetting("php.default", savedDefault); err != nil {
		t.Fatal(err)
	}
	var builds []binman.Build
	for _, c := range catalog {
		builds = append(builds, binman.Build{Name: "php", Version: c + ".0", Channel: c, OS: runtime.GOOS, Arch: runtime.GOARCH})
	}
	removed := &[]string{}
	s := &Server{
		Token:     testToken,
		StartedAt: time.Now(),
		Log:       slog.New(slog.NewTextHandler(io.Discard, nil)),
		Heartbeat: time.Minute,
		Binaries:  &binman.Manager{Manifests: map[string]binman.Manifest{"php": {Schema: 1, Name: "php", Builds: builds}}},
		Sites: &sites.Manager{
			Reg: reg, Router: nopRouter{}, Log: slog.New(slog.NewTextHandler(io.Discard, nil)),
			InstalledPHP: installedPHP(installed...),
		},
		RemovePHP: func(channel string) error {
			*removed = append(*removed, channel)
			return nil
		},
	}
	ts := httptest.NewServer(s.Handler())
	t.Cleanup(ts.Close)
	return ts, reg, removed
}

// The owner's machine: 8.4 saved from the first launch, only 8.5 installed.
func TestUninstallPHPRefusesTheInstalledVersionStandingInForAMissingSavedDefault(t *testing.T) {
	ts, _, removed := phpServer(t, []string{"8.4", "8.5"}, []string{"8.5"}, "8.4")

	resp := authedJSON(t, "DELETE", ts.URL+"/api/binaries/php/8.5", "")
	resp.Body.Close()
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("uninstall 8.5 = %d, want 409: it is the default", resp.StatusCode)
	}
	if len(*removed) != 0 {
		t.Fatalf("removed %v despite refusing", *removed)
	}
}

func TestSettingAnUnusablePHPDefaultIsA400AndSavesNothing(t *testing.T) {
	ts, reg, _ := phpServer(t, []string{"8.3", "8.4"}, []string{"8.4"}, "8.4")
	for _, tc := range []struct {
		name, value, want string
	}{
		{name: "empty", value: "", want: "name a PHP version"},
		{name: "not in the catalog", value: "7.1", want: "PHP 7.1 isn't in Bench's catalog"},
		{name: "not installed", value: "8.3", want: "PHP 8.3 isn't installed; install it on the Runtimes page"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			resp := authedJSON(t, "POST", ts.URL+"/api/settings", `{"key":"php.default","value":"`+tc.value+`"}`)
			body, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			if resp.StatusCode != http.StatusBadRequest || !strings.Contains(string(body), tc.want) {
				t.Fatalf("set default %q = %d %q, want 400 with %q", tc.value, resp.StatusCode, body, tc.want)
			}
			if got, _ := reg.Setting("php.default"); got != "8.4" {
				t.Fatalf("php.default = %q after a refused change, want 8.4", got)
			}
		})
	}
}

// installedPHP stands in for the PHP channels installed on disk.
func installedPHP(channels ...string) func() []string {
	return func() []string { return channels }
}

// nopRouter accepts every Caddy config.
type nopRouter struct{}

func (nopRouter) Ensure(context.Context) error              { return nil }
func (nopRouter) Apply(context.Context, []caddy.Site) error { return nil }

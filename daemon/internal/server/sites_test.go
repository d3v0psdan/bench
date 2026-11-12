package server

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/d3v0psdan/bench/daemon/internal/registry"
	"github.com/d3v0psdan/bench/daemon/internal/sites"
)

func TestSitePathHandlersAcceptOnlyAnAbsolutePath(t *testing.T) {
	root := t.TempDir()
	reg, err := registry.Open(filepath.Join(root, "bench.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { reg.Close() })
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	siteMgr := &sites.Manager{Reg: reg, Router: nopRouter{}, Pools: nopPools{}, Log: log, InstalledPHP: installedPHP("8.4")}
	s := &Server{Token: testToken, StartedAt: time.Now(), Heartbeat: time.Minute, Log: log, Sites: siteMgr}
	ts := httptest.NewServer(s.Handler())
	t.Cleanup(ts.Close)

	tests := []struct {
		name     string
		endpoint string
		path     string
		want     int
	}{
		{name: "park a relative path", endpoint: "park", path: ".", want: http.StatusBadRequest},
		{name: "park no path", endpoint: "park", path: "", want: http.StatusBadRequest},
		{name: "unpark a relative path", endpoint: "unpark", path: "code", want: http.StatusBadRequest},
		{name: "link a relative path", endpoint: "link", path: ".", want: http.StatusBadRequest},
		{name: "park an absolute path", endpoint: "park", path: root, want: http.StatusOK},
		{name: "unpark an absolute path", endpoint: "unpark", path: root, want: http.StatusOK},
		{name: "link an absolute path", endpoint: "link", path: root, want: http.StatusOK},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body, _ := json.Marshal(map[string]string{"path": tt.path, "name": "shop"})
			resp := authedJSON(t, "POST", ts.URL+"/api/sites/"+tt.endpoint, string(body))
			msg, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			if resp.StatusCode != tt.want {
				t.Fatalf("status %d (%s), want %d", resp.StatusCode, msg, tt.want)
			}
			if tt.want == http.StatusBadRequest && !strings.Contains(string(msg), "absolute") {
				t.Errorf("message %q doesn't say the path must be absolute", msg)
			}
		})
	}
}

func TestLinkAndProxyAnswer400WithTheMessageForABadSiteName(t *testing.T) {
	root := t.TempDir()
	reg, err := registry.Open(filepath.Join(root, "bench.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { reg.Close() })
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	siteMgr := &sites.Manager{Reg: reg, Router: nopRouter{}, Pools: nopPools{}, Log: log, InstalledPHP: installedPHP("8.4")}
	s := &Server{Token: testToken, StartedAt: time.Now(), Heartbeat: time.Minute, Log: log, Sites: siteMgr}
	ts := httptest.NewServer(s.Handler())
	t.Cleanup(ts.Close)

	tests := []struct {
		name     string
		endpoint string
		siteName string
		wantMsg  string
	}{
		{name: "link an invalid name", endpoint: "link", siteName: "my_shop", wantMsg: "letters, digits and hyphens"},
		{name: "link a 64-character name", endpoint: "link", siteName: strings.Repeat("a", 64), wantMsg: "63 characters or fewer"},
		{name: "proxy an invalid name", endpoint: "proxy", siteName: "my_shop", wantMsg: "letters, digits and hyphens"},
		{name: "proxy a 64-character name", endpoint: "proxy", siteName: strings.Repeat("a", 64), wantMsg: "63 characters or fewer"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body, _ := json.Marshal(map[string]string{"path": root, "name": tt.siteName, "target": "3000"})
			resp := authedJSON(t, "POST", ts.URL+"/api/sites/"+tt.endpoint, string(body))
			msg, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			if resp.StatusCode != http.StatusBadRequest {
				t.Fatalf("status %d (%s), want 400", resp.StatusCode, msg)
			}
			if !strings.Contains(string(msg), tt.wantMsg) {
				t.Errorf("message %q doesn't contain %q", msg, tt.wantMsg)
			}
		})
	}
}

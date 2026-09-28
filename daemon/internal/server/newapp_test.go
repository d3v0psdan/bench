package server

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/d3v0psdan/bench/daemon/internal/newapp"
	"github.com/d3v0psdan/bench/daemon/internal/registry"
	"github.com/d3v0psdan/bench/daemon/internal/sites"
)

func TestNewSiteRejectsBadRequestsBeforeStarting(t *testing.T) {
	root := t.TempDir()
	reg, err := registry.Open(filepath.Join(root, "bench.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { reg.Close() })
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	siteMgr := &sites.Manager{Reg: reg, Router: nopRouter{}, Log: log}
	s := &Server{
		Token: testToken, StartedAt: time.Now(), Heartbeat: time.Minute, Log: log, Sites: siteMgr,
		NewApp: &newapp.Creator{Root: root, Sites: siteMgr, Log: log},
	}
	projects := filepath.Join(root, "projects")
	if err := os.MkdirAll(filepath.Join(projects, "taken"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := reg.SetSetting(newapp.SettingProjectsDir, projects); err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(s.Handler())
	t.Cleanup(ts.Close)

	valid := `"starter_kit":true,"stack":"react","auth":"laravel","testing":"pest","database":"sqlite","package_manager":"npm"`
	for name, body := range map[string]string{
		"bad name":        `{"name":"-shop",` + valid + `}`,
		"blade kit":       `{"name":"shop","starter_kit":true,"stack":"blade","auth":"laravel","testing":"pest","database":"sqlite"}`,
		"sql server":      `{"name":"shop","stack":"blade","testing":"pest","database":"sqlsrv"}`,
		"folder exists":   `{"name":"taken",` + valid + `}`,
		"not json at all": `{bad`,
	} {
		resp := authedJSON(t, "POST", ts.URL+"/api/sites/new", body)
		msg, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("%s: status %d (%s), want 400", name, resp.StatusCode, msg)
		}
	}
	resp := authedJSON(t, "GET", ts.URL+"/api/tools", "")
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("GET /api/tools: %d", resp.StatusCode)
	}
	resp = authedJSON(t, "GET", ts.URL+"/api/tasks/nope/log", "")
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("GET /api/tasks/nope/log: %d, want 404", resp.StatusCode)
	}
}

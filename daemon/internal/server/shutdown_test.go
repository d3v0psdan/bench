package server

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/d3v0psdan/bench/daemon/internal/newapp"
	"github.com/d3v0psdan/bench/daemon/internal/registry"
	"github.com/d3v0psdan/bench/daemon/internal/services"
	"github.com/d3v0psdan/bench/daemon/internal/sitedelete"
	"github.com/d3v0psdan/bench/daemon/internal/sites"
	"github.com/d3v0psdan/bench/daemon/internal/tasks"
)

// jobServer serves New Laravel app and site delete over one proxy site,
// api.test, with a Go that accepts work or refuses it as after shutdown.
func jobServer(t *testing.T, accept bool) *httptest.Server {
	t.Helper()
	root := t.TempDir()
	reg, err := registry.Open(filepath.Join(root, "bench.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { reg.Close() })
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	siteMgr := &sites.Manager{Reg: reg, Router: nopRouter{}, Log: log, InstalledPHP: installedPHP("8.4")}
	if _, err := siteMgr.Proxy(t.Context(), "api", "127.0.0.1:3000"); err != nil {
		t.Fatal(err)
	}
	if err := reg.SetSetting(newapp.SettingProjectsDir, filepath.Join(root, "projects")); err != nil {
		t.Fatal(err)
	}
	taskReg := &tasks.Registry{}
	goFn := func(func()) bool { return accept } // never runs the job here
	s := &Server{
		Token: testToken, StartedAt: time.Now(), Heartbeat: time.Minute, Log: log, Sites: siteMgr, Tasks: taskReg,
		NewApp:     &newapp.Creator{Root: root, Sites: siteMgr, Tasks: taskReg, Log: log, Go: goFn},
		SiteDelete: &sitedelete.Deleter{Root: root, Sites: siteMgr, Services: &services.Manager{Reg: reg, Log: log}, Tasks: taskReg, Log: log, Go: goFn},
	}
	ts := httptest.NewServer(s.Handler())
	t.Cleanup(ts.Close)
	return ts
}

func wantStatus(t *testing.T, resp *http.Response, status int, mentions string) {
	t.Helper()
	msg, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != status || !strings.Contains(string(msg), mentions) {
		t.Fatalf("status %d (%s), want %d mentioning %q", resp.StatusCode, msg, status, mentions)
	}
}

const sqliteApp = `"stack":"blade","testing":"pest","database":"sqlite"`

func TestNewSiteDuringShutdownAnswers503(t *testing.T) {
	ts := jobServer(t, false)
	resp := authedJSON(t, "POST", ts.URL+"/api/sites/new", `{"name":"shop",`+sqliteApp+`}`)
	wantStatus(t, resp, http.StatusServiceUnavailable, "shutting down")
}

func TestSiteDeleteDuringShutdownAnswers503(t *testing.T) {
	ts := jobServer(t, false)
	resp := authedJSON(t, "POST", ts.URL+"/api/sites/api/delete", `{}`)
	wantStatus(t, resp, http.StatusServiceUnavailable, "shutting down")
}

func TestNewSiteRefusesANameLongerThan63Characters(t *testing.T) {
	ts := jobServer(t, true)
	resp := authedJSON(t, "POST", ts.URL+"/api/sites/new", `{"name":"`+strings.Repeat("a", 64)+`",`+sqliteApp+`}`)
	wantStatus(t, resp, http.StatusBadRequest, "too long")
}

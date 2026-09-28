package server

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/d3v0psdan/bench/daemon/internal/api"
	"github.com/d3v0psdan/bench/daemon/internal/registry"
	"github.com/d3v0psdan/bench/daemon/internal/sites"
)

func TestUpdatesAskGitHubOnceADayAndNeverWhenOff(t *testing.T) {
	var hits atomic.Int32
	gh := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.Write([]byte(`{"tag_name":"v0.2.0","html_url":"https://example.test/r","published_at":"2026-10-01T00:00:00Z"}`))
	}))
	defer gh.Close()
	reg, err := registry.Open(filepath.Join(t.TempDir(), "bench.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { reg.Close() })
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	s := &Server{
		Token: testToken, StartedAt: time.Now(), Heartbeat: time.Minute, Log: log, Version: "0.1.0",
		Sites: &sites.Manager{Reg: reg, Router: nopRouter{}, Log: log}, UpdatesURL: gh.URL,
	}
	ts := httptest.NewServer(s.Handler())
	t.Cleanup(ts.Close)
	get := func(path string) api.UpdateInfo {
		resp := authedJSON(t, "GET", ts.URL+path, "")
		defer resp.Body.Close()
		var info api.UpdateInfo
		if err := json.NewDecoder(resp.Body).Decode(&info); err != nil {
			t.Fatal(err)
		}
		return info
	}

	if info := get("/api/updates"); !info.Newer || info.Latest != "0.2.0" || hits.Load() != 1 {
		t.Fatalf("first check: %+v, %d hits", info, hits.Load())
	}
	get("/api/updates")
	if hits.Load() != 1 {
		t.Fatalf("a second check within the day asked GitHub again (%d hits)", hits.Load())
	}
	if err := reg.SetSetting(settingUpdatesCheck, "off"); err != nil {
		t.Fatal(err)
	}
	if err := reg.SetSetting(settingUpdatesCache, ""); err != nil {
		t.Fatal(err)
	}
	if info := get("/api/updates"); info.Enabled || hits.Load() != 1 {
		t.Fatalf("checks off still asked GitHub: %+v, %d hits", info, hits.Load())
	}
	get("/api/updates?force=1")
	if hits.Load() != 2 {
		t.Fatalf("Check now didn't ask GitHub (%d hits)", hits.Load())
	}
}

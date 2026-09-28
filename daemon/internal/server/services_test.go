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

	"github.com/d3v0psdan/bench/daemon/internal/binman"
	"github.com/d3v0psdan/bench/daemon/internal/registry"
	"github.com/d3v0psdan/bench/daemon/internal/services"
	"github.com/d3v0psdan/bench/daemon/internal/supervisor"
)

func newServicesServer(t *testing.T) *httptest.Server {
	t.Helper()
	root := t.TempDir()
	reg, err := registry.Open(filepath.Join(root, "bench.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { reg.Close() })
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	s := &Server{
		Token: testToken, StartedAt: time.Now(), Log: log,
		Services: &services.Manager{Root: root, Reg: reg, Log: log, Sup: supervisor.New(log),
			Bin: &binman.Manager{Root: root, Log: log, Manifests: map[string]binman.Manifest{}}},
	}
	ts := httptest.NewServer(s.Handler())
	t.Cleanup(ts.Close)
	return ts
}

func TestServiceRoutesMapErrors(t *testing.T) {
	ts := newServicesServer(t)
	for _, c := range []struct {
		method, path, body string
		want               int
	}{
		{"GET", "/api/services", "", http.StatusOK},
		{"GET", "/api/services/catalog", "", http.StatusOK},
		{"POST", "/api/services", `{"service":"oracle"}`, http.StatusBadRequest},
		{"POST", "/api/services", `{bad json`, http.StatusBadRequest},
		{"POST", "/api/services/nope/start", "", http.StatusNotFound},
		{"POST", "/api/services/nope/stop", "", http.StatusNotFound},
		{"POST", "/api/services/nope/clone", `{"name":"copy"}`, http.StatusNotFound},
		{"DELETE", "/api/services/nope", "", http.StatusNotFound},
		{"GET", "/api/services/nope/logs", "", http.StatusNotFound},
		{"GET", "/api/services/nope/logs/follow", "", http.StatusNotFound},
		{"POST", "/api/services/nope/open", "", http.StatusNotFound},
	} {
		req, _ := http.NewRequest(c.method, ts.URL+c.path, strings.NewReader(c.body))
		req.Header.Set("Authorization", "Bearer "+testToken)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode != c.want {
			t.Errorf("%s %s: status %d (%s), want %d", c.method, c.path, resp.StatusCode, body, c.want)
		}
		if c.path == "/api/services" && c.method == "GET" && strings.TrimSpace(string(body)) != "[]" {
			t.Errorf("empty list must be [], got %s", body)
		}
	}
}

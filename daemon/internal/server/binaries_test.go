package server

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"

	apipkg "github.com/d3v0psdan/bench/daemon/internal/api"
	"github.com/d3v0psdan/bench/daemon/internal/binman"
)

// newBinariesServer returns an API test server whose binary manager can
// install one fake build ("tool" 1.0.0) from a local artifact server.
func newBinariesServer(t *testing.T) *httptest.Server {
	t.Helper()

	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	f, err := zw.Create("tool.exe")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.Write([]byte("bits")); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	body := buf.Bytes()
	sum := sha256.Sum256(body)

	artifacts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(body)
	}))
	t.Cleanup(artifacts.Close)

	mgr := &binman.Manager{
		Root: t.TempDir(),
		Log:  slog.New(slog.NewTextHandler(io.Discard, nil)),
		Manifests: map[string]binman.Manifest{
			"tool": {Schema: 1, Name: "tool", Builds: []binman.Build{{
				Name: "tool", Version: "1.0.0", Channel: "1",
				OS: runtime.GOOS, Arch: runtime.GOARCH,
				Downloads: []binman.Download{{URL: artifacts.URL, SHA256: hex.EncodeToString(sum[:]), Archive: "zip"}},
			}}},
		},
	}
	s := &Server{
		Version:   "test",
		Token:     testToken,
		StartedAt: time.Now(),
		Log:       slog.New(slog.NewTextHandler(io.Discard, nil)),
		Heartbeat: time.Minute, // only explicit events matter here
		Binaries:  mgr,
	}
	mgr.OnProgress = func(p binman.Progress) {
		s.Publish(apipkg.Event{Type: "download", Download: &p})
	}
	ts := httptest.NewServer(s.Handler())
	t.Cleanup(ts.Close)
	return ts
}

func authedJSON(t *testing.T, method, url string, body string) *http.Response {
	t.Helper()
	req, err := http.NewRequest(method, url, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+testToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

func TestBinariesListAndInstall(t *testing.T) {
	ts := newBinariesServer(t)

	// Subscribe to events before installing so progress is observable.
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http") + "/api/events?token=" + testToken
	conn, _, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.CloseNow()

	resp := authedJSON(t, "GET", ts.URL+"/api/binaries", "")
	var list []apipkg.BinaryInfo
	if err := json.NewDecoder(resp.Body).Decode(&list); err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if len(list) != 1 || list[0].Installed || list[0].Channel != "1" {
		t.Fatalf("list = %+v", list)
	}

	resp = authedJSON(t, "POST", ts.URL+"/api/binaries/install", `{"name":"tool","channel":"1"}`)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("install status = %d", resp.StatusCode)
	}
	var info apipkg.BinaryInfo
	if err := json.NewDecoder(resp.Body).Decode(&info); err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if !info.Installed || info.Version != "1.0.0" {
		t.Fatalf("install response = %+v", info)
	}

	// The WS stream must have carried download events ending in "done".
	sawDone := false
	for !sawDone {
		var ev apipkg.Event
		if err := wsjson.Read(ctx, conn, &ev); err != nil {
			t.Fatalf("waiting for download events: %v", err)
		}
		if ev.Type == "download" && ev.Download != nil && ev.Download.Phase == "done" {
			sawDone = true
		}
	}

	resp = authedJSON(t, "GET", ts.URL+"/api/binaries", "")
	list = nil
	if err := json.NewDecoder(resp.Body).Decode(&list); err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if len(list) != 1 || !list[0].Installed {
		t.Fatalf("list after install = %+v", list)
	}
}

func TestBinariesInstallValidation(t *testing.T) {
	ts := newBinariesServer(t)
	for name, body := range map[string]string{
		"empty":   `{}`,
		"unknown": `{"name":"nope","channel":"1"}`,
		"garbage": `{`,
	} {
		resp := authedJSON(t, "POST", ts.URL+"/api/binaries/install", body)
		resp.Body.Close()
		if resp.StatusCode < 400 {
			t.Errorf("%s: status = %d, want error", name, resp.StatusCode)
		}
	}
}

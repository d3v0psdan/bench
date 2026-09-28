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
	"testing"
	"time"

	apipkg "github.com/d3v0psdan/bench/daemon/internal/api"
	"github.com/d3v0psdan/bench/daemon/internal/binman"
	"github.com/d3v0psdan/bench/daemon/internal/tasks"
)

func TestTaskRoutesMapErrors(t *testing.T) {
	s := &Server{Token: testToken, Log: slog.New(slog.NewTextHandler(io.Discard, nil)), Tasks: &tasks.Registry{}}
	clone := s.Tasks.Begin("service.clone", "Cloning", "db", false)
	defer clone.End(nil)
	ts := httptest.NewServer(s.Handler())
	t.Cleanup(ts.Close)

	for _, tc := range []struct {
		method, path string
		want         int
	}{
		{"GET", "/api/tasks", http.StatusOK},
		{"POST", "/api/tasks/t404/cancel", http.StatusNotFound},
		{"POST", "/api/tasks/t1/cancel", http.StatusConflict}, // a clone can't be cancelled
	} {
		resp := authedJSON(t, tc.method, ts.URL+tc.path, "")
		resp.Body.Close()
		if resp.StatusCode != tc.want {
			t.Errorf("%s %s = %d, want %d", tc.method, tc.path, resp.StatusCode, tc.want)
		}
	}
}

// TestCancelInstallThroughTheAPI drives the Activity flow end to end: an
// install shows up as a running, cancellable task with live progress;
// cancelling it through the API aborts the download, the install request
// answers 409 "cancelled", and nothing is installed.
func TestCancelInstallThroughTheAPI(t *testing.T) {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	f, _ := zw.Create("tool.exe")
	_, _ = f.Write([]byte("bits"))
	_ = zw.Close()
	body := buf.Bytes()
	sum := sha256.Sum256(body)

	aborted := make(chan struct{})
	artifacts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(body[:1])
		w.(http.Flusher).Flush()
		<-r.Context().Done() // never finishes on its own
		close(aborted)
	}))
	t.Cleanup(artifacts.Close)

	reg := &tasks.Registry{}
	mgr := &binman.Manager{
		Root: t.TempDir(),
		Log:  slog.New(slog.NewTextHandler(io.Discard, nil)),
		Manifests: map[string]binman.Manifest{"tool": {Schema: 1, Name: "tool", Builds: []binman.Build{{
			Name: "tool", Version: "1.0.0", Channel: "1", OS: runtime.GOOS, Arch: runtime.GOARCH,
			Downloads: []binman.Download{{URL: artifacts.URL, SHA256: hex.EncodeToString(sum[:]), Archive: "zip"}},
		}}}},
		OnProgress: reg.Progress,
	}
	s := &Server{Token: testToken, Log: slog.New(slog.NewTextHandler(io.Discard, nil)), Binaries: mgr, Tasks: reg}
	ts := httptest.NewServer(s.Handler())
	t.Cleanup(ts.Close)

	installed := make(chan *http.Response, 1)
	go func() {
		installed <- authedJSON(t, "POST", ts.URL+"/api/binaries/install", `{"name":"tool","channel":"1"}`)
	}()

	var task apipkg.Task
	deadline := time.Now().Add(5 * time.Second)
	for {
		resp := authedJSON(t, "GET", ts.URL+"/api/tasks", "")
		var list []apipkg.Task
		_ = json.NewDecoder(resp.Body).Decode(&list)
		resp.Body.Close()
		if len(list) == 1 && list[0].Download != nil {
			task = list[0]
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("no running install task with progress, got %+v", list)
		}
		time.Sleep(20 * time.Millisecond)
	}
	if task.Kind != "binary.install" || task.State != apipkg.TaskRunning || !task.Cancellable {
		t.Fatalf("task = %+v", task)
	}

	resp := authedJSON(t, "POST", ts.URL+"/api/tasks/"+task.ID+"/cancel", "")
	resp.Body.Close()
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("cancel = %d, want 202", resp.StatusCode)
	}
	select {
	case resp := <-installed:
		msg, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode != http.StatusConflict || !bytes.Contains(msg, []byte("cancelled")) {
			t.Fatalf("install answered %d %q, want 409 cancelled", resp.StatusCode, msg)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("install request didn't return after cancel")
	}
	select {
	case <-aborted:
	case <-time.After(5 * time.Second):
		t.Fatal("the download kept running after cancel")
	}
	if mgr.IsInstalled("tool", "1.0.0") {
		t.Fatal("a cancelled install must not be installed")
	}
	if got := reg.List()[0].State; got != apipkg.TaskCancelled {
		t.Fatalf("task state = %q, want cancelled", got)
	}
}

func TestSetupUndoRunsTheTeardownAsATask(t *testing.T) {
	reg := &tasks.Registry{}
	s := &Server{Token: testToken, Log: slog.New(slog.NewTextHandler(io.Discard, nil)), Tasks: reg}
	ts := httptest.NewServer(s.Handler())
	t.Cleanup(ts.Close)

	resp := authedJSON(t, "POST", ts.URL+"/api/setup/undo", "")
	resp.Body.Close()
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("without a teardown = %d, want 503", resp.StatusCode)
	}

	var gotDNS, gotTrust bool
	s.Teardown = func(_ context.Context, dns, trust bool) ([]string, error) {
		gotDNS, gotTrust = dns, trust
		return []string{"*.test no longer routes to Bench"}, nil
	}
	resp = authedJSON(t, "POST", ts.URL+"/api/setup/undo", `{"dns":false,"trust":false}`)
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("undo with nothing to remove = %d, want 400", resp.StatusCode)
	}
	resp = authedJSON(t, "POST", ts.URL+"/api/setup/undo", `{"dns":true,"trust":false}`)
	var out apipkg.SetupResponse
	_ = json.NewDecoder(resp.Body).Decode(&out)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK || len(out.Messages) != 1 {
		t.Fatalf("undo = %d %+v", resp.StatusCode, out)
	}
	if !gotDNS || gotTrust {
		t.Fatalf("teardown got dns=%v trust=%v, want only the DNS part", gotDNS, gotTrust)
	}
	task := reg.List()[0]
	if task.Kind != "setup" || task.State != apipkg.TaskDone || task.Cancellable {
		t.Fatalf("task = %+v, want a finished, never-cancellable setup task", task)
	}
}

package services

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/d3v0psdan/bench/daemon/internal/api"
	"github.com/d3v0psdan/bench/daemon/internal/binman"
	"github.com/d3v0psdan/bench/daemon/internal/tasks"
)

// TestCancelCreateWhileDownloading: a create cancelled during its download
// stops the download and leaves no trace: no pending row, no data dir, no
// installed build, and the task reads "cancelled".
func TestCancelCreateWhileDownloading(t *testing.T) {
	m := newTestManager(t, fakeDriver(freePort(t)))
	if err := os.RemoveAll(m.Bin.Dir("fake", "1.2.3")); err != nil {
		t.Fatal(err)
	}
	started := make(chan struct{})
	artifacts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("P"))
		w.(http.Flusher).Flush()
		close(started)
		<-r.Context().Done() // never finishes on its own
	}))
	t.Cleanup(artifacts.Close)
	m.Bin.Manifests["fake"] = binman.Manifest{Schema: 1, Name: "fake", Builds: []binman.Build{{
		Name: "fake", Version: "1.2.3", Channel: "1", OS: runtime.GOOS, Arch: runtime.GOARCH,
		Downloads: []binman.Download{{URL: artifacts.URL, SHA256: "00", Archive: "zip"}},
	}}}
	m.Tasks = &tasks.Registry{}
	m.Bin.OnProgress = m.Tasks.Progress

	errc := make(chan error, 1)
	go func() {
		_, err := m.Create(api.CreateServiceRequest{Service: "fake", Name: "app-db"})
		errc <- err
	}()
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("download never started")
	}
	task := m.Tasks.List()[0]
	if task.Kind != "service.create" || task.Phase != "downloading" || !task.Cancellable {
		t.Fatalf("task while downloading = %+v", task)
	}
	if err := m.Tasks.Cancel(task.ID); err != nil {
		t.Fatalf("Cancel: %v", err)
	}

	select {
	case err := <-errc:
		if !errors.Is(err, tasks.ErrCancelled) {
			t.Fatalf("Create = %v, want tasks.ErrCancelled", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Create didn't return after cancel")
	}
	if got := m.Tasks.List()[0].State; got != api.TaskCancelled {
		t.Fatalf("task state = %q, want cancelled", got)
	}
	list, err := m.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 0 {
		t.Fatalf("a cancelled create left instances behind: %+v", list)
	}
	if _, err := os.Stat(filepath.Join(m.Root, "data", "fake", "app-db")); !os.IsNotExist(err) {
		t.Fatalf("a cancelled create left a data dir: %v", err)
	}
	if m.Bin.IsInstalled("fake", "1.2.3") {
		t.Fatal("a cancelled create must not install the build")
	}
}

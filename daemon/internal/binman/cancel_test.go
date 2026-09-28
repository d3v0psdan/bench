package binman

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

// newGatedManager serves one artifact whose response starts, then waits
// for release (or for the client to go away) before sending body. started
// is closed on the first request; aborted is closed if the client
// disconnects before release.
func newGatedManager(t *testing.T, body []byte) (m *Manager, started, release, aborted chan struct{}) {
	t.Helper()
	started, release, aborted = make(chan struct{}), make(chan struct{}), make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(body[:1]) // a first chunk, so the download is under way
		w.(http.Flusher).Flush()
		close(started)
		select {
		case <-release:
			_, _ = w.Write(body[1:])
		case <-r.Context().Done():
			close(aborted)
		}
	}))
	t.Cleanup(srv.Close)
	m = &Manager{
		Root: t.TempDir(),
		Log:  slog.New(slog.NewTextHandler(io.Discard, nil)),
		Manifests: map[string]Manifest{"tool": {Schema: 1, Name: "tool", Builds: []Build{{
			Name: "tool", Version: "1.0.0", Channel: "1", OS: runtime.GOOS, Arch: runtime.GOARCH,
			Downloads: []Download{{URL: srv.URL + "/a", SHA256: sha256hex(body), Archive: "zip"}},
		}}}},
	}
	return m, started, release, aborted
}

func waitFor(t *testing.T, ch <-chan struct{}, what string) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(5 * time.Second):
		t.Fatalf("timed out waiting for %s", what)
	}
}

func TestCancelStopsTheDownloadAndCleansUp(t *testing.T) {
	m, started, release, aborted := newGatedManager(t, buildZip(t, map[string]string{"tool.exe": "x"}))
	defer close(release)

	ctx, cancel := context.WithCancel(context.Background())
	errc := make(chan error, 1)
	go func() {
		_, err := m.Install(ctx, "tool", "1")
		errc <- err
	}()
	waitFor(t, started, "the download to start")
	cancel()

	select {
	case err := <-errc:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("Install = %v, want context.Canceled", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Install didn't return after cancel")
	}
	waitFor(t, aborted, "the HTTP download to be aborted")

	// The staging dir goes away once the flight unwinds.
	deadline := time.Now().Add(5 * time.Second)
	for {
		entries, _ := os.ReadDir(filepath.Join(m.Root, "tmp"))
		if len(entries) == 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("staging left behind: %v", entries)
		}
		time.Sleep(20 * time.Millisecond)
	}
	if m.IsInstalled("tool", "1.0.0") {
		t.Fatal("a cancelled install must not be marked installed")
	}
}

func TestOneCallerLeavingKeepsTheSharedDownload(t *testing.T) {
	m, started, release, aborted := newGatedManager(t, buildZip(t, map[string]string{"tool.exe": "x"}))

	leaving, leave := context.WithCancel(context.Background())
	leftErr := make(chan error, 1)
	stayedErr := make(chan error, 1)
	go func() {
		_, err := m.Install(leaving, "tool", "1")
		leftErr <- err
	}()
	waitFor(t, started, "the download to start")
	go func() {
		_, err := m.Install(context.Background(), "tool", "1")
		stayedErr <- err
	}()
	// Let the second caller join the flight before the first one leaves.
	deadline := time.Now().Add(5 * time.Second)
	for {
		m.mu.Lock()
		f := m.inflight["tool/1.0.0"]
		joined := f != nil && f.waiters == 2
		m.mu.Unlock()
		if joined {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("second caller never joined the flight")
		}
		time.Sleep(10 * time.Millisecond)
	}

	leave()
	if err := <-leftErr; !errors.Is(err, context.Canceled) {
		t.Fatalf("leaving caller got %v, want context.Canceled", err)
	}
	select {
	case <-aborted:
		t.Fatal("the download was aborted while another caller still waited")
	case <-time.After(100 * time.Millisecond):
	}
	close(release)
	if err := <-stayedErr; err != nil {
		t.Fatalf("remaining caller: %v", err)
	}
	if !m.IsInstalled("tool", "1.0.0") {
		t.Fatal("the shared download should have installed")
	}
}

// TestCancelAfterTheFlightFinishedReportsItsOutcome: when a caller's ctx
// ends just as the shared download finishes, both select cases are ready
// and Go picks one at random. The caller must still get the flight's real
// outcome, so a task never reads "cancelled" for a build that installed.
func TestCancelAfterTheFlightFinishedReportsItsOutcome(t *testing.T) {
	m, _, release, _ := newGatedManager(t, buildZip(t, map[string]string{"tool.exe": "x"}))
	close(release)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	for range 100 {
		done := make(chan struct{})
		close(done)
		m.mu.Lock()
		m.inflight = map[string]*flight{"tool/1.0.0": {done: done, cancel: func() {}}}
		m.mu.Unlock()
		if _, err := m.Install(ctx, "tool", "1"); err != nil {
			t.Fatalf("Install = %v, want the finished flight's nil error", err)
		}
	}
}

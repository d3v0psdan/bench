package binman

import (
	"context"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/hex"
	"errors"
	"fmt"
	"hash"
	"io"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// Progress reports install progress for one build. Received/Total are for
// the current file (vendors don't publish total sizes up front).
type Progress struct {
	Name     string `json:"name"`
	Version  string `json:"version"`
	Phase    string `json:"phase"` // downloading | extracting | done | error
	File     int    `json:"file"`  // 1-based index of the current download
	Files    int    `json:"files"`
	Received int64  `json:"received"`
	Total    int64  `json:"total"` // 0 = unknown
	Error    string `json:"error,omitempty"`
}

// Manager installs catalog builds under Root/bin/<name>/<version>/.
// A version directory's existence is the installed marker: extraction
// happens in a staging dir that is renamed into place only after every
// download verified.
type Manager struct {
	Root      string
	Log       *slog.Logger
	Manifests map[string]Manifest
	// OnProgress, if set, receives throttled progress updates (feeds the
	// WS event stream). Called from installing goroutines; must not block.
	OnProgress func(Progress)

	client   *http.Client
	mu       sync.Mutex
	inflight map[string]*flight
}

// flight is one shared download. It runs detached from any single caller
// and is cancelled only when every caller waiting on it has gone, so one
// cancelled create can't kill a download another caller still needs.
type flight struct {
	done    chan struct{}
	err     error
	waiters int // guarded by Manager.mu
	cancel  context.CancelFunc
}

// Dir is where a build lives (or would live) on disk.
func (m *Manager) Dir(name, version string) string {
	return filepath.Join(m.Root, "bin", name, version)
}

// IsInstalled reports whether the build's directory exists.
func (m *Manager) IsInstalled(name, version string) bool {
	fi, err := os.Stat(m.Dir(name, version))
	return err == nil && fi.IsDir()
}

// DiskSize is the total size in bytes of an installed build's files (0 when
// not installed). Unreadable entries are skipped: it's for display.
func (m *Manager) DiskSize(name, version string) int64 {
	var total int64
	_ = filepath.WalkDir(m.Dir(name, version), func(_ string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		if info, err := d.Info(); err == nil {
			total += info.Size()
		}
		return nil
	})
	return total
}

// Installed lists installed versions of name, sorted.
func (m *Manager) Installed(name string) []string {
	entries, err := os.ReadDir(filepath.Join(m.Root, "bin", name))
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range entries {
		if e.IsDir() {
			out = append(out, e.Name())
		}
	}
	sort.Strings(out)
	return out
}

// Install resolves channel and installs the build if missing. Concurrent
// installs of the same build coalesce into one download, which stops only
// when every caller's ctx has ended (a cancelled task, benchd shutting
// down); the partial download is then removed.
func (m *Manager) Install(ctx context.Context, name, channel string) (Build, error) {
	b, err := m.Resolve(name, channel)
	if err != nil {
		return Build{}, err
	}
	key := name + "/" + b.Version

	m.mu.Lock()
	if m.IsInstalled(name, b.Version) {
		m.mu.Unlock()
		return b, nil
	}
	f, ok := m.inflight[key]
	if !ok {
		fctx, cancel := context.WithCancel(context.WithoutCancel(ctx))
		f = &flight{done: make(chan struct{}), cancel: cancel}
		if m.inflight == nil {
			m.inflight = map[string]*flight{}
		}
		m.inflight[key] = f
		go m.run(fctx, key, f, b)
	}
	f.waiters++
	m.mu.Unlock()

	select {
	case <-f.done:
		return b, f.err
	case <-ctx.Done():
		m.mu.Lock()
		// select picks at random when both are ready: a flight that already
		// finished reports its real outcome, not the cancel.
		select {
		case <-f.done:
			m.mu.Unlock()
			return b, f.err
		default:
		}
		f.waiters--
		last := f.waiters == 0
		if last {
			f.cancel()
			// A caller arriving now must start a fresh download rather than
			// join this cancelled one.
			if m.inflight[key] == f {
				delete(m.inflight, key)
			}
		}
		m.mu.Unlock()
		if last {
			// Wait for the cancelled flight to unwind (a download stops at
			// once; an extraction finishes its archive first), so the answer
			// is the truth: it may have installed before the cancel landed.
			<-f.done
			if f.err == nil {
				return b, nil
			}
		}
		return b, ctx.Err()
	}
}

// Uninstall removes an installed build. The directory is first moved into
// tmp in one rename, so a build still in use (Windows locks running
// executables) fails whole instead of being left half-deleted. Removing a
// build that isn't installed is a no-op.
func (m *Manager) Uninstall(name, version string) error {
	dir := m.Dir(name, version)
	tmpRoot := filepath.Join(m.Root, "tmp")
	if err := os.MkdirAll(tmpRoot, 0o755); err != nil {
		return fmt.Errorf("creating tmp dir: %w", err)
	}
	trash, err := os.MkdirTemp(tmpRoot, "remove-"+name+"-")
	if err != nil {
		return fmt.Errorf("creating tmp dir: %w", err)
	}
	defer func() {
		if err := os.RemoveAll(trash); err != nil {
			m.Log.Warn("deleting an uninstalled build failed; its files stay in tmp", "dir", trash, "err", err)
		}
	}()

	// Under mu so an install can't start between the check and the move.
	m.mu.Lock()
	if _, busy := m.inflight[name+"/"+version]; busy {
		m.mu.Unlock()
		return fmt.Errorf("%s %s is still installing; cancel it in Activity first", name, version)
	}
	err = os.Rename(dir, filepath.Join(trash, "build"))
	m.mu.Unlock()
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		// Windows won't move a folder while a program runs from it.
		return fmt.Errorf("%s %s is in use by another program (a terminal or editor running it); close it and try again (%w)",
			name, version, err)
	}
	m.Log.Info("uninstalled binary", "name", name, "version", version, "dir", dir)
	return nil
}

// run performs one flight's install and releases its waiters.
func (m *Manager) run(ctx context.Context, key string, f *flight, b Build) {
	f.err = m.install(ctx, b)
	m.mu.Lock()
	if m.inflight[key] == f {
		delete(m.inflight, key)
	}
	m.mu.Unlock()
	f.cancel()
	close(f.done)
}

func (m *Manager) emit(p Progress) {
	if m.OnProgress != nil {
		m.OnProgress(p)
	}
}

func (m *Manager) install(ctx context.Context, b Build) (err error) {
	defer func() {
		if err != nil {
			m.emit(Progress{Name: b.Name, Version: b.Version, Phase: "error", Error: err.Error()})
		}
	}()
	if len(b.Downloads) == 0 {
		return fmt.Errorf("%s %s has no downloads in the catalog", b.Name, b.Version)
	}

	tmpRoot := filepath.Join(m.Root, "tmp")
	if err := os.MkdirAll(tmpRoot, 0o755); err != nil {
		return fmt.Errorf("creating tmp dir: %w", err)
	}
	stage, err := os.MkdirTemp(tmpRoot, b.Name+"-")
	if err != nil {
		return fmt.Errorf("creating staging dir: %w", err)
	}
	defer os.RemoveAll(stage)
	out := filepath.Join(stage, "out")
	if err := os.MkdirAll(out, 0o755); err != nil {
		return fmt.Errorf("creating staging dir: %w", err)
	}

	for i, d := range b.Downloads {
		archive := filepath.Join(stage, fmt.Sprintf("dl-%d", i))
		if err := m.download(ctx, b, d, archive, i+1, len(b.Downloads)); err != nil {
			return err
		}
		m.emit(Progress{Name: b.Name, Version: b.Version, Phase: "extracting", File: i + 1, Files: len(b.Downloads)})
		if err := extract(d, archive, out); err != nil {
			return fmt.Errorf("extracting %s: %w", d.URL, err)
		}
		// The archive is not needed anymore; drop it early so big installs
		// don't hold 2× the size in tmp. Best effort.
		_ = os.Remove(archive)
	}

	// Extraction doesn't watch ctx; a cancel that landed during it must
	// still keep the build from being marked installed.
	if err := ctx.Err(); err != nil {
		return err
	}
	dest := m.Dir(b.Name, b.Version)
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return fmt.Errorf("creating install dir: %w", err)
	}
	if err := os.Rename(out, dest); err != nil {
		if m.IsInstalled(b.Name, b.Version) {
			// Lost a race with another daemon/process: same content.
			m.emit(Progress{Name: b.Name, Version: b.Version, Phase: "done"})
			return nil
		}
		return fmt.Errorf("moving %s into place: %w", dest, err)
	}
	m.Log.Info("installed binary", "name", b.Name, "version", b.Version, "dir", dest)
	m.emit(Progress{Name: b.Name, Version: b.Version, Phase: "done"})
	return nil
}

// download fetches d.URL into path, verifying its checksum. The file is
// removed on any failure: nothing unverified survives.
func (m *Manager) download(ctx context.Context, b Build, d Download, path string, file, files int) (err error) {
	h, want, err := checksum(d)
	if err != nil {
		return err
	}
	if err := checkURL(d.URL); err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, d.URL, nil)
	if err != nil {
		return fmt.Errorf("building request for %s: %w", d.URL, err)
	}
	resp, err := m.httpClient().Do(req)
	if err != nil {
		return fmt.Errorf("downloading %s: %w", d.URL, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("downloading %s: %s", d.URL, resp.Status)
	}

	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("creating download file: %w", err)
	}
	defer func() {
		f.Close()
		if err != nil {
			_ = os.Remove(path)
		}
	}()

	pw := &progressWriter{
		total: resp.ContentLength,
		emit: func(received, total int64) {
			m.emit(Progress{
				Name: b.Name, Version: b.Version, Phase: "downloading",
				File: file, Files: files, Received: received, Total: total,
			})
		},
	}
	pw.emit(0, resp.ContentLength) // downloading started, even if slow
	if _, err := io.Copy(io.MultiWriter(f, h, pw), resp.Body); err != nil {
		return fmt.Errorf("downloading %s: %w", d.URL, err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("writing download: %w", err)
	}

	got := hex.EncodeToString(h.Sum(nil))
	if !strings.EqualFold(got, want) {
		return fmt.Errorf("checksum mismatch for %s: manifest %s, got %s", d.URL, want, got)
	}
	pw.flush()
	return nil
}

// checksum picks the hash the manifest pinned for this download.
func checksum(d Download) (hash.Hash, string, error) {
	switch {
	case d.SHA256 != "" && d.SHA512 != "":
		return nil, "", fmt.Errorf("download %s pins both sha256 and sha512", d.URL)
	case d.SHA256 != "":
		return sha256.New(), d.SHA256, nil
	case d.SHA512 != "":
		return sha512.New(), d.SHA512, nil
	default:
		return nil, "", fmt.Errorf("download %s has no checksum pinned", d.URL)
	}
}

// checkURL enforces https for real downloads; plain http is allowed only
// on loopback (tests).
func checkURL(rawURL string) error {
	u, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("invalid download url %q: %w", rawURL, err)
	}
	switch u.Scheme {
	case "https":
		return nil
	case "http":
		host := u.Hostname()
		if host == "localhost" {
			return nil
		}
		if ip := net.ParseIP(host); ip != nil && ip.IsLoopback() {
			return nil
		}
	}
	return fmt.Errorf("refusing non-https download url %s", rawURL)
}

func (m *Manager) httpClient() *http.Client {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.client == nil {
		// No overall timeout: PHP downloads legitimately take minutes.
		// Cancellation comes from ctx; the transport bounds dial/TLS.
		m.client = &http.Client{}
	}
	return m.client
}

// progressWriter emits throttled progress as bytes flow through it.
type progressWriter struct {
	received int64
	total    int64
	last     time.Time
	emit     func(received, total int64)
}

func (w *progressWriter) Write(p []byte) (int, error) {
	w.received += int64(len(p))
	if time.Since(w.last) >= 100*time.Millisecond {
		w.flush()
	}
	return len(p), nil
}

func (w *progressWriter) flush() {
	w.last = time.Now()
	w.emit(w.received, w.total)
}

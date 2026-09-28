package binman

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/ulikunitz/xz"
)

func buildZip(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	for name, content := range files {
		f, err := w.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := f.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func buildTarGz(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for name, content := range files {
		if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0o755, Size: int64(len(content))}); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func sha256hex(b []byte) string {
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

// newTestManager serves the given artifact bodies over a local HTTP server
// and returns a manager whose manifest has one build ("tool" 1.0) with one
// download per artifact.
func newTestManager(t *testing.T, archive string, bodies ...[]byte) (*Manager, *atomic.Int64) {
	t.Helper()
	var requests atomic.Int64
	mux := http.NewServeMux()
	downloads := make([]Download, len(bodies))
	for i, body := range bodies {
		path := "/artifact-" + string(rune('a'+i))
		mux.HandleFunc(path, func(w http.ResponseWriter, r *http.Request) {
			requests.Add(1)
			_, _ = w.Write(body)
		})
		downloads[i] = Download{SHA256: sha256hex(body), Archive: archive, URL: path}
	}
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	for i := range downloads {
		downloads[i].URL = srv.URL + downloads[i].URL
	}
	m := &Manager{
		Root: t.TempDir(),
		Log:  slog.New(slog.NewTextHandler(io.Discard, nil)),
		Manifests: map[string]Manifest{
			"tool": {Schema: 1, Name: "tool", Builds: []Build{{
				Name: "tool", Version: "1.0.0", Channel: "1",
				OS: runtime.GOOS, Arch: runtime.GOARCH, Downloads: downloads,
			}}},
		},
	}
	return m, &requests
}

func TestInstallZip(t *testing.T) {
	body := buildZip(t, map[string]string{"tool.exe": "binary bits", "ext/mod.dll": "mod"})
	m, _ := newTestManager(t, "zip", body)

	var mu sync.Mutex
	var phases []string
	m.OnProgress = func(p Progress) {
		mu.Lock()
		phases = append(phases, p.Phase)
		mu.Unlock()
	}

	b, err := m.Install(context.Background(), "tool", "1")
	if err != nil {
		t.Fatalf("Install: %v", err)
	}
	if b.Version != "1.0.0" {
		t.Fatalf("resolved wrong build: %+v", b)
	}
	for _, f := range []string{"tool.exe", filepath.Join("ext", "mod.dll")} {
		if _, err := os.Stat(filepath.Join(m.Dir("tool", "1.0.0"), f)); err != nil {
			t.Fatalf("missing extracted file %s: %v", f, err)
		}
	}
	if !m.IsInstalled("tool", "1.0.0") {
		t.Fatal("IsInstalled = false after install")
	}
	if got := m.Installed("tool"); len(got) != 1 || got[0] != "1.0.0" {
		t.Fatalf("Installed = %v", got)
	}
	mu.Lock()
	defer mu.Unlock()
	joined := strings.Join(phases, ",")
	if !strings.Contains(joined, "downloading") || !strings.HasSuffix(joined, "done") {
		t.Fatalf("progress phases = %v", phases)
	}
	// Staging must be cleaned up.
	entries, _ := os.ReadDir(filepath.Join(m.Root, "tmp"))
	if len(entries) != 0 {
		t.Fatalf("staging leftovers: %v", entries)
	}
}

func TestInstallTarGzMultipleDownloads(t *testing.T) {
	cli := buildTarGz(t, map[string]string{"php": "cli binary"})
	fpm := buildTarGz(t, map[string]string{"php-fpm": "fpm binary"})
	m, requests := newTestManager(t, "tar.gz", cli, fpm)

	if _, err := m.Install(context.Background(), "tool", "1"); err != nil {
		t.Fatalf("Install: %v", err)
	}
	if requests.Load() != 2 {
		t.Fatalf("want 2 downloads, got %d", requests.Load())
	}
	for _, f := range []string{"php", "php-fpm"} {
		path := filepath.Join(m.Dir("tool", "1.0.0"), f)
		fi, err := os.Stat(path)
		if err != nil {
			t.Fatalf("missing %s: %v", f, err)
		}
		if runtime.GOOS != "windows" && fi.Mode().Perm()&0o100 == 0 {
			t.Fatalf("%s lost its exec bit: %v", f, fi.Mode())
		}
	}
}

func TestChecksumMismatchAbortsCleanly(t *testing.T) {
	body := buildZip(t, map[string]string{"tool.exe": "binary bits"})
	m, _ := newTestManager(t, "zip", body)
	man := m.Manifests["tool"]
	man.Builds[0].Downloads[0].SHA256 = strings.Repeat("ab", 32) // wrong
	m.Manifests["tool"] = man

	_, err := m.Install(context.Background(), "tool", "1")
	if err == nil || !strings.Contains(err.Error(), "checksum mismatch") {
		t.Fatalf("want checksum mismatch, got %v", err)
	}
	if m.IsInstalled("tool", "1.0.0") {
		t.Fatal("failed install must not leave the version dir")
	}
	entries, _ := os.ReadDir(filepath.Join(m.Root, "tmp"))
	if len(entries) != 0 {
		t.Fatalf("staging leftovers after failure: %v", entries)
	}
}

func TestMissingChecksumRefused(t *testing.T) {
	body := buildZip(t, map[string]string{"tool.exe": "x"})
	m, requests := newTestManager(t, "zip", body)
	man := m.Manifests["tool"]
	man.Builds[0].Downloads[0].SHA256 = ""
	m.Manifests["tool"] = man

	if _, err := m.Install(context.Background(), "tool", "1"); err == nil {
		t.Fatal("want error for missing checksum")
	}
	if requests.Load() != 0 {
		t.Fatal("must refuse before downloading anything")
	}
}

func TestZipSlipRejected(t *testing.T) {
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	f, err := w.CreateRaw(&zip.FileHeader{Name: "../evil.txt", Method: zip.Store})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.Write([]byte("evil")); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	m, _ := newTestManager(t, "zip", buf.Bytes())

	if _, err := m.Install(context.Background(), "tool", "1"); err == nil {
		t.Fatal("want error for traversal entry")
	}
	if _, err := os.Stat(filepath.Join(m.Root, "tmp", "evil.txt")); err == nil {
		t.Fatal("traversal entry escaped the staging dir")
	}
	if m.IsInstalled("tool", "1.0.0") {
		t.Fatal("malicious archive must not install")
	}
}

func TestTarSlipRejected(t *testing.T) {
	body := buildTarGz(t, map[string]string{"../evil.txt": "evil"})
	m, _ := newTestManager(t, "tar.gz", body)
	if _, err := m.Install(context.Background(), "tool", "1"); err == nil {
		t.Fatal("want error for traversal entry")
	}
	if m.IsInstalled("tool", "1.0.0") {
		t.Fatal("malicious archive must not install")
	}
}

func TestConcurrentInstallCoalesces(t *testing.T) {
	body := buildZip(t, map[string]string{"tool.exe": "x"})
	m, requests := newTestManager(t, "zip", body)

	var wg sync.WaitGroup
	errs := make([]error, 4)
	for i := range errs {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, errs[i] = m.Install(context.Background(), "tool", "1")
		}()
	}
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			t.Fatalf("Install %d: %v", i, err)
		}
	}
	if requests.Load() != 1 {
		t.Fatalf("want 1 coalesced download, got %d", requests.Load())
	}
}

func TestInstallIsIdempotent(t *testing.T) {
	body := buildZip(t, map[string]string{"tool.exe": "x"})
	m, requests := newTestManager(t, "zip", body)
	for range 2 {
		if _, err := m.Install(context.Background(), "tool", "1"); err != nil {
			t.Fatalf("Install: %v", err)
		}
	}
	if requests.Load() != 1 {
		t.Fatalf("second install must be a no-op, got %d downloads", requests.Load())
	}
}

func TestRefusesNonHTTPSURL(t *testing.T) {
	m := &Manager{
		Root: t.TempDir(),
		Log:  slog.New(slog.NewTextHandler(io.Discard, nil)),
		Manifests: map[string]Manifest{"tool": {Name: "tool", Builds: []Build{{
			Name: "tool", Version: "1.0.0", Channel: "1", OS: runtime.GOOS, Arch: runtime.GOARCH,
			Downloads: []Download{{URL: "http://example.com/x.zip", SHA256: strings.Repeat("ab", 32), Archive: "zip"}},
		}}}},
	}
	if _, err := m.Install(context.Background(), "tool", "1"); err == nil || !strings.Contains(err.Error(), "non-https") {
		t.Fatalf("want non-https refusal, got %v", err)
	}
}

func TestResolveUnknown(t *testing.T) {
	m := &Manager{Manifests: map[string]Manifest{}}
	if _, err := m.Resolve("php", "8.4"); err == nil {
		t.Fatal("want error for unknown manifest")
	}
}

// TestEmbeddedManifestsAreSane validates the real catalog data: parseable,
// https URLs, well-formed checksums, known archive formats, and the
// platform coverage Phase 1 promises.
func TestEmbeddedManifestsAreSane(t *testing.T) {
	ms, err := LoadEmbedded()
	if err != nil {
		t.Fatal(err)
	}
	hexRe := regexp.MustCompile(`^[0-9a-f]+$`)
	for name, man := range ms {
		if man.Schema != 1 || man.Name != name {
			t.Fatalf("manifest %s: bad schema/name: %+v", name, man)
		}
		for _, b := range man.Builds {
			id := name + " " + b.Version + " " + b.OS + "/" + b.Arch
			if b.Name != name || b.Channel == "" || b.OS == "" || b.Arch == "" || len(b.Downloads) == 0 {
				t.Fatalf("%s: incomplete build: %+v", id, b)
			}
			for _, d := range b.Downloads {
				if !strings.HasPrefix(d.URL, "https://") {
					t.Fatalf("%s: non-https url %s", id, d.URL)
				}
				if !slices.Contains([]string{"zip", "tar.gz", "tar.xz", "raw"}, d.Archive) || (d.Archive == "raw") != (d.File != "") {
					t.Fatalf("%s: unknown archive %q", id, d.Archive)
				}
				sum, wantLen := d.SHA256, 64
				if sum == "" {
					sum, wantLen = d.SHA512, 128
				}
				if len(sum) != wantLen || !hexRe.MatchString(sum) {
					t.Fatalf("%s: malformed checksum for %s", id, d.URL)
				}
			}
		}
	}

	// Phase 1 coverage: caddy on 5 platforms, php 8.3–8.5 on all of
	// windows/linux/darwin (fpm alongside cli off Windows).
	coverage := map[string]map[string]bool{} // name → "channel|os" seen
	for name, man := range ms {
		coverage[name] = map[string]bool{}
		for _, b := range man.Builds {
			coverage[name][b.Channel+"|"+b.OS] = true
			if name == "php" && b.OS != "windows" && len(b.Downloads) != 2 {
				t.Fatalf("php %s %s: want cli+fpm downloads, got %d", b.Version, b.OS, len(b.Downloads))
			}
		}
	}
	// Phase 2: every service ships for the two focus platforms.
	for _, name := range []string{"mysql", "mariadb", "postgresql", "valkey", "meilisearch", "rustfs", "mailpit"} {
		for _, platform := range []string{"windows/amd64", "linux/amd64"} {
			found := false
			for _, b := range ms[name].Builds {
				found = found || b.OS+"/"+b.Arch == platform
			}
			if !found {
				t.Fatalf("%s catalog missing %s", name, platform)
			}
		}
	}
	for _, os := range []string{"windows", "linux", "darwin"} {
		if !coverage["caddy"]["2|"+os] {
			t.Fatalf("caddy catalog missing %s", os)
		}
		for _, ch := range []string{"8.3", "8.4", "8.5"} {
			if !coverage["php"][ch+"|"+os] {
				t.Fatalf("php catalog missing %s on %s", ch, os)
			}
		}
	}
}

// TestEmbeddedManifestsMatchRepo guards the embedded snapshot against
// drifting from the canonical repo-root manifests/.
func TestEmbeddedManifestsMatchRepo(t *testing.T) {
	repoDir := filepath.Join("..", "..", "..", "manifests")
	entries, err := embeddedFS.ReadDir("manifests")
	if err != nil {
		t.Fatal(err)
	}
	repoJSON, err := filepath.Glob(filepath.Join(repoDir, "*.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(repoJSON) != len(entries) {
		t.Fatalf("repo has %d manifest json files, embedded has %d; re-copy manifests/*.json to daemon/internal/binman/manifests/", len(repoJSON), len(entries))
	}
	for _, e := range entries {
		emb, err := embeddedFS.ReadFile("manifests/" + e.Name())
		if err != nil {
			t.Fatal(err)
		}
		repo, err := os.ReadFile(filepath.Join(repoDir, e.Name()))
		if err != nil {
			t.Fatalf("embedded manifest %s missing from repo manifests/: %v", e.Name(), err)
		}
		if !bytes.Equal(bytes.ReplaceAll(emb, []byte("\r\n"), []byte("\n")), bytes.ReplaceAll(repo, []byte("\r\n"), []byte("\n"))) {
			t.Fatalf("embedded manifest %s drifted from repo manifests/%s; re-copy it", e.Name(), e.Name())
		}
	}
}

// tarEntry is one entry for buildTarWith; Link is a symlink target when set.
type tarEntry struct {
	Name, Body, Link string
	Hard             bool
}

func buildTarWith(t *testing.T, compress func(io.Writer) io.WriteCloser, entries []tarEntry) []byte {
	t.Helper()
	var buf bytes.Buffer
	cw := compress(&buf)
	tw := tar.NewWriter(cw)
	for _, e := range entries {
		hdr := &tar.Header{Name: e.Name, Mode: 0o755, Size: int64(len(e.Body)), Typeflag: tar.TypeReg}
		switch {
		case e.Link != "" && e.Hard:
			hdr = &tar.Header{Name: e.Name, Linkname: e.Link, Typeflag: tar.TypeLink, Mode: 0o755}
		case e.Link != "":
			hdr = &tar.Header{Name: e.Name, Linkname: e.Link, Typeflag: tar.TypeSymlink, Mode: 0o777}
		}
		if err := tw.WriteHeader(hdr); err != nil {
			t.Fatal(err)
		}
		if hdr.Typeflag == tar.TypeReg {
			if _, err := tw.Write([]byte(e.Body)); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := cw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func gzipWriter(w io.Writer) io.WriteCloser { return gzip.NewWriter(w) }

func xzWriter(w io.Writer) io.WriteCloser {
	xw, err := xz.NewWriter(w)
	if err != nil {
		panic(err)
	}
	return xw
}

func TestTarXzStripAndLinks(t *testing.T) {
	body := buildTarWith(t, xzWriter, []tarEntry{
		{Name: "pg-18/bin/postgres", Body: "server"},
		{Name: "pg-18/lib/libpq.so.5.18", Body: "lib"},
		{Name: "pg-18/lib/libpq.so.5", Link: "libpq.so.5.18"},
		{Name: "pg-18/bin/postmaster", Link: "pg-18/bin/postgres", Hard: true},
	})
	m, _ := newTestManager(t, "tar.xz", body)
	m.Manifests["tool"].Builds[0].Downloads[0].Strip = 1

	if _, err := m.Install(context.Background(), "tool", "1"); err != nil {
		t.Fatalf("Install: %v", err)
	}
	dir := m.Dir("tool", "1.0.0")
	for rel, want := range map[string]string{
		"bin/postgres":      "server",
		"lib/libpq.so.5":    "lib", // symlink (or copy on Windows) resolves to the real lib
		"bin/postmaster":    "server",
		"lib/libpq.so.5.18": "lib",
	} {
		got, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(rel)))
		if err != nil || string(got) != want {
			t.Fatalf("%s: got %q, %v; want %q", rel, got, err, want)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "pg-18")); err == nil {
		t.Fatal("strip left the top-level directory behind")
	}
}

func TestSymlinkEscapingInstallRejected(t *testing.T) {
	targets := []string{"../../../etc/passwd", "/etc/passwd", `\Windows\System32`}
	if runtime.GOOS == "windows" {
		targets = append(targets, "C:evil") // drive-relative there; a plain name elsewhere
	}
	for _, target := range targets {
		body := buildTarWith(t, gzipWriter, []tarEntry{{Name: "lib/evil", Link: target}})
		m, _ := newTestManager(t, "tar.gz", body)
		if _, err := m.Install(context.Background(), "tool", "1"); err == nil {
			t.Fatalf("target %q: want error for escaping symlink", target)
		}
		if m.IsInstalled("tool", "1.0.0") {
			t.Fatalf("target %q: malicious archive must not install", target)
		}
	}
}

func TestRawDownloadInstallsExecutable(t *testing.T) {
	m, _ := newTestManager(t, "raw", []byte("meili bits"))
	m.Manifests["tool"].Builds[0].Downloads[0].File = "meilisearch.exe"
	if _, err := m.Install(context.Background(), "tool", "1"); err != nil {
		t.Fatalf("Install: %v", err)
	}
	got, err := os.ReadFile(filepath.Join(m.Dir("tool", "1.0.0"), "meilisearch.exe"))
	if err != nil || string(got) != "meili bits" {
		t.Fatalf("got %q, %v", got, err)
	}
}

func TestRawDownloadNeedsPlainFileName(t *testing.T) {
	m, _ := newTestManager(t, "raw", []byte("x"))
	m.Manifests["tool"].Builds[0].Downloads[0].File = "../escape"
	if _, err := m.Install(context.Background(), "tool", "1"); err == nil {
		t.Fatal("want error for a path in File")
	}
}

func TestSymlinkChainedThroughDirLinkRejected(t *testing.T) {
	// "sub" is a harmless link to the install root, but "sub/x" -> "../evil"
	// then resolves one level above the install on disk.
	body := buildTarWith(t, gzipWriter, []tarEntry{
		{Name: "sub", Link: "."},
		{Name: "sub/x", Link: "../evil"},
	})
	m, _ := newTestManager(t, "tar.gz", body)
	if _, err := m.Install(context.Background(), "tool", "1"); err == nil {
		t.Fatal("want error for a link chained out through a directory link")
	}
	if m.IsInstalled("tool", "1.0.0") {
		t.Fatal("malicious archive must not install")
	}
}

func TestLinkThroughLinkInTargetRejected(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("needs real symlinks (Windows copies link targets instead)")
	}
	// "deep" -> "a/b/c" is fine, and "x" -> "deep/../../.." looks like it
	// stays at the root on paper, but on disk deep/.. is a/b, so x escapes.
	body := buildTarWith(t, gzipWriter, []tarEntry{
		{Name: "a/b/c/keep", Body: "x"},
		{Name: "deep", Link: "a/b/c"},
		{Name: "x", Link: "deep/../../../.."},
	})
	m, _ := newTestManager(t, "tar.gz", body)
	if _, err := m.Install(context.Background(), "tool", "1"); err == nil {
		t.Fatal("want error for a link that resolves outside through another link")
	}
}

func TestWriteThroughEarlierDownloadsLinkRejected(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("needs real symlinks (Windows copies link targets instead)")
	}
	first := buildTarWith(t, gzipWriter, []tarEntry{{Name: "lib/real", Body: "x"}, {Name: "up", Link: "lib/.."}})
	second := buildTarWith(t, gzipWriter, []tarEntry{{Name: "up/../escaped", Body: "evil"}})
	m, _ := newTestManager(t, "tar.gz", first, second)
	_, err := m.Install(context.Background(), "tool", "1")
	if err == nil {
		t.Fatal("want error")
	}
	if _, statErr := os.Stat(filepath.Join(m.Root, "tmp", "escaped")); statErr == nil {
		t.Fatal("second download wrote outside the install")
	}
}

func TestUninstallRemovesTheBuildWhole(t *testing.T) {
	m, _ := newTestManager(t, "zip", buildZip(t, map[string]string{"tool.exe": "bits"}))
	if _, err := m.Install(context.Background(), "tool", "1"); err != nil {
		t.Fatal(err)
	}
	if err := m.Uninstall("tool", "1.0.0"); err != nil {
		t.Fatalf("Uninstall: %v", err)
	}
	if m.IsInstalled("tool", "1.0.0") {
		t.Fatal("still installed after Uninstall")
	}
	if entries, _ := os.ReadDir(filepath.Join(m.Root, "tmp")); len(entries) != 0 {
		t.Fatalf("tmp leftovers: %v", entries)
	}
	if err := m.Uninstall("tool", "1.0.0"); err != nil {
		t.Fatalf("second Uninstall = %v, want nil (nothing to remove)", err)
	}
}

func TestDiskSizeSumsTheInstalledFiles(t *testing.T) {
	m, _ := newTestManager(t, "zip", buildZip(t, map[string]string{"tool.exe": "bits", "ext/mod.dll": "module"}))
	if got := m.DiskSize("tool", "1.0.0"); got != 0 {
		t.Fatalf("DiskSize before install = %d, want 0", got)
	}
	if _, err := m.Install(context.Background(), "tool", "1"); err != nil {
		t.Fatal(err)
	}
	if got, want := m.DiskSize("tool", "1.0.0"), int64(len("bits")+len("module")); got != want {
		t.Fatalf("DiskSize = %d, want %d", got, want)
	}
}

func TestChannelMapsStoredVersions(t *testing.T) {
	m := &Manager{Manifests: map[string]Manifest{"php": {Schema: 1, Name: "php", Builds: []Build{
		{Name: "php", Version: "8.4.12", Channel: "8.4", OS: runtime.GOOS, Arch: runtime.GOARCH},
	}}}}
	tests := []struct {
		stored, want string
		ok           bool
	}{
		{"8.4", "8.4", true},
		{"8.4.12", "8.4", true},
		{"8.4.3", "8.4", true}, // a build the catalog dropped
		{"7.4.33", "", false},  // no such channel: leave it
		{"8.3", "", false},     // a channel the catalog dropped
		{"8.4.12.1", "", false},
		{"8", "", false},
		{"", "", false},
	}
	for _, tt := range tests {
		got, ok := m.Channel("php", tt.stored)
		if got != tt.want || ok != tt.ok {
			t.Errorf("Channel(%q) = %q, %v; want %q, %v", tt.stored, got, ok, tt.want, tt.ok)
		}
	}
}

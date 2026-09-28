package services

import (
	"context"
	"encoding/binary"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/d3v0psdan/bench/daemon/internal/api"
	"github.com/d3v0psdan/bench/daemon/internal/binman"
	"github.com/d3v0psdan/bench/daemon/internal/registry"
	"github.com/d3v0psdan/bench/daemon/internal/supervisor"
)

// TestMain doubles as the fake service server: with BENCH_FAKE_SERVICE
// set, the test binary listens on BENCH_PORT until killed.
func TestMain(m *testing.M) {
	if os.Getenv("BENCH_FAKE_SERVICE") == "" {
		os.Exit(m.Run())
	}
	ln, err := net.Listen("tcp", "127.0.0.1:"+os.Getenv("BENCH_PORT"))
	if err != nil {
		os.Exit(2)
	}
	go func() {
		time.Sleep(time.Minute)
		os.Exit(0)
	}()
	for {
		c, err := ln.Accept()
		if err != nil {
			os.Exit(0)
		}
		c.Close()
	}
}

// freePort grabs a port the OS considers free right now.
func freePort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	return ln.Addr().(*net.TCPAddr).Port
}

// fakeDriver runs the test binary as a TCP server. init drops a data file
// and an identity file (scrubbed on clone).
func fakeDriver(port int) driver {
	return driver{
		label:       "Fake",
		binary:      "fake",
		defaultPort: port,
		init: func(ctx context.Context, in instance) error {
			if err := os.MkdirAll(in.DataDir, 0o700); err != nil {
				return err
			}
			if err := os.WriteFile(filepath.Join(in.DataDir, "data.txt"), []byte("rows"), 0o644); err != nil {
				return err
			}
			return os.WriteFile(filepath.Join(in.DataDir, "identity"), []byte(in.Name), 0o644)
		},
		spec: func(in instance) supervisor.Spec {
			return supervisor.Spec{
				Command: os.Args[0],
				Env:     []string{"BENCH_FAKE_SERVICE=1", "BENCH_PORT=" + strconv.Itoa(in.Port)},
				Health:  tcpReady(loopback(in.Port)),
			}
		},
		env: func(in instance) []string { return []string{"FAKE_PORT=" + strconv.Itoa(in.Port)} },
		afterClone: func(dataDir string) error {
			return removeIfExists(filepath.Join(dataDir, "identity"))
		},
	}
}

// tcpReady is the fake driver's health probe: the fake server is ready
// once it accepts a TCP connection.
func tcpReady(addr string) func(context.Context) error {
	return func(ctx context.Context) error {
		conn, err := dial(ctx, addr)
		if err != nil {
			return err
		}
		return conn.Close()
	}
}

func newTestManager(t *testing.T, d driver) *Manager {
	t.Helper()
	drivers["fake"] = d
	t.Cleanup(func() { delete(drivers, "fake") })

	root := t.TempDir()
	reg, err := registry.Open(filepath.Join(root, "bench.db"))
	if err != nil {
		t.Fatal(err)
	}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	bin := &binman.Manager{Root: root, Log: log, Manifests: map[string]binman.Manifest{
		"fake": {Schema: 1, Name: "fake", Builds: []binman.Build{{
			Name: "fake", Version: "1.2.3", Channel: "1", OS: runtime.GOOS, Arch: runtime.GOARCH,
		}}},
	}}
	// Pre-installed: the build dir existing is binman's installed marker.
	if err := os.MkdirAll(bin.Dir("fake", "1.2.3"), 0o755); err != nil {
		t.Fatal(err)
	}
	sup := supervisor.New(log)
	sup.ReadyTimeout = 5 * time.Second
	sup.HealthInterval = 20 * time.Millisecond
	sup.StopTimeout = 2 * time.Second
	m := &Manager{Root: root, Reg: reg, Bin: bin, Sup: sup, Log: log}
	// LIFO: children die before the registry closes and TempDir is removed.
	t.Cleanup(func() { reg.Close() })
	t.Cleanup(sup.StopAll)
	return m
}

func create(t *testing.T, m *Manager, name string) api.Service {
	t.Helper()
	s, err := m.Create(api.CreateServiceRequest{Service: "fake", Name: name})
	if err != nil {
		t.Fatalf("Create %s: %v", name, err)
	}
	return s
}

func TestCreateInitializesAndStarts(t *testing.T) {
	port := freePort(t)
	m := newTestManager(t, fakeDriver(port))

	s := create(t, m, "app-db")
	if s.State != "running" || s.Port != port || s.Version != "1.2.3" || s.Channel != "1" {
		t.Fatalf("created: %+v", s)
	}
	if len(s.Env) != 1 || s.Env[0] != "FAKE_PORT="+strconv.Itoa(port) {
		t.Fatalf("env: %v", s.Env)
	}
	if b, err := os.ReadFile(filepath.Join(s.DataDir, markerFile)); err != nil || strings.TrimSpace(string(b)) != "fake 1" {
		t.Fatalf("marker: %q, %v", b, err)
	}
	if conn, err := net.Dial("tcp", loopback(port)); err != nil {
		t.Fatalf("server not reachable: %v", err)
	} else {
		conn.Close()
	}
}

func TestCreateMovesPastBusyDefaultPort(t *testing.T) {
	busy, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer busy.Close()
	port := busy.Addr().(*net.TCPAddr).Port
	m := newTestManager(t, fakeDriver(port))

	s := create(t, m, "db")
	if s.Port == port || s.State != "running" {
		t.Fatalf("want a moved port and a running server, got %+v", s)
	}
	if !strings.Contains(s.Notice, strconv.Itoa(port)) {
		t.Fatalf("want a notice naming the busy port, got %q", s.Notice)
	}
}

func TestCreateRejectsExplicitBusyPort(t *testing.T) {
	busy, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer busy.Close()
	m := newTestManager(t, fakeDriver(freePort(t)))
	_, err = m.Create(api.CreateServiceRequest{
		Service: "fake", Name: "db", Port: busy.Addr().(*net.TCPAddr).Port,
	})
	if !errors.As(err, new(InputError)) {
		t.Fatalf("want InputError for an explicit busy port, got %v", err)
	}
	if list, _ := m.List(); len(list) != 0 {
		t.Fatalf("failed create must leave nothing behind: %+v", list)
	}
}

func TestCreateValidation(t *testing.T) {
	m := newTestManager(t, fakeDriver(freePort(t)))
	for name, req := range map[string]api.CreateServiceRequest{
		"unknown service": {Service: "oracle"},
		"bad name":        {Service: "fake", Name: "no spaces"},
		"unknown channel": {Service: "fake", Channel: "99"},
	} {
		if _, err := m.Create(req); !errors.As(err, new(InputError)) {
			t.Errorf("%s: want InputError, got %v", name, err)
		}
	}
	create(t, m, "db")
	if _, err := m.Create(api.CreateServiceRequest{Service: "fake", Name: "db"}); !errors.As(err, new(InputError)) {
		t.Errorf("duplicate name: want InputError, got %v", err)
	}
}

func TestSingletonAllowsOneInstance(t *testing.T) {
	d := fakeDriver(freePort(t))
	d.singleton = true
	m := newTestManager(t, d)
	create(t, m, "mail")
	if _, err := m.Create(api.CreateServiceRequest{Service: "fake", Name: "mail2"}); !errors.As(err, new(InputError)) {
		t.Fatalf("want InputError for a second singleton, got %v", err)
	}
}

func TestCloneCopiesDataAndRestartsSource(t *testing.T) {
	m := newTestManager(t, fakeDriver(freePort(t)))
	src := create(t, m, "app-db")

	clone, err := m.Clone("app-db", "app-db-copy")
	if err != nil {
		t.Fatalf("Clone: %v", err)
	}
	if clone.State != "running" || clone.Port == src.Port {
		t.Fatalf("clone: %+v", clone)
	}
	if b, err := os.ReadFile(filepath.Join(clone.DataDir, "data.txt")); err != nil || string(b) != "rows" {
		t.Fatalf("clone data: %q, %v", b, err)
	}
	if fileExists(filepath.Join(clone.DataDir, "identity")) {
		t.Fatal("afterClone must scrub identity files from the copy")
	}
	if got, _ := m.Get("app-db"); got.State != "running" {
		t.Fatalf("source must be running again after clone, got %s", got.State)
	}
}

func TestStopStartAndAutostart(t *testing.T) {
	m := newTestManager(t, fakeDriver(freePort(t)))
	create(t, m, "cache")
	if s, err := m.Stop("cache"); err != nil || s.State != "stopped" {
		t.Fatalf("Stop: %+v, %v", s, err)
	}
	if _, err := m.SetAutostart("cache", true); err != nil {
		t.Fatal(err)
	}
	m.StartAutostart()
	if s, _ := m.Get("cache"); s.State != "running" || !s.Autostart {
		t.Fatalf("after autostart: %+v", s)
	}
}

func TestDeleteRemovesDataUnlessKept(t *testing.T) {
	m := newTestManager(t, fakeDriver(freePort(t)))
	kept := create(t, m, "kept")
	gone := create(t, m, "gone")
	if err := m.Delete("kept", true); err != nil {
		t.Fatal(err)
	}
	if err := m.Delete("gone", false); err != nil {
		t.Fatal(err)
	}
	if !fileExists(filepath.Join(kept.DataDir, "data.txt")) {
		t.Fatal("keepData must leave the data dir")
	}
	if fileExists(gone.DataDir) {
		t.Fatal("delete must remove the data dir")
	}
	if _, err := m.Get("gone"); !errors.Is(err, registry.ErrServiceNotFound) {
		t.Fatalf("want not found after delete, got %v", err)
	}
	// Re-creating over kept data reuses it instead of re-initializing.
	if err := os.WriteFile(filepath.Join(kept.DataDir, "data.txt"), []byte("user rows"), 0o644); err != nil {
		t.Fatal(err)
	}
	again := create(t, m, "kept")
	if b, _ := os.ReadFile(filepath.Join(again.DataDir, "data.txt")); string(b) != "user rows" {
		t.Fatalf("kept data was re-initialized: %q", b)
	}
}

// writeFile creates path (and its parents) with body, failing the test on error.
func writeFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestCrashedCreateLeftoversAreReinitialized(t *testing.T) {
	m := newTestManager(t, fakeDriver(freePort(t)))
	stale := filepath.Join(m.Root, "data", "fake", "db")
	writeFile(t, filepath.Join(stale, "half-written"), "")
	writeFile(t, stale+creatingSuffix, "") // the create that crashed left this
	s := create(t, m, "db")
	if fileExists(filepath.Join(s.DataDir, "half-written")) || !fileExists(filepath.Join(s.DataDir, "data.txt")) {
		t.Fatal("a crashed create's leftovers must be wiped and re-initialized")
	}
	if fileExists(stale + creatingSuffix) {
		t.Fatal("the in-progress sentinel must be removed once the create finishes")
	}
}

func TestCreateNeverWipesForeignData(t *testing.T) {
	m := newTestManager(t, fakeDriver(freePort(t)))
	foreign := filepath.Join(m.Root, "data", "fake", "db", "users.ibd")
	writeFile(t, foreign, "someone's rows")
	_, err := m.Create(api.CreateServiceRequest{Service: "fake", Name: "db"})
	if !errors.As(err, new(InputError)) {
		t.Fatalf("want InputError for a dir Bench didn't create, got %v", err)
	}
	if b, _ := os.ReadFile(foreign); string(b) != "someone's rows" {
		t.Fatal("foreign data must be left untouched")
	}
}

func TestStartNeverWipesUnmarkedData(t *testing.T) {
	m := newTestManager(t, fakeDriver(freePort(t)))
	s := create(t, m, "db")
	if _, err := m.Stop("db"); err != nil {
		t.Fatal(err)
	}
	// e.g. restored from a backup that skipped dotfiles
	if err := os.Remove(filepath.Join(s.DataDir, markerFile)); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Start(context.Background(), "db"); err == nil || !strings.Contains(err.Error(), markerFile) {
		t.Fatalf("want a refusal naming the marker, got %v", err)
	}
	if b, _ := os.ReadFile(filepath.Join(s.DataDir, "data.txt")); string(b) != "rows" {
		t.Fatal("start must never wipe existing data")
	}
}

func TestStartReinitializesDeletedDataDir(t *testing.T) {
	m := newTestManager(t, fakeDriver(freePort(t)))
	s := create(t, m, "db")
	if _, err := m.Stop("db"); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(s.DataDir); err != nil {
		t.Fatal(err)
	}
	got, err := m.Start(context.Background(), "db")
	if err != nil || got.State != "running" || !fileExists(filepath.Join(s.DataDir, markerFile)) {
		t.Fatalf("a deleted data dir starts fresh: %+v, %v", got, err)
	}
}

func TestKeptDataFromAnotherVersionIsRefused(t *testing.T) {
	m := newTestManager(t, fakeDriver(freePort(t)))
	kept := filepath.Join(m.Root, "data", "fake", "db")
	writeFile(t, filepath.Join(kept, markerFile), "fake 0\n")
	writeFile(t, filepath.Join(kept, "data.txt"), "old-format rows")
	_, err := m.Create(api.CreateServiceRequest{Service: "fake", Name: "db"})
	if !errors.As(err, new(InputError)) || !strings.Contains(err.Error(), "version 0") {
		t.Fatalf("want an InputError naming the data's version, got %v", err)
	}
	if b, _ := os.ReadFile(filepath.Join(kept, "data.txt")); string(b) != "old-format rows" {
		t.Fatal("kept data must be left untouched")
	}
}

func TestCloneIntoKeptDataIsRefusedAndKeepsIt(t *testing.T) {
	m := newTestManager(t, fakeDriver(freePort(t)))
	create(t, m, "src")
	kept := create(t, m, "kept")
	writeFile(t, filepath.Join(kept.DataDir, "data.txt"), "precious")
	if err := m.Delete("kept", true); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Clone("src", "kept"); !errors.As(err, new(InputError)) {
		t.Fatalf("want InputError for a clone onto kept data, got %v", err)
	}
	if b, _ := os.ReadFile(filepath.Join(kept.DataDir, "data.txt")); string(b) != "precious" {
		t.Fatal("a refused clone must not delete the kept data")
	}
}

func TestCloneCleansInterruptedPartialCopy(t *testing.T) {
	m := newTestManager(t, fakeDriver(freePort(t)))
	create(t, m, "src")
	partial := filepath.Join(m.Root, "data", "fake", "copy.partial")
	writeFile(t, filepath.Join(partial, "torn"), "half")
	c, err := m.Clone("src", "copy")
	if err != nil {
		t.Fatal(err)
	}
	if fileExists(partial) || fileExists(filepath.Join(c.DataDir, "torn")) {
		t.Fatal("a leftover partial copy must be discarded, never promoted")
	}
}

func TestConcurrentCreatesGetDistinctPorts(t *testing.T) {
	m := newTestManager(t, fakeDriver(freePort(t)))
	names := []string{"a", "b", "c", "d"}
	ports := make([]int, len(names))
	errs := make([]error, len(names))
	var wg sync.WaitGroup
	for i, n := range names {
		wg.Go(func() {
			s, err := m.Create(api.CreateServiceRequest{Service: "fake", Name: n})
			ports[i], errs[i] = s.Port, err
		})
	}
	wg.Wait()
	seen := map[int]bool{}
	for i, p := range ports {
		if errs[i] != nil {
			t.Fatalf("create %s: %v", names[i], errs[i])
		}
		if seen[p] {
			t.Fatalf("port %d assigned twice: %v", p, ports)
		}
		seen[p] = true
	}
}

func TestLongNamesAreRejected(t *testing.T) {
	m := newTestManager(t, fakeDriver(freePort(t)))
	_, err := m.Create(api.CreateServiceRequest{Service: "fake", Name: strings.Repeat("a", maxNameLen+1)})
	if !errors.As(err, new(InputError)) {
		t.Fatalf("want InputError for a long name, got %v", err)
	}
}

func TestBusyNameIsRefused(t *testing.T) {
	m := newTestManager(t, fakeDriver(freePort(t)))
	create(t, m, "db")
	if err := m.begin("deleting", "db"); err != nil {
		t.Fatal(err)
	}
	defer m.end("db")
	if _, err := m.Start(context.Background(), "db"); !errors.Is(err, ErrBusy) {
		t.Fatalf("start during delete: want ErrBusy, got %v", err)
	}
	if err := m.reserve(registry.Service{Kind: "fake", Name: "db"}, false); !errors.Is(err, ErrBusy) {
		t.Fatalf("reserve during delete: want ErrBusy, got %v", err)
	}
}

func TestSafeDataDirRefusesOutsideRoot(t *testing.T) {
	m := &Manager{Root: t.TempDir()}
	for _, dir := range []string{t.TempDir(), m.Root, filepath.Join(m.Root, "data")} {
		if err := m.safeDataDir(dir); err == nil {
			t.Errorf("%s: want refusal", dir)
		}
	}
	if err := m.safeDataDir(filepath.Join(m.Root, "data", "mysql", "db")); err != nil {
		t.Fatal(err)
	}
}

func TestAllocatePort(t *testing.T) {
	free := func(busy ...int) func(int) bool {
		return func(p int) bool {
			for _, b := range busy {
				if p == b {
					return false
				}
			}
			return true
		}
	}
	cases := []struct {
		name     string
		want     int
		explicit bool
		taken    map[int]bool
		free     func(int) bool
		got      int
		wantErr  bool
	}{
		{"default free", 3306, false, nil, free(), 3306, false},
		{"skips taken and busy", 3306, false, map[int]bool{3306: true}, free(3307), 3308, false},
		{"skips herd php-cgi range", 9000, false, nil, free(9000, 9001), 9200, false},
		{"explicit busy is an error", 3306, true, nil, free(3306), 0, true},
		{"explicit taken is an error", 3306, true, map[int]bool{3306: true}, free(), 0, true},
		{"explicit herd port is allowed", 9084, true, nil, free(), 9084, false},
	}
	for _, c := range cases {
		got, err := allocatePort(c.want, c.explicit, c.taken, c.free)
		if (err != nil) != c.wantErr || got != c.got {
			t.Errorf("%s: got %d, %v", c.name, got, err)
		}
	}
}

func TestHerdReserved(t *testing.T) {
	cases := []struct {
		name string
		port int
		want bool
	}{
		{"herd helper", 5000, true},
		{"next to herd helper", 5001, false},
		{"below php-cgi range", 9001, false},
		{"php-cgi range start", 9002, true},
		{"php-cgi range end", 9199, true},
		{"above php-cgi range", 9200, false},
		{"mysql default", 3306, false},
	}
	for _, c := range cases {
		if got := herdReserved(c.port); got != c.want {
			t.Errorf("%s: herdReserved(%d) = %v, want %v", c.name, c.port, got, c.want)
		}
	}
}

// serveOnce accepts one connection and runs handle on it.
func serveOnce(t *testing.T, handle func(net.Conn)) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	go func() {
		c, err := ln.Accept()
		if err != nil {
			return
		}
		defer c.Close()
		handle(c)
	}()
	return ln.Addr().String()
}

// readStartup reads a whole postgres StartupMessage. Closing with unread
// bytes makes Windows reset the connection, which can reach the client
// before the reply does.
func readStartup(c net.Conn) {
	var size [4]byte
	if _, err := io.ReadFull(c, size[:]); err != nil {
		return
	}
	io.ReadFull(c, make([]byte, binary.BigEndian.Uint32(size[:])-4))
}

func TestProbes(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	cases := []struct {
		name  string
		probe func(string) func(context.Context) error
		reply func(net.Conn)
		ok    bool
	}{
		{"mysql greeting", mysqlReady, func(c net.Conn) { c.Write([]byte{0x4a, 0, 0, 0, 10, '8'}) }, true},
		{"mysql error packet", mysqlReady, func(c net.Conn) { c.Write([]byte{0x17, 0, 0, 0, 0xff, 0x10}) }, false},
		{"postgres auth request", postgresReady, func(c net.Conn) { readStartup(c); c.Write([]byte{'R', 0, 0, 0, 8, 0, 0, 0, 0}) }, true},
		{"postgres starting up", postgresReady, func(c net.Conn) { readStartup(c); c.Write([]byte{'E', 0, 0, 0, 4}) }, false},
		{"valkey pong", redisReady, func(c net.Conn) { io.ReadFull(c, make([]byte, 6)); c.Write([]byte("+PONG\r\n")) }, true},
		{"valkey loading", redisReady, func(c net.Conn) { io.ReadFull(c, make([]byte, 6)); c.Write([]byte("-LOADING\r\n")) }, false},
	}
	for _, c := range cases {
		err := c.probe(serveOnce(t, c.reply))(ctx)
		if (err == nil) != c.ok {
			t.Errorf("%s: err = %v, want ok=%v", c.name, err, c.ok)
		}
	}
}

func TestHintNamesKnownFixes(t *testing.T) {
	if h := hint("mysqld: error while loading shared libraries: libaio.so.1: cannot open"); !strings.Contains(h, "libaio") {
		t.Fatalf("hint = %q", h)
	}
	if h := hint("exited during startup: exit status 3221225781"); !strings.Contains(h, "Visual C++") {
		t.Fatalf("hint = %q", h)
	}
	if hint("all good") != "" {
		t.Fatal("no hint expected for unknown output")
	}
}

func TestEnsureBucketSignsAndToleratesExisting(t *testing.T) {
	var auth string
	status, body := http.StatusOK, ""
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth = r.Header.Get("Authorization")
		w.WriteHeader(status)
		io.WriteString(w, body)
	}))
	defer srv.Close()
	ctx := context.Background()

	if err := ensureBucket(ctx, srv.URL, "bench", "benchsecret", "local"); err != nil {
		t.Fatalf("created: %v", err)
	}
	if !strings.HasPrefix(auth, "AWS4-HMAC-SHA256 Credential=bench/") || !strings.Contains(auth, "Signature=") {
		t.Fatalf("authorization header: %q", auth)
	}
	status, body = http.StatusConflict, "<Code>BucketAlreadyOwnedByYou</Code>"
	if err := ensureBucket(ctx, srv.URL, "bench", "benchsecret", "local"); err != nil {
		t.Fatalf("existing bucket must be fine: %v", err)
	}
	status, body = http.StatusForbidden, "<Code>SignatureDoesNotMatch</Code>"
	if err := ensureBucket(ctx, srv.URL, "bench", "benchsecret", "local"); err == nil {
		t.Fatal("want an error for a rejected request")
	}
}

// TestSignV4HeaderShape pins the credential scope and signed-header list;
// the signature itself is proven against a live RustFS in the exit tests.
func TestSignV4HeaderShape(t *testing.T) {
	req := httptest.NewRequest(http.MethodPut, "http://127.0.0.1:9000/local", nil)
	signV4(req, "bench", "benchsecret", time.Date(2026, 9, 24, 12, 0, 0, 0, time.UTC))
	want := "AWS4-HMAC-SHA256 Credential=bench/20260924/us-east-1/s3/aws4_request, SignedHeaders=host;x-amz-content-sha256;x-amz-date, Signature="
	if got := req.Header.Get("Authorization"); !strings.HasPrefix(got, want) || len(got) != len(want)+64 {
		t.Fatalf("authorization = %q", got)
	}
}

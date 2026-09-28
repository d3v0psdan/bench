package supervisor

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// TestMain doubles as the fake child: when BENCH_FAKE_CHILD is set, the
// test binary acts out the named behavior instead of running tests.
func TestMain(m *testing.M) {
	switch os.Getenv("BENCH_FAKE_CHILD") {
	case "":
		os.Exit(m.Run())
	case "sleep":
		fmt.Println("fake child up")
		time.Sleep(time.Minute)
	case "crash":
		fmt.Println("crashing")
		os.Exit(3)
	case "ready-file":
		time.Sleep(100 * time.Millisecond)
		if err := os.WriteFile(os.Getenv("BENCH_READY_FILE"), []byte("ok"), 0o644); err != nil {
			os.Exit(1)
		}
		time.Sleep(time.Minute)
	case "clean-stop":
		// Exits cleanly (leaving a marker) once the stop file appears. It
		// stands in for `mysqladmin shutdown` against a real server.
		stopFile := os.Getenv("BENCH_STOP_FILE")
		for range 600 {
			if _, err := os.Stat(stopFile); err == nil {
				_ = os.WriteFile(stopFile+".clean", []byte("ok"), 0o644)
				os.Exit(0)
			}
			time.Sleep(10 * time.Millisecond)
		}
	}
	os.Exit(0)
}

func newTestSupervisor(t *testing.T) *Supervisor {
	t.Helper()
	s := New(slog.New(slog.NewTextHandler(io.Discard, nil)))
	s.ReadyTimeout = 5 * time.Second
	s.HealthInterval = 20 * time.Millisecond
	s.BackoffMin = 30 * time.Millisecond
	s.BackoffMax = 100 * time.Millisecond
	s.BackoffReset = 5 * time.Second
	s.StopTimeout = 2 * time.Second
	return s
}

func childSpec(t *testing.T, s *Supervisor, name, mode string, env ...string) Spec {
	t.Helper()
	dir := t.TempDir()
	// Registered after TempDir so it runs first (LIFO): children must die
	// before the dir is removed: Windows can't delete open log files.
	t.Cleanup(s.StopAll)
	return Spec{
		Name:    name,
		Command: os.Args[0],
		Env:     append([]string{"BENCH_FAKE_CHILD=" + mode}, env...),
		LogFile: filepath.Join(dir, name+".log"),
	}
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

func TestStartCapturesLogsAndStops(t *testing.T) {
	s := newTestSupervisor(t)
	spec := childSpec(t, s, "sleeper", "sleep")
	if err := s.Start(context.Background(), spec); err != nil {
		t.Fatalf("Start: %v", err)
	}
	st, ok := s.Status("sleeper")
	if !ok || st.State != StateRunning || st.PID == 0 {
		t.Fatalf("want running with pid, got %+v (ok=%v)", st, ok)
	}
	waitFor(t, "log output", func() bool {
		b, _ := os.ReadFile(spec.LogFile)
		return strings.Contains(string(b), "fake child up")
	})
	s.Stop("sleeper")
	st, _ = s.Status("sleeper")
	if st.State != StateStopped {
		t.Fatalf("want stopped, got %+v", st)
	}
}

func TestRestartsAfterCrashWithBackoff(t *testing.T) {
	s := newTestSupervisor(t)
	var mu sync.Mutex
	var states []State
	s.OnEvent = func(st Status) {
		mu.Lock()
		states = append(states, st.State)
		mu.Unlock()
	}
	if err := s.Start(context.Background(), childSpec(t, s, "crasher", "crash")); err != nil {
		t.Fatalf("Start: %v", err)
	}
	waitFor(t, "restarts", func() bool {
		st, _ := s.Status("crasher")
		return st.Restarts >= 2
	})
	s.Stop("crasher")
	mu.Lock()
	defer mu.Unlock()
	var sawBackoff bool
	for _, st := range states {
		if st == StateBackoff {
			sawBackoff = true
		}
	}
	if !sawBackoff {
		t.Fatalf("no backoff event fired; states: %v", states)
	}
}

func TestFirstStartFailureIsTerminal(t *testing.T) {
	s := newTestSupervisor(t)
	spec := Spec{Name: "missing", Command: filepath.Join(t.TempDir(), "no-such-binary")}
	if err := s.Start(context.Background(), spec); err == nil {
		t.Fatal("want error for missing binary")
	}
	st, _ := s.Status("missing")
	if st.State != StateFailed {
		t.Fatalf("want failed, got %+v", st)
	}
	if st.Restarts != 0 {
		t.Fatalf("first-start failure must not retry, got %+v", st)
	}
}

func TestHealthGatesReadiness(t *testing.T) {
	s := newTestSupervisor(t)
	readyFile := filepath.Join(t.TempDir(), "ready")
	spec := childSpec(t, s, "gated", "ready-file", "BENCH_READY_FILE="+readyFile)
	spec.Health = func(ctx context.Context) error {
		_, err := os.Stat(readyFile)
		return err
	}
	start := time.Now()
	if err := s.Start(context.Background(), spec); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if time.Since(start) < 100*time.Millisecond {
		t.Fatal("Start returned before the child was healthy")
	}
	st, _ := s.Status("gated")
	if st.State != StateRunning {
		t.Fatalf("want running, got %+v", st)
	}
}

func TestUnhealthyFirstStartFails(t *testing.T) {
	s := newTestSupervisor(t)
	s.ReadyTimeout = 150 * time.Millisecond
	spec := childSpec(t, s, "never-ready", "sleep")
	spec.Health = func(ctx context.Context) error { return errors.New("nope: port refused") }
	err := s.Start(context.Background(), spec)
	if err == nil {
		t.Fatal("want readiness failure")
	}
	// The timeout error must carry the underlying health-probe cause.
	if !strings.Contains(err.Error(), "port refused") {
		t.Fatalf("timeout error lost the health cause: %v", err)
	}
	st, _ := s.Status("never-ready")
	if st.State != StateFailed {
		t.Fatalf("want failed, got %+v", st)
	}
}

func TestStopClearsCrashError(t *testing.T) {
	s := newTestSupervisor(t)
	if err := s.Start(context.Background(), childSpec(t, s, "crasher2", "crash")); err != nil {
		t.Fatalf("Start: %v", err)
	}
	waitFor(t, "a crash", func() bool {
		st, _ := s.Status("crasher2")
		return st.Restarts >= 1
	})
	s.Stop("crasher2")
	st, _ := s.Status("crasher2")
	if st.State != StateStopped || st.Error != "" {
		t.Fatalf("deliberate stop must clear the crash error, got %+v", st)
	}
}

func TestStartAfterStopAllRefused(t *testing.T) {
	s := newTestSupervisor(t)
	s.StopAll()
	if err := s.Start(context.Background(), childSpec(t, s, "late", "sleep")); err == nil {
		t.Fatal("Start after StopAll must be refused (no orphaned children)")
	}
}

func TestConcurrentStartIsIdempotent(t *testing.T) {
	s := newTestSupervisor(t)
	spec := childSpec(t, s, "shared", "sleep")
	var wg sync.WaitGroup
	errs := make([]error, 4)
	for i := range errs {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs[i] = s.Start(context.Background(), spec)
		}()
	}
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			t.Fatalf("Start %d: %v", i, err)
		}
	}
	st, _ := s.Status("shared")
	if st.State != StateRunning || st.Restarts != 0 {
		t.Fatalf("want one running instance, got %+v", st)
	}
}

func TestStopUsesCleanShutdownFirst(t *testing.T) {
	s := newTestSupervisor(t)
	stopFile := filepath.Join(t.TempDir(), "stop")
	spec := childSpec(t, s, "db", "clean-stop", "BENCH_STOP_FILE="+stopFile)
	spec.Shutdown = func(ctx context.Context) error { return os.WriteFile(stopFile, nil, 0o644) }
	if err := s.Start(context.Background(), spec); err != nil {
		t.Fatalf("Start: %v", err)
	}
	s.Stop("db")
	if _, err := os.Stat(stopFile + ".clean"); err != nil {
		t.Fatal("child was killed instead of exiting through Shutdown")
	}
	if st, _ := s.Status("db"); st.State != StateStopped {
		t.Fatalf("state = %s, want stopped", st.State)
	}
}

func TestStopFallsBackWhenCleanShutdownFails(t *testing.T) {
	s := newTestSupervisor(t)
	spec := childSpec(t, s, "db", "sleep")
	spec.Shutdown = func(ctx context.Context) error { return errors.New("mysqladmin: connection refused") }
	if err := s.Start(context.Background(), spec); err != nil {
		t.Fatalf("Start: %v", err)
	}
	s.Stop("db")
	if st, _ := s.Status("db"); st.State != StateStopped || st.PID != 0 {
		t.Fatalf("status = %+v, want stopped", st)
	}
}

func TestExecReturnsOutputAndExitError(t *testing.T) {
	out, err := Exec(context.Background(), Spec{
		Command: os.Args[0],
		Env:     []string{"BENCH_FAKE_CHILD=crash"},
	})
	if err == nil || !strings.Contains(out, "crashing") {
		t.Fatalf("want exit error with captured output, got %q, %v", out, err)
	}
}

func TestExecKilledWhenContextEnds(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, err := Exec(ctx, Spec{Command: os.Args[0], Env: []string{"BENCH_FAKE_CHILD=sleep"}})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("want deadline error, got %v", err)
	}
	if time.Since(start) > 10*time.Second {
		t.Fatal("Exec outlived its context")
	}
}

func TestExecStreamsToOutput(t *testing.T) {
	var live bytes.Buffer
	out, _ := Exec(context.Background(), Spec{
		Command: os.Args[0],
		Env:     []string{"BENCH_FAKE_CHILD=crash"},
		Output:  &live,
	})
	if !strings.Contains(out, "crashing") || !strings.Contains(live.String(), out) {
		t.Fatalf("Output got %q, want the same text Exec returned (%q)", live.String(), out)
	}
}

func TestTailBufferKeepsTheLastBytes(t *testing.T) {
	head := strings.Repeat("h", maxExecOutput)
	tail := strings.Repeat("t", maxExecOutput)
	cases := []struct {
		name   string
		writes []string
		want   string
	}{
		{"under the cap", []string{"abc", "def"}, "abcdef"},
		{"exactly the cap", []string{tail}, tail},
		{"one write over the cap", []string{"xyz" + tail}, tail},
		{"writes crossing the cap", []string{head, "x", tail}, tail},
		{"cap straddles writes", []string{head, "tail"}, head[4:] + "tail"},
	}
	for _, c := range cases {
		var b tailBuffer
		for _, w := range c.writes {
			if n, err := b.Write([]byte(w)); n != len(w) || err != nil {
				t.Fatalf("%s: Write = %d, %v; want %d, nil", c.name, n, err, len(w))
			}
		}
		if got := b.String(); got != c.want {
			t.Errorf("%s: kept %d bytes that aren't the last %d written", c.name, len(got), len(c.want))
		}
	}
}

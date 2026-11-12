package main

import (
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/d3v0psdan/bench/daemon/internal/caddy"
	"github.com/d3v0psdan/bench/daemon/internal/registry"
	"github.com/d3v0psdan/bench/daemon/internal/sites"
	"github.com/d3v0psdan/bench/daemon/internal/supervisor"
)

// TestMain doubles as a fake Caddy: with BENCH_FAKE_CADDY set, the test
// binary just stays up until it is killed.
func TestMain(m *testing.M) {
	if os.Getenv("BENCH_FAKE_CADDY") != "" {
		time.Sleep(time.Minute)
		os.Exit(0)
	}
	os.Exit(m.Run())
}

// supervisedRouter starts a fake Caddy through a real supervisor, the way
// caddy.Ensure does, and counts the configs loaded into it.
type supervisedRouter struct {
	sup  *supervisor.Supervisor
	spec supervisor.Spec

	mu      sync.Mutex
	applied [][]caddy.Site
}

func (r *supervisedRouter) Ensure(ctx context.Context) error { return r.sup.Start(ctx, r.spec) }

func (r *supervisedRouter) Apply(_ context.Context, list []caddy.Site) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.applied = append(r.applied, list)
	return nil
}

func (r *supervisedRouter) loads() [][]caddy.Site {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([][]caddy.Site(nil), r.applied...)
}

// serveSitesFixture wires a sites manager with one proxy site to a
// supervised fake Caddy, with the supervisor's events going through
// onCaddyReady as in run.
func serveSitesFixture(t *testing.T) (*supervisor.Supervisor, *supervisedRouter) {
	t.Helper()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	reg, err := registry.Open(filepath.Join(t.TempDir(), "bench.db"))
	if err != nil {
		t.Fatal(err)
	}
	sup := supervisor.New(log)
	sup.BackoffMin = 20 * time.Millisecond
	work := &lifeWork{}
	life, endLife := context.WithCancel(context.Background())
	// Cleanups run last first: work ends, then the children, then the registry.
	t.Cleanup(func() { reg.Close() })
	t.Cleanup(sup.StopAll)
	t.Cleanup(func() { endLife(); work.Stop() })

	router := &supervisedRouter{sup: sup, spec: supervisor.Spec{
		Name:    caddy.ProcName,
		Command: os.Args[0],
		Env:     []string{"BENCH_FAKE_CADDY=1"},
	}}
	siteMgr := &sites.Manager{Reg: reg, Router: router, Log: log, InstalledPHP: func() []string { return nil }}
	sup.OnEvent = func(st supervisor.Status) {
		onCaddyReady(st, func() {
			work.Go(func() { _, _ = siteMgr.Apply(life) })
		})
	}
	if _, err := siteMgr.Proxy(life, "api", "127.0.0.1:3000"); err != nil {
		t.Fatal(err)
	}
	return sup, router
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

func TestSitesAreLoadedIntoCaddyAgainAfterItCrashes(t *testing.T) {
	sup, router := serveSitesFixture(t)
	waitFor(t, "the load that follows Caddy's first start", func() bool { return len(router.loads()) == 2 })

	st, _ := sup.Status(caddy.ProcName)
	p, err := os.FindProcess(st.PID)
	if err != nil {
		t.Fatal(err)
	}
	if err := p.Kill(); err != nil {
		t.Fatal(err)
	}

	waitFor(t, "the sites loaded into the restarted Caddy", func() bool { return len(router.loads()) == 3 })
	last := router.loads()[2]
	if len(last) != 1 || last[0].Host != "api.test" {
		t.Fatalf("the restarted Caddy got %+v, want api.test", last)
	}
}

func TestCaddyStartingLoadsTheSitesOnceNotInALoop(t *testing.T) {
	_, router := serveSitesFixture(t)
	waitFor(t, "the load that follows Caddy's start", func() bool { return len(router.loads()) == 2 })
	time.Sleep(300 * time.Millisecond)
	if n := len(router.loads()); n != 2 {
		t.Fatalf("%d config loads after one Caddy start, want 2 (the change, then one reload)", n)
	}
}

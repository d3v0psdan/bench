package newapp

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/d3v0psdan/bench/daemon/internal/api"
	"github.com/d3v0psdan/bench/daemon/internal/registry"
	"github.com/d3v0psdan/bench/daemon/internal/services"
	"github.com/d3v0psdan/bench/daemon/internal/sites"
	"github.com/d3v0psdan/bench/daemon/internal/supervisor"
	"github.com/d3v0psdan/bench/daemon/internal/tasks"
)

// fakeDatabasesEnv lists the databases the fake mysql and psql clients
// report, comma-separated.
const fakeDatabasesEnv = "BENCH_FAKE_DATABASES"

// fakeCreatedSinceEnv lists databases made after the existence check: the
// fake's SELECT misses them, its strict CREATE still fails on them.
const fakeCreatedSinceEnv = "BENCH_FAKE_CREATED_SINCE"

// TestMain doubles as a fake instance process (BENCH_FAKE_CHILD=sleep)
// and, when copied as bin/mysql or bin/psql, as a fake database client.
func TestMain(m *testing.M) {
	if os.Getenv("BENCH_FAKE_CHILD") == "sleep" {
		time.Sleep(time.Minute)
		os.Exit(0)
	}
	switch strings.TrimSuffix(filepath.Base(os.Args[0]), ".exe") {
	case "mysql", "psql":
		os.Exit(fakeClient(os.Args[1:]))
	}
	os.Exit(m.Run())
}

// fakeClient answers a SELECT with every listed database, refuses a
// strict CREATE of a listed or since-created one, and lets IF NOT EXISTS
// pass, like MySQL.
func fakeClient(args []string) int {
	sql := args[len(args)-1]
	existing := strings.Split(os.Getenv(fakeDatabasesEnv), ",")
	switch {
	case strings.HasPrefix(sql, "SELECT"):
		os.Stdout.WriteString(strings.Join(existing, "\n") + "\n")
	case strings.Contains(sql, "IF NOT EXISTS"):
	case strings.HasPrefix(sql, "CREATE DATABASE"):
		for _, name := range append(existing, strings.Split(os.Getenv(fakeCreatedSinceEnv), ",")...) {
			if name != "" && strings.Contains(sql, name) {
				os.Stdout.WriteString("ERROR: database " + name + " exists\n")
				return 1
			}
		}
	}
	return 0
}

type startFixture struct {
	c      *Creator
	reg    *registry.Registry
	sup    *supervisor.Supervisor
	ran    int  // creations Go accepted
	refuse bool // Go refuses, as after shutdown began
}

func newStartFixture(t *testing.T) *startFixture {
	t.Helper()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	reg, err := registry.Open(filepath.Join(t.TempDir(), "bench.db"))
	if err != nil {
		t.Fatal(err)
	}
	sup := supervisor.New(log)
	t.Cleanup(func() { reg.Close() })
	t.Cleanup(sup.StopAll)
	f := &startFixture{reg: reg, sup: sup}
	siteMgr := &sites.Manager{Reg: reg, Log: log, InstalledPHP: func() []string { return nil }}
	f.c = &Creator{
		Root:     t.TempDir(),
		Sites:    siteMgr,
		Services: &services.Manager{Root: t.TempDir(), Reg: reg, Sup: sup, Log: log},
		Tasks:    &tasks.Registry{},
		Log:      log,
		Go: func(func()) bool { // never runs composer here
			if f.refuse {
				return false
			}
			f.ran++
			return true
		},
	}
	return f
}

func (f *startFixture) request(t *testing.T, name, database string) api.NewSiteRequest {
	t.Helper()
	return api.NewSiteRequest{Name: name, Dir: t.TempDir(), Stack: "blade", Testing: "pest", Database: database}
}

// runningInstance registers an instance of kind whose process runs and
// whose client is the fake one, reporting the databases in existing.
func (f *startFixture) runningInstance(t *testing.T, name, kind, existing string) api.Service {
	t.Helper()
	binDir := t.TempDir()
	client := map[string]string{"mysql": "mysql", "postgresql": "psql"}[kind]
	if runtime.GOOS == "windows" {
		client += ".exe"
	}
	if err := os.MkdirAll(filepath.Join(binDir, "bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	self, err := os.ReadFile(os.Args[0])
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(binDir, "bin", client), self, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv(fakeDatabasesEnv, existing)
	if err := f.reg.InsertService(registry.Service{Kind: kind, Channel: "1", Name: name, Port: 3307, BinaryDir: binDir, DataDir: t.TempDir()}); err != nil {
		t.Fatal(err)
	}
	if err := f.sup.Start(t.Context(), supervisor.Spec{Name: services.ProcName(name), Command: os.Args[0], Env: []string{"BENCH_FAKE_CHILD=sleep"}}); err != nil {
		t.Fatal(err)
	}
	svc, err := f.c.Services.Get(name)
	if err != nil || svc.State != "running" {
		t.Fatalf("instance %s: %+v, %v", name, svc, err)
	}
	return svc
}

func wantInputError(t *testing.T, err error, mentions ...string) {
	t.Helper()
	if !errors.As(err, new(InputError)) {
		t.Fatalf("err = %v, want an input error", err)
	}
	for _, m := range mentions {
		if !strings.Contains(err.Error(), m) {
			t.Errorf("%q does not mention %q", err, m)
		}
	}
}

func TestStartHandsTheCreationToGo(t *testing.T) {
	f := newStartFixture(t)
	if _, err := f.c.Start(t.Context(), f.request(t, "shop", "sqlite")); err != nil {
		t.Fatal(err)
	}
	if f.ran != 1 {
		t.Fatalf("Go got %d creations, want 1", f.ran)
	}
}

func TestStartRefusesANameTooLongForItsDatabaseInstance(t *testing.T) {
	f := newStartFixture(t)
	_, err := f.c.Start(t.Context(), f.request(t, strings.Repeat("a", 30), "mysql"))
	wantInputError(t, err, "too long", "32")
	if f.ran != 0 {
		t.Fatal("the creation started anyway")
	}
}

func TestStartAllowsALongNameWhenNoInstanceIsCreated(t *testing.T) {
	f := newStartFixture(t)
	if _, err := f.c.Start(t.Context(), f.request(t, strings.Repeat("a", 30), "sqlite")); err != nil {
		t.Fatalf("a long SQLite app name was refused: %v", err)
	}
}

func TestStartRefusesAnInstanceNameLeftFromAnEarlierApp(t *testing.T) {
	f := newStartFixture(t)
	if err := f.reg.InsertService(registry.Service{Kind: "mysql", Channel: "8.4", Name: "blog-mysql", Port: 3307, DataDir: t.TempDir()}); err != nil {
		t.Fatal(err)
	}
	_, err := f.c.Start(t.Context(), f.request(t, "blog", "mysql"))
	wantInputError(t, err, "blog-mysql", "delete it")
	if f.ran != 0 {
		t.Fatal("the creation started anyway")
	}
}

func TestStartRefusesALeftoverDatabaseInARunningInstance(t *testing.T) {
	f := newStartFixture(t)
	f.runningInstance(t, "shared", "mysql", "blog")
	r := f.request(t, "blog", "mysql")
	r.Service = "shared"
	_, err := f.c.Start(t.Context(), r)
	wantInputError(t, err, "blog", "shared", "drop it")
	if f.ran != 0 {
		t.Fatal("the creation started anyway")
	}
}

func TestCreatingTheDatabaseRefusesALeftoverOneOnBothEngines(t *testing.T) {
	for _, c := range []struct{ database, kind string }{{"mysql", "mysql"}, {"pgsql", "postgresql"}} {
		t.Run(c.kind, func(t *testing.T) {
			f := newStartFixture(t)
			f.runningInstance(t, "shared", c.kind, "blog")
			j := &job{c: f.c, out: io.Discard, dir: t.TempDir(),
				req: api.NewSiteRequest{Name: "blog", Database: c.database, Service: "shared"}}
			_, err := j.database(context.Background())
			if err == nil {
				t.Fatal("the app was put on the existing blog database")
			}
			for _, m := range []string{"blog", "shared", "drop it"} {
				if !strings.Contains(err.Error(), m) {
					t.Errorf("%q does not mention %q", err, m)
				}
			}
		})
	}
}

func TestStartRefusedByGoReportsShuttingDownAndLeavesNoRunningTask(t *testing.T) {
	f := newStartFixture(t)
	f.refuse = true
	if _, err := f.c.Start(t.Context(), f.request(t, "shop", "sqlite")); !errors.Is(err, tasks.ErrShuttingDown) {
		t.Fatalf("err = %v, want %v", err, tasks.ErrShuttingDown)
	}
	for _, task := range f.c.Tasks.List() {
		if task.State == api.TaskRunning {
			t.Fatalf("task %+v is left running", task)
		}
	}
}

func TestCreatingTheDatabaseFailsWhenItAppearsAfterTheCheckOnBothEngines(t *testing.T) {
	for _, c := range []struct{ database, kind string }{{"mysql", "mysql"}, {"pgsql", "postgresql"}} {
		t.Run(c.kind, func(t *testing.T) {
			f := newStartFixture(t)
			f.runningInstance(t, "shared", c.kind, "")
			t.Setenv(fakeCreatedSinceEnv, "blog")
			j := &job{c: f.c, out: io.Discard, dir: t.TempDir(),
				req: api.NewSiteRequest{Name: "blog", Database: c.database, Service: "shared"}}
			if _, err := j.database(context.Background()); err == nil {
				t.Fatal("the app was put on a blog database made after the check")
			}
		})
	}
}

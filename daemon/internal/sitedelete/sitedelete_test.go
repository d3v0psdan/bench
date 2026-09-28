package sitedelete

import (
	"context"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/d3v0psdan/bench/daemon/internal/api"
	"github.com/d3v0psdan/bench/daemon/internal/caddy"
	"github.com/d3v0psdan/bench/daemon/internal/registry"
	"github.com/d3v0psdan/bench/daemon/internal/sites"
	"github.com/d3v0psdan/bench/daemon/internal/tasks"
)

func TestDatabaseOfFindsTheBenchInstanceTheEnvPointsAt(t *testing.T) {
	svcs := []api.Service{
		{Name: "mysql", Service: "mysql", Port: 3307},
		{Name: "shop-postgresql", Service: "postgresql", Port: 5432},
	}
	cases := []struct {
		name string
		env  map[string]string
		want *api.SiteDatabase
	}{
		{"mysql", map[string]string{"DB_CONNECTION": "mysql", "DB_HOST": "127.0.0.1", "DB_PORT": "3307", "DB_DATABASE": "shop"}, &api.SiteDatabase{Service: "mysql", Name: "shop"}},
		{"mariadb speaks to a mysql instance", map[string]string{"DB_CONNECTION": "mariadb", "DB_PORT": "3307", "DB_DATABASE": "blog"}, &api.SiteDatabase{Service: "mysql", Name: "blog"}},
		{"postgres", map[string]string{"DB_CONNECTION": "pgsql", "DB_HOST": "localhost", "DB_PORT": "5432", "DB_DATABASE": "shop"}, &api.SiteDatabase{Service: "shop-postgresql", Name: "shop"}},
		{"sqlite", map[string]string{"DB_CONNECTION": "sqlite"}, nil},
		{"a remote host", map[string]string{"DB_CONNECTION": "mysql", "DB_HOST": "db.example.com", "DB_PORT": "3307", "DB_DATABASE": "shop"}, nil},
		{"no Bench instance on that port", map[string]string{"DB_CONNECTION": "mysql", "DB_PORT": "3306", "DB_DATABASE": "shop"}, nil},
		{"no database name", map[string]string{"DB_CONNECTION": "mysql", "DB_PORT": "3307"}, nil},
	}
	for _, c := range cases {
		got := databaseOf(c.env, svcs)
		if (got == nil) != (c.want == nil) || (got != nil && *got != *c.want) {
			t.Errorf("%s: got %+v, want %+v", c.name, got, c.want)
		}
	}
}

func TestOnlyAnInstanceMadeForTheSiteIsItsOwn(t *testing.T) {
	svcs := []api.Service{{Name: "shop-mysql", Service: "mysql"}, {Name: "mysql", Service: "mysql"}}
	if !ownInstance("shop", "shop-mysql", svcs) {
		t.Error("the wizard's shop-mysql belongs to shop")
	}
	if ownInstance("shop", "mysql", svcs) || ownInstance("blog", "shop-mysql", svcs) {
		t.Error("a shared or another app's instance must never count as the site's own")
	}
}

func TestSafeToTrashRefusesAnythingButAProjectFolder(t *testing.T) {
	root := t.TempDir()
	parked := filepath.Join(root, "Bench")
	project := filepath.Join(parked, "shop")
	if err := os.MkdirAll(project, 0o755); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(parked, "notes.txt")
	if err := os.WriteFile(file, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	benchRoot := filepath.Join(root, ".bench")
	inBench := filepath.Join(benchRoot, "data", "shop")
	if err := os.MkdirAll(inBench, 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(parked, "linked")
	if !makeLink(t, project, link) {
		link = "" // no way to make one here; the other cases still run
	}
	protected := []string{root, parked}
	if err := safeToTrash(project, protected, benchRoot); err != nil {
		t.Fatalf("a project folder must be allowed: %v", err)
	}
	for name, path := range map[string]string{
		"the parked folder":           parked,
		"a folder above the home":     root,
		"a folder inside Bench's own": inBench,
		"a symlink or junction":       link,
		"a file":                      file,
		"a relative path":             "shop",
		"a missing folder":            filepath.Join(parked, "gone"),
		"a drive root":                filepath.VolumeName(root) + string(filepath.Separator),
	} {
		if path == "" {
			continue
		}
		if err := safeToTrash(path, protected, benchRoot); err == nil {
			t.Errorf("%s (%s) must be refused", name, path)
		}
	}
}

// makeLink links link to target: a symlink, or on Windows without the
// symlink privilege a directory junction (which needs none).
func makeLink(t *testing.T, target, link string) bool {
	t.Helper()
	if os.Symlink(target, link) == nil {
		return true
	}
	if runtime.GOOS == "windows" {
		return exec.Command("cmd", "/c", "mklink", "/J", link, target).Run() == nil
	}
	return false
}

func TestOnlyWhatTheSiteAloneUsesIsOffered(t *testing.T) {
	svcs := []api.Service{{Name: "mysql", Service: "mysql"}, {Name: "shop-mysql", Service: "mysql"}}
	shop := api.Site{Name: "shop", AppName: "Shop"}
	blog := api.Site{Name: "blog", AppName: "Laravel"}
	news := api.Site{Name: "news", AppName: "Laravel"}
	own := &api.SiteDatabase{Service: "shop-mysql", Name: "shop"}
	shared := &api.SiteDatabase{Service: "mysql", Name: "laravel"}
	u := newUsage()
	u.add(shop, own)
	u.add(blog, shared)
	u.add(news, shared) // both kept Laravel's default DB_DATABASE and APP_NAME

	db, service, mailTag := u.ownParts(shop, own, svcs)
	if db == nil || service != "shop-mysql" || mailTag != "Shop" {
		t.Errorf("shop's own database, instance and inbox must be offered: %v %q %q", db, service, mailTag)
	}
	db, service, mailTag = u.ownParts(blog, shared, svcs)
	if db != nil || service != "" || mailTag != "" {
		t.Errorf("a database and inbox blog shares with news must never be offered: %v %q %q", db, service, mailTag)
	}
	if _, _, tag := newUsage().ownParts(api.Site{AppName: `a"b`}, nil, svcs); tag != "" {
		t.Error("an inbox name with a quote must not reach Mailpit's search")
	}
}

func TestDropDatabaseRefusesUnsafeNames(t *testing.T) {
	d := &Deleter{}
	for _, name := range []string{"mysql", "postgres", "shop`; DROP", `a"b`} {
		if err := d.dropDatabase(t.Context(), api.SiteDatabase{Service: "x", Name: name}); err == nil {
			t.Errorf("%q must be refused before anything runs", name)
		}
	}
}

type fakeRouter struct{}

func (fakeRouter) Ensure(context.Context) error              { return nil }
func (fakeRouter) Apply(context.Context, []caddy.Site) error { return nil }

type fakePools struct{}

func (fakePools) Ensure(_ context.Context, channel string) (string, error) {
	return "127.0.0.1:9000", nil
}

func TestDeletingAPinnedParkedSiteDropsItsSettingsButDoesNotIgnoreIt(t *testing.T) {
	ctx := t.Context()
	reg, err := registry.Open(filepath.Join(t.TempDir(), "bench.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { reg.Close() })
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	m := &sites.Manager{Reg: reg, Router: fakeRouter{}, Pools: fakePools{}, Log: log}
	parent := t.TempDir()
	if err := os.Mkdir(filepath.Join(parent, "app"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Park(ctx, parent); err != nil {
		t.Fatal(err)
	}
	if _, err := m.SetSitePHP(ctx, "app", "8.3"); err != nil {
		t.Fatal(err)
	}
	list, err := m.List(ctx)
	if err != nil {
		t.Fatal(err)
	}

	// No folder in the request: the trash step must not touch disk here.
	taskReg := &tasks.Registry{}
	d := &Deleter{Sites: m, Tasks: taskReg, Log: log}
	d.run(api.SiteDeletePlan{Site: list[0]}, api.SiteDeleteRequest{}, taskReg.Begin("site.delete", "Deleting app.test", "app", false))

	if got := taskReg.List()[0]; got.State != api.TaskDone {
		t.Fatalf("task = %+v", got)
	}
	rows, err := reg.Sites()
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 0 {
		t.Fatalf("settings rows left behind: %+v", rows)
	}
	// A folder restored from the trash must be served again.
	if list, err := m.List(ctx); err != nil || len(list) != 1 || list[0].PHPPinned != "" {
		t.Fatalf("list = %+v, err = %v", list, err)
	}
}

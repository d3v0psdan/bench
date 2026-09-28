package sites

import (
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/d3v0psdan/bench/daemon/internal/api"
	"github.com/d3v0psdan/bench/daemon/internal/caddy"
	"github.com/d3v0psdan/bench/daemon/internal/registry"
)

type fakeRouter struct {
	mu      sync.Mutex
	ensured int
	applied [][]caddy.Site
	failing error // Ensure returns this when set
}

func (f *fakeRouter) Ensure(ctx context.Context) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.ensured++
	return f.failing
}

func (f *fakeRouter) Apply(ctx context.Context, sites []caddy.Site) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.applied = append(f.applied, sites)
	return nil
}

func (f *fakeRouter) last(t *testing.T) []caddy.Site {
	t.Helper()
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.applied) == 0 {
		t.Fatal("no caddy config was applied")
	}
	return f.applied[len(f.applied)-1]
}

type fakePools struct {
	mu      sync.Mutex
	ensured []string
	fail    map[string]error
}

func (f *fakePools) Ensure(ctx context.Context, channel string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.fail[channel]; err != nil {
		return "", err
	}
	f.ensured = append(f.ensured, channel)
	return "127.0.0.1:90" + channel[len(channel)-1:], nil
}

func newTestManager(t *testing.T) (*Manager, *fakeRouter, *fakePools) {
	t.Helper()
	reg, err := registry.Open(filepath.Join(t.TempDir(), "bench.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { reg.Close() })
	router := &fakeRouter{}
	pools := &fakePools{fail: map[string]error{}}
	m := &Manager{
		Reg:    reg,
		Router: router,
		Pools:  pools,
		Log:    slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
	return m, router, pools
}

// projectDir creates dir/<name>(/public if withPublic) and returns dir/<name>.
func projectDir(t *testing.T, parent, name string, withPublic bool) string {
	t.Helper()
	p := filepath.Join(parent, name)
	target := p
	if withPublic {
		target = filepath.Join(p, "public")
	}
	if err := os.MkdirAll(target, 0o755); err != nil {
		t.Fatal(err)
	}
	return p
}

func routeByHost(t *testing.T, routes []caddy.Site, host string) caddy.Site {
	t.Helper()
	for _, r := range routes {
		if r.Host == host {
			return r
		}
	}
	t.Fatalf("no route for %s in %+v", host, routes)
	return caddy.Site{}
}

func TestParkServesSubdirsWithDocroot(t *testing.T) {
	m, router, pools := newTestManager(t)
	parent := t.TempDir()
	laravel := projectDir(t, parent, "shop", true)
	plain := projectDir(t, parent, "legacy", false)
	projectDir(t, parent, ".hidden", false) // must be ignored

	list, err := m.Park(context.Background(), parent)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 {
		t.Fatalf("want 2 sites, got %+v", list)
	}
	routes := router.last(t)
	if got := routeByHost(t, routes, "shop.test").Root; got != filepath.Join(laravel, "public") {
		t.Fatalf("shop docroot = %s", got)
	}
	if got := routeByHost(t, routes, "legacy.test").Root; got != plain {
		t.Fatalf("legacy docroot = %s", got)
	}
	// One pool ensure for the single default channel.
	if len(pools.ensured) != 1 || pools.ensured[0] != DefaultPHP {
		t.Fatalf("pools ensured = %v", pools.ensured)
	}
}

func TestLinkAndUnlink(t *testing.T) {
	m, router, _ := newTestManager(t)
	dir := projectDir(t, t.TempDir(), "myapp", true)

	list, err := m.Link(context.Background(), dir, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || list[0].Name != "myapp" || list[0].Kind != "linked" {
		t.Fatalf("list = %+v", list)
	}
	routeByHost(t, router.last(t), "myapp.test")

	if _, err := m.Unlink(context.Background(), "MyApp.test"); err != nil {
		t.Fatal(err) // name normalization: case + .test suffix
	}
	if got := router.last(t); len(got) != 0 {
		t.Fatalf("routes after unlink = %+v", got)
	}
}

func TestUnlinkedParkedSiteStaysHiddenUntilLinked(t *testing.T) {
	m, router, _ := newTestManager(t)
	ctx := context.Background()
	parent := t.TempDir()
	app := projectDir(t, parent, "app", false)
	projectDir(t, parent, "other", false)
	if _, err := m.Park(ctx, parent); err != nil {
		t.Fatal(err)
	}

	list, err := m.Unlink(ctx, "app")
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || list[0].Name != "other" {
		t.Fatalf("list after unlink = %+v", list)
	}
	if _, err := os.Stat(app); err != nil {
		t.Fatalf("unlink must leave the folder: %v", err)
	}
	list, err = m.Apply(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 {
		t.Fatalf("unlinked parked site came back on rescan: %+v", list)
	}

	list, err = m.Link(ctx, app, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 {
		t.Fatalf("list after relink = %+v", list)
	}
	routeByHost(t, router.last(t), "app.test")

	// The relink cleared the ignore: dropping the linked row leaves the
	// parked folder served again.
	list, err = m.Unlink(ctx, "app")
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 || list[0].Name != "app" || list[0].Kind != "parked" {
		t.Fatalf("list after unlinking the relinked site = %+v", list)
	}
}

func TestUnlinkParkedDropsItsSettingsRow(t *testing.T) {
	m, _, _ := newTestManager(t)
	ctx := context.Background()
	parent := t.TempDir()
	projectDir(t, parent, "app", false)
	if _, err := m.Park(ctx, parent); err != nil {
		t.Fatal(err)
	}
	// Pinning then clearing leaves a settings row with no pin.
	if _, err := m.SetSitePHP(ctx, "app", "8.3"); err != nil {
		t.Fatal(err)
	}
	if _, err := m.SetSitePHP(ctx, "app", ""); err != nil {
		t.Fatal(err)
	}

	if _, err := m.Unlink(ctx, "app"); err != nil {
		t.Fatal(err)
	}
	rows, err := m.Reg.Sites()
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 0 {
		t.Fatalf("rows after unlink = %+v", rows)
	}
}

func TestIgnoreKeyFoldsCaseOnlyWhereTheFileSystemDoes(t *testing.T) {
	path := filepath.Join("Sites", "App")
	for goos, want := range map[string]string{
		"windows": filepath.Join("sites", "app"),
		"darwin":  filepath.Join("sites", "app"),
		"linux":   path,
	} {
		if got := ignoreKey(goos, path); got != want {
			t.Errorf("ignoreKey(%s, %s) = %s, want %s", goos, path, got, want)
		}
	}
}

func TestRelinkWithOtherCasingClearsTheIgnore(t *testing.T) {
	if runtime.GOOS != "windows" && runtime.GOOS != "darwin" {
		t.Skip("paths are case-sensitive here")
	}
	m, _, _ := newTestManager(t)
	ctx := context.Background()
	parent := t.TempDir()
	app := projectDir(t, parent, "app", false)
	if _, err := m.Park(ctx, parent); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Unlink(ctx, "app"); err != nil {
		t.Fatal(err)
	}

	if _, err := m.Link(ctx, strings.ToUpper(app), "app"); err != nil {
		t.Fatal(err)
	}
	list, err := m.Unlink(ctx, "app")
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || list[0].Kind != "parked" {
		t.Fatalf("relinking with other casing must clear the ignore; list = %+v", list)
	}
}

func TestLinkedRowBeatsParkedSubdir(t *testing.T) {
	m, router, _ := newTestManager(t)
	parent := t.TempDir()
	projectDir(t, parent, "app", false)
	other := projectDir(t, t.TempDir(), "elsewhere", true)

	if _, err := m.Park(context.Background(), parent); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Link(context.Background(), other, "app"); err != nil {
		t.Fatal(err)
	}
	route := routeByHost(t, router.last(t), "app.test")
	if route.Root != filepath.Join(other, "public") {
		t.Fatalf("linked site must win over parked subdir; root = %s", route.Root)
	}
}

func TestIsolateParkedAndLinked(t *testing.T) {
	m, router, pools := newTestManager(t)
	parent := t.TempDir()
	projectDir(t, parent, "app", true)
	if _, err := m.Park(context.Background(), parent); err != nil {
		t.Fatal(err)
	}

	list, err := m.SetSitePHP(context.Background(), "app", "8.3")
	if err != nil {
		t.Fatal(err)
	}
	if list[0].PHP != "8.3" || list[0].PHPPinned != "8.3" {
		t.Fatalf("isolated site = %+v", list[0])
	}
	found := false
	for _, ch := range pools.ensured {
		if ch == "8.3" {
			found = true
		}
	}
	if !found {
		t.Fatalf("8.3 pool never ensured: %v", pools.ensured)
	}
	if up := routeByHost(t, router.last(t), "app.test").FastCGI; up != "127.0.0.1:903" {
		t.Fatalf("route upstream = %s", up)
	}

	// Clearing the pin returns to the default channel.
	list, err = m.SetSitePHP(context.Background(), "app", "")
	if err != nil {
		t.Fatal(err)
	}
	if list[0].PHP != DefaultPHP || list[0].PHPPinned != "" {
		t.Fatalf("unisolated site = %+v", list[0])
	}
}

func TestUseSwitchesGlobalDefault(t *testing.T) {
	m, _, pools := newTestManager(t)
	dir := projectDir(t, t.TempDir(), "app", true)
	if _, err := m.Link(context.Background(), dir, ""); err != nil {
		t.Fatal(err)
	}
	list, err := m.SetDefaultPHP(context.Background(), "8.5")
	if err != nil {
		t.Fatal(err)
	}
	if list[0].PHP != "8.5" {
		t.Fatalf("site after use 8.5 = %+v", list[0])
	}
	sawNew := false
	for _, ch := range pools.ensured {
		if ch == "8.5" {
			sawNew = true
		}
	}
	if !sawNew {
		t.Fatalf("8.5 pool never ensured: %v", pools.ensured)
	}
}

func TestProxySite(t *testing.T) {
	m, router, _ := newTestManager(t)
	list, err := m.Proxy(context.Background(), "myapi", "3000")
	if err != nil {
		t.Fatal(err)
	}
	if list[0].Kind != "proxy" || list[0].ProxyTo != "127.0.0.1:3000" {
		t.Fatalf("proxy site = %+v", list[0])
	}
	route := routeByHost(t, router.last(t), "myapi.test")
	if route.ProxyTo != "127.0.0.1:3000" || route.FastCGI != "" {
		t.Fatalf("proxy route = %+v", route)
	}
	if _, err := m.SetSitePHP(context.Background(), "myapi", "8.3"); err == nil {
		t.Fatal("isolating a proxy site must fail")
	}
}

func TestProxyTargetValidation(t *testing.T) {
	m, _, _ := newTestManager(t)
	// Bare port and host:port normalize; a portless host or bad port are
	// rejected at the boundary (not persisted as a silent 502).
	for target, want := range map[string]string{
		"3000":           "127.0.0.1:3000",
		"127.0.0.1:8080": "127.0.0.1:8080",
		"localhost:5173": "localhost:5173",
	} {
		list, err := m.Proxy(context.Background(), "api", target)
		if err != nil {
			t.Fatalf("Proxy(%q): %v", target, err)
		}
		if list[0].ProxyTo != want {
			t.Fatalf("Proxy(%q) → %q, want %q", target, list[0].ProxyTo, want)
		}
	}
	for _, bad := range []string{"example.com", "localhost", "127.0.0.1:99999", "127.0.0.1:abc", "  "} {
		if _, err := m.Proxy(context.Background(), "api", bad); err == nil {
			t.Fatalf("Proxy(%q) should be rejected", bad)
		}
	}
}

func TestMissingPHPSkipsSiteButServesOthers(t *testing.T) {
	m, router, pools := newTestManager(t)
	pools.fail["8.3"] = os.ErrNotExist
	parent := t.TempDir()
	projectDir(t, parent, "good", true)
	projectDir(t, parent, "broken", true)
	if _, err := m.Park(context.Background(), parent); err != nil {
		t.Fatal(err)
	}

	list, err := m.SetSitePHP(context.Background(), "broken", "8.3")
	if err != nil {
		t.Fatal(err)
	}
	byName := map[string]api.Site{}
	for _, s := range list {
		byName[s.Name] = s
	}
	if byName["broken"].Error == "" {
		t.Fatalf("broken site should carry an error: %+v", byName["broken"])
	}
	if byName["good"].Error != "" {
		t.Fatalf("good site should be clean: %+v", byName["good"])
	}
	routes := router.last(t)
	if len(routes) != 1 || routes[0].Host != "good.test" {
		t.Fatalf("routes = %+v", routes)
	}

	// List (no side effects) still reports the sticky error.
	list, err = m.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range list {
		if s.Name == "broken" && s.Error == "" {
			t.Fatal("List lost the apply error")
		}
	}
}

func TestNormalizeName(t *testing.T) {
	for in, want := range map[string]string{
		"MyApp":      "myapp",
		"shop.test":  "shop",
		" api.TEST ": "api",
		"my-app-2":   "my-app-2",
	} {
		got, err := NormalizeName(in)
		if err != nil || got != want {
			t.Errorf("NormalizeName(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	for _, bad := range []string{"", "-app", "app-", "my app", "sub.domain", "app!"} {
		if _, err := NormalizeName(bad); err == nil {
			t.Errorf("NormalizeName(%q) should fail", bad)
		}
	}
}

func TestFavoritesStarParkedSitesAndSurviveRelist(t *testing.T) {
	m, _, _ := newTestManager(t)
	parent := t.TempDir()
	projectDir(t, parent, "shop", true)
	projectDir(t, parent, "blog", true)
	if _, err := m.Park(context.Background(), parent); err != nil {
		t.Fatal(err)
	}

	list, err := m.SetFavorite("shop", true)
	if err != nil {
		t.Fatal(err)
	}
	favorite := map[string]bool{}
	for _, s := range list {
		favorite[s.Name] = s.Favorite
		if s.ParkedIn != parent {
			t.Errorf("%s parked_in = %q, want %q", s.Name, s.ParkedIn, parent)
		}
	}
	if !favorite["shop"] || favorite["blog"] {
		t.Fatalf("favorites = %v, want only shop", favorite)
	}

	if _, err := m.SetFavorite("shop", false); err != nil {
		t.Fatal(err)
	}
	list, err = m.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range list {
		if s.Favorite {
			t.Errorf("%s still a favorite after unstarring", s.Name)
		}
	}
	if _, err := m.SetFavorite("nope", true); err == nil {
		t.Error("starring a site that doesn't exist must fail")
	}
}

func TestEnvPortsFindsLocalServicePorts(t *testing.T) {
	got := envPorts(map[string]string{
		"DB_PORT": "3307", "REDIS_PORT": "6380", "MAIL_PORT": "2526",
		"MEILISEARCH_HOST": "http://127.0.0.1:7700", "AWS_ENDPOINT": "https://s3.amazonaws.com",
	})
	want := []int{3307, 6380, 2526, 7700}
	if !slices.Equal(got, want) {
		t.Fatalf("envPorts = %v, want %v (a remote S3 endpoint isn't a local service)", got, want)
	}
}

func TestLinkedSiteWithAMissingFolderIsFlagged(t *testing.T) {
	m, _, _ := newTestManager(t)
	project := projectDir(t, t.TempDir(), "shop", true)
	if _, err := m.Link(context.Background(), project, "shop"); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(project); err != nil {
		t.Fatal(err)
	}
	list, err := m.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || !list[0].PathMissing || list[0].Error == "" {
		t.Fatalf("a linked site whose folder is gone: %+v", list)
	}
}

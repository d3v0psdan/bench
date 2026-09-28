// Package sites is the orchestrator behind the site model: it turns the
// registry (linked sites, parked dirs, proxies, settings) into running
// infrastructure: PHP pools per referenced version and a live Caddy
// config. Every mutation re-applies the world; Apply is idempotent.
package sites

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"maps"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/d3v0psdan/bench/daemon/internal/api"
	"github.com/d3v0psdan/bench/daemon/internal/caddy"
	"github.com/d3v0psdan/bench/daemon/internal/dotenv"
	"github.com/d3v0psdan/bench/daemon/internal/registry"
)

// DefaultPHP is the global fallback when php.default is somehow unset.
const DefaultPHP = "8.4"

// Router is the Caddy surface the manager drives (interface for tests).
type Router interface {
	Ensure(ctx context.Context) error
	Apply(ctx context.Context, sites []caddy.Site) error
}

// PoolEnsurer starts a PHP pool for a channel and returns its upstream.
type PoolEnsurer interface {
	Ensure(ctx context.Context, channel string) (string, error)
}

// Manager owns the registry→infrastructure reconciliation.
type Manager struct {
	Reg    *registry.Registry
	Router Router
	Pools  PoolEnsurer
	Log    *slog.Logger
	// OnChange, if set, receives the fresh site list after every applied
	// mutation (feeds the "sites" WS event).
	OnChange func([]api.Site)

	mu          sync.Mutex
	lastErr     map[string]string // site name → last apply error
	everApplied bool              // routes have reached Caddy this run
}

var nameRe = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]*[a-z0-9])?$`)

// NormalizeName lowercases and validates a site name (one DNS label).
func NormalizeName(name string) (string, error) {
	n := strings.ToLower(strings.TrimSpace(name))
	n = strings.TrimSuffix(n, ".test")
	if !nameRe.MatchString(n) {
		return "", fmt.Errorf("invalid site name %q: use letters, digits and hyphens", name)
	}
	return n, nil
}

// site is one effective (servable) site before routing.
type site struct {
	name     string
	path     string
	kind     string // linked | parked | proxy
	pinned   string // per-site PHP channel ("" = default)
	proxyTo  string
	parkedIn string // the parked directory, for parked sites
}

// effectiveSites merges linked/proxy rows with parked-directory scans.
// Precedence: explicit rows win over parked subdirs of the same name;
// parked rows only carry settings for subdirs that still exist on disk.
func (m *Manager) effectiveSites() ([]site, string, error) {
	rows, err := m.Reg.Sites()
	if err != nil {
		return nil, "", err
	}
	parked, err := m.Reg.ParkedDirs()
	if err != nil {
		return nil, "", err
	}
	defaultPHP, err := m.Reg.Setting("php.default")
	if err != nil {
		return nil, "", err
	}
	if defaultPHP == "" {
		defaultPHP = DefaultPHP
	}
	ignored, err := m.Reg.IgnoredPaths()
	if err != nil {
		return nil, "", err
	}

	byName := map[string]site{}
	parkedPins := map[string]string{}
	for _, r := range rows {
		switch r.Kind {
		case "linked":
			byName[r.Name] = site{name: r.Name, path: r.Path, kind: "linked", pinned: r.PHPVersion}
		case "proxy":
			byName[r.Name] = site{name: r.Name, kind: "proxy", proxyTo: r.ProxyTo}
		case "parked":
			parkedPins[r.Name] = r.PHPVersion
		}
	}
	for _, dir := range parked {
		entries, err := os.ReadDir(dir)
		if err != nil {
			m.Log.Warn("cannot scan parked dir", "dir", dir, "err", err)
			continue
		}
		for _, e := range entries {
			if !e.IsDir() || strings.HasPrefix(e.Name(), ".") {
				continue
			}
			name, err := NormalizeName(e.Name())
			if err != nil {
				continue // unservable dir name; skip silently like Valet
			}
			if _, taken := byName[name]; taken {
				continue
			}
			path := filepath.Join(dir, e.Name())
			if ignored[ignoreKey(runtime.GOOS, path)] {
				continue // unlinked by the user
			}
			byName[name] = site{name: name, path: path, kind: "parked", pinned: parkedPins[name], parkedIn: dir}
		}
	}

	out := make([]site, 0, len(byName))
	for _, s := range byName {
		out = append(out, s)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].name < out[j].name })
	return out, defaultPHP, nil
}

// docroot serves path/public when it exists (Laravel), else the path.
func docroot(path string) string {
	pub := filepath.Join(path, "public")
	if fi, err := os.Stat(pub); err == nil && fi.IsDir() {
		return pub
	}
	return path
}

// Apply reconciles pools and Caddy with the registry. Sites whose PHP
// version is missing are skipped and reported via their Error field:
// one broken site must not take the rest down.
func (m *Manager) Apply(ctx context.Context) ([]api.Site, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.applyLocked(ctx)
}

func (m *Manager) applyLocked(ctx context.Context) ([]api.Site, error) {
	list, defaultPHP, err := m.effectiveSites()
	if err != nil {
		return nil, err
	}

	errs := map[string]string{}
	upstreams := map[string]string{} // channel → upstream (or "" after failure)
	var routes []caddy.Site
	for _, s := range list {
		if s.kind == "proxy" {
			routes = append(routes, caddy.Site{Host: s.name + ".test", ProxyTo: s.proxyTo})
			continue
		}
		channel := s.pinned
		if channel == "" {
			channel = defaultPHP
		}
		upstream, seen := upstreams[channel]
		if !seen {
			var perr error
			upstream, perr = m.Pools.Ensure(ctx, channel)
			if perr != nil {
				upstream = ""
				m.Log.Warn("php pool unavailable", "channel", channel, "err", perr)
				upstreams[channel] = ""
				errs[s.name] = perr.Error()
				continue
			}
			upstreams[channel] = upstream
		}
		if upstream == "" {
			errs[s.name] = fmt.Sprintf("php %s pool unavailable", channel)
			continue
		}
		routes = append(routes, caddy.Site{Host: s.name + ".test", Root: docroot(s.path), FastCGI: upstream})
	}

	// No sites ever configured → don't drag Caddy in (fresh installs
	// shouldn't download a web server for zero sites). But once routes
	// have been applied, removing the last site must clear them too.
	if len(list) > 0 || m.everApplied {
		if err := m.serve(ctx, routes); err != nil {
			// The registry change is saved; only serving failed (typically
			// another server holds port 443). Report that on every site and
			// still publish, so clients show the truth instead of a failed
			// save and a stale list.
			m.Log.Warn("sites saved but not served", "err", err)
			for _, s := range list {
				if _, ok := errs[s.name]; !ok {
					errs[s.name] = "not served: " + err.Error()
				}
			}
		} else {
			m.everApplied = true
		}
	}

	m.lastErr = errs
	out := m.toAPI(list, defaultPHP, errs)
	if m.OnChange != nil {
		m.OnChange(out)
	}
	return out, nil
}

// serve starts Caddy if needed and loads routes into it.
func (m *Manager) serve(ctx context.Context, routes []caddy.Site) error {
	if err := m.Router.Ensure(ctx); err != nil {
		return fmt.Errorf("starting caddy: %w", err)
	}
	return m.Router.Apply(ctx, routes)
}

// ParkedDirs lists the parked directories.
func (m *Manager) ParkedDirs() ([]string, error) { return m.Reg.ParkedDirs() }

// DefaultPHP reports the global default PHP channel.
func (m *Manager) DefaultPHP() (string, error) {
	v, err := m.Reg.Setting("php.default")
	if err != nil {
		return "", err
	}
	if v == "" {
		v = DefaultPHP
	}
	return v, nil
}

// List reports the current site model without touching infrastructure,
// merged with the errors from the last Apply.
func (m *Manager) List(ctx context.Context) ([]api.Site, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	list, defaultPHP, err := m.effectiveSites()
	if err != nil {
		return nil, err
	}
	return m.toAPI(list, defaultPHP, m.lastErr), nil
}

func (m *Manager) toAPI(list []site, defaultPHP string, errs map[string]string) []api.Site {
	favorites, err := m.favorites()
	if err != nil {
		// Favorites only order lists; a broken value mustn't hide sites.
		m.Log.Warn("reading favorite sites failed", "err", err)
	}
	out := make([]api.Site, 0, len(list)) // never null on the wire
	for _, s := range list {
		a := api.Site{
			Name:      s.name,
			Host:      s.name + ".test",
			URL:       "https://" + s.name + ".test",
			Path:      s.path,
			Kind:      s.kind,
			PHPPinned: s.pinned,
			ProxyTo:   s.proxyTo,
			Error:     errs[s.name],
			Favorite:  favorites[s.name],
			ParkedIn:  s.parkedIn,
		}
		if s.kind == "linked" {
			if _, err := os.Stat(s.path); errors.Is(err, fs.ErrNotExist) {
				a.PathMissing = true
				if a.Error == "" {
					a.Error = "its folder " + s.path + " was moved or deleted"
				}
			}
		}
		if s.kind != "proxy" {
			// A missing or unreadable .env just means no inbox name.
			if env, err := dotenv.Read(filepath.Join(s.path, ".env")); err == nil {
				a.AppName = env["APP_NAME"]
				a.EnvPorts = envPorts(env)
			}
			a.PHP = s.pinned
			if a.PHP == "" {
				a.PHP = defaultPHP
			}
		}
		out = append(out, a)
	}
	return out
}

// findByName locates an effective site (linked, parked or proxy).
func (m *Manager) findByName(name string) (site, error) {
	list, _, err := m.effectiveSites()
	if err != nil {
		return site{}, err
	}
	for _, s := range list {
		if s.name == name {
			return s, nil
		}
	}
	return site{}, fmt.Errorf("no site named %q", name)
}

// Park registers a directory whose subdirectories all serve as sites.
func (m *Manager) Park(ctx context.Context, path string) ([]api.Site, error) {
	abs, err := checkDir(path)
	if err != nil {
		return nil, err
	}
	if err := m.Reg.AddParkedDir(abs); err != nil {
		return nil, err
	}
	return m.Apply(ctx)
}

// Unpark removes a parked directory.
func (m *Manager) Unpark(ctx context.Context, path string) ([]api.Site, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	if err := m.Reg.RemoveParkedDir(abs); err != nil {
		return nil, err
	}
	return m.Apply(ctx)
}

// Link serves a single directory as name.test.
func (m *Manager) Link(ctx context.Context, path, name string) ([]api.Site, error) {
	abs, err := checkDir(path)
	if err != nil {
		return nil, err
	}
	if name == "" {
		name = filepath.Base(abs)
	}
	n, err := NormalizeName(name)
	if err != nil {
		return nil, err
	}
	if err := m.Reg.UpsertSite(registry.Site{Name: n, Path: abs, Kind: "linked"}); err != nil {
		return nil, err
	}
	// Linking a folder on purpose undoes an earlier unlink of it.
	if err := m.Reg.UnignorePath(ignoreKey(runtime.GOOS, abs)); err != nil {
		return nil, err
	}
	return m.Apply(ctx)
}

// Unlink stops serving a site and forgets it, leaving its folder alone.
// Linked and proxy sites lose their row; a parked site's folder is
// ignored from then on, so the parked-directory scan doesn't bring it
// back, and its settings (PHP pin) are dropped.
func (m *Manager) Unlink(ctx context.Context, name string) ([]api.Site, error) {
	n, err := NormalizeName(name)
	if err != nil {
		return nil, err
	}
	s, err := m.findByName(n)
	if err != nil {
		return nil, err
	}
	if s.kind == "parked" {
		if err := m.Reg.IgnorePath(ignoreKey(runtime.GOOS, s.path)); err != nil {
			return nil, err
		}
		return m.ForgetParkedSettings(ctx, n)
	}
	if err := m.Reg.DeleteSite(n); err != nil {
		return nil, err
	}
	return m.Apply(ctx)
}

// ForgetParkedSettings drops a parked site's settings (its PHP pin) and
// keeps serving it by the defaults. A parked site without settings is
// left as it is.
func (m *Manager) ForgetParkedSettings(ctx context.Context, name string) ([]api.Site, error) {
	n, err := NormalizeName(name)
	if err != nil {
		return nil, err
	}
	if err := m.Reg.DeleteParkedSettings(n); err != nil {
		return nil, err
	}
	return m.Apply(ctx)
}

// ignoreKey is how an unlinked folder is stored and matched: folded to
// lower case where the file system ignores case (Windows, and macOS by
// default), so linking c:\sites\app clears an ignore on C:\Sites\app.
func ignoreKey(goos, path string) string {
	path = filepath.Clean(path)
	if goos == "windows" || goos == "darwin" {
		return strings.ToLower(path)
	}
	return path
}

// Proxy serves name.test as a reverse proxy to a local address.
func (m *Manager) Proxy(ctx context.Context, name, target string) ([]api.Site, error) {
	n, err := NormalizeName(name)
	if err != nil {
		return nil, err
	}
	t, err := normalizeTarget(target)
	if err != nil {
		return nil, err
	}
	if err := m.Reg.UpsertSite(registry.Site{Name: n, Kind: "proxy", ProxyTo: t}); err != nil {
		return nil, err
	}
	return m.Apply(ctx)
}

// SetSitePHP pins (or clears with version "") a site's PHP version.
// Parked sites get a settings-carrier row on first isolation.
func (m *Manager) SetSitePHP(ctx context.Context, name, version string) ([]api.Site, error) {
	n, err := NormalizeName(name)
	if err != nil {
		return nil, err
	}
	s, err := m.findByName(n)
	if err != nil {
		return nil, err
	}
	if s.kind == "proxy" {
		return nil, fmt.Errorf("%s is a proxy site; it has no PHP version", n)
	}
	if s.kind == "parked" {
		if err := m.Reg.UpsertSite(registry.Site{Name: n, Path: s.path, Kind: "parked", PHPVersion: version}); err != nil {
			return nil, err
		}
	} else if err := m.Reg.SetSitePHP(n, version); err != nil {
		return nil, err
	}
	return m.Apply(ctx)
}

// NormalizePHP rewrites stored PHP versions (the default and site pins)
// to their channel: "8.4.12" becomes "8.4". Rows written before versions
// were stored by channel can hold a full version; channel reports the
// channel for a stored value, or false to leave it alone. Run it before
// the first Apply.
// One failing row doesn't stop the rest; the errors are joined.
func (m *Manager) NormalizePHP(channel func(string) (string, bool)) error {
	var errs []error
	def, err := m.Reg.Setting("php.default")
	if err != nil {
		errs = append(errs, err)
	}
	if c, ok := channel(def); err == nil && ok && c != def {
		if err := m.Reg.SetSetting("php.default", c); err != nil {
			errs = append(errs, err)
		} else {
			m.Log.Info("stored default PHP by channel", "was", def, "now", c)
		}
	}
	rows, err := m.Reg.Sites()
	if err != nil {
		return errors.Join(append(errs, err)...)
	}
	for _, s := range rows {
		c, ok := channel(s.PHPVersion)
		if !ok || c == s.PHPVersion {
			continue
		}
		if err := m.Reg.SetSitePHP(s.Name, c); err != nil {
			errs = append(errs, err)
			continue
		}
		m.Log.Info("stored site PHP pin by channel", "site", s.Name, "was", s.PHPVersion, "now", c)
	}
	return errors.Join(errs...)
}

// SetDefaultPHP switches the global default PHP version.
func (m *Manager) SetDefaultPHP(ctx context.Context, channel string) ([]api.Site, error) {
	if err := m.Reg.SetSetting("php.default", channel); err != nil {
		return nil, err
	}
	return m.Apply(ctx)
}

func checkDir(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	fi, err := os.Stat(abs)
	if err != nil {
		return "", fmt.Errorf("checking %s: %w", abs, err)
	}
	if !fi.IsDir() {
		return "", fmt.Errorf("%s is not a directory", abs)
	}
	return abs, nil
}

// normalizeTarget turns "3000" or "host:port" into a validated dialable
// address. Without the port check a typo like "example.com" (no port)
// becomes "127.0.0.1:example.com", which Caddy accepts at config-load
// time and only 502s per request, a silently broken site.
func normalizeTarget(target string) (string, error) {
	t := strings.TrimSpace(target)
	if t == "" {
		return "", fmt.Errorf("proxy target is required")
	}
	if !strings.Contains(t, ":") {
		t = "127.0.0.1:" + t
	}
	host, port, err := net.SplitHostPort(t)
	if err != nil {
		return "", fmt.Errorf("invalid proxy target %q: use a port or host:port", target)
	}
	n, err := strconv.Atoi(port)
	if err != nil || n < 1 || n > 65535 {
		return "", fmt.Errorf("invalid proxy target %q: port must be 1–65535", target)
	}
	if host == "" {
		host = "127.0.0.1"
	}
	return net.JoinHostPort(host, port), nil
}

// settingFavorites holds the favorite site names as a JSON array. A global
// setting rather than a per-site row, so parked sites need no carrier row;
// a name whose site is gone is simply never matched.
const settingFavorites = "sites.favorites"

func (m *Manager) favorites() (map[string]bool, error) {
	raw, err := m.Reg.Setting(settingFavorites)
	if err != nil || raw == "" {
		return map[string]bool{}, err
	}
	var names []string
	if err := json.Unmarshal([]byte(raw), &names); err != nil {
		return map[string]bool{}, fmt.Errorf("parsing %s: %w", settingFavorites, err)
	}
	set := make(map[string]bool, len(names))
	for _, n := range names {
		set[n] = true
	}
	return set, nil
}

// SetFavorite stars or unstars a site. Nothing is re-applied: favorites
// don't change routing, so the fresh list is only published.
func (m *Manager) SetFavorite(name string, favorite bool) ([]api.Site, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	n, err := NormalizeName(name)
	if err != nil {
		return nil, err
	}
	if _, err := m.findByName(n); err != nil {
		return nil, err
	}
	set, err := m.favorites()
	if err != nil {
		m.Log.Warn("replacing unreadable favorites", "err", err)
	}
	if favorite {
		set[n] = true
	} else {
		delete(set, n)
	}
	names := slices.Sorted(maps.Keys(set))
	raw, err := json.Marshal(names)
	if err != nil {
		return nil, err
	}
	if err := m.Reg.SetSetting(settingFavorites, string(raw)); err != nil {
		return nil, err
	}
	list, defaultPHP, err := m.effectiveSites()
	if err != nil {
		return nil, err
	}
	out := m.toAPI(list, defaultPHP, m.lastErr)
	if m.OnChange != nil {
		m.OnChange(out)
	}
	return out, nil
}

// portKeys are .env keys holding a bare port; urlKeys hold a URL whose
// port names a local service.
var (
	portKeys = []string{"DB_PORT", "REDIS_PORT", "MAIL_PORT"}
	urlKeys  = []string{"MEILISEARCH_HOST", "AWS_ENDPOINT"}
)

// envPorts lists the local service ports an app's .env points at.
func envPorts(env map[string]string) []int {
	var ports []int
	add := func(p string) {
		if n, err := strconv.Atoi(p); err == nil && n > 0 && !slices.Contains(ports, n) {
			ports = append(ports, n)
		}
	}
	for _, k := range portKeys {
		add(env[k])
	}
	for _, k := range urlKeys {
		if u, err := url.Parse(env[k]); err == nil && isLocalHost(u.Hostname()) {
			add(u.Port())
		}
	}
	return ports
}

func isLocalHost(h string) bool {
	return h == "127.0.0.1" || h == "localhost" || h == "::1"
}

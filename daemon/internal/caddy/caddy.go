// Package caddy supervises the bundled Caddy web server and applies site
// configuration live through its admin API (loopback :2019), no config
// file templating. Caddy starts from a minimal bootstrap config; the
// registry is the source of truth, so benchd rebuilds and re-applies the
// full config on every boot rather than relying on Caddy's autosave.
package caddy

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/d3v0psdan/bench/daemon/internal/binman"
	"github.com/d3v0psdan/bench/daemon/internal/portowner"
	"github.com/d3v0psdan/bench/daemon/internal/supervisor"
)

// AdminAddr is Caddy's admin API endpoint (loopback only).
const AdminAddr = "127.0.0.1:2019"

// Site is one routable host. Exactly one of FastCGI/ProxyTo is set:
// FastCGI serves Root via php_fastcgi semantics against that upstream,
// ProxyTo reverse-proxies the whole host to a local port.
type Site struct {
	Host    string // app.test
	Root    string // absolute path to the public/ dir (FastCGI sites)
	FastCGI string // fastcgi upstream: "unix//path.sock" or "127.0.0.1:9084"
	ProxyTo string // plain reverse-proxy target: "127.0.0.1:3000"
}

// Caddy manages the supervised Caddy instance.
type Caddy struct {
	Root string // bench home
	Bin  *binman.Manager
	Sup  *supervisor.Supervisor
	Log  *slog.Logger

	AdminAddr  string // "" = AdminAddr default (tests override)
	HTTPSPort  int    // 0 = 443
	HTTPClient *http.Client
}

func (c *Caddy) adminAddr() string {
	if c.AdminAddr != "" {
		return c.AdminAddr
	}
	return AdminAddr
}

func (c *Caddy) client() *http.Client {
	if c.HTTPClient != nil {
		return c.HTTPClient
	}
	return http.DefaultClient
}

// Dir is Caddy's state root (storage, bootstrap config) under bench home.
func (c *Caddy) Dir() string { return filepath.Join(c.Root, "caddy") }

// RootCAPath is where Caddy's local CA root certificate lands once the
// pki app provisions (bench-helper installs it into trust stores). Lives
// under the explicit storage root from buildConfig.
func (c *Caddy) RootCAPath() string {
	return filepath.Join(c.Dir(), "storage", "pki", "authorities", "local", "root.crt")
}

// Ensure installs (if needed) and starts the supervised Caddy, returning
// once its admin API answers.
func (c *Caddy) Ensure(ctx context.Context) error {
	b, err := c.Bin.Install(ctx, "caddy", "2")
	if err != nil {
		return fmt.Errorf("installing caddy: %w", err)
	}
	exe := "caddy"
	if runtime.GOOS == "windows" {
		exe += ".exe"
	}

	if err := os.MkdirAll(c.Dir(), 0o755); err != nil {
		return fmt.Errorf("creating caddy dir: %w", err)
	}
	bootstrap := filepath.Join(c.Dir(), "bootstrap.json")
	cfg, err := json.MarshalIndent(c.buildConfig(nil), "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(bootstrap, cfg, 0o644); err != nil {
		return fmt.Errorf("writing caddy bootstrap config: %w", err)
	}
	// The registry is the source of truth: benchd rebuilds and re-applies
	// the full config on every boot (site restore), so we start from our
	// bootstrap, not Caddy's autosave. Removing the autosave stops a stale
	// config (e.g. from an older Bench) resurfacing on --config load.
	_ = os.Remove(filepath.Join(c.Dir(), "config", "caddy", "autosave.json"))

	return c.Sup.Start(ctx, supervisor.Spec{
		Name:    "caddy",
		Command: filepath.Join(c.Bin.Dir("caddy", b.Version), exe),
		Args:    []string{"run", "--config", bootstrap},
		Env: []string{
			// Keep Caddy's storage (CA, certs, autosave) under bench home.
			"XDG_CONFIG_HOME=" + filepath.Join(c.Dir(), "config"),
			"XDG_DATA_HOME=" + filepath.Join(c.Dir(), "data"),
		},
		LogFile: filepath.Join(c.Root, "logs", "caddy.log"),
		Health: func(hctx context.Context) error {
			req, err := http.NewRequestWithContext(hctx, http.MethodGet, "http://"+c.adminAddr()+"/config/", nil)
			if err != nil {
				return err
			}
			resp, err := c.client().Do(req)
			if err != nil {
				return err
			}
			resp.Body.Close()
			if resp.StatusCode != http.StatusOK {
				return fmt.Errorf("caddy admin returned %s", resp.Status)
			}
			return nil
		},
	})
}

// Apply swaps the full Caddy config for the given sites via POST /load.
// Caddy autosaves the loaded config, so --resume restores it on restart.
func (c *Caddy) Apply(ctx context.Context, sites []Site) error {
	body, err := json.Marshal(c.buildConfig(sites))
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://"+c.adminAddr()+"/load", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.client().Do(req)
	if err != nil {
		return fmt.Errorf("applying caddy config: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return fmt.Errorf("caddy rejected config: %s: %s%s", resp.Status, strings.TrimSpace(string(msg)), c.bindCulprit(string(msg)))
	}
	c.Log.Info("caddy config applied", "sites", len(sites))
	return nil
}

// bindCulprit names who holds the HTTPS port when Caddy failed to bind
// it, so the error says "Laravel Herd holds port 443" rather than only
// "bind: address already in use".
func (c *Caddy) bindCulprit(msg string) string {
	if !strings.Contains(msg, "bind") {
		return ""
	}
	port := c.HTTPSPort
	if port == 0 {
		port = 443
	}
	p, ok, err := portowner.Listener(port)
	if err != nil || !ok {
		return ""
	}
	who := portowner.Describe(p)
	if portowner.IsHerd(p) {
		who = "Laravel Herd's " + who
	}
	return fmt.Sprintf(" (port %d is held by %s; run `bench doctor` for the fix)", port, who)
}

// Stop gracefully stops Caddy via its admin API, then reaps the
// supervised process (Windows has no SIGTERM; this is the graceful path).
func (c *Caddy) Stop(ctx context.Context) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://"+c.adminAddr()+"/stop", nil)
	if err == nil {
		if resp, err := c.client().Do(req); err == nil {
			resp.Body.Close()
		}
	}
	c.Sup.Stop("caddy")
}

// buildConfig produces the full Caddy JSON config document. Untyped maps:
// Caddy's config schema is enormous and we need a sliver of it; the shape
// is locked by unit tests and by Caddy validating every /load.
func (c *Caddy) buildConfig(sites []Site) map[string]any {
	httpsPort := c.HTTPSPort
	if httpsPort == 0 {
		httpsPort = 443
	}

	routes := make([]any, 0, len(sites))
	for _, s := range sites {
		routes = append(routes, siteRoute(s))
	}

	return map[string]any{
		"admin": map[string]any{"listen": c.adminAddr()},
		"storage": map[string]any{
			"module": "file_system",
			"root":   filepath.Join(c.Dir(), "storage"),
		},
		"apps": map[string]any{
			"http": map[string]any{
				"https_port": httpsPort,
				"servers": map[string]any{
					"bench": map[string]any{
						// Loopback only: an empty host (":443") binds every
						// interface, exposing local dev sites to the LAN.
						// Bench's whole trust model is loopback (benchd API,
						// Caddy admin), so the site listener must match.
						"listen": []any{
							fmt.Sprintf("127.0.0.1:%d", httpsPort),
							fmt.Sprintf("[::1]:%d", httpsPort),
						},
						// No HTTP→HTTPS redirect vhost: Bench is HTTPS-only,
						// and the redirect would bind port 80 (needs
						// privilege on Unix; conflicts on reload). *.test is
						// reached over https directly.
						"automatic_https": map[string]any{"disable_redirects": true},
						"routes":          routes,
					},
				},
			},
			"tls": map[string]any{
				"automation": map[string]any{
					"policies": []any{map[string]any{
						// Any *.test host gets a cert from the local CA at
						// first request: parked sites work with zero config.
						"subjects":  []any{"*.test"},
						"on_demand": true,
						"issuers":   []any{map[string]any{"module": "internal"}},
					}},
				},
			},
			"pki": map[string]any{
				"certificate_authorities": map[string]any{
					// Trust-store install needs elevation and is owned by
					// bench-helper, not Caddy.
					"local": map[string]any{"install_trust": false},
				},
			},
		},
	}
}

// siteRoute is the JSON expansion of Caddyfile `php_fastcgi` (rewrite to
// index.php via try_files, *.php to the pool, file_server for the rest)
// or a plain reverse_proxy for proxy sites.
func siteRoute(s Site) map[string]any {
	var handlers []any
	switch {
	case s.ProxyTo != "":
		handlers = []any{map[string]any{
			"handler":   "reverse_proxy",
			"upstreams": []any{map[string]any{"dial": s.ProxyTo}},
		}}
	default:
		handlers = []any{map[string]any{
			"handler": "subroute",
			"routes": []any{
				map[string]any{
					"handle": []any{map[string]any{
						"handler": "vars",
						// Caddy joins root+path with forward slashes; PHP on
						// Windows accepts them.
						"root": filepath.ToSlash(s.Root),
					}},
				},
				map[string]any{
					"match": []any{map[string]any{
						"file": map[string]any{
							"try_files":  []any{"{http.request.uri.path}", "{http.request.uri.path}/index.php", "index.php"},
							"split_path": []any{".php"},
						},
					}},
					"handle": []any{map[string]any{
						"handler": "rewrite",
						"uri":     "{http.matchers.file.relative}",
					}},
				},
				map[string]any{
					"match": []any{map[string]any{"path": []any{"*.php"}}},
					"handle": []any{map[string]any{
						"handler": "reverse_proxy",
						"transport": map[string]any{
							"protocol":   "fastcgi",
							"split_path": []any{".php"},
						},
						"upstreams": []any{map[string]any{"dial": s.FastCGI}},
					}},
				},
				map[string]any{
					"handle": []any{map[string]any{"handler": "file_server"}},
				},
			},
		}}
	}
	return map[string]any{
		"match":    []any{map[string]any{"host": []any{s.Host}}},
		"handle":   handlers,
		"terminal": true,
	}
}

// WaitAdmin polls the admin API until it answers or ctx ends. Used by
// callers that need Caddy ready without going through Ensure.
func (c *Caddy) WaitAdmin(ctx context.Context) error {
	for {
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "http://"+c.adminAddr()+"/config/", nil)
		resp, err := c.client().Do(req)
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return nil
			}
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(100 * time.Millisecond):
		}
	}
}

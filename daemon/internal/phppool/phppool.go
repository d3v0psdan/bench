// Package phppool runs one FastCGI pool per PHP version: php-fpm on a
// Unix socket (macOS/Linux), php-cgi on a loopback port (Windows has no
// fpm). Pools are started on demand (when a site actually uses the
// version) and supervised like every other child.
package phppool

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"time"

	"github.com/d3v0psdan/bench/daemon/internal/api"
	"github.com/d3v0psdan/bench/daemon/internal/binman"
	"github.com/d3v0psdan/bench/daemon/internal/supervisor"
)

// Pools starts and tracks per-version FastCGI pools.
type Pools struct {
	Root string // bench home
	Bin  *binman.Manager
	Sup  *supervisor.Supervisor
	Log  *slog.Logger
}

// ErrNotInstalled reports the requested PHP version has no installed build.
type ErrNotInstalled struct{ Channel string }

func (e ErrNotInstalled) Error() string {
	return fmt.Sprintf("PHP %s isn't installed; install it on the Runtimes page (or with bench php:install %s)", e.Channel, e.Channel)
}

// Ensure starts (if not already running) the pool for a catalog channel
// like "8.4" and returns its Caddy fastcgi upstream address:
// "127.0.0.1:9084" on Windows, "unix/<socket path>" elsewhere.
func (p *Pools) Ensure(ctx context.Context, channel string) (string, error) {
	b, err := p.Bin.Resolve("php", channel)
	if err != nil {
		return "", err
	}
	if !p.Bin.IsInstalled("php", b.Version) {
		return "", ErrNotInstalled{Channel: channel}
	}

	runDir := filepath.Join(p.Root, "run", "php-"+b.Channel)
	if err := os.MkdirAll(runDir, 0o755); err != nil {
		return "", fmt.Errorf("creating pool run dir: %w", err)
	}
	spec, upstream, err := p.poolSpec(b, runDir)
	if err != nil {
		return "", err
	}
	if err := p.Sup.Start(ctx, spec); err != nil {
		return "", fmt.Errorf("starting php %s pool: %w", b.Channel, err)
	}
	return upstream, nil
}

// Command runs an installed channel's php CLI. Argv is the executable
// (plus, on Windows, "-c <ini>" loading the extensions the pools use: a
// bare php.exe has no openssl, zip or mbstring, so composer fails). Env
// puts that php first on PATH, and on Windows points PHPRC at the same
// ini, so php processes it starts (composer's "@php artisan", a Vite
// plugin running artisan) get the same php.
type Command struct {
	Argv []string
	Env  []string
}

// CLI is the Command for a channel; ErrNotInstalled when it isn't.
func (p *Pools) CLI(channel string) (Command, error) {
	b, err := p.Bin.Resolve("php", channel)
	if err != nil {
		return Command{}, err
	}
	if !p.Bin.IsInstalled("php", b.Version) {
		return Command{}, ErrNotInstalled{Channel: channel}
	}
	runDir := filepath.Join(p.Root, "run", "php-"+b.Channel)
	if err := os.MkdirAll(runDir, 0o755); err != nil {
		return Command{}, fmt.Errorf("creating php run dir: %w", err)
	}
	installDir := p.Bin.Dir("php", b.Version)
	cmd, err := cliCommand(installDir, runDir)
	if err != nil {
		return Command{}, err
	}
	cmd.Env = append(cmd.Env, "PATH="+installDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	return cmd, nil
}

// Remove stops a channel's pool, if running, and uninstalls its build.
func (p *Pools) Remove(channel string) error {
	b, err := p.Bin.Resolve("php", channel)
	if err != nil {
		return err
	}
	p.Sup.Stop(PoolName(b.Channel))
	return p.Bin.Uninstall("php", b.Version)
}

// PoolName is the supervisor process name for a channel.
func PoolName(channel string) string { return "php-" + channel }

// portFor maps a PHP channel to a stable loopback port: "8.4" → 23684.
// Deterministic across restarts, collision-free across PHP versions, and
// deliberately outside the 9000–9100 range other PHP tools (Laragon,
// Valet ports, IDE fastcgi defaults) squat on: Windows allows silent
// double-binds without SO_EXCLUSIVEADDRUSE, so a contested port serves
// the wrong PHP without any error.
func portFor(channel string) (int, error) {
	digits := strings.ReplaceAll(channel, ".", "")
	if len(digits) != 2 || digits[0] < '0' || digits[0] > '9' || digits[1] < '0' || digits[1] > '9' {
		return 0, fmt.Errorf("unexpected php channel %q", channel)
	}
	return 23600 + int(digits[0]-'0')*10 + int(digits[1]-'0'), nil
}

func dialCheck(network, addr string) func(context.Context) error {
	return func(ctx context.Context) error {
		var d net.Dialer
		conn, err := d.DialContext(ctx, network, addr)
		if err != nil {
			return err
		}
		return conn.Close()
	}
}

// fpmConf is the minimal php-fpm pool config (Unix only). clear_env=no so
// per-site env injection (VITE_*, XDEBUG_*) can arrive via fastcgi params
// and the daemon environment in later phases.
func fpmConf(root, channel, sock string) string {
	return fmt.Sprintf(`[global]
error_log = %s
daemonize = no

[www]
listen = %s
pm = dynamic
pm.max_children = 8
pm.start_servers = 1
pm.min_spare_servers = 1
pm.max_spare_servers = 3
clear_env = no
catch_workers_output = yes
`, filepath.Join(root, "logs", "php-"+channel+"-fpm.log"), sock)
}

// windowsIni enables the DLL extensions Laravel needs: the official
// php.net zips ship them in ext/ but activate none by default (the
// static-php Unix builds compile everything in, so no ini is needed there).
func windowsIni(installDir string) string {
	exts := []string{
		"bz2", "curl", "fileinfo", "gd", "gettext", "gmp", "intl", "mbstring",
		"exif", "mysqli", "openssl", "pdo_mysql", "pdo_pgsql", "pdo_sqlite",
		"pgsql", "shmop", "soap", "sockets", "sodium", "sqlite3", "zip",
	}
	var b strings.Builder
	fmt.Fprintf(&b, "; generated by benchd; edits are overwritten on pool start\n")
	fmt.Fprintf(&b, "extension_dir = \"%s\"\n", filepath.Join(installDir, "ext"))
	for _, e := range exts {
		fmt.Fprintf(&b, "extension=%s\n", e)
	}
	// PHP 8.5 builds OPcache in; loading it again only prints a warning
	// on every start, so it is loaded only where it ships as a DLL.
	if _, err := os.Stat(filepath.Join(installDir, "ext", "php_opcache.dll")); err == nil {
		b.WriteString("zend_extension=opcache\n")
	}
	b.WriteString(`opcache.enable = 1
memory_limit = 512M
post_max_size = 100M
upload_max_filesize = 100M
error_reporting = E_ALL
display_errors = On
`)
	return b.String()
}

// Info describes an installed channel for the PHP detail sheet: where it
// lives, which process serves sites, its php.ini (Windows; the static
// Unix builds compile their settings in) and the loaded extensions.
func (p *Pools) Info(ctx context.Context, channel string) (api.PHPInfo, error) {
	cmd, err := p.CLI(channel)
	if err != nil {
		return api.PHPInfo{}, err
	}
	b, err := p.Bin.Resolve("php", channel)
	if err != nil {
		return api.PHPInfo{}, err
	}
	info := api.PHPInfo{Channel: b.Channel, Version: b.Version, Dir: p.Bin.Dir("php", b.Version), Server: serverName}
	if runtime.GOOS == "windows" {
		info.Ini = filepath.Join(p.Root, "run", "php-"+b.Channel, "php.ini")
	}
	mctx, cancel := context.WithTimeout(ctx, modulesTimeout)
	defer cancel()
	out, err := supervisor.Exec(mctx, supervisor.Spec{Command: cmd.Argv[0], Args: append(cmd.Argv[1:], "-m"), Env: cmd.Env})
	if err != nil {
		return api.PHPInfo{}, fmt.Errorf("listing php %s extensions: %w", channel, err)
	}
	info.Extensions = parseModules(out)
	return info, nil
}

// modulesTimeout bounds `php -m`, which answers at once.
const modulesTimeout = 10 * time.Second

// parseModules reads `php -m` output: one module per line under the
// "[PHP Modules]" and "[Zend Modules]" headings, deduplicated and sorted.
func parseModules(out string) []string {
	seen := map[string]bool{}
	var mods []string
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "[") || seen[strings.ToLower(line)] {
			continue
		}
		seen[strings.ToLower(line)] = true
		mods = append(mods, line)
	}
	slices.SortFunc(mods, func(a, b string) int { return strings.Compare(strings.ToLower(a), strings.ToLower(b)) })
	return mods
}

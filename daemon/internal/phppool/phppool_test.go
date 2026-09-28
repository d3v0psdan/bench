package phppool

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/d3v0psdan/bench/daemon/internal/binman"
	"github.com/d3v0psdan/bench/daemon/internal/supervisor"
)

func TestPortFor(t *testing.T) {
	for channel, want := range map[string]int{"8.3": 23683, "8.4": 23684, "8.5": 23685, "7.4": 23674} {
		got, err := portFor(channel)
		if err != nil || got != want {
			t.Errorf("portFor(%s) = %d, %v; want %d", channel, got, err, want)
		}
	}
	for _, bad := range []string{"", "8", "8.4.1", "weird"} {
		if _, err := portFor(bad); err == nil {
			t.Errorf("portFor(%q) should fail", bad)
		}
	}
}

func TestEnsureRequiresInstalledBuild(t *testing.T) {
	p := &Pools{
		Root: t.TempDir(),
		Log:  slog.New(slog.NewTextHandler(io.Discard, nil)),
		Sup:  supervisor.New(slog.New(slog.NewTextHandler(io.Discard, nil))),
		Bin: &binman.Manager{
			Root: t.TempDir(),
			Log:  slog.New(slog.NewTextHandler(io.Discard, nil)),
			Manifests: map[string]binman.Manifest{
				"php": {Name: "php", Builds: []binman.Build{{
					Name: "php", Version: "8.4.99", Channel: "8.4",
					OS: runtime.GOOS, Arch: runtime.GOARCH,
					Downloads: []binman.Download{{URL: "https://example.invalid/x", SHA256: strings.Repeat("ab", 32), Archive: "zip"}},
				}}},
			},
		},
	}
	_, err := p.Ensure(context.Background(), "8.4")
	var notInstalled ErrNotInstalled
	if !errors.As(err, &notInstalled) || notInstalled.Channel != "8.4" {
		t.Fatalf("want ErrNotInstalled{8.4}, got %v", err)
	}
	if _, err := p.Ensure(context.Background(), "9.9"); err == nil {
		t.Fatal("unknown channel must error")
	}
}

func TestFpmConfShape(t *testing.T) {
	conf := fpmConf("/home/u/.bench", "8.4", "/home/u/.bench/run/php-8.4/php.sock")
	for _, want := range []string{"daemonize = no", "listen = /home/u/.bench/run/php-8.4/php.sock", "clear_env = no", "pm = dynamic"} {
		if !strings.Contains(conf, want) {
			t.Errorf("fpm.conf missing %q:\n%s", want, conf)
		}
	}
}

func TestWindowsIniShape(t *testing.T) {
	ini := windowsIni(`C:\Users\u\.bench\bin\php\8.4.23`)
	for _, want := range []string{"extension=openssl", "extension=mbstring", "extension=pdo_sqlite", "extension=curl", "ext"} {
		if !strings.Contains(ini, want) {
			t.Errorf("php.ini missing %q", want)
		}
	}
}

func TestWindowsIniLoadsOPcacheOnlyWhenItShipsAsADLL(t *testing.T) {
	builtIn := t.TempDir() // PHP 8.5: OPcache compiled in, no DLL
	if strings.Contains(windowsIni(builtIn), "zend_extension=opcache") {
		t.Error("loading a built-in OPcache again only warns on every start")
	}
	shipped := t.TempDir()
	if err := os.MkdirAll(filepath.Join(shipped, "ext"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(shipped, "ext", "php_opcache.dll"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(windowsIni(shipped), "zend_extension=opcache") {
		t.Error("PHP 8.4 and older ship OPcache as a DLL that must be loaded")
	}
}

func TestParseModulesListsEachModuleOnce(t *testing.T) {
	out := "[PHP Modules]\nCore\nmbstring\nZend OPcache\nopenssl\n\n[Zend Modules]\nZend OPcache\n"
	got := parseModules(out)
	want := []string{"Core", "mbstring", "openssl", "Zend OPcache"}
	if !slices.Equal(got, want) {
		t.Fatalf("parseModules = %v, want %v (headings skipped, OPcache listed once)", got, want)
	}
}

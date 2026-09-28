//go:build windows

package phppool

import (
	"fmt"
	"net"
	"os"
	"path/filepath"

	"github.com/d3v0psdan/bench/daemon/internal/binman"
	"github.com/d3v0psdan/bench/daemon/internal/supervisor"
)

// serverName is the process that serves sites on this OS.
const serverName = "php-cgi"

// cliCommand is php.exe with its own ini beside the pool's (the CLI ini
// differs only in not being rewritten by pool starts).
func cliCommand(installDir, runDir string) (Command, error) {
	ini := filepath.Join(runDir, "php-cli.ini")
	if err := os.WriteFile(ini, []byte(windowsIni(installDir)), 0o644); err != nil {
		return Command{}, fmt.Errorf("writing php-cli.ini: %w", err)
	}
	return Command{Argv: []string{filepath.Join(installDir, "php.exe"), "-c", ini}, Env: []string{"PHPRC=" + ini}}, nil
}

// poolSpec on Windows runs php-cgi.exe as a FastCGI server on a loopback
// port with a generated php.ini.
//
// Shortcut: one php-cgi process per version = one PHP request at a time
// (Windows php-cgi can't fork workers; static files are served by Caddy so
// this mostly bites parallel XHR). Upgrade path: N processes on
// consecutive ports load-balanced by Caddy's upstream list.
func (p *Pools) poolSpec(b binman.Build, runDir string) (supervisor.Spec, string, error) {
	port, err := portFor(b.Channel)
	if err != nil {
		return supervisor.Spec{}, "", err
	}
	// Preflight: if the pool isn't ours-and-running, the port must be
	// free. Go's listener binds exclusively on Windows, so this catches
	// foreign php-cgi squatters that php-cgi itself would double-bind
	// beside silently.
	if _, ok := p.Sup.Status(PoolName(b.Channel)); !ok {
		probe, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
		if err != nil {
			return supervisor.Spec{}, "", fmt.Errorf("php %s pool port %d is in use by another process: %w", b.Channel, port, err)
		}
		probe.Close()
	}
	installDir := p.Bin.Dir("php", b.Version)
	ini := filepath.Join(runDir, "php.ini")
	if err := os.WriteFile(ini, []byte(windowsIni(installDir)), 0o644); err != nil {
		return supervisor.Spec{}, "", fmt.Errorf("writing php.ini: %w", err)
	}
	addr := fmt.Sprintf("127.0.0.1:%d", port)
	return supervisor.Spec{
		Name:    PoolName(b.Channel),
		Command: filepath.Join(installDir, "php-cgi.exe"),
		Args:    []string{"-b", addr, "-c", ini},
		// Never self-exit after N requests; the supervisor owns lifecycle.
		Env:     []string{"PHP_FCGI_MAX_REQUESTS=0"},
		Dir:     installDir, // DLLs beside php-cgi.exe resolve from cwd
		LogFile: filepath.Join(p.Root, "logs", "php-"+b.Channel+"-cgi.log"),
		Health:  dialCheck("tcp", addr),
	}, addr, nil
}

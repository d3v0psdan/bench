//go:build !windows

package phppool

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/d3v0psdan/bench/daemon/internal/binman"
	"github.com/d3v0psdan/bench/daemon/internal/supervisor"
)

// serverName is the process that serves sites on this OS.
const serverName = "php-fpm"

// cliCommand is the static php CLI, which has its extensions compiled in.
func cliCommand(installDir, _ string) (Command, error) {
	return Command{Argv: []string{filepath.Join(installDir, "php")}}, nil
}

// poolSpec on macOS/Linux runs the static php-fpm build in the foreground
// on a Unix socket with a generated pool config.
func (p *Pools) poolSpec(b binman.Build, runDir string) (supervisor.Spec, string, error) {
	sock := filepath.Join(runDir, "php.sock")
	conf := filepath.Join(runDir, "fpm.conf")
	if err := os.WriteFile(conf, []byte(fpmConf(p.Root, b.Channel, sock)), 0o644); err != nil {
		return supervisor.Spec{}, "", fmt.Errorf("writing fpm config: %w", err)
	}
	// Clear a stale socket from a crashed daemon before a cold start, but
	// NOT when the pool is already running (Ensure calls poolSpec on every
	// site mutation; Sup.Start then joins the live pool without respawning,
	// so removing the live socket here would orphan the path and 502 every
	// PHP site on this version). Mirror the Windows port preflight guard.
	if _, running := p.Sup.Status(PoolName(b.Channel)); !running {
		_ = os.Remove(sock)
	}
	return supervisor.Spec{
		Name:    PoolName(b.Channel),
		Command: filepath.Join(p.Bin.Dir("php", b.Version), "php-fpm"),
		Args:    []string{"-F", "-y", conf},
		LogFile: filepath.Join(p.Root, "logs", "php-"+b.Channel+"-fpm.log"),
		Health:  dialCheck("unix", sock),
	}, "unix/" + sock, nil
}

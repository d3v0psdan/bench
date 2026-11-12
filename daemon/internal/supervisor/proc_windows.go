//go:build windows

package supervisor

import (
	"os"
	"syscall"
)

// CREATE_NO_WINDOW: php-cgi and caddy are console apps that would
// otherwise flash console windows when spawned from the windowless daemon.
const createNoWindow = 0x08000000

func sysProcAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{HideWindow: true, CreationFlags: createNoWindow}
}

// Shortcut: no graceful shutdown on Windows (no SIGTERM equivalent);
// callers needing grace (Caddy) stop via their own APIs first. Upgrade
// path: Job Objects with kill-on-close, which also reaps orphans if
// benchd crashes.
func signalStop(p *os.Process) { _ = p.Kill() }

// shortcut: Kill ends the direct child only; its own children keep running
// (node under npm.cmd's cmd.exe, tools composer starts), so a cancelled
// New Laravel app can leave them behind. Upgrade to a Job Object per child
// before relying on cancel to clean up on Windows.
func killHard(p *os.Process) { _ = p.Kill() }

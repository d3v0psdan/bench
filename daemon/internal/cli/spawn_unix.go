//go:build !windows

package cli

import (
	"os/exec"
	"syscall"
)

// spawnDetached starts exe in its own session so it survives the CLI (and
// any parent terminal) exiting.
func spawnDetached(exe string) error {
	cmd := exec.Command(exe)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return err
	}
	return cmd.Process.Release()
}

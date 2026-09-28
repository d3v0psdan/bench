//go:build !windows

package desktop

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"syscall"
)

// start launches argv in its own session, so it outlives benchd.
func start(argv []string, dir string) error {
	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.Dir = dir
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("starting %s: %w", filepath.Base(argv[0]), err)
	}
	// Reap it when it exits so short-lived openers (`open -a`) don't
	// linger as zombies under benchd.
	go func() { _ = cmd.Wait() }()
	return nil
}

// startTerminal is start: terminal emulators open their own window.
func startTerminal(argv []string, dir string) error { return start(argv, dir) }

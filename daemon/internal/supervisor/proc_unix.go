//go:build !windows

package supervisor

import (
	"os"
	"syscall"
)

// Children get their own process group so terminal signals aimed at benchd
// don't reach them, and so stop can signal the whole group (php-fpm
// workers, future artisan-dev children).
func sysProcAttr() *syscall.SysProcAttr { return &syscall.SysProcAttr{Setpgid: true} }

func signalStop(p *os.Process) {
	if err := syscall.Kill(-p.Pid, syscall.SIGTERM); err != nil {
		_ = p.Signal(syscall.SIGTERM) // best effort; exit is detected via Wait
	}
}

func killHard(p *os.Process) {
	if err := syscall.Kill(-p.Pid, syscall.SIGKILL); err != nil {
		_ = p.Kill()
	}
}

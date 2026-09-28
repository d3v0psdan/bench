// Package portowner names the process listening on a TCP port and lists
// running processes, so port conflicts can say "Laravel Herd (nginx.exe)
// holds :443" instead of surfacing a raw bind error.
package portowner

import (
	"path/filepath"
	"strconv"
	"strings"
)

// Process is a running process as far as the OS lets us see it.
type Process struct {
	PID  int    `json:"pid"`
	Name string `json:"name"`           // executable base name
	Path string `json:"path,omitempty"` // full image path when readable
}

// Listener returns the process listening on TCP port (any local address).
// ok=false with a nil error means nothing listens there. When the port is
// held but the owner can't be read (another user's process on Linux),
// ok is true and the Process is empty or PID-only.
func Listener(port int) (p Process, ok bool, err error) { return listener(port) }

// Processes lists running processes (names and, where readable, paths).
func Processes() ([]Process, error) { return processes() }

// herdRoots are where Laravel Herd installs itself and its bundled
// nginx/php/services (seen on a live Windows install: Program Files\Herd
// and ~\.config\herd). Matching these, not any "herd" directory, keeps
// the user's own ~/Herd projects (vite, artisan serve) from counting.
var herdRoots = []string{
	"/program files/herd/", "/.config/herd/",
	"/applications/herd.app/", "/library/application support/herd/",
}

// IsHerd reports whether p belongs to Laravel Herd: its own binaries, or
// the nginx/php/dnsmasq/services it bundles (by install path).
func IsHerd(p Process) bool {
	name := strings.ToLower(strings.TrimSuffix(p.Name, filepath.Ext(p.Name)))
	if name == "herd" || name == "herdhelper" {
		return true
	}
	path := strings.ToLower(strings.ReplaceAll(p.Path, `\`, "/"))
	for _, root := range herdRoots {
		if strings.Contains(path, root) {
			return true
		}
	}
	return false
}

// Describe renders a process for messages: "nginx.exe (pid 1234)".
func Describe(p Process) string {
	switch {
	case p.PID == 4 && p.Name == "":
		// PID 4 is the Windows kernel: port held by HTTP.sys (IIS, WinRM,
		// or another service registered with it).
		return "the Windows HTTP service (http.sys, pid 4)"
	case p.PID == 0 && p.Name == "":
		return "a process owned by another user"
	case p.Name == "":
		return "pid " + strconv.Itoa(p.PID)
	default:
		return p.Name + " (pid " + strconv.Itoa(p.PID) + ")"
	}
}

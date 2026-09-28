//go:build windows

package doctor

import (
	"os"
	"path/filepath"

	"github.com/d3v0psdan/bench/daemon/internal/api"
)

// checkVCRuntime: the Windows MySQL and PostgreSQL builds need the MSVC
// runtime and die silently at load without it.
func checkVCRuntime() api.Check {
	c := api.Check{ID: "vcruntime", Title: "Visual C++ runtime"}
	if _, err := os.Stat(filepath.Join(os.Getenv("SystemRoot"), "System32", "vcruntime140.dll")); err == nil {
		c.Status, c.Detail = OK, "Installed (MySQL and PostgreSQL need it)."
		return c
	}
	c.Status = Warn
	c.Detail = "The Microsoft Visual C++ runtime is missing, so MySQL and PostgreSQL won't start."
	c.Steps = []string{"Download and run the installer: https://aka.ms/vs/17/release/vc_redist.x64.exe", "Check again."}
	c.Action = ActionRecheck
	return c
}

// Package cli implements the bench command tree. Commands talk to benchd
// exclusively through its HTTP API, never the database.
package cli

import (
	"errors"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/d3v0psdan/bench/daemon/internal/version"
)

// New builds a fresh command tree (fresh per Execute: cobra accumulates
// flag state across runs).
func New() *cobra.Command {
	root := &cobra.Command{
		Use:           "bench",
		Short:         "Bench: local Laravel/PHP development environment",
		Version:       version.Version,
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.AddCommand(
		newStartCmd(), newStopCmd(), newStatusCmd(), newSetupCmd(), newPHPInstallCmd(), newPHPUninstallCmd(),
		newSitesCmd(), newParkCmd(), newUnparkCmd(), newLinkCmd(), newUnlinkCmd(),
		newUseCmd(), newIsolateCmd(), newUnisolateCmd(), newProxyCmd(), newUnproxyCmd(),
	)
	root.AddCommand(newServicesCmds()...)
	root.AddCommand(newDoctorCmd(), newDBCmd(), newNewCmd())
	return root
}

// exitCode passes a child's exit status through as bench's own, with no
// message of its own (the child already reported).
type exitCode int

func (e exitCode) Error() string { return fmt.Sprintf("exit status %d", int(e)) }

// Execute runs the CLI and returns the process exit code.
func Execute() int {
	cmd := New()
	if err := cmd.Execute(); err != nil {
		if code, ok := errors.AsType[exitCode](err); ok {
			return int(code)
		}
		fmt.Fprintln(cmd.ErrOrStderr(), "bench:", err)
		return 1
	}
	return 0
}

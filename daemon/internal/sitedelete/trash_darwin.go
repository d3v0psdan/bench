//go:build darwin

package sitedelete

import (
	"context"
	"fmt"

	"github.com/d3v0psdan/bench/daemon/internal/supervisor"
)

// trash asks Finder to move path to the Trash, so it can be put back. The
// path is an osascript argument, never part of the script.
func trash(ctx context.Context, path string) error {
	out, err := supervisor.Exec(ctx, supervisor.Spec{
		Command: "osascript",
		Args: []string{
			"-e", "on run argv",
			"-e", `tell application "Finder" to delete (POSIX file (item 1 of argv) as alias)`,
			"-e", "end run",
			path,
		},
	})
	if err != nil {
		return fmt.Errorf("Finder refused it (allow Bench to control Finder in Privacy & Security): %w: %s", err, lastLine(out))
	}
	return nil
}

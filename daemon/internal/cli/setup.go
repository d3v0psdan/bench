package cli

import (
	"errors"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/d3v0psdan/bench/daemon/internal/api"
	"github.com/d3v0psdan/bench/daemon/internal/paths"
	"github.com/d3v0psdan/bench/daemon/internal/setup"
)

func newSetupCmd() *cobra.Command {
	var undo bool
	cmd := &cobra.Command{
		Use:   "setup",
		Short: "Configure .test DNS and trust the local HTTPS certificate authority (asks for elevation once)",
		Long: "Configures .test DNS and trusts Bench's local HTTPS certificate authority, asking for\n" +
			"elevation once. --undo removes both again; it works whether or not Bench is running,\n" +
			"so uninstallers can call it.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			out := cmd.OutOrStdout()
			fmt.Fprintln(out, "requesting elevation…")
			var msgs []string
			var err error
			if undo {
				msgs, err = undoSetup(cmd)
			} else {
				var c *api.Client
				if c, err = daemonClient(); err != nil {
					return err
				}
				msgs, err = c.Setup(cmd.Context(), true, true)
			}
			if errors.Is(err, setup.ErrNothingToRemove) {
				fmt.Fprintln(out, "nothing to remove: the HTTPS and DNS setup isn't in place")
				return nil
			}
			if err != nil {
				return err
			}
			for _, m := range msgs {
				fmt.Fprintln(out, "✓ "+m)
			}
			if !undo {
				fmt.Fprintln(out, "done: https://<site>.test now works in your browser")
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&undo, "undo", false, "remove the .test DNS rule and the trusted certificate authority")
	return cmd
}

// undoSetup asks the running daemon (so its Activity shows the task), or
// runs the elevated helper itself when Bench isn't running, including a
// stale address file left by a crash (uninstallers rely on this).
func undoSetup(cmd *cobra.Command) ([]string, error) {
	c, err := api.Discover()
	if err == nil {
		msgs, err := c.Teardown(cmd.Context(), true, true)
		if !errors.Is(err, api.ErrNotRunning) {
			return msgs, err
		}
	} else if !errors.Is(err, api.ErrNotRunning) {
		return nil, fmt.Errorf("finding Bench: %w", err)
	}
	root, err := paths.Root()
	if err != nil {
		return nil, fmt.Errorf("finding the Bench home: %w", err)
	}
	return setup.Teardown(cmd.Context(), root, true, true)
}

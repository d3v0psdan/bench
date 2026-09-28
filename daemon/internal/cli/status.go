package cli

import (
	"errors"
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"github.com/d3v0psdan/bench/daemon/internal/api"
)

func newStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show daemon status",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := api.Discover()
			if err != nil {
				if errors.Is(err, api.ErrNotRunning) {
					fmt.Fprintln(cmd.OutOrStdout(), "benchd is not running")
					return nil
				}
				return err
			}
			st, err := c.Status(cmd.Context())
			if err != nil {
				if errors.Is(err, api.ErrNotRunning) {
					fmt.Fprintln(cmd.OutOrStdout(), "benchd is not running")
					return nil
				}
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(),
				"benchd running: v%s on %s/%s, pid %d, up %s, listening at %s\n",
				st.Version, st.OS, st.Arch, st.PID,
				(time.Duration(st.UptimeSeconds) * time.Second).String(), c.Addr)
			return nil
		},
	}
}

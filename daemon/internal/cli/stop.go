package cli

import (
	"errors"
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"github.com/d3v0psdan/bench/daemon/internal/api"
)

// stopTimeout covers databases flushing on clean shutdown (a supervised
// service gets up to 60s before it is killed).
const stopTimeout = 90 * time.Second

func newStopCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "stop",
		Short: "Stop the benchd daemon",
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
			if err := c.Shutdown(cmd.Context()); err != nil {
				if errors.Is(err, api.ErrNotRunning) {
					fmt.Fprintln(cmd.OutOrStdout(), "benchd is not running")
					return nil
				}
				return err
			}

			deadline := time.Now().Add(stopTimeout)
			for time.Now().Before(deadline) {
				if _, err := c.Status(cmd.Context()); errors.Is(err, api.ErrNotRunning) {
					fmt.Fprintln(cmd.OutOrStdout(), "benchd stopped")
					return nil
				}
				time.Sleep(100 * time.Millisecond)
			}
			return errors.New("benchd did not stop in time")
		},
	}
}

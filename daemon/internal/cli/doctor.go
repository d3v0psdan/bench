package cli

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/spf13/cobra"

	"github.com/d3v0psdan/bench/daemon/internal/api"
)

func newDoctorCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "doctor",
		Short: "Diagnose port conflicts (e.g. Laravel Herd), DNS and prerequisites",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := daemonClient()
			if err != nil {
				return err
			}
			checks, err := c.Doctor(cmd.Context())
			if err != nil {
				return err
			}
			printChecks(cmd.OutOrStdout(), checks, true)
			if failed(checks) {
				return errors.New("doctor found problems that stop Bench from working")
			}
			return nil
		},
	}
}

// printChecks renders doctor findings; all=false shows only problems.
func printChecks(out io.Writer, checks []api.Check, all bool) {
	for _, c := range checks {
		if c.Status == "ok" && !all {
			continue
		}
		fmt.Fprintf(out, "[%s] %s: %s\n", c.Status, c.Title, c.Detail)
		for i, step := range c.Steps {
			fmt.Fprintf(out, "       %d. %s\n", i+1, step)
		}
	}
}

func failed(checks []api.Check) bool {
	for _, c := range checks {
		if c.Status == "fail" {
			return true
		}
	}
	return false
}

// preflight runs the doctor after the daemon comes up and reports
// problems, so a start that can't serve names the culprit (e.g. Herd
// holding :443) instead of failing later with a raw bind error.
func preflight(ctx context.Context, out io.Writer) error {
	c, err := api.Discover()
	if err != nil {
		return nil // start already reported the daemon's state
	}
	checks, err := c.Doctor(ctx)
	if err != nil {
		return nil // an older daemon without /api/doctor: nothing to add
	}
	printChecks(out, checks, false)
	// Only the HTTPS port decides whether sites can be served; other
	// problems (a stopped service's port) are shown but don't fail start.
	for _, c := range checks {
		if c.ID == "https" && c.Status == "fail" {
			return errors.New("benchd is running, but it can't serve sites until the HTTPS port is free")
		}
	}
	return nil
}

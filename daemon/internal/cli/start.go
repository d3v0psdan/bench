package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"time"

	"github.com/spf13/cobra"

	"github.com/d3v0psdan/bench/daemon/internal/api"
	"github.com/d3v0psdan/bench/daemon/internal/paths"
)

const startTimeout = 10 * time.Second

func newStartCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "start",
		Short: "Start the benchd daemon",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			st, err := runningStatus(cmd.Context())
			if err == nil {
				fmt.Fprintf(cmd.OutOrStdout(), "benchd already running (pid %d)\n", st.PID)
				return preflight(cmd.Context(), cmd.OutOrStdout())
			}
			if !errors.Is(err, api.ErrNotRunning) {
				// e.g. a token mismatch against a live daemon: spawning a
				// second one would just die on the port and hide the cause.
				return err
			}

			exe, err := findBenchd()
			if err != nil {
				return err
			}
			if err := spawnDetached(exe); err != nil {
				return fmt.Errorf("starting benchd: %w", err)
			}

			deadline := time.Now().Add(startTimeout)
			var lastErr error
			for time.Now().Before(deadline) {
				st, err := runningStatus(cmd.Context())
				if err == nil {
					fmt.Fprintf(cmd.OutOrStdout(), "benchd started (pid %d, v%s)\n", st.PID, st.Version)
					return preflight(cmd.Context(), cmd.OutOrStdout())
				}
				if !errors.Is(err, api.ErrNotRunning) {
					lastErr = err
				}
				time.Sleep(100 * time.Millisecond)
			}
			if lastErr != nil {
				return fmt.Errorf("benchd did not become ready: %w", lastErr)
			}
			logFile, _ := paths.LogFile()
			return fmt.Errorf("benchd did not become ready in time; check %s", logFile)
		},
	}
}

func runningStatus(ctx context.Context) (*api.Status, error) {
	c, err := api.Discover()
	if err != nil {
		return nil, err
	}
	return c.Status(ctx)
}

// findBenchd looks for the daemon binary next to the bench executable
// first (how releases ship), then on PATH.
func findBenchd() (string, error) {
	name := "benchd"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	if self, err := os.Executable(); err == nil {
		candidate := filepath.Join(filepath.Dir(self), name)
		if _, err := os.Stat(candidate); err == nil {
			return candidate, nil
		}
	}
	path, err := exec.LookPath(name)
	if err != nil {
		return "", fmt.Errorf("benchd binary not found next to bench or on PATH: %w", err)
	}
	return path, nil
}

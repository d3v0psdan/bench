package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"

	"github.com/d3v0psdan/bench/daemon/internal/api"
)

func newPHPInstallCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "php:install <version>",
		Short: "Download and install a PHP version (e.g. 8.4)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return installBinary(cmd, "php", args[0])
		},
	}
}

func newPHPUninstallCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "php:uninstall <version>",
		Short: "Delete an installed PHP version (e.g. 8.3)",
		Long: "Delete an installed PHP version. Bench refuses the default version and versions " +
			"sites have pinned: switch them first with `bench use` or `bench isolate`.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := daemonClient()
			if err != nil {
				return err
			}
			channel := args[0]
			list, err := c.Binaries(cmd.Context())
			if err != nil {
				return err
			}
			var build *api.BinaryInfo
			for i := range list {
				if list[i].Name == "php" && (list[i].Channel == channel || list[i].Version == channel) {
					build = &list[i]
				}
			}
			switch {
			case build == nil:
				return fmt.Errorf("PHP %s isn't in Bench's catalog", channel)
			case !build.Installed:
				return fmt.Errorf("PHP %s isn't installed", channel)
			}
			if err := c.UninstallBinary(cmd.Context(), "php", channel); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "php %s uninstalled, %s freed\n", build.Version, fmtBytes(build.Size))
			return nil
		},
	}
}

// installBinary drives POST /api/binaries/install while rendering the
// daemon's WS download-progress events on one rewriting terminal line.
func installBinary(cmd *cobra.Command, name, channel string) error {
	c, err := api.Discover()
	if err != nil {
		if errors.Is(err, api.ErrNotRunning) {
			return errors.New("benchd is not running; run `bench start` first")
		}
		return err
	}
	ctx, cancel := context.WithCancel(cmd.Context())
	defer cancel()
	out := cmd.OutOrStdout()

	events, evErr := c.Events(ctx)
	if evErr != nil {
		events = nil // no live progress; the install itself still works
	}
	rendered := make(chan struct{})
	go func() {
		defer close(rendered)
		renderProgress(out, events, name)
	}()

	info, err := c.InstallBinary(ctx, name, channel)
	cancel()
	<-rendered
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "%s %s installed\n", info.Name, info.Version)
	return nil
}

func renderProgress(out io.Writer, events <-chan api.Event, name string) {
	if events == nil {
		return
	}
	var width int
	defer func() {
		if width > 0 { // wipe the progress line before the final message
			fmt.Fprintf(out, "\r%s\r", strings.Repeat(" ", width))
		}
	}()
	for ev := range events {
		p := ev.Download
		if ev.Type != "download" || p == nil || p.Name != name {
			continue
		}
		var line string
		switch p.Phase {
		case "downloading":
			if p.Total > 0 {
				line = fmt.Sprintf("downloading %s %s (file %d/%d): %s / %s (%d%%)",
					name, p.Version, p.File, p.Files, fmtBytes(p.Received), fmtBytes(p.Total), p.Received*100/p.Total)
			} else {
				line = fmt.Sprintf("downloading %s %s (file %d/%d): %s",
					name, p.Version, p.File, p.Files, fmtBytes(p.Received))
			}
		case "extracting":
			line = fmt.Sprintf("extracting %s %s (file %d/%d)", name, p.Version, p.File, p.Files)
		case "done", "error":
			return // outcome is reported by the blocking install call
		default:
			continue
		}
		if len(line) > width {
			width = len(line)
		}
		fmt.Fprintf(out, "\r%-*s", width, line)
	}
}

func fmtBytes(n int64) string {
	switch {
	case n >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%d KB", n/(1<<10))
	default:
		return fmt.Sprintf("%d B", n)
	}
}

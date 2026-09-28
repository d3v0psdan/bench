package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/d3v0psdan/bench/daemon/internal/api"
)

// daemonClient discovers the running daemon or explains how to start it.
func daemonClient() (*api.Client, error) {
	c, err := api.Discover()
	if errors.Is(err, api.ErrNotRunning) {
		return nil, errors.New("benchd is not running; run `bench start` first")
	}
	return c, err
}

func newSitesCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "sites",
		Short: "List all served sites",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := daemonClient()
			if err != nil {
				return err
			}
			list, err := c.Sites(cmd.Context())
			if err != nil {
				return err
			}
			printSites(cmd, list)
			return nil
		},
	}
}

func printSites(cmd *cobra.Command, list []api.Site) {
	out := cmd.OutOrStdout()
	if len(list) == 0 {
		fmt.Fprintln(out, "no sites yet: `bench park` a directory or `bench link` a project")
		return
	}
	w := tabwriter.NewWriter(out, 2, 4, 2, ' ', 0)
	fmt.Fprintln(w, "SITE\tKIND\tPHP\tTARGET")
	for _, s := range list {
		target := s.Path
		if s.Kind == "proxy" {
			target = "→ " + s.ProxyTo
		}
		php := s.PHP
		if s.PHPPinned != "" {
			php += " (isolated)"
		}
		if s.Error != "" {
			target += "  [" + s.Error + "]"
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", s.URL, s.Kind, php, target)
	}
	w.Flush()
}

func newParkCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "park [path]",
		Short: "Serve every subdirectory of a directory as <name>.test",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			path, err := argOrCwd(args)
			if err != nil {
				return err
			}
			c, err := daemonClient()
			if err != nil {
				return err
			}
			list, err := c.Park(cmd.Context(), path)
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "parked %s\n", path)
			printSites(cmd, list)
			return nil
		},
	}
}

func newUnparkCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "unpark [path]",
		Aliases: []string{"forget"},
		Short:   "Stop serving a parked directory",
		Args:    cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			path, err := argOrCwd(args)
			if err != nil {
				return err
			}
			c, err := daemonClient()
			if err != nil {
				return err
			}
			if _, err := c.Unpark(cmd.Context(), path); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "unparked %s\n", path)
			return nil
		},
	}
}

func newLinkCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "link [name]",
		Short: "Serve the current directory as <name>.test (default: directory name)",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cwd, err := os.Getwd()
			if err != nil {
				return err
			}
			name := ""
			if len(args) == 1 {
				name = args[0]
			}
			c, err := daemonClient()
			if err != nil {
				return err
			}
			list, err := c.Link(cmd.Context(), cwd, name)
			if err != nil {
				return err
			}
			if name == "" {
				name = filepath.Base(cwd)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "linked https://%s.test → %s\n", name, cwd)
			printSiteError(cmd, list, name)
			return nil
		},
	}
}

func newUnlinkCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "unlink [name]",
		Short: "Stop serving a site, keeping its files (default: current directory's name)",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name, err := argOrCwdName(args)
			if err != nil {
				return err
			}
			c, err := daemonClient()
			if err != nil {
				return err
			}
			if _, err := c.Unlink(cmd.Context(), name); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "unlinked %s.test\n", name)
			return nil
		},
	}
}

func newUseCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "use <version>",
		Short: "Switch the global default PHP version (e.g. 8.4)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if strings.TrimSpace(args[0]) == "" {
				return errors.New("name a PHP version, e.g. `bench use 8.4`")
			}
			c, err := daemonClient()
			if err != nil {
				return err
			}
			if _, err := c.SetSetting(cmd.Context(), "php.default", args[0]); err != nil {
				return err
			}
			// Report what was stored: the daemon keeps the channel ("8.4"
			// for "8.4.12").
			settings, err := c.Settings(cmd.Context())
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "default PHP is now %s\n", settings["php.default"])
			return nil
		},
	}
}

func newIsolateCmd() *cobra.Command {
	var siteName string
	cmd := &cobra.Command{
		Use:   "isolate <version>",
		Short: "Pin the current site to a PHP version (e.g. 8.3)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return setSitePHP(cmd, siteName, args[0])
		},
	}
	cmd.Flags().StringVar(&siteName, "site", "", "site name (default: current directory)")
	return cmd
}

func newUnisolateCmd() *cobra.Command {
	var siteName string
	cmd := &cobra.Command{
		Use:   "unisolate",
		Short: "Remove the current site's PHP pin (back to the global default)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return setSitePHP(cmd, siteName, "")
		},
	}
	cmd.Flags().StringVar(&siteName, "site", "", "site name (default: current directory)")
	return cmd
}

func setSitePHP(cmd *cobra.Command, name, version string) error {
	c, err := daemonClient()
	if err != nil {
		return err
	}
	if name == "" {
		name, err = siteForCwd(cmd.Context(), c)
		if err != nil {
			return err
		}
	}
	// The daemon names sites like this (sites.NormalizeName); match its
	// list and print the host with one ".test".
	name = strings.TrimSuffix(strings.ToLower(strings.TrimSpace(name)), ".test")
	list, err := c.SetSitePHP(cmd.Context(), name, version)
	if err != nil {
		return err
	}
	if version == "" {
		fmt.Fprintf(cmd.OutOrStdout(), "%s.test now uses the global default PHP\n", name)
	} else {
		fmt.Fprintf(cmd.OutOrStdout(), "%s.test now uses PHP %s\n", name, pinnedPHP(list, name, version))
	}
	printSiteError(cmd, list, name)
	return nil
}

func newProxyCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "proxy <name> <port|host:port>",
		Short: "Serve https://<name>.test as a reverse proxy to a local port",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := daemonClient()
			if err != nil {
				return err
			}
			if _, err := c.Proxy(cmd.Context(), args[0], args[1]); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "proxying https://%s.test → %s\n", args[0], args[1])
			return nil
		},
	}
}

func newUnproxyCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "unproxy <name>",
		Short: "Remove a proxied site",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := daemonClient()
			if err != nil {
				return err
			}
			if _, err := c.Unlink(cmd.Context(), args[0]); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "removed proxy %s.test\n", args[0])
			return nil
		},
	}
}

// siteForCwd resolves which site the current directory belongs to: exact
// path match first (linked/parked), then directory-name match.
func siteForCwd(ctx context.Context, c *api.Client) (string, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	list, err := c.Sites(ctx)
	if err != nil {
		return "", err
	}
	for _, s := range list {
		if s.Path != "" && sameFile(s.Path, cwd) {
			return s.Name, nil
		}
	}
	base := filepath.Base(cwd)
	for _, s := range list {
		if s.Name == base {
			return s.Name, nil
		}
	}
	return "", fmt.Errorf("%s is not a known site; `bench link` it or park its parent first", cwd)
}

func sameFile(a, b string) bool {
	fa, err1 := os.Stat(a)
	fb, err2 := os.Stat(b)
	return err1 == nil && err2 == nil && os.SameFile(fa, fb)
}

// pinnedPHP is the version the daemon stored for a site's pin (the channel,
// "8.4" for "8.4.12"), falling back to what was asked for.
func pinnedPHP(list []api.Site, name, asked string) string {
	for _, s := range list {
		if s.Name == name && s.PHPPinned != "" {
			return s.PHPPinned
		}
	}
	return asked
}

// printSiteError surfaces a per-site apply error (e.g. missing PHP build).
func printSiteError(cmd *cobra.Command, list []api.Site, name string) {
	for _, s := range list {
		if s.Name == name && s.Error != "" {
			fmt.Fprintf(cmd.ErrOrStderr(), "warning: %s\n", s.Error)
		}
	}
}

func argOrCwd(args []string) (string, error) {
	if len(args) == 1 {
		return args[0], nil
	}
	return os.Getwd()
}

func argOrCwdName(args []string) (string, error) {
	if len(args) == 1 {
		return args[0], nil
	}
	cwd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	return filepath.Base(cwd), nil
}

package cli

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"runtime"
	"strings"

	"github.com/spf13/cobra"

	"github.com/d3v0psdan/bench/daemon/internal/api"
	"github.com/d3v0psdan/bench/daemon/internal/desktop"
)

func newDBCmd() *cobra.Command {
	var cli, printURL bool
	cmd := &cobra.Command{
		Use:   "db [name]",
		Short: "Open a database instance in TablePlus, DBeaver, or its terminal client",
		Long: "Opens a MySQL, MariaDB, PostgreSQL or Valkey instance in the first client found:\n" +
			"TablePlus, then DBeaver, then the terminal client bundled with the instance\n" +
			"(mysql, mariadb, psql or valkey-cli). With one database instance the name is optional.",
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := daemonClient()
			if err != nil {
				return err
			}
			list, err := c.Services(cmd.Context())
			if err != nil {
				return err
			}
			s, err := pickDB(list, args)
			if err != nil {
				return err
			}
			if s.State != "running" {
				return fmt.Errorf("%s is %s; start it with `bench services:start %s`", s.Name, s.State, s.Name)
			}
			if printURL {
				fmt.Fprintln(cmd.OutOrStdout(), desktop.DatabaseURL(s))
				return nil
			}
			if !cli {
				if app, err := desktop.OpenDatabase(s); err == nil {
					fmt.Fprintf(cmd.OutOrStdout(), "opened %s in %s\n", s.Name, app)
					return nil
				} else if !errors.Is(err, desktop.ErrNoApp) {
					return err
				}
			}
			return runTerminalClient(cmd, s, !cli)
		},
	}
	cmd.Flags().BoolVar(&cli, "cli", false, "use the bundled terminal client even if a GUI client is installed")
	cmd.Flags().BoolVar(&printURL, "url", false, "print the connection URL instead of opening a client")
	return cmd
}

// pickDB resolves the instance: by name, or the only database instance.
func pickDB(list []api.Service, args []string) (api.Service, error) {
	var dbs []api.Service
	for _, s := range list {
		if desktop.IsDatabase(s.Service) {
			dbs = append(dbs, s)
		}
	}
	if len(args) == 1 {
		for _, s := range dbs {
			if s.Name == args[0] {
				return s, nil
			}
		}
		return api.Service{}, fmt.Errorf("no database instance named %q", args[0])
	}
	switch len(dbs) {
	case 0:
		return api.Service{}, errors.New("no database instances; create one with `bench services:create mysql`")
	case 1:
		return dbs[0], nil
	}
	var names []string
	for _, s := range dbs {
		names = append(names, s.Name)
	}
	return api.Service{}, fmt.Errorf("several database instances; name one: bench db <%s>", strings.Join(names, "|"))
}

// runTerminalClient runs the client bundled with the instance's build,
// attached to this terminal. Its exit code becomes bench's.
func runTerminalClient(cmd *cobra.Command, s api.Service, lookedForGUI bool) error {
	argv := desktop.TerminalClient(s, runtime.GOOS)
	url := desktop.DatabaseURL(s)
	if _, err := os.Stat(argv[0]); err != nil {
		return fmt.Errorf("no terminal client for %s at %s; connect any client to %s", s.Name, argv[0], url)
	}
	why := ""
	if lookedForGUI {
		why = "; no GUI client found, using the terminal client"
	}
	fmt.Fprintf(cmd.ErrOrStderr(), "connecting to %s (%s)%s\n", s.Name, url, why)
	client := exec.Command(argv[0], argv[1:]...)
	client.Stdin, client.Stdout, client.Stderr = os.Stdin, cmd.OutOrStdout(), cmd.ErrOrStderr()
	// Ctrl+C belongs to the client (cancel a query), not to bench: dying
	// here would orphan the client on the terminal.
	signal.Ignore(os.Interrupt)
	defer signal.Reset(os.Interrupt)
	err := client.Run()
	if exitErr, ok := errors.AsType[*exec.ExitError](err); ok {
		return exitCode(exitErr.ExitCode())
	}
	return err
}

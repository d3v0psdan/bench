package cli

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/d3v0psdan/bench/daemon/internal/api"
)

// serviceAliases maps names people type to catalog kinds.
var serviceAliases = map[string]string{"redis": "valkey", "postgres": "postgresql", "pgsql": "postgresql",
	"meili": "meilisearch", "s3": "rustfs", "mail": "mailpit"}

func newServicesCmds() []*cobra.Command {
	return []*cobra.Command{
		newServicesListCmd(), newServicesAvailableCmd(), newServicesCreateCmd(),
		newServiceActionCmd("start", "Start a service instance"),
		newServiceActionCmd("stop", "Stop a service instance"),
		newServicesCloneCmd(), newServicesDeleteCmd(), newServicesAutostartCmd(), newServicesEnvCmd(),
	}
}

func newServicesListCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "services:list",
		Aliases: []string{"services"},
		Short:   "List service instances",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := daemonClient()
			if err != nil {
				return err
			}
			list, err := c.Services(cmd.Context())
			if err != nil {
				return err
			}
			printServices(cmd, list)
			return nil
		},
	}
}

func printServices(cmd *cobra.Command, list []api.Service) {
	out := cmd.OutOrStdout()
	if len(list) == 0 {
		fmt.Fprintln(out, "no service instances; create one with `bench services:create mysql`")
		return
	}
	w := tabwriter.NewWriter(out, 2, 4, 2, ' ', 0)
	fmt.Fprintln(w, "NAME\tSERVICE\tVERSION\tPORT\tSTATE\tAUTOSTART")
	for _, s := range list {
		state := s.State
		if s.Error != "" {
			state += " (" + firstLine(s.Error) + ")"
		}
		auto := "no"
		if s.Autostart {
			auto = "yes"
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\n", s.Name, s.Service, s.Version, ports(s), state, auto)
	}
	w.Flush()
}

func ports(s api.Service) string {
	p := strconv.Itoa(s.Port)
	for role, port := range s.Ports {
		p += fmt.Sprintf(" (%s %d)", role, port)
	}
	return p
}

func firstLine(s string) string {
	line, _, _ := strings.Cut(s, "\n")
	return line
}

func newServicesAvailableCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "services:available",
		Short: "List the services and versions you can create on this machine",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := daemonClient()
			if err != nil {
				return err
			}
			types, err := c.ServiceCatalog(cmd.Context())
			if err != nil {
				return err
			}
			w := tabwriter.NewWriter(cmd.OutOrStdout(), 2, 4, 2, ' ', 0)
			fmt.Fprintln(w, "SERVICE\tVERSIONS\tDEFAULT PORT\tNAME")
			for _, t := range types {
				fmt.Fprintf(w, "%s\t%s\t%d\t%s\n", t.Service, strings.Join(t.Channels, ", "), t.DefaultPort, t.Label)
			}
			return w.Flush()
		},
	}
}

func newServicesCreateCmd() *cobra.Command {
	var port int
	var autostart bool
	cmd := &cobra.Command{
		Use:   "services:create <service> [version] [name]",
		Short: "Install, initialize and start a service instance (e.g. mysql 8.4 myapp-db)",
		Long: "Creates a native service instance: downloads the build if needed, initializes its data\n" +
			"directory, and starts it on 127.0.0.1. Services: mysql, mariadb, postgresql, valkey (redis),\n" +
			"meilisearch, rustfs (s3), mailpit (mail). Version defaults to the newest; name to the service.",
		Args: cobra.RangeArgs(1, 3),
		RunE: func(cmd *cobra.Command, args []string) error {
			kind := args[0]
			if alias, ok := serviceAliases[kind]; ok {
				kind = alias
			}
			req := api.CreateServiceRequest{Service: kind, Port: port, Autostart: autostart}
			if len(args) > 1 {
				req.Channel = args[1]
			}
			if len(args) > 2 {
				req.Name = args[2]
			}
			c, err := daemonClient()
			if err != nil {
				return err
			}
			var svc *api.Service
			err = withProgress(cmd, c, kind, func(ctx context.Context) error {
				var err error
				svc, err = c.CreateService(ctx, req)
				return err
			})
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "%s %s is %s on 127.0.0.1:%d\n", svc.Service, svc.Version, svc.State, svc.Port)
			printServiceDetails(cmd, *svc)
			return nil
		},
	}
	cmd.Flags().IntVar(&port, "port", 0, "listen port (default: the service's standard port, or the next free one)")
	cmd.Flags().BoolVar(&autostart, "autostart", false, "start the instance whenever benchd starts")
	return cmd
}

// withProgress runs fn while rendering binary download progress for name.
func withProgress(cmd *cobra.Command, c *api.Client, name string, fn func(context.Context) error) error {
	ctx, cancel := context.WithCancel(cmd.Context())
	defer cancel()
	events, err := c.Events(ctx)
	if err != nil {
		events = nil // no live progress; the operation itself still works
	}
	rendered := make(chan struct{})
	go func() {
		defer close(rendered)
		renderProgress(cmd.OutOrStdout(), events, name)
	}()
	err = fn(ctx)
	cancel()
	<-rendered
	return err
}

// printServiceDetails shows the notice (moved port) and .env block.
func printServiceDetails(cmd *cobra.Command, s api.Service) {
	out := cmd.OutOrStdout()
	if s.Notice != "" {
		fmt.Fprintf(out, "note: %s\n", s.Notice)
	}
	if len(s.Env) > 0 {
		fmt.Fprintln(out, "\nConnect a Laravel app with these .env lines:")
		for _, line := range s.Env {
			fmt.Fprintln(out, "  "+line)
		}
	}
	if s.Service == "mailpit" && s.Ports["http"] != 0 {
		fmt.Fprintf(out, "\nInbox: http://127.0.0.1:%d\n", s.Ports["http"])
	}
}

func newServiceActionCmd(action, short string) *cobra.Command {
	return &cobra.Command{
		Use:   "services:" + action + " <name>",
		Short: short,
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := daemonClient()
			if err != nil {
				return err
			}
			svc, err := c.ServiceAction(cmd.Context(), args[0], action)
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "%s is %s\n", svc.Name, svc.State)
			return nil
		},
	}
}

func newServicesCloneCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "services:clone <name> <new-name>",
		Short: "Copy a service instance, with its data, into a new instance",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := daemonClient()
			if err != nil {
				return err
			}
			svc, err := c.CloneService(cmd.Context(), args[0], args[1])
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "cloned %s → %s (%s on 127.0.0.1:%d)\n", args[0], svc.Name, svc.State, svc.Port)
			printServiceDetails(cmd, *svc)
			return nil
		},
	}
}

func newServicesDeleteCmd() *cobra.Command {
	var keepData bool
	cmd := &cobra.Command{
		Use:   "services:delete <name>",
		Short: "Stop and remove a service instance and its data",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := daemonClient()
			if err != nil {
				return err
			}
			if err := c.DeleteService(cmd.Context(), args[0], keepData); err != nil {
				return err
			}
			msg := "deleted " + args[0]
			if keepData {
				msg += " (data kept; creating it again reuses the data)"
			}
			fmt.Fprintln(cmd.OutOrStdout(), msg)
			return nil
		},
	}
	cmd.Flags().BoolVar(&keepData, "keep-data", false, "leave the data directory on disk")
	return cmd
}

func newServicesAutostartCmd() *cobra.Command {
	return &cobra.Command{
		Use:       "services:autostart <name> <on|off>",
		Short:     "Start (or stop starting) an instance whenever benchd starts",
		Args:      cobra.ExactArgs(2),
		ValidArgs: []string{"on", "off"},
		RunE: func(cmd *cobra.Command, args []string) error {
			var enabled bool
			switch args[1] {
			case "on":
				enabled = true
			case "off":
			default:
				return fmt.Errorf("want on or off, got %q", args[1])
			}
			c, err := daemonClient()
			if err != nil {
				return err
			}
			if _, err := c.SetServiceAutostart(cmd.Context(), args[0], enabled); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "autostart %s for %s\n", args[1], args[0])
			return nil
		},
	}
}

func newServicesEnvCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "services:env <name>",
		Short: "Print the .env lines that connect a Laravel app to an instance",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := daemonClient()
			if err != nil {
				return err
			}
			list, err := c.Services(cmd.Context())
			if err != nil {
				return err
			}
			for _, s := range list {
				if s.Name == args[0] {
					fmt.Fprintln(cmd.OutOrStdout(), strings.Join(s.Env, "\n"))
					return nil
				}
			}
			return fmt.Errorf("no service instance named %q", args[0])
		},
	}
}

package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"time"

	"github.com/spf13/cobra"

	"github.com/d3v0psdan/bench/daemon/internal/api"
)

// cancelTimeout bounds asking the daemon to cancel after Ctrl+C.
const cancelTimeout = 10 * time.Second

func newNewCmd() *cobra.Command {
	var (
		react, svelte, vue, livewire     bool
		workos, teams, noAuth, classComp bool
		pest, phpunit, boost, noBoost    bool
		npm, bun, noNode, noMigrate      bool
		database, dir, service           string
	)
	cmd := &cobra.Command{
		Use:   "new <name>",
		Short: "Create a Laravel app the way `laravel new` does, and serve it at https://<name>.test",
		Long: "Creates a Laravel app with the same choices as the Laravel installer: a starter kit\n" +
			"(--react, --svelte, --vue, --livewire; add --no-authentication for a blank kit), WorkOS\n" +
			"or teams, Pest or PHPUnit, Laravel Boost, the database (a Bench service is created or\n" +
			"reused), and npm or bun. It goes into the projects folder (default ~/Bench), which Bench\n" +
			"parks, so the app is served as soon as it is created.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			req := api.NewSiteRequest{
				Name: args[0], Dir: dir, Stack: "blade", Testing: "pest", Boost: !noBoost,
				Database: database, Service: service, Migrate: !noMigrate, PackageManager: "npm",
			}
			for stack, on := range map[string]bool{"react": react, "svelte": svelte, "vue": vue, "livewire": livewire} {
				if on {
					req.Stack = stack
				}
			}
			if req.Stack != "blade" && !noAuth {
				req.StarterKit, req.Auth = true, "laravel"
				if workos {
					req.Auth = "workos"
				}
				req.Teams, req.ClassComponents = teams, classComp
			}
			if phpunit && !pest {
				req.Testing = "phpunit"
			}
			switch {
			case noNode:
				req.PackageManager = ""
			case bun && !npm:
				req.PackageManager = "bun"
			}
			return runNew(cmd, req)
		},
	}
	f := cmd.Flags()
	f.BoolVar(&react, "react", false, "React starter kit")
	f.BoolVar(&svelte, "svelte", false, "Svelte starter kit")
	f.BoolVar(&vue, "vue", false, "Vue starter kit")
	f.BoolVar(&livewire, "livewire", false, "Livewire starter kit")
	f.BoolVar(&noAuth, "no-authentication", false, "use the blank kit for the stack, without auth")
	f.BoolVar(&workos, "workos", false, "WorkOS AuthKit instead of Laravel's built-in auth")
	f.BoolVar(&teams, "teams", false, "add teams support")
	f.BoolVar(&classComp, "livewire-class-components", false, "Livewire class components instead of single-file ones")
	f.BoolVar(&pest, "pest", false, "Pest for tests (the default)")
	f.BoolVar(&phpunit, "phpunit", false, "PHPUnit for tests")
	f.BoolVar(&boost, "boost", false, "install Laravel Boost (the default)")
	f.BoolVar(&noBoost, "no-boost", false, "skip Laravel Boost")
	f.StringVar(&database, "database", "sqlite", "sqlite, mysql, mariadb or pgsql")
	f.StringVar(&service, "service", "", "Bench instance for the database (default: create one)")
	f.BoolVar(&noMigrate, "no-migrate", false, "skip the default migrations")
	f.BoolVar(&npm, "npm", false, "npm for JavaScript packages (the default)")
	f.BoolVar(&bun, "bun", false, "bun for JavaScript packages")
	f.BoolVar(&noNode, "no-node", false, "skip installing and building JavaScript packages")
	f.StringVar(&dir, "dir", "", "parent folder (default: the projects folder)")
	return cmd
}

// runNew starts the creation and prints its steps until it ends; Ctrl+C
// cancels it in the daemon.
func runNew(cmd *cobra.Command, req api.NewSiteRequest) error {
	c, err := daemonClient()
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt)
	defer stop()
	events, err := c.Events(ctx)
	if err != nil {
		return err
	}
	task, err := c.NewSite(ctx, req)
	if err != nil {
		return err
	}
	out := cmd.OutOrStdout()
	fmt.Fprintf(out, "creating %s.test (log: %s)\n", req.Name, task.LogFile)
	phase := ""
	for {
		select {
		case <-ctx.Done():
			cctx, cancel := context.WithTimeout(context.Background(), cancelTimeout)
			defer cancel()
			if err := c.CancelTask(cctx, task.ID); err != nil {
				return fmt.Errorf("cancelling: %w", err)
			}
			return errors.New("cancelled; Bench is stopping the creation")
		case ev, ok := <-events:
			if !ok {
				return errors.New("lost the connection to Bench; the creation continues, see `bench status`")
			}
			if ev.Type != "tasks" {
				continue
			}
			for _, t := range ev.Tasks {
				if t.ID != task.ID {
					continue
				}
				if t.Phase != "" && t.Phase != phase {
					phase = t.Phase
					fmt.Fprintln(out, phase)
				}
				switch t.State {
				case api.TaskDone:
					fmt.Fprintf(out, "done: https://%s.test\n", req.Name)
					return nil
				case api.TaskFailed:
					return errors.New(t.Error)
				case api.TaskCancelled:
					return errors.New("cancelled")
				}
			}
		}
	}
}

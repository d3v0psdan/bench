package newapp

import (
	"fmt"
	"slices"

	"github.com/d3v0psdan/bench/daemon/internal/api"
)

// The installer's choices (laravel/installer src/NewCommand.php), and the
// subset of its databases Bench can serve: SQL Server has no Bench service.
var (
	stacks          = []string{"blade", "react", "svelte", "vue", "livewire"}
	databases       = []string{"sqlite", "mysql", "mariadb", "pgsql"}
	packageManagers = []string{"", "npm", "bun"}
)

// Validate checks a request the way the installer's prompts constrain it:
// auth, teams and class components only exist for starter kits.
func Validate(r api.NewSiteRequest) error {
	switch {
	case !slices.Contains(stacks, r.Stack):
		return fmt.Errorf("unknown stack %q: use blade, react, svelte, vue or livewire", r.Stack)
	case r.StarterKit && r.Stack == "blade":
		return fmt.Errorf("starter kits are React, Svelte, Vue or Livewire")
	case r.StarterKit && r.Auth != "laravel" && r.Auth != "workos":
		return fmt.Errorf("unknown auth %q: use laravel or workos", r.Auth)
	case !r.StarterKit && (r.Auth != "" || r.Teams || r.ClassComponents):
		return fmt.Errorf("auth, teams and class components need a starter kit")
	case r.Teams && r.Auth != "laravel":
		return fmt.Errorf("teams need Laravel's built-in auth")
	case r.ClassComponents && (r.Stack != "livewire" || r.Auth != "laravel"):
		return fmt.Errorf("class components are a Livewire starter kit option with Laravel's auth")
	case r.Teams && r.ClassComponents:
		return fmt.Errorf("teams need single-file Livewire components")
	case r.Testing != "pest" && r.Testing != "phpunit":
		return fmt.Errorf("unknown testing framework %q: use pest or phpunit", r.Testing)
	case !slices.Contains(databases, r.Database):
		return fmt.Errorf("unknown database %q: use sqlite, mysql, mariadb or pgsql", r.Database)
	case !slices.Contains(packageManagers, r.PackageManager):
		return fmt.Errorf("unknown package manager %q: use npm or bun", r.PackageManager)
	}
	return nil
}

// createProjectArgs are the composer arguments that create the app in
// dir, exactly as the installer builds them: laravel/laravel for Blade
// without a kit, a blank kit for another stack without one, else the
// starter kit on the branch its options select.
func createProjectArgs(r api.NewSiteRequest, dir string) []string {
	if !r.StarterKit && r.Stack == "blade" {
		return []string{"create-project", "laravel/laravel", dir, "--remove-vcs", "--prefer-dist", "--no-scripts"}
	}
	kit := "laravel/" + r.Stack + "-starter-kit"
	if !r.StarterKit {
		kit = "laravel/blank-" + r.Stack + "-starter-kit"
	}
	switch {
	case r.ClassComponents:
		kit += ":dev-components"
	case r.Auth == "workos" && r.Teams:
		kit += ":dev-workos-teams"
	case r.Auth == "workos":
		kit += ":dev-workos"
	case r.Teams:
		kit += ":dev-teams"
	}
	return []string{"create-project", kit, dir, "--stability=dev"}
}

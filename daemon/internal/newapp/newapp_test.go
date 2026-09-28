package newapp

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/d3v0psdan/bench/daemon/internal/api"
	"github.com/d3v0psdan/bench/daemon/internal/dotenv"
	"github.com/d3v0psdan/bench/daemon/internal/phppool"
	"github.com/d3v0psdan/bench/daemon/internal/tasks"
)

func phpCommand() phppool.Command {
	return phppool.Command{Argv: []string{"/bench/php", "-c", "/run/php-cli.ini"}}
}

func kit(stack, auth string, mods ...func(*api.NewSiteRequest)) api.NewSiteRequest {
	r := api.NewSiteRequest{StarterKit: true, Stack: stack, Auth: auth, Testing: "pest", Database: "sqlite"}
	for _, m := range mods {
		m(&r)
	}
	return r
}

func TestCreateProjectMatchesTheInstaller(t *testing.T) {
	teams := func(r *api.NewSiteRequest) { r.Teams = true }
	classes := func(r *api.NewSiteRequest) { r.ClassComponents = true }
	for _, c := range []struct {
		name string
		req  api.NewSiteRequest
		want []string
	}{
		{"blade, no kit", api.NewSiteRequest{Stack: "blade"},
			[]string{"create-project", "laravel/laravel", "shop", "--remove-vcs", "--prefer-dist", "--no-scripts"}},
		{"react, no kit", api.NewSiteRequest{Stack: "react"},
			[]string{"create-project", "laravel/blank-react-starter-kit", "shop", "--stability=dev"}},
		{"react kit", kit("react", "laravel"), []string{"create-project", "laravel/react-starter-kit", "shop", "--stability=dev"}},
		{"svelte kit, teams", kit("svelte", "laravel", teams),
			[]string{"create-project", "laravel/svelte-starter-kit:dev-teams", "shop", "--stability=dev"}},
		{"vue kit, workos", kit("vue", "workos"), []string{"create-project", "laravel/vue-starter-kit:dev-workos", "shop", "--stability=dev"}},
		{"vue kit, workos teams", kit("vue", "workos", teams),
			[]string{"create-project", "laravel/vue-starter-kit:dev-workos-teams", "shop", "--stability=dev"}},
		{"livewire kit, class components", kit("livewire", "laravel", classes),
			[]string{"create-project", "laravel/livewire-starter-kit:dev-components", "shop", "--stability=dev"}},
	} {
		if got := createProjectArgs(c.req, "shop"); !slices.Equal(got, c.want) {
			t.Errorf("%s: %v, want %v", c.name, got, c.want)
		}
	}
}

func TestValidateFollowsThePrompts(t *testing.T) {
	ok := kit("react", "laravel")
	if err := Validate(ok); err != nil {
		t.Fatalf("a React kit is valid: %v", err)
	}
	for name, mod := range map[string]func(*api.NewSiteRequest){
		"blade kit":                  func(r *api.NewSiteRequest) { r.Stack = "blade" },
		"sql server":                 func(r *api.NewSiteRequest) { r.Database = "sqlsrv" },
		"auth without a kit":         func(r *api.NewSiteRequest) { r.StarterKit = false },
		"class components for react": func(r *api.NewSiteRequest) { r.ClassComponents = true },
		"teams with class components": func(r *api.NewSiteRequest) {
			r.Stack, r.Teams, r.ClassComponents = "livewire", true, true
		},
		"yarn": func(r *api.NewSiteRequest) { r.PackageManager = "yarn" },
	} {
		r := ok
		mod(&r)
		if err := Validate(r); err == nil {
			t.Errorf("%s: want a validation error", name)
		}
	}
}

const template = `APP_NAME=Laravel
APP_URL=http://localhost

DB_CONNECTION=sqlite
# DB_HOST=127.0.0.1
# DB_PORT=3306
# DB_DATABASE=laravel
# DB_USERNAME=root
# DB_PASSWORD=
`

func TestConfigureEnvForAServerDatabase(t *testing.T) {
	env := []string{"DB_CONNECTION=mysql", "DB_HOST=127.0.0.1", "DB_PORT=3307", "DB_USERNAME=root", "DB_PASSWORD="}
	got := configureEnv(template, "https://my-shop.test", "mysql", env, databaseName("my-shop"))
	for _, line := range []string{"APP_URL=https://my-shop.test", "DB_CONNECTION=mysql", "DB_PORT=3307", "DB_DATABASE=my_shop", "DB_PASSWORD="} {
		if !strings.Contains(got, "\n"+line+"\n") {
			t.Errorf("missing %q in\n%s", line, got)
		}
	}
	if strings.Contains(got, "# DB_") {
		t.Errorf("server database lines stay commented:\n%s", got)
	}
}

func TestConfigureEnvForSQLiteCommentsServerLines(t *testing.T) {
	in := strings.ReplaceAll(template, "# DB_", "DB_")
	got := configureEnv(in, "https://shop.test", "sqlite", nil, "shop")
	if !strings.Contains(got, "DB_CONNECTION=sqlite") || !strings.Contains(got, "# DB_HOST=127.0.0.1") {
		t.Fatalf("SQLite .env wrong:\n%s", got)
	}
	if strings.Contains(got, "\nDB_PORT=") {
		t.Errorf("DB_PORT left active for SQLite:\n%s", got)
	}
}

func TestErrorsReadLikeTheCommandsTyped(t *testing.T) {
	j := &job{php: phpCommand(), phar: "/bench/composer.phar"}
	for _, c := range []struct{ argv, want string }{
		{"/bench/php|-c|/run/php-cli.ini|/bench/composer.phar|create-project|laravel/laravel|shop", "composer create-project laravel/laravel shop"},
		{"/bench/php|-c|/run/php-cli.ini|artisan|migrate|--force", "php artisan migrate --force"},
		{"/usr/bin/npm.cmd|run|build", "npm run build"},
	} {
		if got := j.shortCommand(strings.Split(c.argv, "|")); got != c.want {
			t.Errorf("shortCommand(%s) = %q, want %q", c.argv, got, c.want)
		}
	}
}

func TestLastLinesDropsTerminalCodes(t *testing.T) {
	out := "Installing\n\x1b[41;1m Pest\\Exceptions\\InvalidOption \x1b[49;22m\n\r  spinning\r  done\n"
	if got, want := lastLines(out, 2), `Pest\Exceptions\InvalidOption done`; got != want {
		t.Errorf("lastLines = %q, want %q", got, want)
	}
}

func TestConfigureEnvSetsExtraLines(t *testing.T) {
	got := configureEnv(template, "https://shop.test", "sqlite", nil, "shop", "APP_NAME=shop", "MAIL_PORT=2526")
	env := dotenv.Parse(got)
	if env["APP_NAME"] != "shop" || env["MAIL_PORT"] != "2526" {
		t.Errorf("extra lines not set:%s", got)
	}
}

func TestACancelledCreationSaysWhatItLeft(t *testing.T) {
	for _, served := range []bool{false, true} {
		r := &tasks.Registry{}
		task := r.Begin("site.new", "Creating shop.test", "shop", true)
		j := &job{task: task, req: api.NewSiteRequest{Name: "shop"}, dir: "/code/shop", served: served}
		if err := r.Cancel(r.List()[0].ID); err != nil {
			t.Fatal(err)
		}
		if err := task.End(j.leftover(context.Canceled)); !errors.Is(err, tasks.ErrCancelled) {
			t.Fatalf("End = %v, want cancelled", err)
		}
		got := r.List()[0]
		want := "kept in /code/shop"
		if served {
			want = "shop.test is created and served from /code/shop"
		}
		if got.State != api.TaskCancelled || !strings.Contains(got.Error, want) {
			t.Errorf("served=%v: task %s %q, want cancelled naming %q", served, got.State, got.Error, want)
		}
	}
}

func TestBoostUpdateJoinsPostUpdateCmd(t *testing.T) {
	path := filepath.Join(t.TempDir(), "composer.json")
	in := "{\n    \"scripts\": {\n        \"post-update-cmd\": [\n            \"@php artisan vendor:publish --tag=laravel-assets --ansi --force\"\n        ],\n        \"dev\": [\"npm run dev\"]\n    }\n}\n"
	if err := os.WriteFile(path, []byte(in), 0o644); err != nil {
		t.Fatal(err)
	}
	added, err := addBoostUpdateScript(path)
	if err != nil || !added {
		t.Fatalf("added %v, err %v", added, err)
	}
	b, _ := os.ReadFile(path)
	var c struct {
		Scripts map[string][]string `json:"scripts"`
	}
	if err := json.Unmarshal(b, &c); err != nil {
		t.Fatalf("composer.json no longer parses: %v\n%s", err, b)
	}
	want := []string{"@php artisan vendor:publish --tag=laravel-assets --ansi --force", "@php artisan boost:update --ansi"}
	if !slices.Equal(c.Scripts["post-update-cmd"], want) || len(c.Scripts["dev"]) != 1 {
		t.Fatalf("scripts = %v", c.Scripts)
	}
}

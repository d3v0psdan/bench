// Package newapp creates a Laravel app the way `laravel new` does (the
// same questions, starter-kit packages and post-create steps, from
// laravel/installer's NewCommand.php), as one cancellable daemon task,
// then serves it from Bench.
package newapp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"

	"github.com/d3v0psdan/bench/daemon/internal/api"
	"github.com/d3v0psdan/bench/daemon/internal/binman"
	"github.com/d3v0psdan/bench/daemon/internal/desktop"
	"github.com/d3v0psdan/bench/daemon/internal/phppool"
	"github.com/d3v0psdan/bench/daemon/internal/services"
	"github.com/d3v0psdan/bench/daemon/internal/sites"
	"github.com/d3v0psdan/bench/daemon/internal/supervisor"
	"github.com/d3v0psdan/bench/daemon/internal/tasks"
)

// SettingProjectsDir is the folder new apps go into ("" = ~/Bench). Bench
// parks it, so every app created there is served without a link.
const SettingProjectsDir = "projects.dir"

// createTimeout bounds a whole creation: composer and npm downloads on a
// slow connection take minutes, never an hour.
const createTimeout = 45 * time.Minute

// InputError is a request the caller can fix (maps to 400).
type InputError struct{ Err error }

func (e InputError) Error() string { return e.Err.Error() }

// Creator runs creations; wired by main.
type Creator struct {
	Root     string // bench home (logs)
	Bin      *binman.Manager
	Pools    *phppool.Pools
	Sites    *sites.Manager
	Services *services.Manager
	Tasks    *tasks.Registry
	Log      *slog.Logger
}

// ProjectsDir is where new apps go by default.
func (c *Creator) ProjectsDir() (string, error) {
	dir, err := c.Sites.Reg.Setting(SettingProjectsDir)
	if err != nil || dir != "" {
		return dir, err
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, "Bench"), nil
}

// Start validates the request and begins the task; the creation runs in
// the background and the task is returned at once.
func (c *Creator) Start(ctx context.Context, r api.NewSiteRequest) (api.Task, error) {
	name, err := sites.NormalizeName(r.Name)
	if err != nil {
		return api.Task{}, InputError{err}
	}
	r.Name = name
	if err := Validate(r); err != nil {
		return api.Task{}, InputError{err}
	}
	projects, err := c.ProjectsDir()
	if err != nil {
		return api.Task{}, err
	}
	parent := r.Dir
	if parent == "" {
		parent = projects
	}
	if parent, err = filepath.Abs(parent); err != nil {
		return api.Task{}, InputError{err}
	}
	target := filepath.Join(parent, name)
	if _, err := os.Stat(target); err == nil {
		return api.Task{}, InputError{fmt.Errorf("%s already exists; pick another name or folder", target)}
	}
	list, err := c.Sites.List(ctx)
	if err != nil {
		return api.Task{}, err
	}
	for _, s := range list {
		if s.Name == name {
			return api.Task{}, InputError{fmt.Errorf("%s already exists", s.Host)}
		}
	}

	task := c.Tasks.Begin("site.new", "Creating "+name+".test", name, true)
	logFile := filepath.Join(c.Root, "logs", "new-"+name+".log")
	task.SetLogFile(logFile)
	job := &job{c: c, task: task, req: r, parent: parent, dir: target, parksParent: sameDir(parent, projects)}
	go job.run(logFile)
	return task.Snapshot(), nil
}

// job is one creation in flight.
type job struct {
	c           *Creator
	task        *tasks.Task
	req         api.NewSiteRequest
	parent, dir string
	parksParent bool // the parent is the projects folder, so park it

	out     io.Writer
	php     phppool.Command
	phar    string
	step    int
	steps   int
	served  bool // the site is up; later failures are partial success
	service api.Service
}

func (j *job) run(logFile string) {
	ctx, cancel := context.WithTimeout(j.task.Context(), createTimeout)
	defer cancel()
	err := j.create(ctx, logFile)
	if err != nil {
		err = j.leftover(err)
	}
	if err = j.task.End(err); err != nil {
		j.c.Log.Warn("creating a site failed", "name", j.req.Name, "err", err)
	}
}

// leftover says what a failed or cancelled creation left behind.
func (j *job) leftover(err error) error {
	if j.served {
		return tasks.Leftover{Err: err, What: fmt.Sprintf("%s.test is created and served from %s", j.req.Name, j.dir)}
	}
	return tasks.Leftover{Err: err, What: "whatever was created is kept in " + j.dir}
}

func (j *job) create(ctx context.Context, logFile string) error {
	if err := os.MkdirAll(filepath.Dir(logFile), 0o755); err != nil {
		return err
	}
	f, err := os.Create(logFile)
	if err != nil {
		return fmt.Errorf("opening the log: %w", err)
	}
	defer f.Close()
	j.out = f

	r := j.req
	steps := []struct {
		label string
		skip  bool
		run   func(context.Context) error
	}{
		{"Preparing PHP and Composer", false, j.prepare},
		{"Creating the project", false, j.createProject},
		{"Configuring the app", false, j.configure},
		{"Migrating the database", r.Database != "sqlite" && !r.Migrate, j.migrate},
		{"Serving " + r.Name + ".test", false, j.serve},
		{"Installing Pest", r.Testing != "pest", j.installPest},
		{"Installing " + r.PackageManager + " packages", r.PackageManager == "", j.installPackages},
		{"Building assets", r.PackageManager == "", j.build},
		{"Installing Laravel Boost", !r.Boost, j.installBoost},
	}
	for _, s := range steps {
		if !s.skip {
			j.steps++
		}
	}
	for _, s := range steps {
		if s.skip {
			continue
		}
		j.step++
		j.task.SetPhase(fmt.Sprintf("Step %d of %d: %s", j.step, j.steps, strings.ToLower(s.label[:1])+s.label[1:]))
		fmt.Fprintf(j.out, "\n== %s\n", s.label)
		if err := s.run(ctx); err != nil {
			return fmt.Errorf("%s failed: %w", strings.ToLower(s.label[:1])+s.label[1:], err)
		}
	}
	return nil
}

// prepare installs the default PHP and Composer if they're missing.
func (j *job) prepare(ctx context.Context) error {
	channel, err := j.c.Sites.DefaultPHP()
	if err != nil {
		return fmt.Errorf("reading the default PHP: %w", err)
	}
	for _, bin := range []struct{ name, channel string }{{"php", channel}, {"composer", "2"}} {
		b, err := j.c.Bin.Resolve(bin.name, bin.channel)
		if err != nil {
			return fmt.Errorf("finding %s %s: %w", bin.name, bin.channel, err)
		}
		j.task.Watch(bin.name, b.Version)
		if _, err := j.c.Bin.Install(ctx, bin.name, bin.channel); err != nil {
			return fmt.Errorf("installing %s %s: %w", bin.name, b.Version, err)
		}
		if bin.name == "composer" {
			j.phar = filepath.Join(j.c.Bin.Dir("composer", b.Version), "composer.phar")
		}
	}
	if j.php, err = j.c.Pools.CLI(channel); err != nil {
		return fmt.Errorf("preparing PHP %s: %w", channel, err)
	}
	if err := os.MkdirAll(j.parent, 0o755); err != nil {
		return fmt.Errorf("creating %s: %w", j.parent, err)
	}
	return nil
}

func (j *job) createProject(ctx context.Context) error {
	return j.composer(ctx, j.parent, createProjectArgs(j.req, j.req.Name)...)
}

// configure runs the installer's post-create steps and writes .env.
func (j *job) configure(ctx context.Context) error {
	if err := j.composer(ctx, j.dir, "run", "post-root-package-install"); err != nil {
		return err
	}
	if err := j.artisan(ctx, "key:generate", "--ansi"); err != nil {
		return err
	}
	var serviceEnv []string
	if j.req.Database == "sqlite" {
		db := filepath.Join(j.dir, "database", "database.sqlite")
		if _, err := os.Stat(db); errors.Is(err, os.ErrNotExist) {
			if err := os.WriteFile(db, nil, 0o644); err != nil {
				return err
			}
		}
	} else {
		svc, err := j.database(ctx)
		if err != nil {
			return err
		}
		serviceEnv = svc.Env
	}
	envPath := filepath.Join(j.dir, ".env")
	b, err := os.ReadFile(envPath)
	if err != nil {
		return err
	}
	env := configureEnv(string(b), "https://"+j.req.Name+".test", j.req.Database, serviceEnv, databaseName(j.req.Name), j.appLines()...)
	return os.WriteFile(envPath, []byte(env), 0o644)
}

// appLines name the app after its site, so its mail gets its own inbox
// (MAIL_USERNAME="${APP_NAME}"), and point it at Bench's mail catcher when
// there is one.
func (j *job) appLines() []string {
	lines := []string{"APP_NAME=" + j.req.Name}
	list, err := j.c.Services.List()
	if err != nil {
		return lines
	}
	for _, s := range list {
		if s.Service == "mailpit" {
			return append(lines, s.Env...)
		}
	}
	return lines
}

// database readies the Bench instance the app uses (the chosen one,
// started if stopped, or a new one) and creates the app's database in it.
func (j *job) database(ctx context.Context) (api.Service, error) {
	kind := map[string]string{"mysql": "mysql", "mariadb": "mariadb", "pgsql": "postgresql"}[j.req.Database]
	var svc api.Service
	var err error
	if j.req.Service == "" {
		fmt.Fprintf(j.out, "creating a %s instance for %s\n", kind, j.req.Name)
		svc, err = j.c.Services.Create(api.CreateServiceRequest{Service: kind, Name: j.req.Name + "-" + kind, Autostart: true})
	} else if svc, err = j.c.Services.Get(j.req.Service); err == nil && svc.State != "running" {
		svc, err = j.c.Services.Start(ctx, svc.Name)
	}
	if err != nil {
		return api.Service{}, err
	}
	if svc.Service != kind {
		return api.Service{}, fmt.Errorf("%s is %s, not %s", svc.Name, svc.Service, kind)
	}
	argv := desktop.TerminalClient(svc, runtime.GOOS)
	name := databaseName(j.req.Name) // letters, digits and underscores only
	if kind == "postgresql" {
		// PostgreSQL has no IF NOT EXISTS here; a new app's name is new.
		argv = append(argv, "-c", `CREATE DATABASE "`+name+`"`)
	} else {
		argv = append(argv, "-e", "CREATE DATABASE IF NOT EXISTS `"+name+"`")
	}
	j.service = svc
	return svc, j.exec(ctx, j.dir, argv, nil)
}

func (j *job) migrate(ctx context.Context) error {
	return j.artisan(ctx, "migrate", "--force", "--ansi")
}

// serve parks the projects folder (so this and later apps there are
// served), or links the app when it went elsewhere.
func (j *job) serve(ctx context.Context) error {
	var err error
	if j.parksParent {
		_, err = j.c.Sites.Park(ctx, j.parent)
	} else {
		_, err = j.c.Sites.Link(ctx, j.dir, j.req.Name)
	}
	if err == nil {
		j.served = true
	}
	return err
}

// installPest follows the installer's Pest setup, skipped when the
// project already requires Pest (the starter kits ship it).
func (j *job) installPest(ctx context.Context) error {
	if j.requires("pestphp/pest") {
		fmt.Fprintln(j.out, "pest is already installed")
		return nil
	}
	for _, args := range [][]string{
		{"remove", "phpunit/phpunit", "--dev", "--no-update"},
		{"require", "pestphp/pest", "pestphp/pest-plugin-laravel", "--no-update", "--dev"},
		{"update"},
	} {
		if err := j.composer(ctx, j.dir, args...); err != nil {
			return err
		}
	}
	if err := j.exec(ctx, j.dir, append(j.phpArgv(), "./vendor/bin/pest", "--init"), []string{"PEST_NO_SUPPORT=true"}); err != nil {
		return err
	}
	if err := j.composer(ctx, j.dir, "require", "pestphp/pest-plugin-drift", "--dev"); err != nil {
		return err
	}
	// Drift only rewrites the example tests in Pest's style; Pest runs them
	// either way. Pest 5's drift rejects the installer's call, so a failure
	// is noted in the log rather than failing the app.
	if err := j.exec(ctx, j.dir, append(j.phpArgv(), "./vendor/bin/pest", "--drift"), []string{"PEST_NO_SUPPORT=true"}); err != nil {
		fmt.Fprintf(j.out, "note: converting the example tests to Pest's style failed; they still run on Pest (%v)\n", err)
	}
	return j.composer(ctx, j.dir, "remove", "pestphp/pest-plugin-drift", "--dev")
}

// installPackages points composer's scripts at the chosen package manager
// (as the installer does for bun), then installs.
func (j *job) installPackages(ctx context.Context) error {
	pm, err := findTool(j.req.PackageManager)
	if err != nil {
		return err
	}
	if j.req.PackageManager == "bun" {
		if err := useBunInScripts(filepath.Join(j.dir, "composer.json")); err != nil {
			return err
		}
	}
	return j.exec(ctx, j.dir, []string{pm, "install"}, nil)
}

func (j *job) build(ctx context.Context) error {
	pm, err := findTool(j.req.PackageManager)
	if err != nil {
		return err
	}
	return j.exec(ctx, j.dir, []string{pm, "run", "build"}, nil)
}

func (j *job) installBoost(ctx context.Context) error {
	if err := j.composer(ctx, j.dir, "require", "laravel/boost:^2.2", "--dev", "-W"); err != nil {
		return err
	}
	if err := j.artisan(ctx, "boost:install", "--no-interaction"); err != nil {
		return err
	}
	added, err := addBoostUpdateScript(filepath.Join(j.dir, "composer.json"))
	if err != nil {
		return fmt.Errorf("adding boost:update to composer.json: %w", err)
	}
	if !added {
		fmt.Fprintln(j.out, "composer.json has no post-update-cmd; add \"@php artisan boost:update --ansi\" yourself")
	}
	return nil
}

// requires reports whether composer.json requires pkg (dev or not).
func (j *job) requires(pkg string) bool {
	b, err := os.ReadFile(filepath.Join(j.dir, "composer.json"))
	if err != nil {
		return false
	}
	var c struct {
		Require    map[string]string `json:"require"`
		RequireDev map[string]string `json:"require-dev"`
	}
	if json.Unmarshal(b, &c) != nil {
		return false
	}
	_, a := c.Require[pkg]
	_, b2 := c.RequireDev[pkg]
	return a || b2
}

func (j *job) phpArgv() []string { return append([]string(nil), j.php.Argv...) }

func (j *job) composer(ctx context.Context, dir string, args ...string) error {
	argv := append(append(j.phpArgv(), j.phar), args...)
	return j.exec(ctx, dir, argv, []string{"COMPOSER_NO_INTERACTION=1"})
}

func (j *job) artisan(ctx context.Context, args ...string) error {
	return j.exec(ctx, j.dir, append(append(j.phpArgv(), "artisan"), args...), nil)
}

// exec runs one command through the supervisor (killed on cancel),
// streaming its output into the task log. A failure carries the output's
// last lines, where tools put the reason.
func (j *job) exec(ctx context.Context, dir string, argv, env []string) error {
	short := j.shortCommand(argv)
	fmt.Fprintf(j.out, "$ %s\n", short)
	out, err := supervisor.Exec(ctx, supervisor.Spec{
		Command: argv[0],
		Args:    argv[1:],
		Dir:     dir,
		Env:     append(append(append([]string(nil), j.php.Env...), env...), "NO_COLOR=1"),
		Output:  j.out,
	})
	if err == nil {
		return nil
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	msg := fmt.Sprintf("`%s` failed", short)
	if tail := lastLines(out, 3); tail != "" {
		msg += ": " + tail
	}
	return errors.New(msg)
}

// shortCommand is argv as a person would type it: "composer install",
// "php artisan migrate", "npm run build", without paths or ini flags.
func (j *job) shortCommand(argv []string) string {
	rest := argv
	if len(j.php.Argv) > 0 && len(rest) >= len(j.php.Argv) && rest[0] == j.php.Argv[0] {
		rest = rest[len(j.php.Argv):]
		if len(rest) > 0 && rest[0] == j.phar {
			return strings.Join(append([]string{"composer"}, rest[1:]...), " ")
		}
		return strings.Join(append([]string{"php"}, rest...), " ")
	}
	name := strings.TrimSuffix(strings.TrimSuffix(filepath.Base(argv[0]), ".exe"), ".cmd")
	return strings.Join(append([]string{name}, argv[1:]...), " ")
}

// ansi matches terminal colour and cursor codes, which tools print even
// with NO_COLOR and which don't belong in an error shown in the GUI.
var ansi = regexp.MustCompile(`\x1b\[[0-9;?]*[A-Za-z]`)

// lastLines is the output's last n non-blank lines, without terminal
// codes; spinner redraws (\r) keep only their final state.
func lastLines(s string, n int) string {
	var lines []string
	for _, line := range strings.Split(ansi.ReplaceAllString(s, ""), "\n") {
		if i := strings.LastIndex(strings.TrimRight(line, "\r"), "\r"); i >= 0 {
			line = line[i+1:]
		}
		if line = strings.TrimSpace(line); line != "" {
			lines = append(lines, line)
		}
	}
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, " ")
}

// sameDir compares folders the way the OS does: case-insensitively on
// Windows and (by default) macOS.
func sameDir(a, b string) bool {
	a, errA := filepath.Abs(a)
	b, errB := filepath.Abs(b)
	if errA != nil || errB != nil {
		return false
	}
	if runtime.GOOS == "linux" {
		return a == b
	}
	return strings.EqualFold(a, b)
}

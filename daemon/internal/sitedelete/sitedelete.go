// Package sitedelete deletes a site and what Bench made for it: it stops
// serving the site, moves its project folder to the OS trash (recoverable),
// drops its database, deletes an instance made for it alone, and removes
// its inbox and creation log. Each part is optional and reported on its
// own, so a failure in one leaves a clear partial state.
package sitedelete

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/d3v0psdan/bench/daemon/internal/api"
	"github.com/d3v0psdan/bench/daemon/internal/desktop"
	"github.com/d3v0psdan/bench/daemon/internal/dotenv"
	"github.com/d3v0psdan/bench/daemon/internal/mail"
	"github.com/d3v0psdan/bench/daemon/internal/services"
	"github.com/d3v0psdan/bench/daemon/internal/sites"
	"github.com/d3v0psdan/bench/daemon/internal/supervisor"
	"github.com/d3v0psdan/bench/daemon/internal/tasks"
)

// deleteTimeout bounds the whole deletion; moving a big project (a full
// node_modules) to the Recycle Bin is the slow part.
const deleteTimeout = 15 * time.Minute

// ErrNotFound: no site has that name.
var ErrNotFound = errors.New("no such site")

// InputError is a request that can't be carried out as asked (HTTP 400).
type InputError struct{ Err error }

func (e InputError) Error() string { return e.Err.Error() }
func (e InputError) Unwrap() error { return e.Err }

// Deleter plans and runs site deletions.
type Deleter struct {
	Root     string // Bench home
	Sites    *sites.Manager
	Services *services.Manager
	Tasks    *tasks.Registry
	Mail     mail.Finder
	Log      *slog.Logger
}

// Plan says what deleting the site named name can clean up.
func (d *Deleter) Plan(ctx context.Context, name string) (api.SiteDeletePlan, error) {
	list, err := d.Sites.List(ctx)
	if err != nil {
		return api.SiteDeletePlan{}, fmt.Errorf("listing sites: %w", err)
	}
	svcs, err := d.Services.List()
	if err != nil {
		return api.SiteDeletePlan{}, fmt.Errorf("listing services: %w", err)
	}
	var plan api.SiteDeletePlan
	var db *api.SiteDatabase
	found := false
	u := newUsage()
	for _, s := range list {
		sdb := databaseOf(d.readEnv(s), svcs)
		u.add(s, sdb)
		if s.Name == name {
			found, plan.Site, db = true, s, sdb
		}
	}
	if !found {
		return api.SiteDeletePlan{}, ErrNotFound
	}
	s := plan.Site
	if s.Kind != "proxy" && s.Path != "" && !s.PathMissing {
		plan.Folder = s.Path
	}
	plan.Database, plan.Service, plan.MailTag = u.ownParts(s, db, svcs)
	if log := filepath.Join(d.Root, "logs", "new-"+s.Name+".log"); fileExists(log) {
		plan.LogFile = log
	}
	return plan, nil
}

// Start validates the request and begins the deletion as a task; it runs
// in the background and the task is returned at once.
func (d *Deleter) Start(ctx context.Context, name string, req api.SiteDeleteRequest) (api.Task, error) {
	plan, err := d.Plan(ctx, name)
	if err != nil {
		return api.Task{}, err
	}
	// A parked site is any folder in a parked folder: it stops being served
	// only when its folder leaves.
	if plan.Site.Kind == "parked" && (!req.Folder || plan.Folder == "") {
		return api.Task{}, InputError{fmt.Errorf("%s is served because its folder is in %s; move the folder to the trash to delete it, or unpark %s", plan.Site.Host, plan.Site.ParkedIn, plan.Site.ParkedIn)}
	}
	if req.Folder && plan.Folder != "" {
		if err := safeToTrash(plan.Folder, d.protected(plan.Site), d.Root); err != nil {
			return api.Task{}, InputError{err}
		}
	}
	// Not cancellable: stopping halfway leaves a site half removed, and
	// the Recycle Bin move can't be interrupted cleanly.
	task := d.Tasks.Begin("site.delete", "Deleting "+plan.Site.Host, plan.Site.Name, false)
	go d.run(plan, req, task)
	return task.Snapshot(), nil
}

func (d *Deleter) run(plan api.SiteDeletePlan, req api.SiteDeleteRequest, task *tasks.Task) {
	ctx, cancel := context.WithTimeout(task.Context(), deleteTimeout)
	defer cancel()
	s := plan.Site
	var done []string
	var failed []error
	step := func(what string, fn func() error) {
		task.SetPhase(strings.ToUpper(what[:1]) + what[1:])
		if err := fn(); err != nil {
			failed = append(failed, fmt.Errorf("%s: %w", what, err))
			return
		}
		done = append(done, what)
	}

	// Stop serving first, so nothing Bench runs holds files in the folder.
	if s.Kind != "parked" {
		step("stopping serving "+s.Host, func() error {
			_, err := d.Sites.Unlink(ctx, s.Name)
			return err
		})
	} else {
		// Not Unlink: that would also hide the folder if it's ever restored.
		step("forgetting its settings", func() error {
			_, err := d.Sites.ForgetParkedSettings(ctx, s.Name)
			return err
		})
	}
	if req.Folder && plan.Folder != "" {
		step("moving "+plan.Folder+" to the trash", func() error { return trash(ctx, plan.Folder) })
		if s.Kind == "parked" {
			step("stopping serving "+s.Host, func() error {
				_, err := d.Sites.Apply(ctx)
				return err
			})
		}
	}
	switch {
	case req.Service && plan.Service != "":
		step("deleting the "+plan.Service+" instance and its data", func() error {
			return d.Services.Delete(plan.Service, false)
		})
	case req.Database && plan.Database != nil:
		db := plan.Database
		step("dropping the "+db.Name+" database on "+db.Service, func() error { return d.dropDatabase(ctx, *db) })
	}
	if req.Mail && plan.MailTag != "" {
		step("deleting the "+plan.MailTag+" inbox", func() error {
			t, ok := d.Mail()
			if !ok {
				return errors.New("mail isn't running, so the inbox is kept")
			}
			return t.DeleteTag(ctx, plan.MailTag)
		})
	}
	if req.Log && plan.LogFile != "" {
		step("deleting its creation log", func() error { return os.Remove(plan.LogFile) })
	}

	var err error
	switch {
	case len(failed) > 0 && len(done) == 0:
		err = fmt.Errorf("couldn't delete %s: %w", s.Host, errors.Join(failed...))
	case len(failed) > 0:
		err = fmt.Errorf("%s is partly deleted: %w (done: %s)", s.Host, errors.Join(failed...), strings.Join(done, "; "))
	}
	if err = task.End(err); err != nil {
		d.Log.Warn("deleting a site failed", "site", s.Name, "err", err)
	}
}

// dbNameRe is what Bench will put in a DROP DATABASE: Laravel's own names
// (letters, digits, underscores), never quoting tricks.
var dbNameRe = regexp.MustCompile(`^[A-Za-z0-9_]+$`)

// systemDatabases are never dropped, whatever a .env says.
var systemDatabases = map[string]bool{
	"mysql": true, "information_schema": true, "performance_schema": true, "sys": true,
	"postgres": true, "template0": true, "template1": true,
}

func (d *Deleter) dropDatabase(ctx context.Context, db api.SiteDatabase) error {
	if !dbNameRe.MatchString(db.Name) || systemDatabases[strings.ToLower(db.Name)] {
		return fmt.Errorf("won't drop a database named %q", db.Name)
	}
	svc, err := d.Services.Get(db.Service)
	if err != nil {
		return err
	}
	if svc.State != "running" {
		return fmt.Errorf("%s is %s; start it to drop the database", svc.Name, svc.State)
	}
	argv := desktop.TerminalClient(svc, runtime.GOOS)
	if svc.Service == "postgresql" {
		// FORCE ends connections an app still holds (PostgreSQL 13+).
		argv = append(argv, "-c", `DROP DATABASE IF EXISTS "`+db.Name+`" WITH (FORCE)`)
	} else {
		argv = append(argv, "-e", "DROP DATABASE IF EXISTS `"+db.Name+"`")
	}
	if out, err := supervisor.Exec(ctx, supervisor.Spec{Command: argv[0], Args: argv[1:]}); err != nil {
		return fmt.Errorf("%w: %s", err, lastLine(out))
	}
	return nil
}

// readEnv reads a site's .env; a site without one (a proxy, a folder
// that's gone) has none.
func (d *Deleter) readEnv(s api.Site) map[string]string {
	if s.Path == "" || s.PathMissing {
		return nil
	}
	env, err := dotenv.Read(filepath.Join(s.Path, ".env"))
	if err != nil {
		d.Log.Warn("reading a site's .env", "site", s.Name, "err", err)
		return nil
	}
	return env
}

// protected are folders a trashed folder must never hold: the home folder,
// Bench's own, and the parked folder the site lives in.
func (d *Deleter) protected(s api.Site) []string {
	var out []string
	if home, err := os.UserHomeDir(); err == nil {
		out = append(out, home)
	}
	out = append(out, d.Root)
	if s.ParkedIn != "" {
		out = append(out, s.ParkedIn)
	}
	return out
}

// usage counts, across every site, who points at each instance, each
// database on it and each inbox, so a delete only offers what is the
// site's alone.
type usage struct {
	instances, databases, inboxes map[string]int
}

func newUsage() usage {
	return usage{instances: map[string]int{}, databases: map[string]int{}, inboxes: map[string]int{}}
}

func (u usage) add(s api.Site, db *api.SiteDatabase) {
	if db != nil {
		u.instances[db.Service]++
		u.databases[db.Service+"/"+db.Name]++
	}
	if s.AppName != "" {
		u.inboxes[s.AppName]++
	}
}

// ownParts is what of s's data only s uses: its database (apps that keep
// Laravel's default DB_DATABASE=laravel on one instance share it), an
// instance made for it alone, and its inbox (apps that keep the default
// APP_NAME share one). An inbox name with quotes isn't offered: it would
// have to be escaped inside Mailpit's search.
func (u usage) ownParts(s api.Site, db *api.SiteDatabase, svcs []api.Service) (*api.SiteDatabase, string, string) {
	var ownDB *api.SiteDatabase
	service := ""
	if db != nil && u.databases[db.Service+"/"+db.Name] == 1 {
		ownDB = db
		if u.instances[db.Service] == 1 && ownInstance(s.Name, db.Service, svcs) {
			service = db.Service
		}
	}
	mailTag := ""
	if s.AppName != "" && u.inboxes[s.AppName] == 1 && !strings.ContainsAny(s.AppName, `"\`) {
		mailTag = s.AppName
	}
	return ownDB, service, mailTag
}

// dbKinds maps Laravel's DB_CONNECTION to the Bench services that serve it
// (MySQL and MariaDB speak the same protocol).
var dbKinds = map[string][]string{
	"mysql":   {"mysql", "mariadb"},
	"mariadb": {"mariadb", "mysql"},
	"pgsql":   {"postgresql"},
}

// databaseOf finds the Bench instance and database an app's .env points
// at, or nil when it uses SQLite or a server Bench doesn't run.
func databaseOf(env map[string]string, svcs []api.Service) *api.SiteDatabase {
	kinds := dbKinds[env["DB_CONNECTION"]]
	name := env["DB_DATABASE"]
	if len(kinds) == 0 || name == "" {
		return nil
	}
	switch env["DB_HOST"] {
	case "127.0.0.1", "localhost", "":
	default:
		return nil
	}
	port, err := strconv.Atoi(env["DB_PORT"])
	if err != nil {
		return nil
	}
	for _, svc := range svcs {
		for _, k := range kinds {
			if svc.Service == k && svc.Port == port {
				return &api.SiteDatabase{Service: svc.Name, Name: name}
			}
		}
	}
	return nil
}

// ownInstance: the new-app wizard names an instance it makes for an app
// "<site>-<kind>"; any other instance may hold other apps' data.
func ownInstance(site, instance string, svcs []api.Service) bool {
	for _, svc := range svcs {
		if svc.Name == instance {
			return instance == site+"-"+svc.Service
		}
	}
	return false
}

// safeToTrash refuses anything but a real project folder: a directory, not
// a link, not a drive root, not a folder that holds one of protected, and
// nothing inside Bench's own folder (benchRoot: its data, builds and logs).
func safeToTrash(path string, protected []string, benchRoot string) error {
	clean := filepath.Clean(path)
	if !filepath.IsAbs(clean) || filepath.Dir(clean) == clean {
		return fmt.Errorf("won't move %s to the trash", path)
	}
	info, err := os.Lstat(clean)
	if err != nil {
		return fmt.Errorf("checking %s: %w", path, err)
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("%s isn't a plain folder, so it isn't moved", path)
	}
	for _, p := range protected {
		if p == "" {
			continue
		}
		p = filepath.Clean(p)
		if sameOrInside(p, clean) {
			return fmt.Errorf("won't move %s to the trash: it holds %s", path, p)
		}
	}
	if benchRoot != "" && sameOrInside(clean, filepath.Clean(benchRoot)) {
		return fmt.Errorf("won't move %s to the trash: it is inside Bench's own folder", path)
	}
	return nil
}

// sameOrInside reports whether child is dir or inside it.
func sameOrInside(child, dir string) bool {
	rel, err := filepath.Rel(dir, child)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel)
}

func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

// lastLine is the reason a client printed, without its preamble.
func lastLine(out string) string {
	lines := strings.Split(strings.TrimSpace(out), "\n")
	return strings.TrimSpace(lines[len(lines)-1])
}

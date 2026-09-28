package server

import (
	"errors"
	"fmt"
	"net/http"
	"path/filepath"
	"strconv"

	"github.com/d3v0psdan/bench/daemon/internal/api"
	"github.com/d3v0psdan/bench/daemon/internal/desktop"
	"github.com/d3v0psdan/bench/daemon/internal/dotenv"
)

// Opening things in the user's own apps: a database client, an editor, a
// terminal. The daemon launches them so the CLI, the GUI and curl behave
// the same.

// registerOpenRoutes wires the "open in ..." endpoints.
func (s *Server) registerOpenRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/services/{name}/open", s.withServices(func(w http.ResponseWriter, r *http.Request) error {
		svc, err := s.Services.Get(r.PathValue("name"))
		if err != nil {
			return err
		}
		return s.openDatabase(w, svc)
	}))
	mux.HandleFunc("GET /api/editors", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, desktop.Editors())
	})
	mux.HandleFunc("POST /api/sites/{name}/open", s.handleOpenSite)
}

func (s *Server) openDatabase(w http.ResponseWriter, svc api.Service) error {
	if !desktop.IsDatabase(svc.Service) {
		return errBadRequest{fmt.Errorf("%s is not a database", svc.Name)}
	}
	if svc.State != "running" {
		return errConflict{fmt.Errorf("%s isn't running; start it, then open it again", svc.Name)}
	}
	app, err := desktop.OpenDatabase(svc)
	if errors.Is(err, desktop.ErrNoApp) {
		return errConflict{fmt.Errorf("no database app found for %s; install TablePlus or DBeaver, or connect any client to %s", svc.Name, svc.URL)}
	}
	if err != nil {
		return fmt.Errorf("opening %s: %w", svc.Name, err)
	}
	writeJSON(w, api.Opened{App: app})
	return nil
}

func (s *Server) handleOpenSite(w http.ResponseWriter, r *http.Request) {
	if s.Sites == nil {
		http.Error(w, "site manager unavailable", http.StatusServiceUnavailable)
		return
	}
	err := s.openSite(w, r)
	if err == nil {
		return
	}
	status := http.StatusInternalServerError
	switch {
	case errors.As(err, new(errBadRequest)):
		status = http.StatusBadRequest
	case errors.As(err, new(errNotFound)):
		status = http.StatusNotFound
	case errors.As(err, new(errConflict)):
		status = http.StatusConflict
	default:
		s.Log.Warn("open request failed", "path", r.URL.Path, "err", err)
	}
	http.Error(w, err.Error(), status)
}

func (s *Server) openSite(w http.ResponseWriter, r *http.Request) error {
	var req api.OpenSiteRequest
	if err := decodeBody(w, r, &req); err != nil {
		return err
	}
	site, err := s.findSite(r, r.PathValue("name"))
	if err != nil {
		return err
	}
	if site.Path == "" {
		return errBadRequest{fmt.Errorf("%s is a proxy and has no folder to open", site.Host)}
	}
	if site.PathMissing {
		return errConflict{fmt.Errorf("%s's folder %s is gone; relink the site to open it", site.Host, site.Path)}
	}
	var app string
	switch req.In {
	case "editor":
		app, err = s.openInEditor(req.Editor, site.Path)
	case "terminal":
		app, err = desktop.OpenTerminal(site.Path)
	case "database":
		return s.openSiteDatabase(w, site)
	default:
		return errBadRequest{fmt.Errorf("can't open a site in %q: use editor, terminal or database", req.In)}
	}
	if errors.Is(err, desktop.ErrNoApp) {
		return errConflict{err}
	}
	if err != nil {
		return err
	}
	writeJSON(w, api.Opened{App: app})
	return nil
}

// openInEditor uses the named editor, else the editor.default setting,
// else the first installed one.
func (s *Server) openInEditor(id, dir string) (string, error) {
	if id == "" {
		saved, err := s.Sites.Reg.Setting(settingEditor)
		if err != nil {
			return "", err
		}
		id = saved
	}
	if id == "" {
		for _, e := range desktop.Editors() {
			if e.Installed {
				id = e.ID
				break
			}
		}
	}
	if id == "" {
		return "", fmt.Errorf("%w: install VS Code, PhpStorm, Cursor or Zed", desktop.ErrNoApp)
	}
	if !desktop.KnownEditor(id) {
		return "", errBadRequest{fmt.Errorf("unknown editor %q", id)}
	}
	return desktop.OpenInEditor(id, dir)
}

// openSiteDatabase opens the Bench service the site's .env points at.
func (s *Server) openSiteDatabase(w http.ResponseWriter, site api.Site) error {
	if s.Services == nil {
		return errors.New("service manager unavailable")
	}
	list, err := s.Services.List()
	if err != nil {
		return err
	}
	env, err := dotenv.Read(filepath.Join(site.Path, ".env"))
	if err != nil {
		return err
	}
	svc, err := siteDatabase(site.Host, env, list)
	if err != nil {
		return err
	}
	return s.openDatabase(w, svc)
}

// dbDefaults maps Laravel's DB_CONNECTION to the service kinds that can
// serve it and the port Laravel assumes when DB_PORT is unset.
var dbDefaults = map[string]struct {
	kinds []string
	port  int
}{
	"mysql":   {[]string{"mysql", "mariadb"}, 3306},
	"mariadb": {[]string{"mariadb", "mysql"}, 3306},
	"pgsql":   {[]string{"postgresql"}, 5432},
}

// siteDatabase finds the service a Laravel .env connects to, matching the
// connection kind and port on this machine.
func siteDatabase(host string, env map[string]string, list []api.Service) (api.Service, error) {
	conn := env["DB_CONNECTION"]
	if conn == "" {
		return api.Service{}, errConflict{fmt.Errorf("%s has no DB_CONNECTION in its .env; connect it to a Bench database first", host)}
	}
	if conn == "sqlite" {
		return api.Service{}, errConflict{fmt.Errorf("%s uses SQLite, a file in its folder rather than a Bench service; open that file in your database app", host)}
	}
	d, ok := dbDefaults[conn]
	if !ok {
		return api.Service{}, errConflict{fmt.Errorf("%s uses DB_CONNECTION=%s, which no Bench service serves", host, conn)}
	}
	if h := env["DB_HOST"]; h != "" && h != "127.0.0.1" && h != "localhost" {
		return api.Service{}, errConflict{fmt.Errorf("%s's database is on %s, not this machine", host, h)}
	}
	port := d.port
	if p, err := strconv.Atoi(env["DB_PORT"]); err == nil {
		port = p
	}
	for _, svc := range list {
		for _, kind := range d.kinds {
			if svc.Service == kind && svc.Port == port {
				return svc, nil
			}
		}
	}
	return api.Service{}, errConflict{fmt.Errorf("no Bench database listens on port %d, which %s's .env uses; create one, or connect the site to an existing one", port, host)}
}

// findSite looks a site up by name.
func (s *Server) findSite(r *http.Request, name string) (api.Site, error) {
	list, err := s.Sites.List(r.Context())
	if err != nil {
		return api.Site{}, err
	}
	for _, site := range list {
		if site.Name == name {
			return site, nil
		}
	}
	return api.Site{}, errNotFound{fmt.Errorf("no site named %q", name)}
}

type errConflict struct{ err error }

func (e errConflict) Error() string { return e.err.Error() }

type errNotFound struct{ err error }

func (e errNotFound) Error() string { return e.err.Error() }

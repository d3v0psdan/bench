package server

import (
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"

	"github.com/d3v0psdan/bench/daemon/internal/api"
	"github.com/d3v0psdan/bench/daemon/internal/dotenv"
)

// registerEnvRoutes wires writing Bench's connection lines into a site's
// .env; the GUI shows a dry run for consent before writing.
func (s *Server) registerEnvRoutes(mux *http.ServeMux) {
	// A site's own icon, from its public folder (favicon first): the GUI
	// shows it wherever the site is named, and a letter tile without one.
	mux.HandleFunc("GET /api/sites/{name}/icon", func(w http.ResponseWriter, r *http.Request) {
		if s.Sites == nil {
			http.Error(w, "site manager unavailable", http.StatusServiceUnavailable)
			return
		}
		site, err := s.findSite(r, r.PathValue("name"))
		if err != nil || site.Path == "" {
			http.NotFound(w, r)
			return
		}
		for _, name := range siteIcons {
			p := filepath.Join(site.Path, "public", name)
			// Lstat: a symlinked "favicon" could point at any file of the
			// user's (a project folder is untrusted input).
			if fi, err := os.Lstat(p); err == nil && fi.Mode().IsRegular() && fi.Size() <= maxIconBytes {
				w.Header().Set("Cache-Control", "no-cache")
				w.Header().Set("X-Content-Type-Options", "nosniff")
				// An SVG is active content; never let it run anything.
				w.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; sandbox")
				http.ServeFile(w, r, p)
				return
			}
		}
		http.NotFound(w, r)
	})
	mux.HandleFunc("POST /api/sites/{name}/env", func(w http.ResponseWriter, r *http.Request) {
		if s.Sites == nil {
			http.Error(w, "site manager unavailable", http.StatusServiceUnavailable)
			return
		}
		res, err := s.updateEnv(w, r)
		var bad errBadRequest
		var missing errNotFound
		switch {
		case errors.As(err, &bad):
			http.Error(w, err.Error(), http.StatusBadRequest)
		case errors.As(err, &missing):
			http.Error(w, err.Error(), http.StatusNotFound)
		case err != nil:
			s.Log.Warn("updating .env failed", "path", r.URL.Path, "err", err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
		default:
			writeJSON(w, res)
		}
	})
}

// siteIcons are the files a Laravel app's icon can be, best first; the
// names are fixed, so nothing outside public/ can be served.
var siteIcons = []string{"favicon.svg", "favicon.png", "apple-touch-icon.png", "favicon.ico"}

// maxIconBytes skips files too big to be an icon.
const maxIconBytes = 1 << 20

func (s *Server) updateEnv(w http.ResponseWriter, r *http.Request) (api.EnvResult, error) {
	var req api.EnvRequest
	if err := decodeBody(w, r, &req); err != nil {
		return api.EnvResult{}, err
	}
	for _, line := range req.Lines {
		if !dotenv.ValidLine(line) {
			return api.EnvResult{}, errBadRequest{fmt.Errorf("not a .env line: %q", line)}
		}
	}
	site, err := s.findSite(r, r.PathValue("name"))
	if err != nil {
		return api.EnvResult{}, err
	}
	if site.Path == "" {
		return api.EnvResult{}, errBadRequest{fmt.Errorf("%s is a proxy and has no .env", site.Host)}
	}
	path := filepath.Join(site.Path, ".env")
	// A project folder is untrusted: a .env that is a symlink would have
	// Bench rewrite some other file, so only a regular file is edited.
	if fi, err := os.Lstat(path); err == nil && !fi.Mode().IsRegular() {
		return api.EnvResult{}, errBadRequest{fmt.Errorf("%s isn't a regular file (a link?); Bench only edits a plain .env", path)}
	}
	b, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return api.EnvResult{}, err
	}
	content, changes := dotenv.Apply(string(b), req.Lines)
	res := api.EnvResult{Path: path, Changes: make([]api.EnvChange, 0, len(changes))}
	for _, c := range changes {
		res.Changes = append(res.Changes, api.EnvChange(c))
	}
	if req.DryRun || len(changes) == 0 {
		return res, nil
	}
	if err := writeAtomic(path, []byte(content)); err != nil {
		return api.EnvResult{}, fmt.Errorf("writing %s: %w", path, err)
	}
	return res, nil
}

// writeAtomic replaces path through a temp file in the same folder, so a
// crash never leaves half a .env and a link planted meanwhile is replaced,
// not written through.
func writeAtomic(path string, data []byte) error {
	f, err := os.CreateTemp(filepath.Dir(path), ".env.bench-*")
	if err != nil {
		return err
	}
	tmp := f.Name()
	if _, err := f.Write(data); err != nil {
		f.Close()
		os.Remove(tmp)
		return err
	}
	if err := f.Close(); err != nil {
		os.Remove(tmp)
		return err
	}
	if err := os.Chmod(tmp, 0o644); err != nil {
		os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return err
	}
	return nil
}

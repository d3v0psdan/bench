package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"path/filepath"
	"strings"

	"github.com/d3v0psdan/bench/daemon/internal/api"
	"github.com/d3v0psdan/bench/daemon/internal/desktop"
	"github.com/d3v0psdan/bench/daemon/internal/newapp"
)

// registerSiteRoutes wires the site-model endpoints. Every mutation
// responds with the fresh site list after pools + Caddy were re-applied.
func (s *Server) registerSiteRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/sites", s.sitesHandler(func(r *http.Request) ([]api.Site, error) {
		return s.Sites.List(r.Context())
	}))
	mux.HandleFunc("GET /api/parked", func(w http.ResponseWriter, r *http.Request) {
		if s.Sites == nil {
			http.Error(w, "site manager unavailable", http.StatusServiceUnavailable)
			return
		}
		dirs, err := s.Sites.ParkedDirs()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if dirs == nil {
			dirs = []string{}
		}
		writeJSON(w, dirs)
	})
	mux.HandleFunc("GET /api/settings", func(w http.ResponseWriter, r *http.Request) {
		if s.Sites == nil {
			http.Error(w, "site manager unavailable", http.StatusServiceUnavailable)
			return
		}
		php, err := s.Sites.DefaultPHP()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		editor, err := s.Sites.Reg.Setting(settingEditor)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		updatesCheck, err := s.Sites.Reg.Setting(settingUpdatesCheck)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if updatesCheck == "" {
			updatesCheck = "on"
		}
		settings := map[string]string{"php.default": php, settingEditor: editor, settingUpdatesCheck: updatesCheck}
		if s.NewApp != nil {
			dir, err := s.NewApp.ProjectsDir()
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			settings[newapp.SettingProjectsDir] = dir
		}
		writeJSON(w, settings)
	})

	mux.HandleFunc("POST /api/sites/park", s.siteMutation(func(r *http.Request, dec *json.Decoder) ([]api.Site, error) {
		var req api.PathRequest
		if err := dec.Decode(&req); err != nil {
			return nil, errBadRequest{err}
		}
		return s.Sites.Park(r.Context(), req.Path)
	}))
	mux.HandleFunc("POST /api/sites/unpark", s.siteMutation(func(r *http.Request, dec *json.Decoder) ([]api.Site, error) {
		var req api.PathRequest
		if err := dec.Decode(&req); err != nil {
			return nil, errBadRequest{err}
		}
		return s.Sites.Unpark(r.Context(), req.Path)
	}))
	mux.HandleFunc("POST /api/sites/link", s.siteMutation(func(r *http.Request, dec *json.Decoder) ([]api.Site, error) {
		var req api.LinkRequest
		if err := dec.Decode(&req); err != nil {
			return nil, errBadRequest{err}
		}
		return s.Sites.Link(r.Context(), req.Path, req.Name)
	}))
	mux.HandleFunc("POST /api/sites/unlink", s.siteMutation(func(r *http.Request, dec *json.Decoder) ([]api.Site, error) {
		var req api.NameRequest
		if err := dec.Decode(&req); err != nil {
			return nil, errBadRequest{err}
		}
		return s.Sites.Unlink(r.Context(), req.Name)
	}))
	mux.HandleFunc("POST /api/sites/php", s.siteMutation(func(r *http.Request, dec *json.Decoder) ([]api.Site, error) {
		var req api.SitePHPRequest
		if err := dec.Decode(&req); err != nil {
			return nil, errBadRequest{err}
		}
		return s.Sites.SetSitePHP(r.Context(), req.Name, s.phpChannel(req.Version))
	}))
	mux.HandleFunc("POST /api/sites/favorite", s.siteMutation(func(r *http.Request, dec *json.Decoder) ([]api.Site, error) {
		var req api.FavoriteRequest
		if err := dec.Decode(&req); err != nil {
			return nil, errBadRequest{err}
		}
		return s.Sites.SetFavorite(req.Name, req.Favorite)
	}))
	mux.HandleFunc("POST /api/sites/proxy", s.siteMutation(func(r *http.Request, dec *json.Decoder) ([]api.Site, error) {
		var req api.ProxyRequest
		if err := dec.Decode(&req); err != nil {
			return nil, errBadRequest{err}
		}
		return s.Sites.Proxy(r.Context(), req.Name, req.Target)
	}))
	mux.HandleFunc("POST /api/settings", s.siteMutation(func(r *http.Request, dec *json.Decoder) ([]api.Site, error) {
		var req api.SettingRequest
		if err := dec.Decode(&req); err != nil {
			return nil, errBadRequest{err}
		}
		switch req.Key {
		case "php.default":
			return s.Sites.SetDefaultPHP(r.Context(), s.phpChannel(req.Value))
		case settingEditor:
			if req.Value != "" && !desktop.KnownEditor(req.Value) {
				return nil, errBadRequest{fmt.Errorf("unknown editor %q", req.Value)}
			}
			if err := s.Sites.Reg.SetSetting(settingEditor, req.Value); err != nil {
				return nil, err
			}
			return s.Sites.List(r.Context())
		case settingUpdatesCheck:
			if req.Value != "on" && req.Value != "off" {
				return nil, errBadRequest{fmt.Errorf("updates.check is on or off, not %q", req.Value)}
			}
			if err := s.Sites.Reg.SetSetting(settingUpdatesCheck, req.Value); err != nil {
				return nil, err
			}
			return s.Sites.List(r.Context())
		case newapp.SettingProjectsDir:
			if req.Value != "" && !filepath.IsAbs(req.Value) {
				return nil, errBadRequest{fmt.Errorf("the projects folder must be a full path, not %q", req.Value)}
			}
			if err := s.Sites.Reg.SetSetting(newapp.SettingProjectsDir, req.Value); err != nil {
				return nil, err
			}
			return s.Sites.List(r.Context())
		}
		return nil, errBadRequest{errors.New("unknown setting " + req.Key)}
	}))
}

// settingEditor is the editor "Open in editor" uses ("" = first installed).
const settingEditor = "editor.default"

type errBadRequest struct{ err error }

func (e errBadRequest) Error() string { return e.err.Error() }

func (s *Server) sitesHandler(fn func(*http.Request) ([]api.Site, error)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.Sites == nil {
			http.Error(w, "site manager unavailable", http.StatusServiceUnavailable)
			return
		}
		list, err := fn(r)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if list == nil {
			list = []api.Site{}
		}
		writeJSON(w, list)
	}
}

func (s *Server) siteMutation(fn func(*http.Request, *json.Decoder) ([]api.Site, error)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.Sites == nil {
			http.Error(w, "site manager unavailable", http.StatusServiceUnavailable)
			return
		}
		dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
		list, err := fn(r, dec)
		if err != nil {
			status := http.StatusInternalServerError
			var bad errBadRequest
			if errors.As(err, &bad) || strings.Contains(err.Error(), "no site named") {
				status = http.StatusBadRequest
			}
			http.Error(w, err.Error(), status)
			return
		}
		if list == nil {
			list = []api.Site{}
		}
		writeJSON(w, list)
	}
}

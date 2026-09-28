package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"time"

	"github.com/d3v0psdan/bench/daemon/internal/api"
	"github.com/d3v0psdan/bench/daemon/internal/paths"
	"github.com/d3v0psdan/bench/daemon/internal/updates"
)

// Settings for the update check (PLAN.md §4: on by default, one request a
// day, an off switch).
const (
	settingUpdatesCheck = "updates.check" // "off" disables automatic checks
	settingUpdatesCache = "updates.cache" // the last answer, as JSON
	updateCheckEvery    = 24 * time.Hour
	updateCheckTimeout  = 10 * time.Second
)

// registerAboutRoutes wires what Settings > About and Diagnostics show:
// the update check, Bench's own log and the command-line tool's place.
func (s *Server) registerAboutRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/updates", s.handleUpdates)
	mux.HandleFunc("GET /api/php/{channel}", func(w http.ResponseWriter, r *http.Request) {
		if s.PHPInfo == nil {
			http.Error(w, "php details unavailable", http.StatusServiceUnavailable)
			return
		}
		info, err := s.PHPInfo(r.Context(), s.phpChannel(r.PathValue("channel")))
		if err != nil {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		writeJSON(w, info)
	})
	mux.HandleFunc("GET /api/daemon/log", func(w http.ResponseWriter, r *http.Request) {
		path, err := paths.LogFile()
		if err == nil {
			err = serveLogTail(w, r, path)
		}
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	})
	mux.HandleFunc("GET /api/cli", func(w http.ResponseWriter, r *http.Request) {
		info := api.CLIInfo{}
		if p, err := exec.LookPath("bench"); err == nil {
			info.OnPath = p
		}
		if self, err := os.Executable(); err == nil {
			name := "bench"
			if runtime.GOOS == "windows" {
				name += ".exe"
			}
			if p := filepath.Join(filepath.Dir(self), name); fileExists(p) {
				info.Bundled = p
			}
		}
		writeJSON(w, info)
	})
}

// handleUpdates answers from the last check while it is under a day old;
// otherwise, or with ?force=1, it asks GitHub (only when checks are on or
// forced). The answer is kept in settings, so restarts don't re-ask.
func (s *Server) handleUpdates(w http.ResponseWriter, r *http.Request) {
	if s.Sites == nil {
		http.Error(w, "settings unavailable", http.StatusServiceUnavailable)
		return
	}
	s.updMu.Lock()
	defer s.updMu.Unlock()
	reg := s.Sites.Reg
	check, err := reg.Setting(settingUpdatesCheck)
	if err != nil {
		// Unknown means off: never contact GitHub against the user's choice.
		s.Log.Warn("reading the update check setting", "err", err)
	}
	enabled := err == nil && check != "off"
	var info api.UpdateInfo
	raw, err := reg.Setting(settingUpdatesCache)
	if err != nil {
		s.Log.Warn("reading the cached update check", "err", err)
	}
	if raw != "" {
		if err := json.Unmarshal([]byte(raw), &info); err != nil {
			s.Log.Warn("cached update check is unreadable; asking again", "err", err)
		}
	}
	force := r.URL.Query().Get("force") == "1"
	stale := info.CheckedAt == nil || time.Since(*info.CheckedAt) > updateCheckEvery
	if force || (enabled && stale) {
		ctx, cancel := context.WithTimeout(r.Context(), updateCheckTimeout)
		url := s.UpdatesURL
		if url == "" {
			url = updates.LatestURL
		}
		rel, err := updates.Latest(ctx, http.DefaultClient, url)
		cancel()
		now := time.Now()
		info = api.UpdateInfo{CheckedAt: &now}
		switch {
		case errors.Is(err, updates.ErrNoRelease):
			// nothing published yet: not an error, nothing newer
		case err != nil:
			info.Error = err.Error()
		default:
			info.Latest, info.URL = rel.Version, rel.URL
		}
		if b, err := json.Marshal(info); err == nil {
			if err := reg.SetSetting(settingUpdatesCache, string(b)); err != nil {
				s.Log.Warn("saving the update check failed", "err", err)
			}
		}
	}
	info.Enabled = enabled
	info.Current = s.Version
	info.Newer = info.Latest != "" && updates.Newer(info.Latest, s.Version)
	writeJSON(w, info)
}

func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

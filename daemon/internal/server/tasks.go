package server

import (
	"errors"
	"net/http"

	"github.com/d3v0psdan/bench/daemon/internal/tasks"
)

// registerTaskRoutes wires the Activity endpoints: the running and recent
// long operations, and cancelling one. The list also streams as a "tasks"
// WS event on every change.
func (s *Server) registerTaskRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/tasks", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, s.Tasks.List())
	})
	// A task's own output (new-site creations keep one), for the step
	// sections in Activity: the tail, and a socket that follows it.
	mux.HandleFunc("GET /api/tasks/{id}/log", func(w http.ResponseWriter, r *http.Request) {
		path, ok := s.taskLog(w, r)
		if !ok {
			return
		}
		err := serveLogTail(w, r, path)
		if errors.As(err, new(errBadRequest)) {
			http.Error(w, err.Error(), http.StatusBadRequest)
		} else if err != nil {
			s.Log.Warn("reading a task log failed", "path", path, "err", err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	})
	mux.HandleFunc("GET /api/tasks/{id}/log/follow", func(w http.ResponseWriter, r *http.Request) {
		if path, ok := s.taskLog(w, r); ok {
			s.followLog(w, r, path)
		}
	})
	mux.HandleFunc("POST /api/tasks/{id}/cancel", func(w http.ResponseWriter, r *http.Request) {
		err := s.Tasks.Cancel(r.PathValue("id"))
		switch {
		case err == nil:
			w.WriteHeader(http.StatusAccepted)
		case errors.Is(err, tasks.ErrNotFound):
			http.Error(w, err.Error(), http.StatusNotFound)
		case errors.Is(err, tasks.ErrNotCancellable):
			http.Error(w, err.Error(), http.StatusConflict)
		default:
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	})
}

// taskLog finds the log file of the task in the path; it answers 404 when
// there's no such task or it keeps no log.
func (s *Server) taskLog(w http.ResponseWriter, r *http.Request) (string, bool) {
	id := r.PathValue("id")
	for _, t := range s.Tasks.List() {
		if t.ID == id && t.LogFile != "" {
			return t.LogFile, true
		}
	}
	http.Error(w, "no log for task "+id, http.StatusNotFound)
	return "", false
}

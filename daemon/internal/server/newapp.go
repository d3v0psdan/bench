package server

import (
	"errors"
	"net/http"

	"github.com/d3v0psdan/bench/daemon/internal/api"
	"github.com/d3v0psdan/bench/daemon/internal/newapp"
)

// registerNewAppRoutes wires creating Laravel apps and the tool check the
// wizard shows.
func (s *Server) registerNewAppRoutes(mux *http.ServeMux) {
	// Starts the creation and answers at once with its task (202); the
	// task's progress arrives as "tasks" events.
	mux.HandleFunc("POST /api/sites/new", func(w http.ResponseWriter, r *http.Request) {
		if s.NewApp == nil {
			http.Error(w, "creating apps is unavailable", http.StatusServiceUnavailable)
			return
		}
		var req api.NewSiteRequest
		if err := decodeBody(w, r, &req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		task, err := s.NewApp.Start(r.Context(), req)
		if errors.As(err, new(newapp.InputError)) {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if err != nil {
			s.Log.Warn("starting a new site failed", "err", err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSONStatus(w, http.StatusAccepted, task)
	})
	mux.HandleFunc("GET /api/tools", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, newapp.Tools(r.Context()))
	})
}

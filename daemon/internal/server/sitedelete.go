package server

import (
	"errors"
	"net/http"

	"github.com/d3v0psdan/bench/daemon/internal/api"
	"github.com/d3v0psdan/bench/daemon/internal/sitedelete"
)

// registerSiteDeleteRoutes wires deleting a site: what it would clean up,
// then the deletion itself as a task.
func (s *Server) registerSiteDeleteRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/sites/{name}/delete", func(w http.ResponseWriter, r *http.Request) {
		if s.SiteDelete == nil {
			http.Error(w, "deleting sites is unavailable", http.StatusServiceUnavailable)
			return
		}
		plan, err := s.SiteDelete.Plan(r.Context(), r.PathValue("name"))
		if err != nil {
			s.siteDeleteError(w, err)
			return
		}
		writeJSON(w, plan)
	})
	// Starts the deletion and answers at once with its task (202).
	mux.HandleFunc("POST /api/sites/{name}/delete", func(w http.ResponseWriter, r *http.Request) {
		if s.SiteDelete == nil {
			http.Error(w, "deleting sites is unavailable", http.StatusServiceUnavailable)
			return
		}
		var req api.SiteDeleteRequest
		if err := decodeBody(w, r, &req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		task, err := s.SiteDelete.Start(r.Context(), r.PathValue("name"), req)
		if err != nil {
			s.siteDeleteError(w, err)
			return
		}
		writeJSONStatus(w, http.StatusAccepted, task)
	})
}

func (s *Server) siteDeleteError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, sitedelete.ErrNotFound):
		http.Error(w, err.Error(), http.StatusNotFound)
	case errors.As(err, new(sitedelete.InputError)):
		http.Error(w, err.Error(), http.StatusBadRequest)
	default:
		s.Log.Warn("deleting a site failed", "err", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

package server

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/d3v0psdan/bench/daemon/internal/api"
	"github.com/d3v0psdan/bench/daemon/internal/registry"
	"github.com/d3v0psdan/bench/daemon/internal/services"
	"github.com/d3v0psdan/bench/daemon/internal/tasks"
)

// registerServiceRoutes wires the service-instance endpoints. Mutations
// respond with the affected instance; the full list also streams as a
// "services" WS event on every change.
func (s *Server) registerServiceRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/services", s.withServices(func(w http.ResponseWriter, r *http.Request) error {
		list, err := s.Services.List()
		if err != nil {
			return err
		}
		writeJSON(w, list)
		return nil
	}))
	mux.HandleFunc("GET /api/services/catalog", s.withServices(func(w http.ResponseWriter, r *http.Request) error {
		writeJSON(w, s.Services.Catalog())
		return nil
	}))
	mux.HandleFunc("POST /api/services", s.withServices(func(w http.ResponseWriter, r *http.Request) error {
		var req api.CreateServiceRequest
		if err := decodeBody(w, r, &req); err != nil {
			return err
		}
		svc, err := s.Services.Create(req)
		return respond(w, svc, err)
	}))
	mux.HandleFunc("POST /api/services/{name}/start", s.withServices(func(w http.ResponseWriter, r *http.Request) error {
		svc, err := s.Services.Start(r.Context(), r.PathValue("name"))
		return respond(w, svc, err)
	}))
	mux.HandleFunc("POST /api/services/{name}/stop", s.withServices(func(w http.ResponseWriter, r *http.Request) error {
		svc, err := s.Services.Stop(r.PathValue("name"))
		return respond(w, svc, err)
	}))
	mux.HandleFunc("POST /api/services/{name}/clone", s.withServices(func(w http.ResponseWriter, r *http.Request) error {
		var req api.CloneServiceRequest
		if err := decodeBody(w, r, &req); err != nil {
			return err
		}
		svc, err := s.Services.Clone(r.PathValue("name"), req.Name)
		return respond(w, svc, err)
	}))
	mux.HandleFunc("POST /api/services/{name}/autostart", s.withServices(func(w http.ResponseWriter, r *http.Request) error {
		var req api.AutostartRequest
		if err := decodeBody(w, r, &req); err != nil {
			return err
		}
		svc, err := s.Services.SetAutostart(r.PathValue("name"), req.Enabled)
		return respond(w, svc, err)
	}))
	mux.HandleFunc("GET /api/services/{name}/logs", s.withServices(func(w http.ResponseWriter, r *http.Request) error {
		svc, err := s.Services.Get(r.PathValue("name"))
		if err != nil {
			return err
		}
		return serveLogTail(w, r, svc.LogFile)
	}))
	mux.HandleFunc("GET /api/services/{name}/logs/follow", s.withServices(func(w http.ResponseWriter, r *http.Request) error {
		svc, err := s.Services.Get(r.PathValue("name"))
		if err != nil {
			return err
		}
		s.followLog(w, r, svc.LogFile)
		return nil
	}))
	mux.HandleFunc("DELETE /api/services/{name}", s.withServices(func(w http.ResponseWriter, r *http.Request) error {
		if err := s.Services.Delete(r.PathValue("name"), r.URL.Query().Get("keep_data") == "1"); err != nil {
			return err
		}
		w.WriteHeader(http.StatusNoContent)
		return nil
	}))
}

// decodeBody parses a JSON body; an empty body leaves v at its zero value.
func decodeBody(w http.ResponseWriter, r *http.Request, v any) error {
	err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(v)
	if err != nil && !errors.Is(err, io.EOF) {
		return errBadRequest{err}
	}
	return nil
}

// respond writes the instance. A create/start that failed after the
// instance came to exist still reports the failure as an error: the
// client needs the message, and the list event carries the instance.
func respond(w http.ResponseWriter, svc api.Service, err error) error {
	if err != nil {
		return err
	}
	writeJSON(w, svc)
	return nil
}

// withServices guards on the manager and maps errors to HTTP statuses.
func (s *Server) withServices(fn func(http.ResponseWriter, *http.Request) error) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.Services == nil {
			http.Error(w, "service manager unavailable", http.StatusServiceUnavailable)
			return
		}
		err := fn(w, r)
		if err == nil {
			return
		}
		status := http.StatusInternalServerError
		switch {
		case errors.As(err, new(errBadRequest)), errors.As(err, new(services.InputError)):
			status = http.StatusBadRequest
		case errors.Is(err, registry.ErrServiceNotFound):
			status = http.StatusNotFound
		case errors.Is(err, services.ErrBusy), errors.Is(err, tasks.ErrCancelled), errors.As(err, new(errConflict)):
			status = http.StatusConflict
		default:
			s.Log.Warn("service request failed", "method", r.Method, "path", r.URL.Path, "err", err)
		}
		http.Error(w, err.Error(), status)
	}
}

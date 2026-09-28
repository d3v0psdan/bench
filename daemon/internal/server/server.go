// Package server implements benchd's localhost REST + WebSocket API.
// Every request must carry the bench token (Authorization: Bearer, or
// ?token= for WebSocket clients that cannot set headers).
package server

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"runtime"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"

	"github.com/d3v0psdan/bench/daemon/internal/api"
	"github.com/d3v0psdan/bench/daemon/internal/binman"
	"github.com/d3v0psdan/bench/daemon/internal/doctor"
	"github.com/d3v0psdan/bench/daemon/internal/newapp"
	"github.com/d3v0psdan/bench/daemon/internal/services"
	"github.com/d3v0psdan/bench/daemon/internal/sitedelete"
	"github.com/d3v0psdan/bench/daemon/internal/sites"
	"github.com/d3v0psdan/bench/daemon/internal/tasks"
)

// Server holds the daemon API state.
type Server struct {
	Version    string
	Token      string
	StartedAt  time.Time
	Log        *slog.Logger
	OnShutdown func()        // called (once, async) when POST /api/shutdown arrives
	Heartbeat  time.Duration // /api/events emit interval; 0 = 2s (tests shrink it)
	Binaries   *binman.Manager
	Sites      *sites.Manager
	Services   *services.Manager
	Doctor     *doctor.Doctor
	Mail       http.Handler // proxy to Mailpit's REST API (/api/mail/v1/*)
	MailSave   http.Handler // saves an attachment to Downloads (POST /api/mail/save-attachment)
	// Setup runs the elevated helper for DNS + CA trust; wired by main.
	Setup func(ctx context.Context, dns, trust bool) ([]string, error)
	// Teardown undoes Setup (removes the CA from trust stores and the
	// .test resolver rule); wired by main.
	Teardown func(ctx context.Context, dns, trust bool) ([]string, error)
	// RemovePHP stops a PHP version's pool and deletes its build; wired by
	// main.
	RemovePHP func(channel string) error
	// Tasks tracks long operations for the Activity view (nil-safe).
	Tasks *tasks.Registry
	// PHPInfo describes an installed PHP channel; wired by main.
	PHPInfo func(ctx context.Context, channel string) (api.PHPInfo, error)
	// UpdatesURL overrides GitHub's latest-release endpoint (tests).
	UpdatesURL string
	// NewApp creates Laravel apps (POST /api/sites/new); wired by main.
	NewApp *newapp.Creator
	// SiteDelete deletes sites and what Bench made for them; wired by main.
	SiteDelete *sitedelete.Deleter

	updMu sync.Mutex // one update check at a time
	subMu sync.Mutex
	subs  map[chan api.Event]struct{}
}

// Handler returns the full API handler with auth applied.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/status", s.handleStatus)
	mux.HandleFunc("POST /api/shutdown", s.handleShutdown)
	mux.HandleFunc("GET /api/events", s.handleEvents)
	mux.HandleFunc("GET /api/binaries", s.handleBinariesList)
	mux.HandleFunc("POST /api/binaries/install", s.handleBinariesInstall)
	mux.HandleFunc("DELETE /api/binaries/{name}/{channel}", s.handleBinaryUninstall)
	mux.HandleFunc("POST /api/setup", s.handleSetup)
	mux.HandleFunc("POST /api/setup/undo", s.handleTeardown)
	mux.HandleFunc("GET /api/doctor", s.handleDoctor)
	if s.Mail != nil {
		mux.Handle("/api/mail/v1/", s.Mail)
	}
	if s.MailSave != nil {
		mux.Handle("POST /api/mail/save-attachment", s.MailSave)
	}
	s.registerSiteRoutes(mux)
	s.registerServiceRoutes(mux)
	s.registerOpenRoutes(mux)
	s.registerNewAppRoutes(mux)
	s.registerSiteDeleteRoutes(mux)
	s.registerEnvRoutes(mux)
	s.registerAboutRoutes(mux)
	s.registerTaskRoutes(mux)
	return s.auth(mux)
}

func (s *Server) handleSetup(w http.ResponseWriter, r *http.Request) {
	if s.Setup == nil {
		http.Error(w, "setup unavailable", http.StatusServiceUnavailable)
		return
	}
	req := api.SetupRequest{DNS: true, Trust: true}
	if r.ContentLength > 0 {
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&req); err != nil {
			http.Error(w, "invalid request body: "+err.Error(), http.StatusBadRequest)
			return
		}
	}
	// Tracked for the Activity view but never cancellable: the elevated
	// helper outlives a killed prompt wrapper.
	task := s.Tasks.Begin("setup", "Setting up HTTPS and DNS", "setup", false)
	task.SetPhase("waiting for approval")
	msgs, err := s.Setup(r.Context(), req.DNS, req.Trust)
	err = task.End(err)
	if err != nil {
		s.Log.Warn("setup failed", "err", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, api.SetupResponse{Messages: msgs})
}

func (s *Server) handleTeardown(w http.ResponseWriter, r *http.Request) {
	if s.Teardown == nil {
		http.Error(w, "setup undo unavailable", http.StatusServiceUnavailable)
		return
	}
	// Same body as POST /api/setup: which parts to remove.
	req := api.SetupRequest{DNS: true, Trust: true}
	if r.ContentLength > 0 {
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&req); err != nil {
			http.Error(w, "invalid request body: "+err.Error(), http.StatusBadRequest)
			return
		}
	}
	if !req.DNS && !req.Trust {
		http.Error(w, "nothing to remove: pass dns and/or trust", http.StatusBadRequest)
		return
	}
	task := s.Tasks.Begin("setup", "Removing the HTTPS and DNS setup", "setup", false)
	task.SetPhase("waiting for approval")
	msgs, err := s.Teardown(r.Context(), req.DNS, req.Trust)
	if err = task.End(err); err != nil {
		s.Log.Warn("setup undo failed", "err", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, api.SetupResponse{Messages: msgs})
}

func (s *Server) handleDoctor(w http.ResponseWriter, r *http.Request) {
	if s.Doctor == nil {
		http.Error(w, "doctor unavailable", http.StatusServiceUnavailable)
		return
	}
	writeJSON(w, s.Doctor.Run(r.Context()))
}

// Publish broadcasts an event to every connected /api/events client.
// Never blocks: a client that can't keep up loses events (the WS stream
// is a live feed, not a durable queue).
func (s *Server) Publish(ev api.Event) {
	s.subMu.Lock()
	defer s.subMu.Unlock()
	for ch := range s.subs {
		select {
		case ch <- ev:
		default:
		}
	}
}

func (s *Server) subscribe() (chan api.Event, func()) {
	ch := make(chan api.Event, 64)
	s.subMu.Lock()
	if s.subs == nil {
		s.subs = map[chan api.Event]struct{}{}
	}
	s.subs[ch] = struct{}{}
	s.subMu.Unlock()
	return ch, func() {
		s.subMu.Lock()
		delete(s.subs, ch)
		s.subMu.Unlock()
	}
}

func (s *Server) auth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// DNS-rebinding hardening: we only ever bind loopback, so any
		// other Host means the browser was tricked into resolving an
		// attacker's name to 127.0.0.1. Reject before doing anything.
		host := r.Host
		if h, _, err := net.SplitHostPort(host); err == nil {
			host = h
		}
		if host != "127.0.0.1" && host != "localhost" && host != "::1" {
			http.Error(w, "forbidden host", http.StatusForbidden)
			return
		}
		tok := r.URL.Query().Get("token")
		if h := r.Header.Get("Authorization"); len(h) > 7 && h[:7] == "Bearer " {
			tok = h[7:]
		}
		if subtle.ConstantTimeCompare([]byte(tok), []byte(s.Token)) != 1 {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) status() *api.Status {
	return &api.Status{
		Version:       s.Version,
		OS:            runtime.GOOS,
		Arch:          runtime.GOARCH,
		PID:           os.Getpid(),
		StartedAt:     s.StartedAt,
		UptimeSeconds: int64(time.Since(s.StartedAt).Seconds()),
	}
}

func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, s.status())
}

func (s *Server) handleShutdown(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusNoContent)
	if s.OnShutdown != nil {
		go s.OnShutdown() // async so this response gets flushed first
	}
}

// guiOrigins are the GUI's origins: the Vite dev server and the Tauri
// production webviews. Token auth is the real gate on every socket.
var guiOrigins = []string{"localhost:*", "localhost", "127.0.0.1:*", "tauri.localhost", "tauri://localhost"}

func (s *Server) handleEvents(w http.ResponseWriter, r *http.Request) {
	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		OriginPatterns: guiOrigins,
	})
	if err != nil {
		s.Log.Warn("websocket accept failed", "err", err)
		return
	}
	defer conn.CloseNow()

	ctx := conn.CloseRead(r.Context()) // we never read; detect client close

	sub, unsub := s.subscribe()
	defer unsub()

	hb := s.Heartbeat
	if hb <= 0 {
		hb = 2 * time.Second
	}
	ticker := time.NewTicker(hb)
	defer ticker.Stop()
	ev := api.Event{Type: "status", Status: s.status()} // immediate first event
	for {
		// Bounded write: a client that keeps the socket open but stops
		// reading would otherwise block this goroutine forever.
		wctx, cancel := context.WithTimeout(ctx, 5*time.Second)
		err := wsjson.Write(wctx, conn, ev)
		cancel()
		if err != nil {
			return // client gone or stalled
		}
		select {
		case <-ctx.Done():
			return
		case ev = <-sub:
		case <-ticker.C:
			ev = api.Event{Type: "status", Status: s.status()}
		}
	}
}

func (s *Server) handleBinariesList(w http.ResponseWriter, r *http.Request) {
	if s.Binaries == nil {
		http.Error(w, "binary manager unavailable", http.StatusServiceUnavailable)
		return
	}
	names := make([]string, 0, len(s.Binaries.Manifests))
	for name := range s.Binaries.Manifests {
		names = append(names, name)
	}
	sort.Strings(names)
	out := []api.BinaryInfo{} // never null on the wire
	for _, name := range names {
		installed := map[string]bool{}
		for _, v := range s.Binaries.Installed(name) {
			installed[v] = true
		}
		for _, b := range s.Binaries.Channels(name) {
			info := api.BinaryInfo{
				Name: name, Channel: b.Channel, Version: b.Version,
				Installed: installed[b.Version],
				Dir:       s.Binaries.Dir(name, b.Version),
				Sources:   downloadHosts(b),
			}
			if info.Installed {
				// Shortcut: walks each installed build per list call (a few
				// hundred files each); cache per version if the list grows.
				info.Size = s.Binaries.DiskSize(name, b.Version)
			}
			out = append(out, info)
		}
	}
	writeJSON(w, out)
}

func (s *Server) handleBinariesInstall(w http.ResponseWriter, r *http.Request) {
	if s.Binaries == nil {
		http.Error(w, "binary manager unavailable", http.StatusServiceUnavailable)
		return
	}
	var req api.InstallRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&req); err != nil {
		http.Error(w, "invalid request body: "+err.Error(), http.StatusBadRequest)
		return
	}
	if req.Name == "" || req.Channel == "" {
		http.Error(w, "name and channel are required", http.StatusBadRequest)
		return
	}
	b, err := s.Binaries.Resolve(req.Name, req.Channel)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	// The download runs under the task, not the request: it keeps going if
	// the client disconnects and stops only when the task is cancelled.
	task := s.Tasks.Begin("binary.install", fmt.Sprintf("Installing %s %s", displayName(req.Name), req.Channel),
		req.Name+"/"+req.Channel, true)
	task.Watch(b.Name, b.Version)
	task.SetPhase("downloading")
	b, err = s.Binaries.Install(task.Context(), req.Name, req.Channel)
	err = task.End(err)
	switch {
	case errors.Is(err, tasks.ErrCancelled):
		http.Error(w, err.Error(), http.StatusConflict)
		return
	case err != nil:
		s.Log.Warn("binary install failed", "name", req.Name, "channel", req.Channel, "err", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, api.BinaryInfo{Name: b.Name, Channel: b.Channel, Version: b.Version, Installed: true})
}

// handleBinaryUninstall removes an installed PHP version. It refuses while
// any site needs it (the default, or a site's pin), so uninstalling never
// breaks a site. Service binaries aren't removable here: instances use them.
func (s *Server) handleBinaryUninstall(w http.ResponseWriter, r *http.Request) {
	if s.RemovePHP == nil || s.Sites == nil || s.Binaries == nil {
		http.Error(w, "uninstall unavailable", http.StatusServiceUnavailable)
		return
	}
	if r.PathValue("name") != "php" {
		http.Error(w, "only PHP versions can be uninstalled", http.StatusBadRequest)
		return
	}
	// Resolve accepts a full version too ("8.4.12"); every check below
	// compares channels, so use the resolved one, never the raw path value.
	b, err := s.Binaries.Resolve("php", r.PathValue("channel"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	channel := b.Channel
	// Older rows can hold a full version ("8.4.12"), which serves the same
	// build as its channel; compare what each value resolves to.
	needs := func(v string) bool {
		c, ok := s.Binaries.Channel("php", v)
		return v == channel || (ok && c == channel)
	}
	def, err := s.Sites.DefaultPHP()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if needs(def) {
		http.Error(w, fmt.Sprintf("PHP %s is the default; make another version the default first", channel), http.StatusConflict)
		return
	}
	list, err := s.Sites.List(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	var pinned []string
	for _, site := range list {
		if needs(site.PHPPinned) {
			pinned = append(pinned, site.Host)
		}
	}
	// Shortcut: a pin set while the files are being removed isn't blocked;
	// that site then reports "not installed". Lock through sites.Manager if
	// that ever matters.
	if len(pinned) > 0 {
		http.Error(w, fmt.Sprintf("PHP %s is pinned by %s; switch them to another version first",
			channel, strings.Join(pinned, ", ")), http.StatusConflict)
		return
	}
	if err := s.RemovePHP(channel); err != nil {
		s.Log.Warn("php uninstall failed", "channel", channel, "err", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// phpChannel stores PHP versions by channel ("8.4" for "8.4.12"), so every
// comparison against a channel holds. Values the catalog doesn't know pass
// through for the sites manager to judge.
func (s *Server) phpChannel(v string) string {
	if s.Binaries == nil || v == "" {
		return v
	}
	if c, ok := s.Binaries.Channel("php", v); ok {
		return c
	}
	return v
}

// downloadHosts lists the distinct hosts a build downloads from.
func downloadHosts(b binman.Build) []string {
	hosts := []string{}
	for _, d := range b.Downloads {
		u, err := url.Parse(d.URL)
		if err != nil || u.Host == "" || slices.Contains(hosts, u.Host) {
			continue
		}
		hosts = append(hosts, u.Host)
	}
	return hosts
}

// displayName is how a binary is named to users ("php" is "PHP").
func displayName(name string) string {
	if name == "php" {
		return "PHP"
	}
	return name
}

func writeJSON(w http.ResponseWriter, v any) {
	writeJSONStatus(w, http.StatusOK, v)
}

// writeJSONStatus encodes before writing anything, so a value that can't
// be encoded is still answered with a 500 rather than a half-sent status.
func writeJSONStatus(w http.ResponseWriter, status int, v any) {
	b, err := json.Marshal(v)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(append(b, '\n')) // a client that left can't be answered
}

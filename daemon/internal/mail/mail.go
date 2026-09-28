// Package mail exposes the Mailpit instance through benchd: an
// authenticated proxy to Mailpit's REST API (/api/mail/v1/* maps to
// Mailpit's /api/v1/*) and a relay that republishes Mailpit's live
// events on benchd's own WebSocket stream. The GUI stays a client of one
// API with one token. Mailpit listens on loopback behind per-instance
// basic auth (loopback alone doesn't stop DNS rebinding).
package mail

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"

	"github.com/d3v0psdan/bench/daemon/internal/api"
)

// relayRetry is how often the relay looks for a running Mailpit again.
const relayRetry = 2 * time.Second

// Target is a running Mailpit's HTTP address ("127.0.0.1:8025") and its
// basic-auth "user:password" (empty for an instance without one).
type Target struct {
	Addr string
	Auth string
}

// Finder returns the running Mailpit, ok=false when there is none.
type Finder func() (t Target, ok bool)

// FromServices builds a Finder over the service list.
func FromServices(list func() ([]api.Service, error)) Finder {
	return func() (Target, bool) {
		svcs, err := list()
		if err != nil {
			return Target{}, false
		}
		for _, s := range svcs {
			if s.Service == "mailpit" && s.State == "running" && s.Ports["http"] != 0 {
				return Target{Addr: "127.0.0.1:" + strconv.Itoa(s.Ports["http"]), Auth: s.UIAuth}, true
			}
		}
		return Target{}, false
	}
}

// authHeader is the Authorization value for Mailpit's basic auth.
func (t Target) authHeader() string {
	return "Basic " + base64.StdEncoding.EncodeToString([]byte(t.Auth))
}

// Proxy serves /api/mail/v1/* from Mailpit's /api/v1/*. Only the read,
// mark-read and delete verbs the Mail pane uses are allowed.
func Proxy(find Finder) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet, http.MethodPut, http.MethodDelete:
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		rest, ok := strings.CutPrefix(r.URL.Path, "/api/mail/v1/")
		if !ok || strings.Contains(rest, "..") {
			http.NotFound(w, r)
			return
		}
		t, ok := find()
		if !ok {
			http.Error(w, "mail is not running: enable it in the Mail tab or run `bench services:create mailpit`", http.StatusServiceUnavailable)
			return
		}
		target := &url.URL{Scheme: "http", Host: t.Addr}
		proxy := &httputil.ReverseProxy{
			Rewrite: func(pr *httputil.ProxyRequest) {
				pr.SetURL(target)
				pr.Out.URL.Path = "/api/v1/" + rest
				pr.Out.URL.RawPath = ""
				// benchd's token stays with benchd; Mailpit gets its own.
				pr.Out.Header.Del("Authorization")
				if t.Auth != "" {
					pr.Out.Header.Set("Authorization", t.authHeader())
				}
				pr.Out.Header.Del("Origin")
			},
		}
		proxy.ServeHTTP(w, r)
	})
}

// Relay forwards Mailpit's WebSocket events ("new", "stats", "delete",
// ...) to publish until ctx ends, reconnecting whenever Mailpit
// (re)starts.
func Relay(ctx context.Context, find Finder, publish func(api.Event), log *slog.Logger) {
	for {
		if t, ok := find(); ok {
			err := relayOnce(ctx, t, publish)
			if err != nil && ctx.Err() == nil {
				log.Debug("mail relay disconnected", "err", err)
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(relayRetry):
		}
	}
}

func relayOnce(ctx context.Context, t Target, publish func(api.Event)) error {
	opts := &websocket.DialOptions{HTTPHeader: http.Header{}}
	if t.Auth != "" {
		opts.HTTPHeader.Set("Authorization", t.authHeader())
	}
	conn, _, err := websocket.Dial(ctx, "ws://"+t.Addr+"/api/events", opts)
	if err != nil {
		return err
	}
	defer conn.CloseNow()
	conn.SetReadLimit(4 << 20) // message summaries, not bodies
	for {
		var ev json.RawMessage
		if err := wsjson.Read(ctx, conn, &ev); err != nil {
			if errors.Is(err, context.Canceled) {
				return nil
			}
			return err
		}
		publish(api.Event{Type: "mail", Mail: ev})
	}
}

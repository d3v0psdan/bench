package mail

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"

	"github.com/d3v0psdan/bench/daemon/internal/api"
)

// fakeMailpit records what reached it and pushes one "new" event per
// WebSocket connection.
func fakeMailpit(t *testing.T) (*httptest.Server, *http.Request) {
	t.Helper()
	var last http.Request
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/", func(w http.ResponseWriter, r *http.Request) {
		last = *r
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"total":1}`)
	})
	mux.HandleFunc("/api/events", func(w http.ResponseWriter, r *http.Request) {
		c, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer c.CloseNow()
		_ = wsjson.Write(r.Context(), c, map[string]any{"Type": "new", "Data": map[string]any{"Subject": "Welcome"}})
		<-r.Context().Done()
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv, &last
}

func TestProxyMapsPathAndStripsToken(t *testing.T) {
	mp, last := fakeMailpit(t)
	addr := strings.TrimPrefix(mp.URL, "http://")
	h := Proxy(func() (Target, bool) { return Target{Addr: addr, Auth: "bench:s3cret"}, true })

	req := httptest.NewRequest(http.MethodGet, "/api/mail/v1/search?query=tag:myapp", nil)
	req.Header.Set("Authorization", "Bearer secret-bench-token")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"total":1`) {
		t.Fatalf("proxy response: %d %s", rec.Code, rec.Body)
	}
	if last.URL.Path != "/api/v1/search" || last.URL.Query().Get("query") != "tag:myapp" {
		t.Fatalf("mailpit got %s?%s", last.URL.Path, last.URL.RawQuery)
	}
	if got := last.Header.Get("Authorization"); got != "Basic YmVuY2g6czNjcmV0" {
		t.Fatalf("Mailpit must get its own basic auth, not benchd's token: %q", got)
	}
}

func TestProxyRejectsOtherVerbsAndMissingMailpit(t *testing.T) {
	h := Proxy(func() (Target, bool) { return Target{}, false })
	for method, want := range map[string]int{http.MethodPost: http.StatusMethodNotAllowed, http.MethodGet: http.StatusServiceUnavailable} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(method, "/api/mail/v1/messages", nil))
		if rec.Code != want {
			t.Errorf("%s: status %d, want %d", method, rec.Code, want)
		}
	}
}

func TestRelayPublishesMailpitEvents(t *testing.T) {
	mp, _ := fakeMailpit(t)
	addr := strings.TrimPrefix(mp.URL, "http://")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	got := make(chan api.Event, 1)
	go Relay(ctx, func() (Target, bool) { return Target{Addr: addr}, true }, func(ev api.Event) {
		select {
		case got <- ev:
		default:
		}
	}, slog.New(slog.NewTextHandler(io.Discard, nil)))

	select {
	case ev := <-got:
		if ev.Type != "mail" || !strings.Contains(string(ev.Mail), `"Welcome"`) {
			t.Fatalf("event: %+v %s", ev, ev.Mail)
		}
	case <-ctx.Done():
		t.Fatal("no mail event relayed")
	}
}

func TestFromServicesFindsRunningMailpit(t *testing.T) {
	find := FromServices(func() ([]api.Service, error) {
		return []api.Service{
			{Service: "mysql", State: "running", Port: 3306},
			{Service: "mailpit", State: "running", Port: 2525, Ports: map[string]int{"http": 8025}, UIAuth: "bench:pw"},
		}, nil
	})
	if got, ok := find(); !ok || got.Addr != "127.0.0.1:8025" || got.Auth != "bench:pw" {
		t.Fatalf("find = %+v, %v", got, ok)
	}
	stopped := FromServices(func() ([]api.Service, error) {
		return []api.Service{{Service: "mailpit", State: "stopped", Ports: map[string]int{"http": 8025}}}, nil
	})
	if got, ok := stopped(); ok {
		t.Fatalf("stopped mailpit must not be found, got %+v", got)
	}
}

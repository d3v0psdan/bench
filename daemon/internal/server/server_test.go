package server

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"

	apipkg "github.com/d3v0psdan/bench/daemon/internal/api"
)

const testToken = "test-token"

func newTestServer(t *testing.T, onShutdown func()) *httptest.Server {
	t.Helper()
	s := &Server{
		Version:    "test",
		Token:      testToken,
		StartedAt:  time.Now(),
		Log:        slog.New(slog.NewTextHandler(io.Discard, nil)),
		OnShutdown: onShutdown,
		Heartbeat:  10 * time.Millisecond, // keep the recurrence test fast
	}
	ts := httptest.NewServer(s.Handler())
	t.Cleanup(ts.Close)
	return ts
}

func TestAuthRequired(t *testing.T) {
	ts := newTestServer(t, nil)

	for name, req := range map[string]func() *http.Request{
		"no token": func() *http.Request { r, _ := http.NewRequest("GET", ts.URL+"/api/status", nil); return r },
		"wrong token": func() *http.Request {
			r, _ := http.NewRequest("GET", ts.URL+"/api/status", nil)
			r.Header.Set("Authorization", "Bearer wrong")
			return r
		},
	} {
		resp, err := http.DefaultClient.Do(req())
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusUnauthorized {
			t.Errorf("%s: status = %d, want 401", name, resp.StatusCode)
		}
	}
}

func TestRejectsForeignHost(t *testing.T) {
	ts := newTestServer(t, nil)
	// DNS-rebinding hardening: a request with the right token but a
	// non-loopback Host (a browser tricked into resolving attacker.test →
	// 127.0.0.1) must be refused before the handler runs.
	req, _ := http.NewRequest("GET", ts.URL+"/api/status", nil)
	req.Header.Set("Authorization", "Bearer "+testToken)
	req.Host = "attacker.test"
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("foreign Host: status = %d, want 403", resp.StatusCode)
	}
}

func TestStatus(t *testing.T) {
	ts := newTestServer(t, nil)

	req, _ := http.NewRequest("GET", ts.URL+"/api/status", nil)
	req.Header.Set("Authorization", "Bearer "+testToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	var st apipkg.Status
	if err := json.NewDecoder(resp.Body).Decode(&st); err != nil {
		t.Fatal(err)
	}
	if st.Version != "test" || st.PID == 0 || st.OS == "" {
		t.Errorf("unexpected status payload: %+v", st)
	}
}

func TestShutdownInvokesCallback(t *testing.T) {
	var once sync.Once
	done := make(chan struct{})
	ts := newTestServer(t, func() { once.Do(func() { close(done) }) })

	req, _ := http.NewRequest("POST", ts.URL+"/api/shutdown", nil)
	req.Header.Set("Authorization", "Bearer "+testToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", resp.StatusCode)
	}

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("OnShutdown was not called")
	}
}

func TestEventsStreamsHeartbeat(t *testing.T) {
	ts := newTestServer(t, nil)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http") + "/api/events?token=" + testToken
	conn, _, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.CloseNow()

	// The first event is written before the ticker is consulted, so read TWO
	// to prove the heartbeat actually recurs.
	var first, second apipkg.Event
	if err := wsjson.Read(ctx, conn, &first); err != nil {
		t.Fatal(err)
	}
	if err := wsjson.Read(ctx, conn, &second); err != nil {
		t.Fatalf("second heartbeat never arrived: %v", err)
	}
	for _, ev := range []apipkg.Event{first, second} {
		if ev.Type != "status" || ev.Status == nil || ev.Status.Version != "test" {
			t.Errorf("unexpected event: %+v", ev)
		}
	}
	if second.Status.UptimeSeconds < first.Status.UptimeSeconds {
		t.Errorf("uptime went backwards: %d then %d", first.Status.UptimeSeconds, second.Status.UptimeSeconds)
	}
}

func TestWriteJSONStatusAnswers500WhenTheValueCantBeEncoded(t *testing.T) {
	rec := httptest.NewRecorder()
	writeJSONStatus(rec, http.StatusAccepted, map[string]any{"bad": make(chan int)})
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500 (nothing sent before encoding)", rec.Code)
	}
	rec = httptest.NewRecorder()
	writeJSONStatus(rec, http.StatusAccepted, map[string]int{"ok": 1})
	if rec.Code != http.StatusAccepted || strings.TrimSpace(rec.Body.String()) != `{"ok":1}` {
		t.Fatalf("got %d %q", rec.Code, rec.Body.String())
	}
}

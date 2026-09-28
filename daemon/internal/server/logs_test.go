package server

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"

	"github.com/d3v0psdan/bench/daemon/internal/api"
)

func TestReadTailKeepsLastLines(t *testing.T) {
	path := filepath.Join(t.TempDir(), "svc.log")
	if err := os.WriteFile(path, []byte("one\ntwo\nthree\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		lines int
		want  string
	}{
		{1, "three\n"},
		{2, "two\nthree\n"},
		{10, "one\ntwo\nthree\n"},
	} {
		text, size, err := readTail(path, c.lines)
		if err != nil {
			t.Fatal(err)
		}
		if text != c.want || size != 14 {
			t.Errorf("readTail(%d) = %q, %d; want %q, 14", c.lines, text, size, c.want)
		}
	}
	text, size, err := readTail(filepath.Join(t.TempDir(), "missing.log"), 5)
	if err != nil || text != "" || size != 0 {
		t.Errorf("missing log: %q, %d, %v; want an empty tail", text, size, err)
	}
}

func TestFollowLogStreamsAppendsAndResetsOnTruncate(t *testing.T) {
	path := filepath.Join(t.TempDir(), "svc.log")
	if err := os.WriteFile(path, []byte("before\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	s := &Server{Log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { s.followLog(w, r, path) }))
	defer ts.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	conn, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(ts.URL, "http")+"?from=7", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.CloseNow()

	appendTo(t, path, "after\n")
	var chunk api.LogChunk
	if err := wsjson.Read(ctx, conn, &chunk); err != nil {
		t.Fatal(err)
	}
	if chunk.Text != "after\n" || chunk.Reset {
		t.Fatalf("first chunk = %+v; want only the appended line", chunk)
	}

	if err := os.WriteFile(path, []byte("new\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := wsjson.Read(ctx, conn, &chunk); err != nil {
		t.Fatal(err)
	}
	if chunk.Text != "new\n" || !chunk.Reset {
		t.Fatalf("after truncation = %+v; want a reset with the new content", chunk)
	}
}

func appendTo(t *testing.T, path, text string) {
	t.Helper()
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if _, err := f.WriteString(text); err != nil {
		t.Fatal(err)
	}
}

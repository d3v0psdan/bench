package server

import (
	"bytes"
	"context"
	"errors"
	"io"
	"io/fs"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"

	"github.com/d3v0psdan/bench/daemon/internal/api"
)

const (
	defaultTailLines   = 500
	maxTailLines       = 5000
	maxTailBytes       = 1 << 20   // the tail never reads more than this
	maxChunkBytes      = 256 << 10 // one follow message at most
	followInterval     = 500 * time.Millisecond
	followWriteTimeout = 5 * time.Second // a client that stalls this long is dropped
)

// serveLogTail answers with the last ?tail= lines of path. A log that
// doesn't exist yet (the process never ran) is an empty tail, not an error.
func serveLogTail(w http.ResponseWriter, r *http.Request, path string) error {
	lines := defaultTailLines
	if v := r.URL.Query().Get("tail"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 {
			return errBadRequest{errors.New("tail must be a positive number of lines")}
		}
		lines = min(n, maxTailLines)
	}
	text, size, err := readTail(path, lines)
	if err != nil {
		return err
	}
	writeJSON(w, api.LogTail{Path: path, Text: text, Size: size})
	return nil
}

// readTail returns the last n lines of path and the file's size.
func readTail(path string, n int) (string, int64, error) {
	f, err := os.Open(path)
	if errors.Is(err, fs.ErrNotExist) {
		return "", 0, nil
	}
	if err != nil {
		return "", 0, err
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		return "", 0, err
	}
	size := fi.Size()
	start := max(size-maxTailBytes, 0)
	b := make([]byte, size-start)
	if _, err := f.ReadAt(b, start); err != nil && !errors.Is(err, io.EOF) {
		return "", 0, err
	}
	// Keep the last n lines; a trailing newline doesn't start a line.
	end := len(b)
	if end > 0 && b[end-1] == '\n' {
		end--
	}
	cut := 0
	for i, seen := end-1, 0; i >= 0; i-- {
		if b[i] == '\n' {
			seen++
			if seen == n {
				cut = i + 1
				break
			}
		}
	}
	if cut == 0 && start > 0 {
		// The window starts mid-line: drop the partial first line.
		if i := bytes.IndexByte(b, '\n'); i >= 0 {
			cut = i + 1
		}
	}
	return string(b[cut:]), size, nil
}

// followLog streams what is appended to path after byte ?from= over a
// WebSocket until the client leaves. It polls rather than watching: log
// files are appended by child processes, and a stat twice a second is
// cheap and behaves the same on all three OSes.
func (s *Server) followLog(w http.ResponseWriter, r *http.Request, path string) {
	var offset int64
	if from := r.URL.Query().Get("from"); from != "" {
		n, err := strconv.ParseInt(from, 10, 64)
		if err != nil || n < 0 {
			http.Error(w, "from must be a byte offset", http.StatusBadRequest)
			return
		}
		offset = n
	}
	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{OriginPatterns: guiOrigins})
	if err != nil {
		s.Log.Warn("websocket accept failed", "err", err)
		return
	}
	defer conn.CloseNow()
	ctx := conn.CloseRead(r.Context())

	ticker := time.NewTicker(followInterval)
	defer ticker.Stop()
	for {
		chunk, next, err := readFrom(path, offset)
		if err != nil {
			s.Log.Warn("following log failed", "path", path, "err", err)
			conn.Close(websocket.StatusInternalError, "reading the log failed")
			return
		}
		offset = next
		if chunk.Text != "" || chunk.Reset {
			wctx, cancel := context.WithTimeout(ctx, followWriteTimeout)
			err := wsjson.Write(wctx, conn, chunk)
			cancel()
			if err != nil {
				return // client gone or stalled
			}
			if int64(len(chunk.Text)) == maxChunkBytes {
				continue // more is waiting; don't sleep on it
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// readFrom reads what path holds past offset (at most maxChunkBytes) and
// the offset to continue from. A file shorter than offset was rotated or
// truncated, so it restarts from the beginning with Reset set.
func readFrom(path string, offset int64) (api.LogChunk, int64, error) {
	f, err := os.Open(path)
	if errors.Is(err, fs.ErrNotExist) {
		return api.LogChunk{Reset: offset > 0}, 0, nil
	}
	if err != nil {
		return api.LogChunk{}, offset, err
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		return api.LogChunk{}, offset, err
	}
	var chunk api.LogChunk
	if fi.Size() < offset {
		chunk.Reset, offset = true, 0
	}
	n := min(fi.Size()-offset, maxChunkBytes)
	if n <= 0 {
		return chunk, offset, nil
	}
	b := make([]byte, n)
	read, err := f.ReadAt(b, offset)
	if err != nil && !errors.Is(err, io.EOF) {
		return api.LogChunk{}, offset, err
	}
	chunk.Text = string(b[:read])
	return chunk, offset + int64(read), nil
}

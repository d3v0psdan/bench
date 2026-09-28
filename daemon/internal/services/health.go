package services

import (
	"bufio"
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"
)

// Readiness probes, one per wire protocol. Each speaks just enough of the
// protocol to tell "listening but still starting" from "serving": a bare
// TCP dial would pass while Postgres still refuses logins during recovery.

// probeIOTimeout bounds a probe's reads and writes when its ctx has no
// deadline, so a server that accepts but never answers can't hang it.
const probeIOTimeout = 2 * time.Second

func dial(ctx context.Context, addr string) (net.Conn, error) {
	var d net.Dialer
	conn, err := d.DialContext(ctx, "tcp", addr)
	if err != nil {
		return nil, err
	}
	if dl, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(dl)
	} else {
		_ = conn.SetDeadline(time.Now().Add(probeIOTimeout))
	}
	return conn, nil
}

// mysqlReady reads the server greeting: MySQL and MariaDB send a
// protocol-10 handshake packet only once they accept connections; an
// 0xff error packet (e.g. "too many connections") means not ready.
func mysqlReady(addr string) func(context.Context) error {
	return func(ctx context.Context) error {
		conn, err := dial(ctx, addr)
		if err != nil {
			return err
		}
		defer conn.Close()
		var hdr [5]byte // 3-byte length, sequence id, first payload byte
		if _, err := io.ReadFull(conn, hdr[:]); err != nil {
			return fmt.Errorf("reading mysql greeting: %w", err)
		}
		if hdr[4] != 10 {
			return fmt.Errorf("mysql not ready (greeting byte 0x%02x)", hdr[4])
		}
		return nil
	}
}

// postgresReady sends a StartupMessage as user root and expects an
// authentication request ('R'). While starting up or recovering the
// server answers with an ErrorResponse ('E') instead.
func postgresReady(addr string) func(context.Context) error {
	return func(ctx context.Context) error {
		conn, err := dial(ctx, addr)
		if err != nil {
			return err
		}
		defer conn.Close()
		var body bytes.Buffer
		_ = binary.Write(&body, binary.BigEndian, int32(196608)) // protocol 3.0
		body.WriteString("user\x00root\x00database\x00postgres\x00\x00")
		msg := make([]byte, 4, 4+body.Len())
		binary.BigEndian.PutUint32(msg, uint32(4+body.Len()))
		msg = append(msg, body.Bytes()...)
		if _, err := conn.Write(msg); err != nil {
			return fmt.Errorf("sending postgres startup: %w", err)
		}
		var kind [1]byte
		if _, err := io.ReadFull(conn, kind[:]); err != nil {
			return fmt.Errorf("reading postgres startup reply: %w", err)
		}
		if kind[0] != 'R' {
			return errors.New("postgres not accepting connections yet")
		}
		return nil
	}
}

// redisReady sends an inline PING and expects +PONG (Valkey and Redis
// load their dump before answering; -LOADING means not ready).
func redisReady(addr string) func(context.Context) error {
	return func(ctx context.Context) error {
		conn, err := dial(ctx, addr)
		if err != nil {
			return err
		}
		defer conn.Close()
		if _, err := conn.Write([]byte("PING\r\n")); err != nil {
			return err
		}
		line, err := bufio.NewReader(conn).ReadString('\n')
		if err != nil {
			return fmt.Errorf("reading PING reply: %w", err)
		}
		if !strings.HasPrefix(line, "+PONG") {
			return fmt.Errorf("valkey not ready: %s", strings.TrimSpace(line))
		}
		return nil
	}
}

// redisShutdown asks Valkey to save and exit (no SIGTERM on Windows).
// The server closes the connection as it goes down; that's success.
func redisShutdown(addr string) func(context.Context) error {
	return func(ctx context.Context) error {
		conn, err := dial(ctx, addr)
		if err != nil {
			return err
		}
		defer conn.Close()
		if _, err := conn.Write([]byte("SHUTDOWN\r\n")); err != nil {
			return err
		}
		line, err := bufio.NewReader(conn).ReadString('\n')
		if err == nil && strings.HasPrefix(line, "-") {
			return fmt.Errorf("valkey refused shutdown: %s", strings.TrimSpace(line))
		}
		return nil
	}
}

// httpReady expects a 2xx from url (Meilisearch /health, Mailpit /livez).
func httpReady(url string) func(context.Context) error {
	return func(ctx context.Context) error {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return err
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return err
		}
		resp.Body.Close()
		if resp.StatusCode < 200 || resp.StatusCode > 299 {
			return fmt.Errorf("%s: %s", url, resp.Status)
		}
		return nil
	}
}

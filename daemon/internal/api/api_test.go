package api

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// fakeDaemon records the last request and answers with handler.
func fakeDaemon(t *testing.T, handler http.HandlerFunc) (*Client, *http.Request) {
	t.Helper()
	var last http.Request
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		last = *r
		handler(w, r)
	}))
	t.Cleanup(srv.Close)
	return NewClient(strings.TrimPrefix(srv.URL, "http://"), "tok"), &last
}

func TestClientSendsTokenAndEscapesNames(t *testing.T) {
	c, last := fakeDaemon(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"name":"app db","state":"running"}`))
	})
	svc, err := c.ServiceAction(context.Background(), "app db", "start")
	if err != nil || svc.State != "running" {
		t.Fatalf("ServiceAction: %+v, %v", svc, err)
	}
	if last.Header.Get("Authorization") != "Bearer tok" {
		t.Fatalf("token not sent: %q", last.Header.Get("Authorization"))
	}
	if last.URL.EscapedPath() != "/api/services/app%20db/start" {
		t.Fatalf("path = %s", last.URL.EscapedPath())
	}
}

func TestDeleteServiceKeepData(t *testing.T) {
	c, last := fakeDaemon(t, func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusNoContent) })
	if err := c.DeleteService(context.Background(), "db", true); err != nil {
		t.Fatal(err)
	}
	if last.Method != http.MethodDelete || last.URL.Query().Get("keep_data") != "1" {
		t.Fatalf("%s %s", last.Method, last.URL)
	}
	if err := c.DeleteService(context.Background(), "db", false); err != nil {
		t.Fatal(err)
	}
	if last.URL.RawQuery != "" {
		t.Fatalf("delete without keep must not send keep_data: %s", last.URL)
	}
}

func TestDaemonErrorKeepsTheHintAtTheEnd(t *testing.T) {
	long := strings.Repeat("x", 2000) + "\nhint: install the Microsoft Visual C++ Redistributable"
	c, _ := fakeDaemon(t, func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, long, http.StatusInternalServerError)
	})
	_, err := c.Services(context.Background())
	if err == nil || !strings.Contains(err.Error(), "hint: install the Microsoft Visual C++") {
		t.Fatalf("the hint at the end of a long error must survive: %v", err)
	}
}

func TestDialFailureMeansNotRunning(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	ln.Close() // nothing listens there now
	if _, err := NewClient(addr, "tok").Status(context.Background()); !errors.Is(err, ErrNotRunning) {
		t.Fatalf("want ErrNotRunning, got %v", err)
	}
}

package portowner

import (
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestListenerFindsThisProcess(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	p, ok, err := Listener(ln.Addr().(*net.TCPAddr).Port)
	if err != nil || !ok {
		t.Fatalf("Listener: %+v, %v, %v", p, ok, err)
	}
	if p.PID != os.Getpid() {
		t.Fatalf("pid = %d, want %d", p.PID, os.Getpid())
	}
	self, _ := os.Executable()
	// Linux comm is truncated to 15 bytes; compare the common prefix.
	if want := filepath.Base(self); !strings.HasPrefix(strings.ToLower(want), strings.ToLower(p.Name)) || p.Name == "" {
		t.Fatalf("name = %q, want %q", p.Name, want)
	}
}

func TestListenerFreePort(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	ln.Close()
	if p, ok, err := Listener(port); ok || err != nil {
		t.Fatalf("free port reported held: %+v, %v", p, err)
	}
}

func TestIsHerd(t *testing.T) {
	for _, c := range []struct {
		p    Process
		want bool
	}{
		{Process{Name: "Herd.exe"}, true},
		{Process{Name: "HerdHelper.exe"}, true},
		{Process{Name: "nginx.exe", Path: `C:\Program Files\Herd\resources\app.asar.unpacked\resources\bin\nginx\nginx.exe`}, true},
		{Process{Name: "php-cgi.exe", Path: `C:\Users\me\.config\herd\bin\php84\php-cgi.exe`}, true},
		{Process{Name: "node.exe", Path: `C:\Users\me\Herd\blog\node_modules\.bin\node.exe`}, false},
		{Process{Name: "nginx", Path: "/Applications/Herd.app/Contents/Resources/nginx"}, true},
		{Process{Name: "nginx", Path: "/usr/sbin/nginx"}, false},
		{Process{Name: "caddy.exe"}, false},
	} {
		if got := IsHerd(c.p); got != c.want {
			t.Errorf("IsHerd(%+v) = %v", c.p, got)
		}
	}
}

func TestProcessesIncludesSelf(t *testing.T) {
	list, err := Processes()
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range list {
		if p.PID == os.Getpid() {
			return
		}
	}
	t.Fatal("own process missing from the process list")
}

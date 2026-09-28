package doctor

import (
	"context"
	"crypto/x509"
	"errors"
	"strings"
	"testing"

	"github.com/d3v0psdan/bench/daemon/internal/api"
	"github.com/d3v0psdan/bench/daemon/internal/portowner"
)

var herdNginx = portowner.Process{PID: 4242, Name: "nginx.exe", Path: `C:\Users\me\.config\herd\bin\nginx\nginx.exe`}

// fakeDoctor: ports maps port → holder; services is the instance list.
func fakeDoctor(ports map[int]portowner.Process, procs []portowner.Process, services []api.Service) *Doctor {
	return &Doctor{
		Services: func() ([]api.Service, error) { return services, nil },
		Listener: func(port int) (portowner.Process, bool, error) {
			p, ok := ports[port]
			return p, ok, nil
		},
		Processes: func() ([]portowner.Process, error) { return procs, nil },
		Resolve: func(ctx context.Context, host string) ([]string, error) {
			return []string{"127.0.0.1"}, nil
		},
		// A machine where setup ran: the CA exists and is trusted.
		RootCA: "root.crt",
		ReadFile: func(path string) ([]byte, error) {
			if path == "root.crt" {
				return testCAPEM(), nil
			}
			return nil, errors.New("absent")
		},
		Trusted: func(*x509.Certificate) error { return nil },
	}
}

func find(t *testing.T, checks []api.Check, id string) api.Check {
	t.Helper()
	for _, c := range checks {
		if c.ID == id {
			return c
		}
	}
	t.Fatalf("no %q check in %+v", id, checks)
	return api.Check{}
}

func TestHerdHoldingHTTPSIsNamed(t *testing.T) {
	d := fakeDoctor(map[int]portowner.Process{443: herdNginx, 80: herdNginx}, []portowner.Process{herdNginx, {Name: "Herd.exe"}}, nil)
	checks := d.Run(context.Background())

	https := find(t, checks, "https")
	if https.Status != Fail || !strings.Contains(https.Detail, "Laravel Herd") || !strings.Contains(https.Detail, "nginx.exe") || len(https.Steps) == 0 {
		t.Fatalf("https check: %+v", https)
	}
	if h := find(t, checks, "herd"); h.Status != Warn || len(h.Steps) == 0 {
		t.Fatalf("herd check: %+v", h)
	}
	if h := find(t, checks, "http"); h.Status != Warn || !strings.Contains(h.Detail, "Laravel Herd") {
		t.Fatalf("http check: %+v", h)
	}
	for _, id := range []string{"herd", "https", "http"} {
		if c := find(t, checks, id); c.Action != ActionRecheck {
			t.Errorf("%s action = %q, want %q (fix it outside Bench, then check again)", id, c.Action, ActionRecheck)
		}
	}
}

func TestUnresolvedDNSAsksForSetup(t *testing.T) {
	d := fakeDoctor(nil, nil, nil)
	d.Resolve = func(ctx context.Context, host string) ([]string, error) { return nil, errors.New("no such host") }
	dns := find(t, d.Run(context.Background()), "dns")
	if dns.Status != Warn || dns.Action != ActionSetup || strings.Contains(strings.Join(dns.Steps, " "), "Settings, then") {
		t.Fatalf("dns check: %+v", dns)
	}
}

func TestMailFallbackSurfacesMailPort(t *testing.T) {
	d := fakeDoctor(map[int]portowner.Process{2525: herdNginx}, nil, []api.Service{
		{Name: "mailpit", Service: "mailpit", State: "running", Port: 2526, Notice: "port 2525 is in use, so mailpit listens on 2526"},
	})
	mail := find(t, d.Run(context.Background()), "mail")
	if mail.Status != Warn || !strings.Contains(strings.Join(mail.Steps, " "), "MAIL_PORT=2526") {
		t.Fatalf("mail check: %+v", mail)
	}
	if !strings.Contains(mail.Detail, "Laravel Herd") {
		t.Fatalf("the :2525 holder must be named even after Mail moved: %+v", mail)
	}
}

func TestMailPortHeldBeforeEnabling(t *testing.T) {
	d := fakeDoctor(map[int]portowner.Process{2525: herdNginx}, nil, nil)
	mail := find(t, d.Run(context.Background()), "mail")
	if mail.Status != Warn || !strings.Contains(mail.Detail, "Laravel Herd") {
		t.Fatalf("mail check: %+v", mail)
	}
}

func TestStoppedServiceWithStolenPortFails(t *testing.T) {
	d := fakeDoctor(map[int]portowner.Process{3306: {PID: 7, Name: "mysqld.exe"}}, nil, []api.Service{
		{Name: "app-db", Service: "mysql", State: "stopped", Port: 3306},
	})
	c := find(t, d.Run(context.Background()), "service-app-db")
	if c.Status != Fail || !strings.Contains(c.Detail, "mysqld.exe") {
		t.Fatalf("service check: %+v", c)
	}
}

func TestCleanMachineIsAllOK(t *testing.T) {
	d := fakeDoctor(nil, nil, nil)
	for _, c := range d.Run(context.Background()) {
		if c.Status != OK && c.ID != "vcruntime" {
			t.Errorf("%s: %+v", c.ID, c)
		}
	}
}

func TestUnresolvedTestDomainWarns(t *testing.T) {
	d := fakeDoctor(nil, nil, nil)
	d.Resolve = func(ctx context.Context, host string) ([]string, error) { return nil, errors.New("no such host") }
	if c := find(t, d.Run(context.Background()), "dns"); c.Status != Warn || !strings.Contains(strings.Join(c.Steps, " "), "bench setup") {
		t.Fatalf("dns check: %+v", c)
	}
}

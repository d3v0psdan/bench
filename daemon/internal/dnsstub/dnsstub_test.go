package dnsstub

import (
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/miekg/dns"
)

func newTestStub(t *testing.T) *Stub {
	t.Helper()
	s := &Stub{Addr: "127.0.0.1:0", Log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	if err := s.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Stop)
	return s
}

func query(t *testing.T, addr, net, name string, qtype uint16) *dns.Msg {
	t.Helper()
	c := &dns.Client{Net: net, Timeout: 5 * time.Second}
	m := new(dns.Msg)
	m.SetQuestion(dns.Fqdn(name), qtype)
	resp, _, err := c.Exchange(m, addr)
	if err != nil {
		t.Fatalf("query %s %s: %v", name, dns.TypeToString[qtype], err)
	}
	return resp
}

func TestResolvesTestDomains(t *testing.T) {
	s := newTestStub(t)
	for _, network := range []string{"udp", "tcp"} {
		resp := query(t, s.BoundAddr(), network, "myapp.test", dns.TypeA)
		if resp.Rcode != dns.RcodeSuccess || !resp.Authoritative || len(resp.Answer) != 1 {
			t.Fatalf("%s A response = %+v", network, resp)
		}
		a, ok := resp.Answer[0].(*dns.A)
		if !ok || a.A.String() != "127.0.0.1" {
			t.Fatalf("%s A answer = %v", network, resp.Answer[0])
		}
	}
	resp := query(t, s.BoundAddr(), "udp", "deep.sub.myapp.test", dns.TypeAAAA)
	if len(resp.Answer) != 1 {
		t.Fatalf("AAAA response = %+v", resp)
	}
	if aaaa := resp.Answer[0].(*dns.AAAA); aaaa.AAAA.String() != "::1" {
		t.Fatalf("AAAA answer = %v", resp.Answer[0])
	}
}

func TestRefusesOutsideTestZone(t *testing.T) {
	s := newTestStub(t)
	for _, name := range []string{"example.com", "attacker.internal", "test.example.org"} {
		resp := query(t, s.BoundAddr(), "udp", name, dns.TypeA)
		if resp.Rcode != dns.RcodeRefused || len(resp.Answer) != 0 {
			t.Fatalf("%s must be REFUSED, got %+v", name, resp)
		}
	}
}

func TestNoAnswerForOtherQtypes(t *testing.T) {
	s := newTestStub(t)
	resp := query(t, s.BoundAddr(), "udp", "myapp.test", dns.TypeMX)
	if resp.Rcode != dns.RcodeSuccess || len(resp.Answer) != 0 {
		t.Fatalf("MX response = %+v", resp)
	}
}

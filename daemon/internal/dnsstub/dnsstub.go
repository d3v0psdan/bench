// Package dnsstub answers *.test with 127.0.0.1/::1 on a loopback
// listener. The per-OS resolver setup (NRPT rule, /etc/resolver/test,
// systemd-resolved drop-in, installed by bench-helper) routes only the
// .test zone here; everything else is refused, never recursed.
package dnsstub

import (
	"fmt"
	"log/slog"
	"net"
	"runtime"
	"strings"

	"github.com/miekg/dns"
)

// ListenAddr is the stub's per-OS home. Windows NRPT rules cannot carry a
// port, so the stub must own :53, on an exotic loopback IP (all of
// 127/8 is loopback) to avoid fighting Docker/WSL for 127.0.0.1:53.
// macOS /etc/resolver files and systemd-resolved both support ports, so
// a high port on plain loopback works there.
func ListenAddr() string {
	if runtime.GOOS == "windows" {
		return "127.65.43.53:53"
	}
	return "127.0.0.1:23653"
}

// TTL is deliberately short: sites appear and disappear as users park
// and link, and stale positive answers outlive the daemon otherwise.
const ttl = 10

// Stub is the embedded DNS server (runs in-process, not supervised).
type Stub struct {
	Addr string // "" = ListenAddr()
	Log  *slog.Logger

	udp, tcp *dns.Server
	boundUDP net.Addr
}

// Start binds UDP and TCP listeners and serves in the background.
// Bind errors surface synchronously.
func (s *Stub) Start() error {
	addr := s.Addr
	if addr == "" {
		addr = ListenAddr()
	}
	mux := dns.NewServeMux()
	mux.HandleFunc("test.", s.handleTest)
	mux.HandleFunc(".", refuse)

	pc, err := net.ListenPacket("udp", addr)
	if err != nil {
		return fmt.Errorf("dns stub udp bind %s: %w", addr, err)
	}
	// TCP binds whatever port UDP actually got (matters for :0 in tests).
	ln, err := net.Listen("tcp", pc.LocalAddr().String())
	if err != nil {
		pc.Close()
		return fmt.Errorf("dns stub tcp bind %s: %w", pc.LocalAddr(), err)
	}
	s.boundUDP = pc.LocalAddr()
	s.udp = &dns.Server{PacketConn: pc, Handler: mux}
	s.tcp = &dns.Server{Listener: ln, Handler: mux}
	go s.serve(s.udp, "udp")
	go s.serve(s.tcp, "tcp")
	s.Log.Info("dns stub listening", "addr", addr)
	return nil
}

func (s *Stub) serve(srv *dns.Server, net string) {
	if err := srv.ActivateAndServe(); err != nil {
		s.Log.Error("dns stub server exited", "net", net, "err", err)
	}
}

// BoundAddr reports the actual UDP listen address (tests use :0).
func (s *Stub) BoundAddr() string {
	if s.boundUDP == nil {
		return ""
	}
	return s.boundUDP.String()
}

// Stop shuts both listeners down.
func (s *Stub) Stop() {
	if s.udp != nil {
		_ = s.udp.Shutdown()
	}
	if s.tcp != nil {
		_ = s.tcp.Shutdown()
	}
}

// handleTest answers A/AAAA for any name under .test with loopback,
// authoritatively. Other qtypes get an empty NOERROR (so resolvers don't
// retry elsewhere), never a referral or recursion.
func (s *Stub) handleTest(w dns.ResponseWriter, r *dns.Msg) {
	m := new(dns.Msg)
	m.SetReply(r)
	m.Authoritative = true
	m.RecursionAvailable = false
	for _, q := range r.Question {
		if !strings.HasSuffix(strings.ToLower(q.Name), "test.") {
			continue
		}
		hdr := dns.RR_Header{Name: q.Name, Class: dns.ClassINET, Ttl: ttl}
		switch q.Qtype {
		case dns.TypeA:
			hdr.Rrtype = dns.TypeA
			m.Answer = append(m.Answer, &dns.A{Hdr: hdr, A: net.IPv4(127, 0, 0, 1)})
		case dns.TypeAAAA:
			hdr.Rrtype = dns.TypeAAAA
			m.Answer = append(m.Answer, &dns.AAAA{Hdr: hdr, AAAA: net.IPv6loopback})
		}
	}
	_ = w.WriteMsg(m)
}

// refuse answers anything outside .test with REFUSED: this stub is an
// authoritative island, not a resolver.
func refuse(w dns.ResponseWriter, r *dns.Msg) {
	m := new(dns.Msg)
	m.SetRcode(r, dns.RcodeRefused)
	_ = w.WriteMsg(m)
}

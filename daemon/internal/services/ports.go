package services

import (
	"fmt"
	"net"
	"strconv"
	"time"
)

// portSearchSpan bounds how far past the default port allocation looks.
const portSearchSpan = 100

// portProbeTimeout bounds portFree's dial. Windows retries refused
// loopback connects for ~1s, so a short timeout (not a refusal) is the
// normal "free" answer there; a live listener answers in milliseconds.
const portProbeTimeout = 250 * time.Millisecond

// herdReserved reports ports Laravel Herd's own processes use (HerdHelper
// on :5000, php-cgi pools on 90XX/91XX). Auto-allocation steps over them
// so Bench and Herd can share a machine; an explicit request still wins.
func herdReserved(port int) bool {
	return port == 5000 || (port >= 9002 && port <= 9199)
}

// portFree reports whether nothing serves on 127.0.0.1:port. Both checks
// matter: a successful bind alone is not proof on Windows, which lets us
// bind 127.0.0.1 beside another process's 0.0.0.0 listener (Herd's mail
// catcher holds 0.0.0.0:2525) and then silently steal its loopback
// traffic; a live listener answers the dial within milliseconds.
func portFree(port int) bool {
	addr := net.JoinHostPort("127.0.0.1", strconv.Itoa(port))
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return false
	}
	ln.Close()
	conn, err := net.DialTimeout("tcp", addr, portProbeTimeout)
	if err != nil {
		return true
	}
	conn.Close()
	return false
}

// allocatePort returns want if it is free and not taken by another
// instance, else the next free one. explicit=true means the user asked
// for exactly this port: it is never moved, only rejected.
func allocatePort(want int, explicit bool, taken map[int]bool, free func(int) bool) (int, error) {
	if explicit {
		if want < 1 || want > 65535 {
			return 0, inputErrorf("port %d is out of range", want)
		}
		if taken[want] {
			return 0, inputErrorf("port %d is already assigned to another service instance", want)
		}
		if !free(want) {
			return 0, inputErrorf("port %d is in use by another process", want)
		}
		return want, nil
	}
	// Reserved ports don't count against the span: RustFS's default 9000
	// sits right below Herd's 9002–9199 block.
	for p, tried := want, 0; tried < portSearchSpan && p <= 65535; p++ {
		if p != want && herdReserved(p) {
			continue
		}
		tried++
		if taken[p] || !free(p) {
			continue
		}
		return p, nil
	}
	return 0, fmt.Errorf("no free port among %d candidates from %d", portSearchSpan, want)
}

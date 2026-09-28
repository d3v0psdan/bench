//go:build darwin

package portowner

import (
	"context"
	"net"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// commandTimeout caps each lsof/ps run so a wedged tool can't stall the caller.
const commandTimeout = 3 * time.Second

// dialTimeout is plenty for a loopback listener to answer.
const dialTimeout = 300 * time.Millisecond

// listener asks lsof (always present on macOS) for the TCP listener.
// Shortcut: shells out to lsof/ps; move to libproc if doctor latency
// ever matters.
func listener(port int) (Process, bool, error) {
	ctx, cancel := context.WithTimeout(context.Background(), commandTimeout)
	defer cancel()
	out, err := exec.CommandContext(ctx, "lsof", "-nP", "-iTCP:"+strconv.Itoa(port), "-sTCP:LISTEN", "-Fpc").Output()
	if err != nil {
		// lsof exits 1 when nothing matches, and without root it can't see
		// other users' sockets (e.g. a root-owned :443): if something
		// answers, the port is held by an owner we can't name.
		return Process{}, listening(port), nil
	}
	var p Process
	for _, line := range strings.Split(string(out), "\n") {
		switch {
		case strings.HasPrefix(line, "p") && p.PID == 0:
			p.PID, _ = strconv.Atoi(line[1:])
		case strings.HasPrefix(line, "c") && p.Name == "":
			p.Name = line[1:]
		}
	}
	return p, p.PID != 0, nil
}

func processes() ([]Process, error) {
	ctx, cancel := context.WithTimeout(context.Background(), commandTimeout)
	defer cancel()
	out, err := exec.CommandContext(ctx, "ps", "-axo", "pid=,comm=").Output()
	if err != nil {
		return nil, err
	}
	var list []Process
	for _, line := range strings.Split(string(out), "\n") {
		pidStr, path, ok := strings.Cut(strings.TrimSpace(line), " ")
		if !ok {
			continue
		}
		pid, _ := strconv.Atoi(pidStr)
		path = strings.TrimSpace(path)
		list = append(list, Process{PID: pid, Name: filepath.Base(path), Path: path})
	}
	return list, nil
}

// listening reports whether anything accepts connections on the port.
func listening(port int) bool {
	conn, err := net.DialTimeout("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)), dialTimeout)
	if err != nil {
		return false
	}
	conn.Close()
	return true
}

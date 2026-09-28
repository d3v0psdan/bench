//go:build linux

package portowner

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// listener finds the socket inode listening on port in /proc/net/tcp{,6}
// (state 0A = LISTEN), then the process holding that inode. Without root,
// other users' fds are unreadable: the port is then reported as held by
// an unknown owner.
func listener(port int) (Process, bool, error) {
	inode := ""
	for _, f := range []string{"/proc/net/tcp", "/proc/net/tcp6"} {
		if inode = listenInode(f, port); inode != "" {
			break
		}
	}
	if inode == "" {
		return Process{}, false, nil
	}
	target := "socket:[" + inode + "]"
	dirs, _ := filepath.Glob("/proc/[0-9]*/fd")
	for _, dir := range dirs {
		fds, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, fd := range fds {
			if link, err := os.Readlink(filepath.Join(dir, fd.Name())); err == nil && link == target {
				pid, _ := strconv.Atoi(filepath.Base(filepath.Dir(dir)))
				return procInfo(pid), true, nil
			}
		}
	}
	return Process{}, true, nil
}

func listenInode(file string, port int) string {
	f, err := os.Open(file)
	if err != nil {
		return ""
	}
	defer f.Close()
	want := fmt.Sprintf(":%04X", port)
	sc := bufio.NewScanner(f)
	sc.Scan() // header
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		if len(fields) < 10 || fields[3] != "0A" || !strings.HasSuffix(fields[1], want) {
			continue
		}
		return fields[9]
	}
	return ""
}

func procInfo(pid int) Process {
	p := Process{PID: pid}
	if b, err := os.ReadFile(fmt.Sprintf("/proc/%d/comm", pid)); err == nil {
		p.Name = strings.TrimSpace(string(b))
	}
	if exe, err := os.Readlink(fmt.Sprintf("/proc/%d/exe", pid)); err == nil {
		p.Path = exe
	}
	return p
}

func processes() ([]Process, error) {
	dirs, err := filepath.Glob("/proc/[0-9]*")
	if err != nil {
		return nil, err
	}
	var out []Process
	for _, d := range dirs {
		pid, err := strconv.Atoi(filepath.Base(d))
		if err != nil {
			continue
		}
		out = append(out, procInfo(pid))
	}
	return out, nil
}

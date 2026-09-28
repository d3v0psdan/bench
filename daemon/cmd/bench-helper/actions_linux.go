//go:build linux

package main

import (
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
)

// caStore is one distro layout: its CA drop-in dir and update command.
type caStore struct {
	dir    string
	update []string
}

// caStores are the Debian and RHEL layouts, in the order trustCA picks.
var caStores = []caStore{
	{"/usr/local/share/ca-certificates", []string{"update-ca-certificates"}},
	{"/etc/pki/ca-trust/source/anchors", []string{"update-ca-trust", "extract"}},
}

const caFile = "bench-local-ca.crt"

// trustCA writes the validated certificate (not the user's file) through a
// fresh temp file renamed into place, so no link planted at the target is
// followed, then refreshes the store.
func trustCA(c benchCA) error {
	for _, s := range caStores {
		if _, err := os.Stat(s.dir); err != nil {
			continue
		}
		if err := writeReplace(filepath.Join(s.dir, caFile), c.pem()); err != nil {
			return err
		}
		return runCmd(s.update[0], s.update[1:]...)
	}
	return fmt.Errorf("no known CA trust directory found (looked for Debian and RHEL layouts)")
}

func writeReplace(target string, b []byte) error {
	f, err := os.CreateTemp(filepath.Dir(target), ".bench-ca-*")
	if err != nil {
		return fmt.Errorf("writing %s: %w", target, err)
	}
	defer os.Remove(f.Name()) // no-op after the rename
	if _, err := f.Write(b); err != nil {
		f.Close()
		return fmt.Errorf("writing %s: %w", target, err)
	}
	if err := f.Chmod(0o644); err != nil {
		f.Close()
		return fmt.Errorf("writing %s: %w", target, err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("writing %s: %w", target, err)
	}
	if err := os.Rename(f.Name(), target); err != nil {
		return fmt.Errorf("writing %s: %w", target, err)
	}
	return nil
}

// untrustRecorded removes Bench's CA file from every layout (a machine
// can have both dirs), which is where trustCA's record lives on Linux.
func untrustRecorded() (bool, error) {
	for _, s := range caStores {
		if _, err := os.Stat(s.dir); err != nil {
			continue
		}
		target := filepath.Join(s.dir, caFile)
		if err := os.Remove(target); err != nil && !os.IsNotExist(err) {
			return false, fmt.Errorf("removing %s: %w", target, err)
		}
		if err := runCmd(s.update[0], s.update[1:]...); err != nil {
			return false, err
		}
	}
	return true, nil
}

// untrustCert: the store holds Bench's CA under one file name, whatever
// the certificate.
func untrustCert(benchCA) error {
	_, err := untrustRecorded()
	return err
}

const resolvedDropIn = "/etc/systemd/resolved.conf.d/bench-test.conf"

func dnsSetup(addr string) error {
	ip, _, err := net.SplitHostPort(addr)
	if err != nil {
		return fmt.Errorf("dns-setup needs ip:port, got %q: %w", addr, err)
	}
	if net.ParseIP(ip) == nil || !net.ParseIP(ip).IsLoopback() {
		return fmt.Errorf("refusing non-loopback dns server %q", addr)
	}
	if _, err := exec.LookPath("systemctl"); err != nil {
		return fmt.Errorf("systemd-resolved isn't available; add '127.0.0.1 <name>.test' lines to /etc/hosts instead (see the DNS fallback note in README.md)")
	}
	if err := os.MkdirAll("/etc/systemd/resolved.conf.d", 0o755); err != nil {
		return fmt.Errorf("creating resolved.conf.d: %w", err)
	}
	content := fmt.Sprintf("# Bench *.test resolver\n[Resolve]\nDNS=%s\nDomains=~test\n", addr)
	if err := os.WriteFile(resolvedDropIn, []byte(content), 0o644); err != nil {
		return fmt.Errorf("writing %s: %w", resolvedDropIn, err)
	}
	return runCmd("systemctl", "try-restart", "systemd-resolved")
}

// dnsRemove is safe to repeat: with no drop-in (DNS was never set up, or
// setup stopped because systemd-resolved is missing) there's nothing to
// remove and nothing to restart.
func dnsRemove() error {
	err := os.Remove(resolvedDropIn)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("removing %s: %w", resolvedDropIn, err)
	}
	if _, err := exec.LookPath("systemctl"); err != nil {
		return nil // no systemd-resolved to reload; the file is gone
	}
	return runCmd("systemctl", "try-restart", "systemd-resolved")
}

//go:build darwin

package main

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
)

const (
	systemKeychain = "/Library/Keychains/System.keychain"
	// recordFile is root-owned, so nothing running as the user can change
	// what teardown removes.
	recordFile = "/Library/Application Support/Bench/trusted-ca"
)

// trustCA trusts the validated certificate, written by the helper into a
// private temp dir (not the user's file, which could change after the
// check), and records its thumbprint.
func trustCA(c benchCA) error {
	dir, err := os.MkdirTemp("/var/tmp", "bench-ca-")
	if err != nil {
		return fmt.Errorf("staging the certificate: %w", err)
	}
	defer os.RemoveAll(dir)
	pemPath := filepath.Join(dir, "root.crt")
	if err := os.WriteFile(pemPath, c.pem(), 0o600); err != nil {
		return fmt.Errorf("staging the certificate: %w", err)
	}
	if err := runCmd("security", "add-trusted-cert", "-d", "-r", "trustRoot", "-k", systemKeychain, pemPath); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(recordFile), 0o755); err != nil {
		return fmt.Errorf("recording the CA: %w", err)
	}
	return os.WriteFile(recordFile, []byte(c.thumbprint()+"\n"), 0o644)
}

// untrustRecorded removes the CA recorded at install; false when there is
// no record (a setup from before records existed).
func untrustRecorded() (bool, error) {
	b, err := os.ReadFile(recordFile)
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("reading %s: %w", recordFile, err)
	}
	thumb := strings.TrimSpace(string(b))
	if !thumbprintRe.MatchString(thumb) {
		return false, fmt.Errorf("the recorded CA thumbprint %q is malformed", thumb)
	}
	if err := deleteThumbprint(thumb); err != nil {
		return false, err
	}
	if err := os.Remove(recordFile); err != nil {
		return false, fmt.Errorf("removing %s: %w", recordFile, err)
	}
	return true, nil
}

func untrustCert(c benchCA) error { return deleteThumbprint(c.thumbprint()) }

// deleteThumbprint removes exactly one certificate (and its trust
// settings) from the System keychain by its SHA-1 hash.
func deleteThumbprint(thumb string) error {
	return runCmd("security", "delete-certificate", "-Z", thumb, "-t", systemKeychain)
}

const resolverFile = "/etc/resolver/test"

func dnsSetup(addr string) error {
	ip, port, err := net.SplitHostPort(addr)
	if err != nil {
		return fmt.Errorf("dns-setup needs ip:port, got %q: %w", addr, err)
	}
	if net.ParseIP(ip) == nil || !net.ParseIP(ip).IsLoopback() {
		return fmt.Errorf("refusing non-loopback dns server %q", addr)
	}
	if err := os.MkdirAll("/etc/resolver", 0o755); err != nil {
		return fmt.Errorf("creating /etc/resolver: %w", err)
	}
	content := fmt.Sprintf("# Bench *.test resolver\nnameserver %s\nport %s\n", ip, port)
	if err := os.WriteFile(resolverFile, []byte(content), 0o644); err != nil {
		return fmt.Errorf("writing %s: %w", resolverFile, err)
	}
	return nil
}

func dnsRemove() error {
	if err := os.Remove(resolverFile); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("removing %s: %w", resolverFile, err)
	}
	return nil
}

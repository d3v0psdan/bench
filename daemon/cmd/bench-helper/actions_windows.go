//go:build windows

package main

import (
	"encoding/base64"
	"errors"
	"fmt"
	"net"
	"strings"

	"golang.org/x/sys/windows/registry"
)

func powershell(script string) error {
	return runCmd("powershell", "-NoProfile", "-NonInteractive", "-Command", script)
}

// psQuote single-quotes a string for PowerShell, doubling any quote in it.
func psQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
}

// The recorded CA lives in HKLM, which only administrators can write, so
// nothing running as the user can change what teardown removes.
const (
	recordKey    = `HKLM\SOFTWARE\Bench` // for reg.exe
	recordSubkey = `SOFTWARE\Bench`      // under HKEY_LOCAL_MACHINE
	recordValue  = "TrustedCA"
)

// trustCA adds the validated certificate to the machine's Root store from
// its bytes (no file to swap between check and use) and records its
// thumbprint.
func trustCA(c benchCA) error {
	script := `$c = [System.Security.Cryptography.X509Certificates.X509Certificate2]::new([Convert]::FromBase64String(` +
		psQuote(base64.StdEncoding.EncodeToString(c.der)) + `)); ` +
		`$s = [System.Security.Cryptography.X509Certificates.X509Store]::new('Root', 'LocalMachine'); ` +
		`$s.Open('ReadWrite'); $s.Add($c); $s.Close()`
	if err := powershell(script); err != nil {
		return err
	}
	return runCmd("reg", "add", recordKey, "/v", recordValue, "/t", "REG_SZ", "/d", c.thumbprint(), "/f")
}

// untrustRecorded removes the CA recorded at install; false when there is
// no record (a setup from before records existed).
func untrustRecorded() (bool, error) {
	k, err := registry.OpenKey(registry.LOCAL_MACHINE, recordSubkey, registry.QUERY_VALUE)
	if errors.Is(err, registry.ErrNotExist) {
		return false, nil // no record
	}
	if err != nil {
		return false, fmt.Errorf("reading the CA record: %w", err)
	}
	thumb, _, err := k.GetStringValue(recordValue)
	k.Close()
	if errors.Is(err, registry.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("reading the CA record: %w", err)
	}
	if !thumbprintRe.MatchString(thumb) {
		return false, fmt.Errorf("the recorded CA thumbprint %q is malformed", thumb)
	}
	if err := removeThumbprint(thumb); err != nil {
		return false, err
	}
	return true, runCmd("reg", "delete", recordKey, "/v", recordValue, "/f")
}

// untrustCert removes one validated certificate by its thumbprint.
func untrustCert(c benchCA) error { return removeThumbprint(c.thumbprint()) }

// removeThumbprint deletes exactly one certificate from the machine's Root
// store (a thumbprint is a hash; it matches nothing else).
func removeThumbprint(thumb string) error {
	return powershell(`Get-ChildItem Cert:\LocalMachine\Root | Where-Object Thumbprint -eq ` + psQuote(thumb) + ` | Remove-Item`)
}

// benchRule matches only Bench's NRPT rule, never another tool's .test rule.
const benchRule = `Get-DnsClientNrptRule | Where-Object { $_.Namespace -eq '.test' -and $_.Comment -eq 'Bench *.test resolver' }`

func dnsSetup(addr string) error {
	ip, _, err := net.SplitHostPort(addr)
	if err != nil {
		ip = addr
	}
	if net.ParseIP(ip) == nil || !net.ParseIP(ip).IsLoopback() {
		return fmt.Errorf("refusing non-loopback dns server %q", addr)
	}
	// Idempotent: drop Bench's own .test rule, then add it again. NRPT rules
	// cannot carry a port; benchd's stub listens on :53 for this.
	return powershell(
		benchRule + ` | Remove-DnsClientNrptRule -Force; ` +
			`Add-DnsClientNrptRule -Namespace '.test' -NameServers ` + psQuote(ip) + ` -Comment 'Bench *.test resolver' | Out-Null`)
}

func dnsRemove() error {
	return powershell(benchRule + ` | Remove-DnsClientNrptRule -Force`)
}

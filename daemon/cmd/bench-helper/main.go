// bench-helper is the one-shot elevated helper, the ONLY privileged
// Bench code. It does exactly two things: install/remove the local CA in
// the system trust store, and set up/remove the .test resolver rule.
// benchd invokes it with UAC/osascript/pkexec; it never runs long-lived.
package main

import (
	"bytes"
	"crypto/sha1"
	"crypto/x509"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strings"

	"github.com/d3v0psdan/bench/daemon/internal/version"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "bench-helper:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		return usageError()
	}
	switch args[0] {
	case "setup":
		// Combined action so benchd needs only one elevation prompt.
		fs := flag.NewFlagSet("setup", flag.ContinueOnError)
		ca := fs.String("ca", "", "CA certificate to trust")
		dnsAddr := fs.String("dns", "", "DNS stub ip:port for the .test resolver rule")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		if *ca == "" && *dnsAddr == "" {
			return usageError()
		}
		if *ca != "" {
			c, err := loadBenchCA(*ca)
			if err != nil {
				return err
			}
			if err := trustCA(c); err != nil {
				return err
			}
		}
		if *dnsAddr != "" {
			if err := dnsSetup(*dnsAddr); err != nil {
				return err
			}
		}
		return nil
	case "teardown":
		// Undoes setup with one elevation prompt; tries every part so one
		// failure doesn't leave the rest behind.
		fs := flag.NewFlagSet("teardown", flag.ContinueOnError)
		ca := fs.String("ca", "", "CA certificate to remove from the trust store")
		dns := fs.Bool("dns", false, "remove the .test resolver rule")
		// Uninstallers can't read the user's CA file (a root package script,
		// or a Bench home that is gone): the helper removes what it
		// recorded installing, so no certificate is needed.
		installed := fs.Bool("installed-ca", false, "remove the CA the helper recorded installing")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		if *ca == "" && !*dns && !*installed {
			return usageError()
		}
		var errs []error
		if *installed || *ca != "" {
			if err := removeCA(*ca); err != nil {
				errs = append(errs, fmt.Errorf("removing the CA: %w", err))
			}
		}
		if *dns {
			if err := dnsRemove(); err != nil {
				errs = append(errs, fmt.Errorf("removing the .test rule: %w", err))
			}
		}
		return errors.Join(errs...)
	case "trust-ca":
		if len(args) != 2 {
			return usageError()
		}
		c, err := loadBenchCA(args[1])
		if err != nil {
			return err
		}
		return trustCA(c)
	case "untrust-ca":
		if len(args) != 2 {
			return usageError()
		}
		return removeCA(args[1])
	case "dns-setup":
		if len(args) != 2 {
			return usageError()
		}
		return dnsSetup(args[1])
	case "dns-remove":
		if len(args) != 1 {
			return usageError()
		}
		return dnsRemove()
	case "version", "--version":
		fmt.Printf("bench-helper v%s\n", version.Version)
		return nil
	default:
		return usageError()
	}
}

// runCmd runs a system tool and folds its output into the error.
func runCmd(name string, args ...string) error {
	out, err := exec.Command(name, args...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s %s: %w: %s", name, strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return nil
}

func usageError() error {
	return fmt.Errorf("usage: bench-helper setup [-ca <cert.pem>] [-dns <ip:port>] | teardown [-ca <cert.pem>] [-installed-ca] [-dns] | trust-ca <cert.pem> | untrust-ca <cert.pem> | dns-setup <ip:port> | dns-remove")
}

// benchCA is the one kind of certificate the helper trusts or removes: a
// single self-signed CA named like Caddy's local authority. The file sits
// in the user's writable Bench home (anything running as the user can
// replace it), so everything else is refused.
type benchCA struct {
	cert *x509.Certificate
	der  []byte
}

// caSubjectPrefix is how Caddy names its local root ("Caddy Local
// Authority - 2024 ECC Root").
const caSubjectPrefix = "Caddy Local Authority"

// loadBenchCA reads path once and validates it; callers use the returned
// bytes, never the file again, so it can't be swapped after the check.
func loadBenchCA(path string) (benchCA, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return benchCA{}, fmt.Errorf("reading certificate: %w", err)
	}
	return parseBenchCA(b)
}

func parseBenchCA(b []byte) (benchCA, error) {
	block, rest := pem.Decode(b)
	if block == nil || block.Type != "CERTIFICATE" {
		return benchCA{}, errors.New("not a PEM certificate")
	}
	if len(bytes.TrimSpace(rest)) != 0 {
		return benchCA{}, errors.New("holds more than one certificate; Bench trusts exactly one")
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return benchCA{}, fmt.Errorf("parsing certificate: %w", err)
	}
	if !cert.IsCA {
		return benchCA{}, errors.New("not a CA certificate")
	}
	if !strings.HasPrefix(cert.Subject.CommonName, caSubjectPrefix) {
		return benchCA{}, fmt.Errorf("%q isn't Bench's local authority", cert.Subject.CommonName)
	}
	if !bytes.Equal(cert.RawIssuer, cert.RawSubject) || cert.CheckSignatureFrom(cert) != nil {
		return benchCA{}, errors.New("not self-signed, so not Bench's local authority")
	}
	return benchCA{cert: cert, der: block.Bytes}, nil
}

// thumbprint is the SHA-1 fingerprint trust stores key certificates by.
func (c benchCA) thumbprint() string {
	sum := sha1.Sum(c.der)
	return strings.ToUpper(hex.EncodeToString(sum[:]))
}

// thumbprintRe is what a recorded thumbprint must look like before it goes
// anywhere near a command line.
var thumbprintRe = regexp.MustCompile(`^[0-9A-F]{40}$`)

// removeCA removes the CA the helper recorded installing; a setup made
// before the record existed falls back to a validated certificate file.
func removeCA(caPath string) error {
	removed, err := untrustRecorded()
	if err != nil || removed {
		return err
	}
	if caPath == "" {
		// Nothing this helper knows it trusted (never set up, or a setup
		// from before records existed, which only its certificate
		// identifies). Not a failure; the caller words its message.
		fmt.Fprintln(os.Stderr, "no recorded Bench CA to remove")
		return nil
	}
	c, err := loadBenchCA(caPath)
	if err != nil {
		return err
	}
	return untrustCert(c)
}

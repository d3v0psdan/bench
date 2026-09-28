package doctor

import (
	"crypto/x509"
	"encoding/pem"
	"errors"
	"io/fs"

	"github.com/d3v0psdan/bench/daemon/internal/api"
)

// checkTrust reports whether the operating system trusts Bench's local
// certificate authority, so "setup done" is read from the machine rather
// than remembered by a client.
func (d *Doctor) checkTrust() api.Check {
	c := api.Check{ID: "trust", Title: "HTTPS certificates"}
	if d.RootCA == "" {
		c.Status, c.Detail = Warn, "Bench couldn't tell where its certificate authority lives."
		return c
	}
	raw, err := d.ReadFile(d.RootCA)
	if errors.Is(err, fs.ErrNotExist) {
		c.Status = Warn
		c.Detail = "Bench's certificate authority doesn't exist yet, so browsers can't trust https://*.test."
		c.Steps, c.Action = setupSteps, ActionSetup
		return c
	}
	if err != nil {
		c.Status, c.Detail = Warn, "Couldn't read Bench's certificate authority: "+err.Error()
		return c
	}
	block, _ := pem.Decode(raw)
	if block == nil {
		c.Status, c.Detail = Warn, "Bench's certificate authority file isn't a PEM certificate: "+d.RootCA
		return c
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		c.Status, c.Detail = Warn, "Couldn't parse Bench's certificate authority: "+err.Error()
		return c
	}
	if err := d.Trusted(cert); err != nil {
		c.Status = Warn
		c.Detail = "This machine doesn't trust Bench's certificates yet, so browsers warn on https://*.test."
		c.Steps, c.Action = setupSteps, ActionSetup
		return c
	}
	c.Status, c.Detail = OK, "This machine trusts Bench's local certificate authority."
	return c
}

// systemTrusts verifies a root against the OS trust store (the Windows and
// macOS platform verifiers, or the system CA bundle on Linux).
func systemTrusts(cert *x509.Certificate) error {
	_, err := cert.Verify(x509.VerifyOptions{})
	return err
}

package main

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"testing"
	"time"
)

// newCert makes a CA named cn, signed by parent (nil: self-signed).
func newCert(t *testing.T, cn string, isCA bool, parent *x509.Certificate, parentKey *ecdsa.PrivateKey) ([]byte, *x509.Certificate, *ecdsa.PrivateKey) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: cn},
		NotBefore:             time.Now(),
		NotAfter:              time.Now().Add(time.Hour),
		IsCA:                  isCA,
		BasicConstraintsValid: true,
		KeyUsage:              x509.KeyUsageCertSign,
	}
	if parent == nil {
		parent, parentKey = tmpl, key
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, parent, &key.PublicKey, parentKey)
	if err != nil {
		t.Fatal(err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), cert, key
}

func TestParseBenchCATrustsOnlyASingleSelfSignedCaddyRoot(t *testing.T) {
	root, rootCert, rootKey := newCert(t, "Caddy Local Authority - 2026 ECC Root", true, nil, nil)
	c, err := parseBenchCA(root)
	if err != nil {
		t.Fatalf("Caddy's root refused: %v", err)
	}
	if !thumbprintRe.MatchString(c.thumbprint()) {
		t.Errorf("thumbprint %q doesn't match what untrust accepts", c.thumbprint())
	}

	other, _, _ := newCert(t, "Evil Corp Root", true, nil, nil)
	leaf, _, _ := newCert(t, "Caddy Local Authority - leaf", false, nil, nil)
	intermediate, _, _ := newCert(t, "Caddy Local Authority - 2026 ECC Intermediate", true, rootCert, rootKey)
	for name, b := range map[string][]byte{
		"another CA":               other,
		"not a CA":                 leaf,
		"not self-signed":          intermediate,
		"a bundle":                 append(append([]byte{}, root...), other...),
		"not PEM":                  []byte("hello"),
		"a key, not a certificate": pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: []byte{1}}),
	} {
		if _, err := parseBenchCA(b); err == nil {
			t.Errorf("%s was accepted", name)
		}
	}
}

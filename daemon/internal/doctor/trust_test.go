package doctor

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"io/fs"
	"math/big"
	"sync"
	"testing"
	"time"
)

// testCAPEM is one self-signed CA certificate shared by the tests.
var testCAPEM = sync.OnceValue(func() []byte {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		panic(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "Bench test CA"},
		NotBefore: time.Now(), NotAfter: time.Now().Add(time.Hour),
		IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		panic(err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
})

func TestCheckTrust(t *testing.T) {
	ca := testCAPEM()
	untrusted := func(*x509.Certificate) error { return errors.New("unknown authority") }
	trusted := func(*x509.Certificate) error { return nil }

	tests := []struct {
		name       string
		file       []byte
		readErr    error
		verify     func(*x509.Certificate) error
		wantStatus string
		wantFix    bool
	}{
		{"not created yet", nil, fs.ErrNotExist, trusted, Warn, true},
		{"not a certificate", []byte("garbage"), nil, trusted, Warn, false},
		{"not trusted", ca, nil, untrusted, Warn, true},
		{"trusted", ca, nil, trusted, OK, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := &Doctor{
				RootCA:   "root.crt",
				ReadFile: func(string) ([]byte, error) { return tt.file, tt.readErr },
				Trusted:  tt.verify,
			}
			c := d.checkTrust()
			if c.ID != "trust" || c.Status != tt.wantStatus || (len(c.Steps) > 0) != tt.wantFix {
				t.Fatalf("check = %+v, want status %s, fix %v", c, tt.wantStatus, tt.wantFix)
			}
			if tt.wantFix && c.Action != ActionSetup {
				t.Fatalf("action = %q, want %q so a GUI offers the setup", c.Action, ActionSetup)
			}
		})
	}
}

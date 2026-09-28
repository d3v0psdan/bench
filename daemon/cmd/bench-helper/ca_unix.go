//go:build !windows

package main

import "encoding/pem"

// pem is the validated certificate re-encoded: exactly what was checked.
func (c benchCA) pem() []byte {
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: c.der})
}

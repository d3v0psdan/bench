// Package token manages the API auth token file shared by benchd and its
// clients. The token gates every request on the localhost API.
package token

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// Load reads the token from path.
func Load(path string) (string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("reading token file: %w", err)
	}
	t := strings.TrimSpace(string(b))
	if t == "" {
		return "", fmt.Errorf("token file %s is empty", path)
	}
	return t, nil
}

// LoadOrCreate returns the existing token, or generates one (32 random
// bytes, hex) and writes it to path with owner-only permissions. An empty
// or whitespace-only file (e.g. an interrupted first run) is regenerated
// like a missing one: it must not brick startup forever.
func LoadOrCreate(path string) (string, error) {
	b, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return "", fmt.Errorf("reading token file: %w", err)
	}
	if t := strings.TrimSpace(string(b)); t != "" {
		return t, nil
	}

	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("generating token: %w", err)
	}
	t := hex.EncodeToString(buf)

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", fmt.Errorf("creating token directory: %w", err)
	}
	if err := os.WriteFile(path, []byte(t+"\n"), 0o600); err != nil {
		return "", fmt.Errorf("writing token file: %w", err)
	}
	return t, nil
}

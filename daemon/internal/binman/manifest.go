// Package binman downloads, verifies, and installs the managed binaries
// (PHP, Caddy, Composer and the services) described by the manifests/
// catalog into <bench-home>/bin/<name>/<version>/. Every download is
// checksum-verified against the manifest before anything is extracted.
package binman

import (
	"embed"
	"encoding/json"
	"fmt"
	"runtime"
	"strings"
)

// Embedded snapshot of the repo-root manifests/ catalog (Go can't embed
// across the module boundary). A unit test guards against drift; Phase 2
// adds fetching fresh manifests from the repo at runtime.
//
//go:embed manifests/*.json
var embeddedFS embed.FS

// Manifest is one catalog file (one managed binary, all versions/platforms).
type Manifest struct {
	Schema int     `json:"schema"`
	Name   string  `json:"name"`
	Builds []Build `json:"builds"`
}

// Build is one installable version for one OS/arch.
type Build struct {
	Name      string     `json:"-"` // filled from the manifest on load
	Version   string     `json:"version"`
	Channel   string     `json:"channel"` // what users ask for: "8.4", "2"
	OS        string     `json:"os"`
	Arch      string     `json:"arch"`
	Downloads []Download `json:"downloads"`
}

// Download is one artifact to fetch and unpack. Exactly one checksum
// field must be set (vendors differ: Caddy publishes SHA-512, php.net
// SHA-256).
type Download struct {
	URL     string `json:"url"`
	SHA256  string `json:"sha256,omitempty"`
	SHA512  string `json:"sha512,omitempty"`
	Archive string `json:"archive"` // "zip" | "tar.gz" | "tar.xz" | "raw"
	// Strip drops this many leading path components from every entry
	// (vendor archives wrap everything in e.g. mysql-8.4.11-winx64/).
	Strip int `json:"strip,omitempty"`
	// File names the installed executable for "raw" downloads, which are
	// a bare binary rather than an archive.
	File string `json:"file,omitempty"`
}

// LoadEmbedded parses the manifest catalog compiled into the daemon,
// keyed by binary name.
func LoadEmbedded() (map[string]Manifest, error) {
	entries, err := embeddedFS.ReadDir("manifests")
	if err != nil {
		return nil, fmt.Errorf("reading embedded manifests: %w", err)
	}
	out := make(map[string]Manifest, len(entries))
	for _, e := range entries {
		b, err := embeddedFS.ReadFile("manifests/" + e.Name())
		if err != nil {
			return nil, fmt.Errorf("reading manifest %s: %w", e.Name(), err)
		}
		var m Manifest
		if err := json.Unmarshal(b, &m); err != nil {
			return nil, fmt.Errorf("parsing manifest %s: %w", e.Name(), err)
		}
		for i := range m.Builds {
			m.Builds[i].Name = m.Name
		}
		out[m.Name] = m
	}
	return out, nil
}

// Resolve finds the build for name matching channel (or exact version) on
// the current platform.
func (m *Manager) Resolve(name, channel string) (Build, error) {
	man, ok := m.Manifests[name]
	if !ok {
		return Build{}, fmt.Errorf("no manifest for %q", name)
	}
	for _, b := range man.Builds {
		if b.OS != runtime.GOOS || b.Arch != runtime.GOARCH {
			continue
		}
		if b.Channel == channel || b.Version == channel {
			return b, nil
		}
	}
	return Build{}, fmt.Errorf("no %s %s build for %s/%s in the catalog", name, channel, runtime.GOOS, runtime.GOARCH)
}

// Channel maps a version as stored or typed ("8.4", "8.4.12") to its
// catalog channel. A full version the catalog no longer carries ("8.3.31"
// after a bump) maps to its "8.3" channel when that exists: the old build
// can't be installed anymore, and the channel is the nearest that runs.
// Unknown values report false.
func (m *Manager) Channel(name, v string) (string, bool) {
	if v == "" {
		return "", false
	}
	if b, err := m.Resolve(name, v); err == nil {
		return b.Channel, true
	}
	parts := strings.Split(v, ".")
	if len(parts) != 3 {
		return "", false
	}
	if b, err := m.Resolve(name, parts[0]+"."+parts[1]); err == nil {
		return b.Channel, true
	}
	return "", false
}

// Channels lists the installable channels for name on this platform, in
// catalog order.
func (m *Manager) Channels(name string) []Build {
	var out []Build
	for _, b := range m.Manifests[name].Builds {
		if b.OS == runtime.GOOS && b.Arch == runtime.GOARCH {
			out = append(out, b)
		}
	}
	return out
}

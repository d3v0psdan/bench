// Package paths resolves every Bench filesystem location. All state lives
// under one root (~/.bench by default, $BENCH_HOME overrides it), so no
// other package hardcodes OS paths.
package paths

import (
	"fmt"
	"os"
	"path/filepath"
)

// Root returns the bench home directory: $BENCH_HOME if set, else ~/.bench.
func Root() (string, error) {
	if h := os.Getenv("BENCH_HOME"); h != "" {
		return h, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolving home directory: %w", err)
	}
	return filepath.Join(home, ".bench"), nil
}

// EnsureRoot creates the root and the subdirectories benchd writes to,
// returning the root path.
func EnsureRoot() (string, error) {
	root, err := Root()
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Join(root, "logs"), 0o755); err != nil {
		return "", fmt.Errorf("creating bench home: %w", err)
	}
	return root, nil
}

// TokenFile is the API auth token shared by daemon, CLI, and GUI.
func TokenFile() (string, error) { return under("token") }

// DBFile is the SQLite site/service/settings registry.
func DBFile() (string, error) { return under("bench.db") }

// AddrFile holds the address benchd is currently listening on; written on
// start, removed on clean shutdown. Clients use it for discovery.
func AddrFile() (string, error) { return under("benchd.addr") }

// Downloads is the user's downloads folder, where Bench saves files the
// user asks for (mail attachments).
// Shortcut: ~/Downloads on every OS; read XDG_DOWNLOAD_DIR or the Windows
// Known Folder if a user moved theirs.
func Downloads() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, "Downloads"), nil
}

// LogFile is benchd's log destination.
func LogFile() (string, error) { return under("logs", "benchd.log") }

func under(parts ...string) (string, error) {
	root, err := Root()
	if err != nil {
		return "", err
	}
	return filepath.Join(append([]string{root}, parts...)...), nil
}

// Package desktop opens things in the user's own apps: database clients,
// editors and terminals.
//
// These launches are handed off, not supervised: the app belongs to the
// user, must outlive benchd, and has no health to check. That is the one
// sanctioned exception to "every process goes through the supervisor".
package desktop

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/d3v0psdan/bench/daemon/internal/api"
)

// ErrNoApp means none of the apps that could open the thing is installed.
var ErrNoApp = errors.New("no app found")

// ---- databases --------------------------------------------------------------

// dbKinds are the service kinds a database client can open.
var dbKinds = map[string]bool{"mysql": true, "mariadb": true, "postgresql": true, "valkey": true}

// IsDatabase reports whether a service kind opens in a database client.
func IsDatabase(kind string) bool { return dbKinds[kind] }

// DatabaseURL is the connection URL for a database instance (TablePlus
// understands these schemes); "" for other kinds.
func DatabaseURL(s api.Service) string {
	hostPort := "127.0.0.1:" + strconv.Itoa(s.Port)
	switch s.Service {
	case "postgresql":
		return "postgresql://root@" + hostPort + "/postgres"
	case "valkey":
		return "redis://" + hostPort
	case "mysql", "mariadb":
		return "mysql://root@" + hostPort
	}
	return ""
}

// OpenDatabase opens the instance in TablePlus (via its URL scheme) or
// DBeaver (via -con), whichever is installed first, and names the app.
func OpenDatabase(s api.Service) (string, error) {
	if !IsDatabase(s.Service) {
		return "", fmt.Errorf("%s is not a database", s.Name)
	}
	if opener, ok := findTablePlus(); ok {
		return "TablePlus", start(append(opener, DatabaseURL(s)), "")
	}
	if s.Service == "valkey" {
		return "", fmt.Errorf("%w for Valkey: install TablePlus (DBeaver Community has no Redis driver)", ErrNoApp)
	}
	if exe, ok := findDBeaver(); ok {
		spec := fmt.Sprintf("driver=%s|host=127.0.0.1|port=%d|user=root|name=%s|connect=true", s.Service, s.Port, s.Name)
		if s.Service == "postgresql" {
			spec += "|database=postgres"
		}
		return "DBeaver", start(append(exe, "-con", spec), "")
	}
	return "", fmt.Errorf("%w: install TablePlus or DBeaver", ErrNoApp)
}

// TerminalClient is the command line for the client bundled with the
// instance's build (mysql, mariadb, psql, valkey-cli); nil for other kinds.
func TerminalClient(s api.Service, goos string) []string {
	exe := ""
	if goos == "windows" {
		exe = ".exe"
	}
	port := strconv.Itoa(s.Port)
	switch s.Service {
	case "mysql", "mariadb":
		return []string{filepath.Join(s.BinDir, "bin", s.Service+exe), "--protocol=TCP", "-h127.0.0.1", "-P" + port, "-uroot"}
	case "postgresql":
		return []string{filepath.Join(s.BinDir, "bin", "psql"+exe), "-h", "127.0.0.1", "-p", port, "-U", "root", "-d", "postgres"}
	case "valkey":
		bin := filepath.Join(s.BinDir, "bin", "valkey-cli"+exe)
		if !fileExists(bin) { // the Windows build keeps it at the top level
			bin = filepath.Join(s.BinDir, "valkey-cli"+exe)
		}
		return []string{bin, "-p", port}
	}
	return nil
}

// Quote joins a command line for display, quoting arguments with spaces.
func Quote(argv []string) string {
	out := make([]string, len(argv))
	for i, a := range argv {
		if strings.ContainsAny(a, " \t") {
			a = `"` + a + `"`
		}
		out[i] = a
	}
	return strings.Join(out, " ")
}

// ---- editors ----------------------------------------------------------------

// editors are the ones Bench knows how to open a folder in, in menu order.
var editors = []api.Editor{
	{ID: "vscode", Name: "VS Code"},
	{ID: "phpstorm", Name: "PhpStorm"},
	{ID: "cursor", Name: "Cursor"},
	{ID: "zed", Name: "Zed"},
}

// Editors lists the known editors with whether each is installed.
func Editors() []api.Editor {
	out := make([]api.Editor, len(editors))
	for i, e := range editors {
		_, e.Installed = findEditor(e.ID)
		out[i] = e
	}
	return out
}

// KnownEditor reports whether id names an editor Bench knows.
func KnownEditor(id string) bool {
	for _, e := range editors {
		if e.ID == id {
			return true
		}
	}
	return false
}

// OpenInEditor opens dir in the editor with the given id and names it.
func OpenInEditor(id, dir string) (string, error) {
	name := ""
	for _, e := range editors {
		if e.ID == id {
			name = e.Name
		}
	}
	if name == "" {
		return "", fmt.Errorf("unknown editor %q", id)
	}
	launcher, ok := findEditor(id)
	if !ok {
		return "", fmt.Errorf("%w: %s isn't installed", ErrNoApp, name)
	}
	return name, start(append(launcher, dir), "")
}

// ---- terminals --------------------------------------------------------------

// OpenTerminal opens a terminal window in dir and names the terminal.
func OpenTerminal(dir string) (string, error) {
	name, argv, ok := findTerminal(dir)
	if !ok {
		return "", fmt.Errorf("%w: no terminal app found", ErrNoApp)
	}
	return name, startTerminal(argv, dir)
}

// ---- helpers ----------------------------------------------------------------

func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

// onPath returns name's full path when it is on PATH.
func onPath(name string) (string, bool) {
	p, err := exec.LookPath(name)
	return p, err == nil
}

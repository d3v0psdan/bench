//go:build windows

package services

import (
	"os"
	"path/filepath"
)

const exeSuffix = ".exe"

// mysqlPlatformArgs: --console sends the error log to stderr (the
// supervisor's log capture) instead of <host>.err inside the data dir.
func mysqlPlatformArgs(in instance, flavor string) []string {
	args := []string{"--console"}
	if flavor == "mysql" {
		args = append(args, "--mysqlx=OFF") // X Protocol's 33060 would collide across instances
	}
	return args
}

// vcRuntimeMissing reports whether the MSVC runtime the MySQL/Postgres
// Windows builds link against is absent; without it they die at load
// with STATUS_DLL_NOT_FOUND and no output at all.
func vcRuntimeMissing() bool {
	sys := os.Getenv("SystemRoot")
	if sys == "" {
		return false // can't tell; let the real start report it
	}
	_, err := os.Stat(filepath.Join(sys, "System32", "vcruntime140.dll"))
	return err != nil
}

// consoleArgs sends mysqld's init output to stderr, where Exec captures
// it (on Windows it would otherwise go to <host>.err in the data dir).
func consoleArgs() []string { return []string{"--console"} }

// runningAsRoot: Windows elevation is caught by the servers themselves
// (see the "administrative permissions" hint).
func runningAsRoot() bool { return false }

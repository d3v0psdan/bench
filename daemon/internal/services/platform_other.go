//go:build !windows

package services

import (
	"os"
	"path/filepath"
)

const exeSuffix = ""

// mysqlPlatformArgs gives each instance its own socket: the default
// /tmp/mysql.sock would collide between instances.
func mysqlPlatformArgs(in instance, flavor string) []string {
	args := []string{"--socket=" + filepath.Join(in.runDir(), "mysqld.sock")}
	if flavor == "mysql" {
		args = append(args, "--mysqlx=OFF") // X Protocol's 33060 would collide across instances
	}
	return args
}

func vcRuntimeMissing() bool { return false }

func consoleArgs() []string { return nil }

func runningAsRoot() bool { return os.Geteuid() == 0 }

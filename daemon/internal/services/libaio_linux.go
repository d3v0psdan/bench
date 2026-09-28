package services

import (
	"os"
	"path/filepath"
)

// libDirs are where distributions keep libaio.
var libDirs = []string{
	"/usr/lib/x86_64-linux-gnu", "/usr/lib/aarch64-linux-gnu",
	"/lib/x86_64-linux-gnu", "/lib/aarch64-linux-gnu", "/usr/lib64", "/usr/lib",
}

// missingMySQLLibs names the system libraries Oracle's generic Linux
// build needs that aren't installed (libaio under either name, libnuma),
// so a create can stop before a ~500 MB download that can't run.
func missingMySQLLibs() []string {
	has := func(names ...string) bool {
		for _, dir := range libDirs {
			for _, n := range names {
				if fileExists(filepath.Join(dir, n)) {
					return true
				}
			}
		}
		return false
	}
	var missing []string
	if !has("libaio.so.1", "libaio.so.1t64") {
		missing = append(missing, "libaio")
	}
	if !has("libnuma.so.1") {
		missing = append(missing, "libnuma")
	}
	return missing
}

// linkLibaio works around Ubuntu 24.04's t64 rename: mysqld wants
// libaio.so.1, the distro ships libaio.so.1t64. The link goes into the
// build's lib/private (mysqld's RUNPATH), never into system dirs.
func linkLibaio(binDir string) error {
	for _, dir := range libDirs {
		if fileExists(filepath.Join(dir, "libaio.so.1")) {
			return nil // the system provides it under the expected name
		}
	}
	dst := filepath.Join(binDir, "lib", "private", "libaio.so.1")
	if fileExists(dst) {
		return nil
	}
	for _, dir := range libDirs {
		t64 := filepath.Join(dir, "libaio.so.1t64")
		if !fileExists(t64) {
			continue
		}
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return err
		}
		return os.Symlink(t64, dst)
	}
	return nil // not installed at all: mysqld's load error plus the hint explain it
}

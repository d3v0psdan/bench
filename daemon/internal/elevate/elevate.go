// Package elevate locates bench-helper and runs it one-shot with OS
// elevation (UAC / osascript admin prompt / pkexec). Everything else in
// Bench stays unprivileged.
package elevate

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/d3v0psdan/bench/daemon/internal/version"
)

// RunHelper executes `bench-helper args...` elevated and waits for it.
func RunHelper(ctx context.Context, args ...string) error {
	exe, err := findHelper()
	if err != nil {
		return err
	}
	return elevated(ctx, exe, args)
}

// findHelper looks next to the current executable (how releases ship),
// then, in dev builds only, on PATH.
func findHelper() (string, error) {
	name := "bench-helper"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	if self, err := os.Executable(); err == nil {
		candidate := filepath.Join(filepath.Dir(self), name)
		if _, err := os.Stat(candidate); err == nil {
			return candidate, nil
		}
	}
	// A release never looks on PATH: an elevated run of whatever a user
	// directory on PATH holds would hand admin rights to it.
	if !strings.HasSuffix(version.Version, "-dev") {
		return "", fmt.Errorf("bench-helper not found next to benchd; reinstall Bench")
	}
	path, err := exec.LookPath(name)
	if err != nil {
		return "", fmt.Errorf("bench-helper not found next to benchd or on PATH: %w", err)
	}
	return path, nil
}

//go:build linux

package elevate

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

// elevated runs exe through pkexec (polkit auth dialog).
func elevated(ctx context.Context, exe string, args []string) error {
	out, err := exec.CommandContext(ctx, "pkexec", append([]string{exe}, args...)...).CombinedOutput()
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) && exitErr.ExitCode() == 126 {
			return fmt.Errorf("elevation declined")
		}
		return fmt.Errorf("running bench-helper elevated (is polkit/pkexec available?): %w: %s",
			err, strings.TrimSpace(string(out)))
	}
	return nil
}

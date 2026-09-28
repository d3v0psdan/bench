//go:build darwin

package elevate

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
)

// shQuote single-quotes for /bin/sh.
func shQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// elevated runs exe through osascript's administrator prompt.
func elevated(ctx context.Context, exe string, args []string) error {
	parts := make([]string, 0, len(args)+1)
	parts = append(parts, shQuote(exe))
	for _, a := range args {
		parts = append(parts, shQuote(a))
	}
	shell := strings.Join(parts, " ")
	// Escape for an AppleScript string literal.
	esc := strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(shell)
	script := fmt.Sprintf(`do shell script "%s" with administrator privileges with prompt "Bench needs to configure HTTPS trust and .test DNS."`, esc)
	out, err := exec.CommandContext(ctx, "osascript", "-e", script).CombinedOutput()
	if err != nil {
		msg := strings.TrimSpace(string(out))
		if strings.Contains(msg, "User canceled") {
			return fmt.Errorf("elevation declined")
		}
		return fmt.Errorf("running bench-helper elevated: %w: %s", err, msg)
	}
	return nil
}

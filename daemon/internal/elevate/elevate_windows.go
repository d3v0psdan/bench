//go:build windows

package elevate

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"syscall"
)

// psQuote single-quotes s for PowerShell, doubling any quote in it.
func psQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
}

// elevated runs exe via Start-Process -Verb RunAs (UAC) and propagates
// its exit code. Declining the UAC prompt surfaces as an error.
//
// Args are joined into ONE command-line string with syscall.EscapeArg
// quoting. Passing a PowerShell array instead lets Start-Process re-join
// elements on spaces without quoting, so a helper argument like a
// bench-home path with a space (C:\Users\John Doe\...) would reach
// bench-helper split into pieces, breaking `bench setup`.
func elevated(ctx context.Context, exe string, args []string) error {
	escaped := make([]string, len(args))
	for i, a := range args {
		escaped[i] = syscall.EscapeArg(a)
	}
	argList := ""
	if len(escaped) > 0 {
		argList = " -ArgumentList " + psQuote(strings.Join(escaped, " "))
	}
	script := fmt.Sprintf(
		"$p = Start-Process -FilePath %s%s -Verb RunAs -Wait -PassThru -WindowStyle Hidden; exit $p.ExitCode",
		psQuote(exe), argList)
	out, err := exec.CommandContext(ctx, "powershell", "-NoProfile", "-NonInteractive", "-Command", script).CombinedOutput()
	if err != nil {
		msg := strings.TrimSpace(string(out))
		if strings.Contains(msg, "canceled by the user") {
			return fmt.Errorf("elevation declined")
		}
		return fmt.Errorf("running bench-helper elevated: %w: %s", err, msg)
	}
	return nil
}

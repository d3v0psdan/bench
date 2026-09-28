//go:build windows

package desktop

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSafeForBatchRefusesCmdMetacharacters(t *testing.T) {
	if err := safeForBatch([]string{`C:\tools\code.cmd`, `C:\dl\x&calc\app`}); err == nil {
		t.Error("a folder with & reached a batch launcher")
	}
	if err := safeForBatch([]string{`C:\tools\code.cmd`, `C:\Users\me\Bench\shop`}); err != nil {
		t.Errorf("a plain folder was refused: %v", err)
	}
	if err := safeForBatch([]string{`C:\Program Files\Microsoft VS Code\Code.exe`, `C:\dl\x&y`}); err != nil {
		t.Errorf("a real executable gets arguments quoted by Go: %v", err)
	}
}

// shellReportTimeout bounds the wait for the terminal's shell to report
// back; a cold PowerShell start can take a few seconds.
const shellReportTimeout = 30 * time.Second

// A terminal opened by startTerminal must read its own console, not NUL,
// or its shell exits at once; and it must start in the site's folder.
func TestStartTerminalGivesTheShellItsConsoleAndFolder(t *testing.T) {
	shell, err := exec.LookPath("powershell.exe")
	if err != nil {
		t.Skip("no powershell.exe")
	}
	dir := filepath.Join(t.TempDir(), "shop site")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(dir, "out.txt")
	t.Setenv("BENCH_TEST_OUT", out) // startTerminal passes benchd's environment on
	// Written aside and renamed, so the poll below never reads half a line.
	script := `Set-Content -LiteralPath "$env:BENCH_TEST_OUT.tmp" -Value "$([Console]::IsInputRedirected) $((Get-Location).Path)"; ` +
		`Move-Item -LiteralPath "$env:BENCH_TEST_OUT.tmp" -Destination $env:BENCH_TEST_OUT`
	if err := startTerminal([]string{shell, "-WindowStyle", "Hidden", "-NoProfile", "-Command", script}, dir); err != nil {
		t.Fatal(err)
	}
	var got []byte
	for deadline := time.Now().Add(shellReportTimeout); time.Now().Before(deadline); time.Sleep(100 * time.Millisecond) {
		if got, err = os.ReadFile(out); err == nil {
			break
		}
	}
	if err != nil {
		t.Fatalf("the shell didn't report back within %s: %v", shellReportTimeout, err)
	}
	// The temp root may come back as an 8.3 short name, so match the tail.
	if seen := strings.TrimSpace(string(got)); !strings.HasPrefix(seen, "False ") || !strings.HasSuffix(seen, `\shop site`) {
		t.Errorf("shell saw %q, want input from its console (False) in the shop site folder", seen)
	}
}

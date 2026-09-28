//go:build windows

package desktop

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
)

const (
	detachedProcess  = 0x00000008
	createNewConsole = 0x00000010
)

// installedExe returns the first existing <root>\<rel> under the usual
// per-user and machine-wide install roots.
func installedExe(rel string) (string, bool) {
	for _, root := range []string{os.Getenv("LOCALAPPDATA"), os.Getenv("ProgramFiles"), os.Getenv("LOCALAPPDATA") + `\Programs`} {
		if root == "" {
			continue
		}
		if p := filepath.Join(root, rel); fileExists(p) {
			return p, true
		}
	}
	return "", false
}

// findTablePlus: TablePlus registers the mysql:// and postgresql://
// schemes, so the shell's URL handler opens the connection in it.
func findTablePlus() ([]string, bool) {
	if _, ok := installedExe(`TablePlus\TablePlus.exe`); !ok {
		return nil, false
	}
	return []string{"rundll32", "url.dll,FileProtocolHandler"}, true
}

func findDBeaver() ([]string, bool) {
	p, ok := installedExe(`DBeaver\dbeaver.exe`)
	return []string{p}, ok
}

// findEditor prefers the installed executable over a PATH shim: the
// shims are .cmd scripts that would briefly flash a console.
func findEditor(id string) ([]string, bool) {
	installs := map[string][]string{
		"vscode":   {`Microsoft VS Code\Code.exe`},
		"cursor":   {`cursor\Cursor.exe`},
		"zed":      {`Zed\zed.exe`},
		"phpstorm": {`JetBrains\Toolbox\scripts\phpstorm.cmd`},
	}[id]
	for _, rel := range installs {
		if p, ok := installedExe(rel); ok {
			return []string{p}, true
		}
	}
	if id == "phpstorm" { // a standalone install is versioned: PhpStorm 2026.2
		matches, _ := filepath.Glob(filepath.Join(os.Getenv("ProgramFiles"), "JetBrains", "PhpStorm*", "bin", "phpstorm64.exe"))
		if len(matches) > 0 {
			return []string{matches[len(matches)-1]}, true
		}
	}
	shim := map[string]string{"vscode": "code", "cursor": "cursor", "zed": "zed", "phpstorm": "phpstorm"}[id]
	if p, ok := onPath(shim); ok {
		return []string{p}, true
	}
	return nil, false
}

// findTerminal prefers Windows Terminal, then a PowerShell console.
func findTerminal(dir string) (string, []string, bool) {
	if p, ok := onPath("wt.exe"); ok {
		// "-d ." with the working directory set: wt splits its arguments
		// on ";" even inside quotes, so the folder name must not reach it.
		return "Windows Terminal", []string{p, "-d", "."}, true
	}
	if p, ok := onPath("powershell.exe"); ok {
		return "PowerShell", []string{p, "-NoExit"}, true
	}
	return "", nil, false
}

// start launches argv detached from benchd with no console of its own.
func start(argv []string, dir string) error {
	return launch(argv, dir, detachedProcess)
}

// startTerminal launches argv in a console window of its own. It skips
// os/exec on purpose: see terminalAttr.
func startTerminal(argv []string, dir string) error {
	_, handle, err := syscall.StartProcess(argv[0], argv, terminalAttr(dir))
	if err != nil {
		return fmt.Errorf("starting %s: %w", filepath.Base(argv[0]), err)
	}
	return syscall.CloseHandle(syscall.Handle(handle))
}

// terminalAttr starts a process in a new console with NULL standard
// handles, so it reads and writes that console. os/exec would hand it NUL
// instead, and a shell reading NUL sees end of input and exits at once:
// the window flashes and closes.
func terminalAttr(dir string) *syscall.ProcAttr {
	return &syscall.ProcAttr{
		Dir:   dir,
		Env:   os.Environ(),
		Files: []uintptr{0, 0, 0},
		Sys:   &syscall.SysProcAttr{CreationFlags: syscall.CREATE_NEW_PROCESS_GROUP | createNewConsole},
	}
}

func launch(argv []string, dir string, flags uint32) error {
	if err := safeForBatch(argv); err != nil {
		return err
	}
	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.Dir = dir
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: syscall.CREATE_NEW_PROCESS_GROUP | flags}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("starting %s: %w", filepath.Base(argv[0]), err)
	}
	return cmd.Process.Release()
}

// batchMeta are the characters cmd.exe acts on in a batch file's
// arguments; Go quotes arguments for programs, not for cmd.exe.
const batchMeta = "&|<>^%!()\""

// safeForBatch refuses to hand an argument with cmd.exe metacharacters to
// a .cmd or .bat launcher (an editor's PATH shim): a folder named
// "x&calc" would otherwise run a command. Real executables are fine.
func safeForBatch(argv []string) error {
	ext := strings.ToLower(filepath.Ext(argv[0]))
	if ext != ".cmd" && ext != ".bat" {
		return nil
	}
	for _, a := range argv[1:] {
		if strings.ContainsAny(a, batchMeta) {
			return fmt.Errorf("can't pass %q to %s safely: rename the folder, or install the editor so Bench finds its program", a, filepath.Base(argv[0]))
		}
	}
	return nil
}

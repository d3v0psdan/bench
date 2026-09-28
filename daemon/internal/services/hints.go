package services

import (
	"runtime"
	"strings"
)

// knownFailures maps output fragments of well-known environment problems
// to the fix, so a failed init/start says what to do instead of just what
// broke.
var knownFailures = []struct{ fragment, fix string }{
	{"libaio.so.1", "MySQL needs libaio: `sudo apt install libaio1t64` (Ubuntu 24.04+; Bench links it for MySQL automatically) or `libaio1`, `sudo dnf install libaio`"},
	{"administrative permissions", "PostgreSQL refuses to run elevated: start Bench from a normal (non-administrator) terminal"},
	{"cannot be run as root", "this server refuses to run as root: start Bench as your normal user"},
	{"libnuma.so.1", "MySQL needs libnuma: `sudo apt install libnuma1` or `sudo dnf install numactl-libs`"},
	{"libcrypt.so.1", "MariaDB needs libcrypt.so.1: `sudo dnf install libxcrypt-compat`"},
	{"libssl.so.3", "this build needs OpenSSL 3 (Ubuntu 22.04+, Debian 12+, Fedora 36+)"},
	{"libreadline.so.8", "psql needs readline 8: `sudo apt install libreadline8`"},
	{"libncurses.so.6", "the mysql client needs ncurses 6: `sudo apt install libncurses6` or `sudo dnf install ncurses-libs`"},
	{"3221225781", vcRuntimeFix}, // STATUS_DLL_NOT_FOUND as Go prints it
	{"0xc0000135", vcRuntimeFix},
	{"VCRUNTIME140", vcRuntimeFix},
}

const vcRuntimeFix = "install the Microsoft Visual C++ Redistributable (x64): https://aka.ms/vs/17/release/vc_redist.x64.exe"

// hint returns "\nhint: <fix>" for the first known problem in out.
func hint(out string) string {
	for _, k := range knownFailures {
		if strings.Contains(out, k.fragment) {
			return "\nhint: " + k.fix
		}
	}
	return ""
}

// preflight catches missing system prerequisites before a download or
// init fails in a confusing way.
func preflight(d driver) error {
	if runtime.GOOS == "windows" && d.needsVCRT && vcRuntimeMissing() {
		return inputErrorf("%s needs the Microsoft Visual C++ Redistributable; %s", d.label, vcRuntimeFix)
	}
	if d.binary == "mysql" {
		if missing := missingMySQLLibs(); len(missing) > 0 {
			return inputErrorf("MySQL needs system libraries that aren't installed (%s): run `sudo apt install libaio1t64 libnuma1` "+
				"(Ubuntu 24.04+; older: libaio1 libnuma1) or `sudo dnf install libaio numactl-libs`, then try again", strings.Join(missing, ", "))
		}
	}
	if d.refusesRoot && runningAsRoot() {
		return inputErrorf("%s won't run as root; start Bench as your normal user (not with sudo)", d.label)
	}
	return nil
}

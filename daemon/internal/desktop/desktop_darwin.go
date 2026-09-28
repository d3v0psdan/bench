//go:build darwin

package desktop

func findTablePlus() ([]string, bool) {
	if !fileExists("/Applications/TablePlus.app") {
		return nil, false
	}
	return []string{"open", "-a", "TablePlus"}, true
}

// findDBeaver runs DBeaver's own launcher: unlike `open -a`, it hands
// -con to an already-running DBeaver instead of dropping it.
func findDBeaver() ([]string, bool) {
	const launcher = "/Applications/DBeaver.app/Contents/MacOS/dbeaver"
	if !fileExists(launcher) {
		return nil, false
	}
	return []string{launcher}, true
}

func findEditor(id string) ([]string, bool) {
	app := map[string]string{"vscode": "Visual Studio Code", "cursor": "Cursor", "zed": "Zed", "phpstorm": "PhpStorm"}[id]
	if app == "" || !fileExists("/Applications/"+app+".app") {
		return nil, false
	}
	return []string{"open", "-a", app}, true
}

func findTerminal(dir string) (string, []string, bool) {
	return "Terminal", []string{"open", "-a", "Terminal", dir}, true
}

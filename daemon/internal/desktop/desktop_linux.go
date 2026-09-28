//go:build linux

package desktop

// On Linux the clients and editors install a launcher on PATH (deb, rpm,
// snap and flatpak exports); TablePlus takes the connection URL as its
// argument.
//
// Shortcut: launches inherit benchd's environment. A benchd started by
// the GUI has DISPLAY/WAYLAND_DISPLAY; one started by a systemd user unit
// only has them if the session imported them. Pass them through from the
// GUI if that bites.

func findTablePlus() ([]string, bool) {
	p, ok := onPath("tableplus")
	return []string{p}, ok
}

func findDBeaver() ([]string, bool) {
	for _, name := range []string{"dbeaver", "dbeaver-ce"} {
		if p, ok := onPath(name); ok {
			return []string{p}, true
		}
	}
	return nil, false
}

func findEditor(id string) ([]string, bool) {
	names := map[string][]string{
		"vscode":   {"code"},
		"cursor":   {"cursor"},
		"zed":      {"zed", "zeditor"},
		"phpstorm": {"phpstorm", "phpstorm.sh"},
	}[id]
	for _, name := range names {
		if p, ok := onPath(name); ok {
			return []string{p}, true
		}
	}
	return nil, false
}

// findTerminal tries the common terminals, each with its own flag for the
// starting directory; the generic ones fall back to the process's cwd.
func findTerminal(dir string) (string, []string, bool) {
	for _, t := range []struct {
		name string
		args []string
	}{
		{"gnome-terminal", []string{"--working-directory=" + dir}},
		{"konsole", []string{"--workdir", dir}},
		{"xfce4-terminal", []string{"--working-directory=" + dir}},
		{"kitty", []string{"--directory", dir}},
		{"alacritty", []string{"--working-directory", dir}},
		{"wezterm", []string{"start", "--cwd", dir}},
		{"foot", []string{"--working-directory=" + dir}},
		{"x-terminal-emulator", nil},
		{"xterm", nil},
	} {
		if p, ok := onPath(t.name); ok {
			return t.name, append([]string{p}, t.args...), true
		}
	}
	return "", nil, false
}

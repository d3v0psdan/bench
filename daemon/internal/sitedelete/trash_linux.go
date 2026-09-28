//go:build linux

package sitedelete

import (
	"context"
	"errors"
	"fmt"
	"os/exec"

	"github.com/d3v0psdan/bench/daemon/internal/supervisor"
)

// trash moves path to the desktop's Trash with gio (GLib, present on GNOME,
// KDE and most desktops), so it can be restored from the file manager.
func trash(ctx context.Context, path string) error {
	if _, err := exec.LookPath("gio"); err != nil {
		return errors.New("gio isn't installed, so there's no Trash to move it to; install glib2 or delete the folder yourself")
	}
	out, err := supervisor.Exec(ctx, supervisor.Spec{Command: "gio", Args: []string{"trash", "--", path}})
	if err != nil {
		return fmt.Errorf("gio trash refused it: %w: %s", err, lastLine(out))
	}
	return nil
}

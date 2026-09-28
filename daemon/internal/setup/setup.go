// Package setup undoes Bench's machine setup (the local HTTPS authority in
// the trust store and the .test DNS rule) through the elevated helper. It
// works without a running daemon, so an uninstaller can call it.
package setup

import (
	"context"
	"errors"
	"os"
	"path/filepath"

	"github.com/d3v0psdan/bench/daemon/internal/elevate"
)

// RootCAPath is Caddy's local CA root under the Bench home (the file the
// helper trusts during setup).
func RootCAPath(root string) string {
	return filepath.Join(root, "caddy", "storage", "pki", "authorities", "local", "root.crt")
}

// ErrNothingToRemove means neither part of the setup is in place.
var ErrNothingToRemove = errors.New("nothing to remove: the HTTPS and DNS setup isn't in place")

// Teardown removes the parts asked for with one elevation prompt and says
// what it removed. Both parts are always attempted (removing what isn't
// there is a no-op on every OS): the helper removes the CA it recorded
// trusting, plus the one in the Bench home when that still exists.
func Teardown(ctx context.Context, root string, dns, trust bool) ([]string, error) {
	args, msgs := teardownArgs(RootCAPath(root), dns, trust)
	if len(args) == 1 {
		return nil, ErrNothingToRemove
	}
	if err := elevate.RunHelper(ctx, args...); err != nil {
		return nil, err
	}
	return msgs, nil
}

func teardownArgs(ca string, dns, trust bool) ([]string, []string) {
	args := []string{"teardown"}
	var msgs []string
	if dns {
		args = append(args, "-dns")
		msgs = append(msgs, "*.test no longer routes to Bench")
	}
	if trust {
		args = append(args, "-installed-ca")
		if _, err := os.Stat(ca); err == nil {
			args = append(args, "-ca", ca)
			msgs = append(msgs, "local certificate authority removed from the trust store")
		} else {
			// The helper removes the CA it recorded trusting, if any; with
			// no certificate here, whether one was trusted isn't known.
			msgs = append(msgs, "the local certificate authority Bench recorded, if any, removed from the trust store")
		}
	}
	return args, msgs
}

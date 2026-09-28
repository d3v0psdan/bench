package setup

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestTeardownArgsRemoveTheRecordedCAEvenWithoutItsFile(t *testing.T) {
	root := t.TempDir()
	ca := RootCAPath(root)
	if args, _ := teardownArgs(ca, false, false); len(args) != 1 {
		t.Fatalf("nothing asked: %v, want nothing to remove", args)
	}
	args, msgs := teardownArgs(ca, false, true)
	if !slices.Equal(args, []string{"teardown", "-installed-ca"}) {
		t.Fatalf("no certificate file: %v, want the recorded CA removed", args)
	}
	if len(msgs) != 1 || !strings.Contains(msgs[0], "if any") {
		t.Fatalf("no certificate file: %v, must not claim a CA was removed", msgs)
	}
	if err := os.MkdirAll(filepath.Dir(ca), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(ca, []byte("cert"), 0o644); err != nil {
		t.Fatal(err)
	}
	args, msgs = teardownArgs(ca, true, true)
	if !slices.Equal(args, []string{"teardown", "-dns", "-installed-ca", "-ca", ca}) || len(msgs) != 2 {
		t.Fatalf("args %v, msgs %v", args, msgs)
	}
}

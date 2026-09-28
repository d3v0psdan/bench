package sites

import (
	"strings"
	"testing"

	"github.com/d3v0psdan/bench/daemon/internal/registry"
)

func TestNormalizePHPStoresChannels(t *testing.T) {
	m, _, _ := newTestManager(t)
	if err := m.Reg.SetSetting("php.default", "8.4.12"); err != nil {
		t.Fatal(err)
	}
	for _, s := range []registry.Site{
		{Name: "blog", Path: t.TempDir(), Kind: "linked", PHPVersion: "8.3.20"},
		{Name: "shop", Path: t.TempDir(), Kind: "linked", PHPVersion: "8.4"},
		{Name: "old", Path: t.TempDir(), Kind: "linked", PHPVersion: "5.6.40"},
		{Name: "plain", Path: t.TempDir(), Kind: "linked"},
		{Name: "parked-app", Path: t.TempDir(), Kind: "parked", PHPVersion: "8.5.1"},
	} {
		if err := m.Reg.UpsertSite(s); err != nil {
			t.Fatal(err)
		}
	}
	// A stand-in catalog: 8.x versions map to their channel.
	channel := func(v string) (string, bool) {
		if !strings.HasPrefix(v, "8.") {
			return "", false
		}
		return v[:3], true
	}
	if err := m.NormalizePHP(channel); err != nil {
		t.Fatalf("NormalizePHP: %v", err)
	}

	got, err := m.Reg.Setting("php.default")
	if err != nil {
		t.Fatal(err)
	}
	if got != "8.4" {
		t.Errorf("php.default = %q, want 8.4", got)
	}
	rows, err := m.Reg.Sites()
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"blog": "8.3", "shop": "8.4", "old": "5.6.40", "plain": "", "parked-app": "8.5"}
	for _, s := range rows {
		if s.PHPVersion != want[s.Name] {
			t.Errorf("%s pin = %q, want %q", s.Name, s.PHPVersion, want[s.Name])
		}
	}
}

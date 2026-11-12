package sites

import (
	"context"
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

// savedLongSite stores a linked site whose name is one past the limit, as a
// registry from before the limit can hold, and returns the name.
func savedLongSite(t *testing.T, m *Manager) string {
	t.Helper()
	name := strings.Repeat("a", maxNameLen+1)
	if err := m.Reg.UpsertSite(registry.Site{Name: name, Path: t.TempDir(), Kind: "linked"}); err != nil {
		t.Fatal(err)
	}
	return name
}

func TestASavedSiteNamedPastTheLimitCanBePinnedAndUnpinned(t *testing.T) {
	m, _, _ := newTestManager(t)
	name := savedLongSite(t, m)
	for _, version := range []string{"8.3", ""} {
		list, err := m.SetSitePHP(context.Background(), name, version)
		if err != nil {
			t.Fatalf("SetSitePHP(%q): %v", version, err)
		}
		if len(list) != 1 || list[0].PHPPinned != version {
			t.Fatalf("SetSitePHP(%q): list = %+v", version, list)
		}
	}
}

func TestASavedSiteNamedPastTheLimitCanBeStarred(t *testing.T) {
	m, _, _ := newTestManager(t)
	name := savedLongSite(t, m)
	list, err := m.SetFavorite(name, true)
	if err != nil {
		t.Fatalf("SetFavorite: %v", err)
	}
	if len(list) != 1 || !list[0].Favorite {
		t.Fatalf("list = %+v, want the site starred", list)
	}
}

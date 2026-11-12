package sites

import (
	"context"
	"errors"
	"testing"
)

// installedPHP stands in for the PHP builds on disk.
type installedPHP struct{ channels []string }

func (i *installedPHP) list() []string { return i.channels }

func mustDefaultPHP(t *testing.T, m *Manager) string {
	t.Helper()
	got, err := m.DefaultPHP()
	if err != nil {
		t.Fatalf("DefaultPHP: %v", err)
	}
	return got
}

func mustSavedDefault(t *testing.T, m *Manager) string {
	t.Helper()
	got, err := m.Reg.Setting("php.default")
	if err != nil {
		t.Fatal(err)
	}
	return got
}

// The registry seeds php.default = 8.4 (migration 0002), as on a fresh install.
func TestWithNoPHPInstalledThereIsNoDefaultAndSitesSayHowToInstallOne(t *testing.T) {
	m, _, pools := newTestManager(t)
	m.InstalledPHP = (&installedPHP{}).list

	if got := mustDefaultPHP(t, m); got != "" {
		t.Fatalf("default = %q, want none", got)
	}
	dir := projectDir(t, t.TempDir(), "app", true)
	list, err := m.Link(context.Background(), dir, "")
	if err != nil {
		t.Fatal(err)
	}
	want := "PHP isn't installed; install a version on the Runtimes page (or with bench php:install <version>)"
	if list[0].Error != want || len(pools.ensured) != 0 {
		t.Fatalf("site = %+v, pools ensured %v; want error %q and no pool", list[0], pools.ensured, want)
	}
}

// An install ends in Apply, as benchd's OnInstalled hook does.
func TestFirstPHPInstalledBecomesTheDefaultAndServesWaitingSites(t *testing.T) {
	m, router, _ := newTestManager(t)
	disk := &installedPHP{}
	m.InstalledPHP = disk.list
	dir := projectDir(t, t.TempDir(), "app", true)
	if _, err := m.Link(context.Background(), dir, ""); err != nil {
		t.Fatal(err)
	}

	disk.channels = []string{"8.5"}
	list, err := m.Apply(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if list[0].Error != "" || list[0].PHP != "8.5" {
		t.Fatalf("after installing 8.5: site = %+v", list[0])
	}
	if up := routeByHost(t, router.last(t), "app.test").FastCGI; up != "127.0.0.1:905" {
		t.Fatalf("after installing 8.5: route upstream = %q", up)
	}
	if got := mustSavedDefault(t, m); got != "8.5" {
		t.Fatalf("after installing 8.5: saved default = %q, want 8.5", got)
	}
}

// 8.4 is the seeded default, so a first install of 8.5 followed by 8.4
// is the case where the seed could win back.
func TestInstallingASecondPHPKeepsTheFirstAsDefault(t *testing.T) {
	m, _, _ := newTestManager(t)
	disk := &installedPHP{channels: []string{"8.5"}}
	m.InstalledPHP = disk.list
	if _, err := m.Apply(context.Background()); err != nil {
		t.Fatal(err)
	}

	disk.channels = []string{"8.4", "8.5"}
	if _, err := m.Apply(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := mustDefaultPHP(t, m); got != "8.5" {
		t.Fatalf("installing 8.4 second: default = %q, want 8.5 kept", got)
	}
}

func TestMissingSavedDefaultReadsAsNewestInstalledAndApplySavesIt(t *testing.T) {
	m, _, _ := newTestManager(t)
	m.InstalledPHP = (&installedPHP{channels: []string{"8.5", "8.3"}}).list
	if err := m.Reg.SetSetting("php.default", "8.4"); err != nil {
		t.Fatal(err)
	}

	if got := mustDefaultPHP(t, m); got != "8.5" {
		t.Fatalf("default = %q, want the newest installed, 8.5", got)
	}
	if got := mustSavedDefault(t, m); got != "8.4" {
		t.Fatalf("DefaultPHP wrote %q; reading must not save", got)
	}
	if _, err := m.Apply(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := mustSavedDefault(t, m); got != "8.5" {
		t.Fatalf("after Apply: saved default = %q, want 8.5", got)
	}
}

func TestDefaultPHPKeepsAnInstalledSavedDefault(t *testing.T) {
	m, _, _ := newTestManager(t)
	m.InstalledPHP = (&installedPHP{channels: []string{"8.3", "8.5"}}).list
	if err := m.Reg.SetSetting("php.default", "8.3"); err != nil {
		t.Fatal(err)
	}

	if got := mustDefaultPHP(t, m); got != "8.3" {
		t.Fatalf("default = %q, want the saved 8.3", got)
	}
}

func TestDefaultPHPFailsWhenTheInstalledLookupIsMissing(t *testing.T) {
	m, _, _ := newTestManager(t)
	m.InstalledPHP = nil

	if _, err := m.DefaultPHP(); !errors.Is(err, errInstalledPHPNotWired) {
		t.Fatalf("DefaultPHP error = %v, want one naming InstalledPHP", err)
	}
}

func TestSetDefaultPHPRefusesAVersionThatIsNotInstalled(t *testing.T) {
	m, _, _ := newTestManager(t)
	m.InstalledPHP = (&installedPHP{channels: []string{"8.5"}}).list
	if err := m.Reg.SetSetting("php.default", "8.5"); err != nil {
		t.Fatal(err)
	}

	_, err := m.SetDefaultPHP(context.Background(), "8.3")
	var notInstalled PHPNotInstalledError
	if !errors.As(err, &notInstalled) || notInstalled.Channel != "8.3" {
		t.Fatalf("SetDefaultPHP(8.3) error = %v, want PHP 8.3 isn't installed", err)
	}
	if got := mustSavedDefault(t, m); got != "8.5" {
		t.Fatalf("saved default = %q after a refused change, want 8.5", got)
	}
}

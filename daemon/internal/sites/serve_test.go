package sites

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/d3v0psdan/bench/daemon/internal/api"
)

// TestLinkIsSavedWhenCaddyCantStart: with the web server down (another app
// on port 443), a link still succeeds, the site is listed with a "not
// served" error, and the change is published to clients.
func TestLinkIsSavedWhenCaddyCantStart(t *testing.T) {
	m, router, _ := newTestManager(t)
	router.failing = errors.New("exited during startup")
	var published []api.Site
	m.OnChange = func(list []api.Site) { published = list }

	list, err := m.Link(context.Background(), t.TempDir(), "shop")
	if err != nil {
		t.Fatalf("Link = %v, want the save to succeed", err)
	}
	if len(list) != 1 || list[0].Name != "shop" {
		t.Fatalf("list = %+v", list)
	}
	if !strings.Contains(list[0].Error, "not served") || !strings.Contains(list[0].Error, "starting caddy") {
		t.Fatalf("site error = %q, want the serve failure", list[0].Error)
	}
	if len(published) != 1 {
		t.Fatalf("clients weren't told: published %+v", published)
	}

	router.failing = nil
	list, err = m.Apply(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if list[0].Error != "" {
		t.Fatalf("error should clear once Caddy runs, got %q", list[0].Error)
	}
}

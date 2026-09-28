package caddy

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestBuildConfigBindsLoopbackOnly locks the security-critical shape: the
// site server must listen on loopback only (never ":443"/all interfaces),
// and must not create an HTTP→HTTPS redirect listener.
func TestBuildConfigBindsLoopbackOnly(t *testing.T) {
	c := &Caddy{}
	cfg := c.buildConfig([]Site{{Host: "app.test", Root: "/x", FastCGI: "127.0.0.1:9084"}})

	// Round-trip through JSON so we assert on the real wire document.
	b, err := json.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(b, &doc); err != nil {
		t.Fatal(err)
	}

	server := doc["apps"].(map[string]any)["http"].(map[string]any)["servers"].(map[string]any)["bench"].(map[string]any)
	listen := server["listen"].([]any)
	if len(listen) != 2 {
		t.Fatalf("want 2 loopback listeners, got %v", listen)
	}
	for _, l := range listen {
		s := l.(string)
		if !strings.HasPrefix(s, "127.0.0.1:") && !strings.HasPrefix(s, "[::1]:") {
			t.Fatalf("listener %q is not loopback (LAN exposure)", s)
		}
	}
	// The redirect vhost (which would bind :80 on all interfaces) is off.
	ah := server["automatic_https"].(map[string]any)
	if ah["disable_redirects"] != true {
		t.Fatalf("automatic_https redirects must be disabled, got %v", ah)
	}
	if _, hasHTTPPort := doc["apps"].(map[string]any)["http"].(map[string]any)["http_port"]; hasHTTPPort {
		t.Fatal("http_port should not be set (no port 80 usage)")
	}
}

func TestSiteRouteFastCGIvsProxy(t *testing.T) {
	fastcgi := siteRoute(Site{Host: "app.test", Root: "/srv/app/public", FastCGI: "127.0.0.1:9084"})
	if fastcgi["terminal"] != true {
		t.Fatal("route should be terminal")
	}
	// A FastCGI site expands to a subroute (vars/try_files/php/file_server).
	handlers := fastcgi["handle"].([]any)
	if h := handlers[0].(map[string]any)["handler"]; h != "subroute" {
		t.Fatalf("fastcgi site handler = %v, want subroute", h)
	}

	proxy := siteRoute(Site{Host: "api.test", ProxyTo: "127.0.0.1:3000"})
	ph := proxy["handle"].([]any)[0].(map[string]any)
	if ph["handler"] != "reverse_proxy" {
		t.Fatalf("proxy site handler = %v, want reverse_proxy", ph["handler"])
	}
}

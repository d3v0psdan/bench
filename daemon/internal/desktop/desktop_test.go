package desktop

import (
	"testing"

	"github.com/d3v0psdan/bench/daemon/internal/api"
)

func TestDatabaseURL(t *testing.T) {
	for kind, want := range map[string]string{
		"mysql":       "mysql://root@127.0.0.1:3308",
		"mariadb":     "mysql://root@127.0.0.1:3308",
		"postgresql":  "postgresql://root@127.0.0.1:3308/postgres",
		"valkey":      "redis://127.0.0.1:3308",
		"meilisearch": "",
	} {
		if got := DatabaseURL(api.Service{Service: kind, Port: 3308}); got != want {
			t.Errorf("%s: %q, want %q", kind, got, want)
		}
	}
}

func TestQuoteWrapsArgumentsWithSpaces(t *testing.T) {
	got := Quote([]string{`C:\Program Files\mysql.exe`, "-P3307", "-uroot"})
	if want := `"C:\Program Files\mysql.exe" -P3307 -uroot`; got != want {
		t.Errorf("Quote = %s, want %s", got, want)
	}
}

func TestOpenInEditorRejectsUnknownEditors(t *testing.T) {
	if _, err := OpenInEditor("notepad", t.TempDir()); err == nil {
		t.Fatal("an unknown editor id must fail, not launch anything")
	}
}

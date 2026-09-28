package dotenv

import (
	"strings"
	"testing"
)

const laravel = `APP_NAME=Laravel
APP_URL=http://localhost

DB_CONNECTION=sqlite
# DB_HOST=127.0.0.1
# DB_PORT=3306
`

func TestApplyReplacesRevivesAndAppendsWithMarkers(t *testing.T) {
	got, changes := Apply(laravel, []string{
		"APP_URL=https://shop.test", // replaced
		"DB_CONNECTION=mysql",       // replaced
		"DB_PORT=3307",              // revived from a comment
		"REDIS_PORT=6380",           // appended
		"APP_NAME=Laravel",          // unchanged: no change reported
	})
	want := `APP_NAME=Laravel
# Bench generated
APP_URL=https://shop.test

# Bench generated
DB_CONNECTION=mysql
# DB_HOST=127.0.0.1
# Bench generated
DB_PORT=3307

# Bench generated
REDIS_PORT=6380
`
	if got != want {
		t.Fatalf("Apply wrote:\n%s\nwant:\n%s", got, want)
	}
	if len(changes) != 4 {
		t.Fatalf("changes = %+v, want 4", changes)
	}
	if c := changes[0]; c.Key != "APP_URL" || c.Old != "http://localhost" || c.Added {
		t.Errorf("APP_URL change = %+v", c)
	}
	if c := changes[2]; c.Key != "DB_PORT" || c.Old != "" || !c.Added {
		t.Errorf("a revived key counts as added: %+v", c)
	}
}

func TestApplyTwiceAddsNoDuplicateMarkers(t *testing.T) {
	once, _ := Apply(laravel, []string{"APP_URL=https://shop.test"})
	twice, changes := Apply(once, []string{"APP_URL=https://other.test"})
	if n := strings.Count(twice, Marker); n != 1 || len(changes) != 1 {
		t.Fatalf("second apply: %d markers, %d changes\n%s", n, len(changes), twice)
	}
}

func TestParseUnquotesAndSkipsComments(t *testing.T) {
	env := Parse("# c\nA=1\nB=\"two words\"\nC='x'\nD=4 # note\nexport E=5\n")
	for k, want := range map[string]string{"A": "1", "B": "two words", "C": "x", "D": "4", "E": "5"} {
		if env[k] != want {
			t.Errorf("%s = %q, want %q", k, env[k], want)
		}
	}
}

func TestValidLine(t *testing.T) {
	for line, want := range map[string]bool{
		"DB_PORT=3307": true, "MAIL_USERNAME=\"${APP_NAME}\"": true,
		"db_port=1": false, "NOEQUALS": false, "A=multi\nline": false,
	} {
		if ValidLine(line) != want {
			t.Errorf("ValidLine(%q) = %v", line, !want)
		}
	}
}

package newapp

import (
	"strings"

	"github.com/d3v0psdan/bench/daemon/internal/dotenv"
)

// serverDBKeys are the .env lines a server database needs and SQLite
// doesn't (the installer comments them out for SQLite).
var serverDBKeys = []string{"DB_HOST", "DB_PORT", "DB_DATABASE", "DB_USERNAME", "DB_PASSWORD"}

// configureEnv applies the installer's .env edits to content: APP_URL,
// then either SQLite (the server lines commented out) or a Bench service's
// lines plus DB_DATABASE. extra lines (APP_NAME, Mail) come first. Every
// line Bench sets is marked as generated.
func configureEnv(content, appURL, database string, serviceEnv []string, dbName string, extra ...string) string {
	lines := append([]string{"APP_URL=" + appURL}, extra...)
	if database == "sqlite" {
		content, _ = dotenv.Apply(content, append(lines, "DB_CONNECTION=sqlite"))
		for _, key := range serverDBKeys {
			content = dotenv.Comment(content, key)
		}
		return content
	}
	for _, line := range serviceEnv {
		if strings.HasPrefix(line, "DB_") {
			lines = append(lines, line)
		}
	}
	content, _ = dotenv.Apply(content, append(lines, "DB_DATABASE="+dbName))
	return content
}

// databaseName is the installer's DB_DATABASE for an app: its name with
// hyphens as underscores (a site name is already lowercase).
func databaseName(app string) string { return strings.ReplaceAll(app, "-", "_") }

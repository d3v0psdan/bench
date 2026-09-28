// Package dotenv reads and edits Laravel .env files the way a person
// would: keys are replaced in place (a commented-out key is revived where
// it stands), new keys are appended, and every line Bench writes carries a
// "# Bench generated" comment above it. Formatting and other lines are
// left alone.
package dotenv

import (
	"bufio"
	"errors"
	"io/fs"
	"os"
	"regexp"
	"strings"
)

// Marker is the comment above every line Bench writes.
const Marker = "# Bench generated"

// Change is one key Apply sets: Old is "" when the key was absent (or only
// commented out).
type Change struct {
	Key   string `json:"key"`
	Old   string `json:"old"`
	New   string `json:"new"`
	Added bool   `json:"added"`
}

var keyRe = regexp.MustCompile(`^[A-Z][A-Z0-9_]*$`)

// ValidLine reports whether "KEY=value" is a line Apply may write: an
// upper-case key and a single-line value.
func ValidLine(line string) bool {
	key, value, ok := strings.Cut(line, "=")
	return ok && keyRe.MatchString(key) && !strings.ContainsAny(value, "\r\n")
}

// Read parses the .env at path; a missing file is empty.
func Read(path string) (map[string]string, error) {
	b, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return map[string]string{}, nil
	}
	if err != nil {
		return nil, err
	}
	return Parse(string(b)), nil
}

// Parse reads active KEY=VALUE lines, unquoting values and dropping inline
// comments. Variable expansion and multi-line values aren't needed for the
// keys Bench reads and writes.
func Parse(content string) map[string]string {
	env := map[string]string{}
	sc := bufio.NewScanner(strings.NewReader(content))
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		key, value, ok := strings.Cut(line, "=")
		if !ok || strings.HasPrefix(line, "#") {
			continue
		}
		key = strings.TrimSpace(strings.TrimPrefix(key, "export "))
		env[key] = unquote(strings.TrimSpace(value))
	}
	return env
}

func unquote(value string) string {
	if len(value) >= 2 && (value[0] == '"' || value[0] == '\'') && value[len(value)-1] == value[0] {
		return value[1 : len(value)-1]
	}
	if i := strings.Index(value, " #"); i >= 0 {
		return strings.TrimSpace(value[:i])
	}
	return value
}

// Apply sets each "KEY=value" line in content and reports what changed.
// A key already set to the same value is left as it is.
func Apply(content string, lines []string) (string, []Change) {
	current := Parse(content)
	var changes []Change
	var appended []string
	for _, line := range lines {
		key, value, _ := strings.Cut(line, "=")
		old, active := current[key]
		if active && old == unquote(value) {
			continue
		}
		loc := lineOf(content, key)
		if loc == nil {
			appended = append(appended, key+"="+value)
			changes = append(changes, Change{Key: key, New: value, Added: true})
			continue
		}
		content = content[:loc[0]] + markAbove(content[:loc[0]]) + key + "=" + value + content[loc[1]:]
		changes = append(changes, Change{Key: key, Old: old, New: value, Added: !active})
		current[key] = unquote(value)
	}
	if len(appended) > 0 {
		if content != "" && !strings.HasSuffix(content, "\n") {
			content += "\n"
		}
		if content != "" {
			content += "\n"
		}
		content += Marker + "\n" + strings.Join(appended, "\n") + "\n"
	}
	return content, changes
}

// Comment comments out key's active line.
func Comment(content, key string) string {
	line := regexp.MustCompile(`(?m)^` + regexp.QuoteMeta(key) + `=`)
	return line.ReplaceAllString(content, "# "+key+"=")
}

// lineOf finds key's line, active or commented out ("# KEY=").
func lineOf(content, key string) []int {
	return regexp.MustCompile(`(?m)^[ \t]*#?[ \t]*` + regexp.QuoteMeta(key) + `=.*$`).FindStringIndex(content)
}

// markAbove is the marker line to insert before a line, unless the text
// before it already ends with one.
func markAbove(before string) string {
	lines := strings.Split(strings.TrimRight(before, "\n"), "\n")
	if strings.TrimSpace(lines[len(lines)-1]) == Marker {
		return ""
	}
	return Marker + "\n"
}

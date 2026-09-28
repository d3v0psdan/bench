package newapp

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"time"

	"github.com/d3v0psdan/bench/daemon/internal/api"
	"github.com/d3v0psdan/bench/daemon/internal/supervisor"
)

// jsTools are the JavaScript tools a new app can use. Bench detects them
// and never installs them (PLAN.md §3.9).
var jsTools = []string{"node", "npm", "bun"}

const versionTimeout = 5 * time.Second

// Tools reports which JavaScript tools are on PATH, with their versions.
func Tools(ctx context.Context) []api.Tool {
	out := make([]api.Tool, 0, len(jsTools))
	for _, name := range jsTools {
		t := api.Tool{Name: name}
		if path, err := exec.LookPath(name); err == nil {
			t.Path = path
			vctx, cancel := context.WithTimeout(ctx, versionTimeout)
			v, err := supervisor.Exec(vctx, supervisor.Spec{Command: path, Args: []string{"--version"}})
			cancel()
			if err == nil {
				t.Version = strings.TrimPrefix(strings.TrimSpace(v), "v")
			}
		}
		out = append(out, t)
	}
	return out
}

// findTool resolves a package manager on PATH.
func findTool(name string) (string, error) {
	path, err := exec.LookPath(name)
	if err != nil {
		return "", fmt.Errorf("%s isn't installed or isn't on PATH", name)
	}
	return path, nil
}

// useBunInScripts swaps npm and npx for bun and bunx in composer.json, as
// the installer does for its dev/setup scripts.
//
// Shortcut: a text swap over the whole file, not only those scripts, to
// keep composer.json's key order and formatting; nothing else in a fresh
// Laravel composer.json names npm. Parse the scripts if that changes.
func useBunInScripts(path string) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	s := strings.NewReplacer("npx ", "bunx ", "npm ", "bun ").Replace(string(b))
	return os.WriteFile(path, []byte(s), 0o644)
}

// postUpdateRe finds the post-update-cmd list up to its closing bracket
// (Laravel's script lines hold no brackets).
var postUpdateRe = regexp.MustCompile(`("post-update-cmd":\s*\[[^\]]*?)(\s*\])`)

// addBoostUpdateScript adds "@php artisan boost:update --ansi" to
// composer.json's post-update-cmd, as the installer does after Boost, by
// text so the file keeps its order and formatting. False when the file has
// no post-update-cmd list to add to.
func addBoostUpdateScript(path string) (bool, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return false, err
	}
	if !postUpdateRe.Match(b) {
		return false, nil
	}
	out := postUpdateRe.ReplaceAll(b, []byte("$1,\n            \"@php artisan boost:update --ansi\"$2"))
	return true, os.WriteFile(path, out, 0o644)
}

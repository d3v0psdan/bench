// Package version holds the build version shared by all bench binaries,
// and compares version strings.
package version

import (
	"strconv"
	"strings"
)

// Version is overridden at release time via -ldflags "-X ...=v1.2.3".
var Version = "0.1.0-dev"

// Newer reports whether version a is newer than b ("0.2.0" > "0.1.3").
// A prerelease ("0.1.0-dev") is older than its release ("0.1.0").
func Newer(a, b string) bool {
	an, ap := parse(a)
	bn, bp := parse(b)
	for i := range an {
		if an[i] != bn[i] {
			return an[i] > bn[i]
		}
	}
	return ap == "" && bp != ""
}

func parse(v string) ([3]int, string) {
	v = strings.TrimPrefix(strings.TrimSpace(v), "v")
	core, pre, _ := strings.Cut(v, "-")
	var n [3]int
	for i, part := range strings.SplitN(core, ".", 3) {
		n[i], _ = strconv.Atoi(part)
	}
	return n, pre
}

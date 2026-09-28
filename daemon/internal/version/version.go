// Package version holds the build version shared by all bench binaries.
package version

// Version is overridden at release time via -ldflags "-X ...=v1.2.3".
var Version = "0.1.0-dev"

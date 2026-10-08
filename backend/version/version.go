// version/version.go

// Package version is what this server reports and what it demands of a client.
package version

import "strings"

// Release is the repository's VERSION, set at build time by the justfile. A
// development build says so rather than claiming a version nobody shipped.
var Release = "dev"

// Commit and BuildTime are set the same way and are empty in a development
// build.
var (
	Commit    = ""
	BuildTime = ""
)

const (
	// API is the versioned namespace this server serves. It moves when the
	// contract does, and /api/info is how a client learns it.
	API = "v1"

	// MinClient is the oldest Goptivum Desktop this server accepts. Raising it
	// turns away clients that already work, so it moves deliberately.
	MinClient = "0.1.0"

	// Baseline is what an unstamped build announces to a peer that demands a
	// real version. It tracks VERSION.
	Baseline = "0.1.0"
)

// Client is the version this server announces to an Arrango Node.
//
// A node refuses a caller whose version is not major.minor.patch, so an
// unstamped development build cannot announce "dev": `just dev` would fail to
// pair while `just build` succeeded, which reads as a broken node.
func Client() string {
	if isRelease(Release) {
		return Release
	}
	return Baseline
}

// isRelease reports whether a string is major.minor.patch of digits.
func isRelease(value string) bool {
	parts := strings.Split(value, ".")
	if len(parts) != 3 {
		return false
	}
	for _, part := range parts {
		if part == "" {
			return false
		}
		for _, digit := range part {
			if digit < '0' || digit > '9' {
				return false
			}
		}
	}
	return true
}

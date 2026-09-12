// version/version.go

// Package version is what this server reports and what it demands of a client.
package version

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
)

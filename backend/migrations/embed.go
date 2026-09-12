// migrations/embed.go

// Package migrations carries the goose migration set as an embedded FS.
package migrations

import (
	"embed"
	"io/fs"
)

//go:embed *.sql
var files embed.FS

// FS is the migration set rooted at the SQL files, because goose.Up is called
// with "." and would not find them rooted at the module.
var FS fs.FS = files

// internal/testsupport/testsupport.go

// Package testsupport locates the repository's test corpora.
package testsupport

import (
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"testing"
)

// root finds the repository root by walking up to the go.mod.
func root(t *testing.T) string {
	t.Helper()

	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("working directory: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("no go.mod above %s", dir)
		}
		dir = parent
	}
}

// Fixture returns a committed fixture path, failing when it is missing.
func Fixture(t *testing.T, name string) string {
	t.Helper()

	path := filepath.Join(root(t), "tests", "fixtures", name)
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("fixture %s: %v", name, err)
	}
	return path
}

// Golden returns a path under tests/golden.
func Golden(t *testing.T, name string) string {
	t.Helper()
	return filepath.Join(root(t), "tests", "golden", name)
}

// Corpus returns real test files matching pattern, skipping when none exist.
// A clean checkout has none: tests/data is gitignored personal data.
func Corpus(t *testing.T, pattern string) []string {
	t.Helper()

	dir := filepath.Join(root(t), "tests", "data")

	// Walk, not glob: corpora are filed into per-format subdirectories.
	var matches []string
	err := filepath.WalkDir(dir, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			if os.IsNotExist(err) {
				return nil
			}
			return err
		}
		if entry.IsDir() {
			return nil
		}
		ok, matchErr := filepath.Match(pattern, entry.Name())
		if matchErr != nil {
			return matchErr
		}
		if ok {
			matches = append(matches, path)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk tests/data: %v", err)
	}
	sort.Strings(matches)
	if len(matches) == 0 {
		t.Skipf("no %s under tests/data - drop real files there to exercise this", pattern)
	}
	return matches
}

// document/gpf/write.go

package gpf

import (
	"fmt"
	"os"
	"path/filepath"
)

// WriteFile writes a .gpf to disk, atomically.
//
// Temp file, fsync, rename. A .gpf is written whole and once per version, so
// there is no append to be caught halfway: this alone makes a torn file
// impossible and there is no recovery path to build. The temp file is in the
// same directory as the target, because rename is only atomic within one
// filesystem.
func WriteFile(path string, data []byte) (err error) {
	directory := filepath.Dir(path)
	temporary, err := os.CreateTemp(directory, "."+filepath.Base(path)+".*")
	if err != nil {
		return fmt.Errorf("gpf: create temporary file: %w", err)
	}
	name := temporary.Name()
	defer func() {
		if err != nil {
			// Nothing is ever left at the target path, and nothing is left
			// beside it either.
			temporary.Close()
			os.Remove(name)
		}
	}()

	if _, err = temporary.Write(data); err != nil {
		return fmt.Errorf("gpf: write: %w", err)
	}
	// Before the rename, or a crash can leave the new name pointing at a file
	// whose contents never reached the disk.
	if err = temporary.Sync(); err != nil {
		return fmt.Errorf("gpf: sync: %w", err)
	}
	if err = temporary.Close(); err != nil {
		return fmt.Errorf("gpf: close: %w", err)
	}
	if err = os.Chmod(name, 0o600); err != nil {
		return fmt.Errorf("gpf: chmod: %w", err)
	}
	if err = os.Rename(name, path); err != nil {
		return fmt.Errorf("gpf: rename: %w", err)
	}
	return nil
}

// ReadFile reads a .gpf from disk.
func ReadFile(path string, options ReadOptions) (*File, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("gpf: read %s: %w", path, err)
	}
	return Decode(data, options)
}

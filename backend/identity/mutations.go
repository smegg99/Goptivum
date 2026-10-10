// identity/mutations.go

package identity

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"golang.org/x/sys/unix"
)

// lockChanges serializes local server/CLI writers for the same Kratos endpoint.
func (a *Admin) lockChanges(ctx context.Context) (func(), error) {
	cache, err := os.UserCacheDir()
	if err != nil {
		return nil, fmt.Errorf("identity: mutation lock directory: %w", err)
	}
	directory := filepath.Join(cache, "goptivum", "identity")
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return nil, fmt.Errorf("identity: mutation lock directory: %w", err)
	}
	name := fmt.Sprintf("%x.lock", sha256.Sum256([]byte(a.base.String())))
	file, err := os.OpenFile(filepath.Join(directory, name), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("identity: mutation lock: %w", err)
	}
	tick := time.NewTicker(20 * time.Millisecond)
	defer tick.Stop()
	for {
		if err := ctx.Err(); err != nil {
			file.Close()
			return nil, err
		}
		err := unix.Flock(int(file.Fd()), unix.LOCK_EX|unix.LOCK_NB)
		if err == nil {
			return func() { _ = file.Close() }, nil
		}
		if !errors.Is(err, unix.EWOULDBLOCK) && !errors.Is(err, unix.EINTR) {
			file.Close()
			return nil, fmt.Errorf("identity: mutation lock: %w", err)
		}
		select {
		case <-ctx.Done():
		case <-tick.C:
		}
	}
}

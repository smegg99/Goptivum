// core/documents/store.go

// Package documents keeps encoded plan documents in object storage, addressed by the hash of their bytes.
package documents

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/smegg99/goptivum/document"
	goptivumv1 "github.com/smegg99/goptivum/document/gen/goptivumv1"
	"github.com/smegg99/s99ab/data/storage"
)

const (
	// MimeType is what a stored document is.
	MimeType = "application/zstd"
	// KeyPrefix namespaces plan documents inside the object store.
	KeyPrefix = "plans"
)

// ErrNotFound means the object a version names is not in the store. The
// database row is the index, so this is a repairable inconsistency and never
// something to discover by listing a bucket.
var ErrNotFound = errors.New("documents: no such document")

// Garbage is the index's list of keys waiting for the sweep.
type Garbage interface {
	// Revive pushes back the sweep of a key that is about to be stored again.
	Revive(ctx context.Context, key string) error
}

// Store keeps encoded documents in object storage.
type Store struct {
	objects storage.Storage
	garbage Garbage
}

// NewStore wraps object storage. The caller decides whether that is a
// directory or a bucket.
func NewStore(objects storage.Storage) *Store {
	return &Store{objects: objects}
}

// WithGarbage makes every Put first revive its key, so a sweep that finds the
// key unnamed cannot delete the bytes between this write and the row that
// will name them. A server that sweeps must set it.
func (s *Store) WithGarbage(garbage Garbage) *Store {
	s.garbage = garbage
	return s
}

// Put writes a document and returns its key. Writing the same school twice is
// free: the key is the hash of the bytes, so the second write replaces identical
// content.
func (s *Store) Put(ctx context.Context, encoded document.Encoded) (string, error) {
	if encoded.SHA256 == "" || len(encoded.Bytes) == 0 {
		return "", fmt.Errorf("documents: nothing to store")
	}
	key := storage.Key(KeyPrefix, encoded.SHA256)
	if s.garbage != nil {
		if err := s.garbage.Revive(ctx, key); err != nil {
			return "", fmt.Errorf("documents: put %s: %w", key, err)
		}
	}
	if err := s.objects.Put(ctx, key, bytes.NewReader(encoded.Bytes), MimeType); err != nil {
		return "", fmt.Errorf("documents: put %s: %w", key, err)
	}
	return key, nil
}

// Bytes returns the stored document as it was written.
func (s *Store) Bytes(ctx context.Context, key string) ([]byte, error) {
	reader, err := s.objects.Get(ctx, key)
	if err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			return nil, fmt.Errorf("%w: %s", ErrNotFound, key)
		}
		return nil, fmt.Errorf("documents: get %s: %w", key, err)
	}
	defer reader.Close()

	// The stored bytes are already compressed, so the read is bounded by the
	// object and the decode limit does the rest.
	data, err := io.ReadAll(io.LimitReader(reader, document.MaxDecoded))
	if err != nil {
		return nil, fmt.Errorf("documents: read %s: %w", key, err)
	}
	return data, nil
}

// Load returns the document a version names.
func (s *Store) Load(ctx context.Context, key string) (*goptivumv1.Document, error) {
	data, err := s.Bytes(ctx, key)
	if err != nil {
		return nil, err
	}
	return document.Decode(data)
}

// Delete removes a stored document. Only the sweep calls it, once no row names
// the key; deleting one that is already gone succeeds.
func (s *Store) Delete(ctx context.Context, key string) error {
	if err := s.objects.Delete(ctx, key); err != nil {
		return fmt.Errorf("documents: delete %s: %w", key, err)
	}
	return nil
}

// Exists reports whether the object is there, for repairing an index that
// points at nothing.
func (s *Store) Exists(ctx context.Context, key string) (bool, error) {
	ok, err := s.objects.Exists(ctx, key)
	if err != nil {
		return false, fmt.Errorf("documents: exists %s: %w", key, err)
	}
	return ok, nil
}

// probeKey is a key nothing is ever stored under. Asking whether it exists
// reaches the backend and answers without writing anything.
const probeKey = KeyPrefix + "/.probe"

// Probe reports whether object storage answers.
//
// Exists and not Put: a readiness check that writes leaves rubbish behind on
// every poll, and a backend that can answer Exists is a backend that is there.
// The answer itself is ignored; the key is not supposed to be found.
func (s *Store) Probe(ctx context.Context) error {
	if s.objects == nil {
		return fmt.Errorf("documents: no object storage configured")
	}
	if _, err := s.objects.Exists(ctx, probeKey); err != nil {
		return fmt.Errorf("documents: object storage: %w", err)
	}
	return nil
}

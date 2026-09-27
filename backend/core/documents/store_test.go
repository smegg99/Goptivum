// core/documents/store_test.go

package documents_test

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	arrangov1 "github.com/smegg99/arrango/backend/gen/arrangov1"
	"github.com/smegg99/s99ab/data/storage"
	"google.golang.org/protobuf/proto"

	"github.com/smegg99/goptivum/backend/core/documents"
	"github.com/smegg99/goptivum/document"
)

func school() *arrangov1.SchoolModel {
	return &arrangov1.SchoolModel{
		Name:     "Szkoła Testowa",
		Days:     []*arrangov1.Day{{Id: 1, Name: "Poniedziałek", PeriodCount: 2}},
		Periods:  []*arrangov1.Period{{Id: 2, Name: "8:00-8:45"}, {Id: 3, Name: "8:55-9:40"}},
		Teachers: []*arrangov1.Teacher{{Id: 6, Name: "Łukasz Żółć"}},
	}
}

func localStore(t *testing.T) *documents.Store {
	t.Helper()
	return documents.NewStore(storage.NewLocalDir(t.TempDir(), "/objects"))
}

func encoded(t *testing.T, model *arrangov1.SchoolModel) document.Encoded {
	t.Helper()
	encoded, err := document.Encode(document.New(model))
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	return encoded
}

// Content addressing depends on byte stability, so the comparison is on bytes.
func TestDocumentRoundTripsThroughStorageByteIdentically(t *testing.T) {
	ctx := context.Background()
	store := localStore(t)
	written := encoded(t, school())

	key, err := store.Put(ctx, written)
	if err != nil {
		t.Fatalf("put: %v", err)
	}
	stored, err := store.Bytes(ctx, key)
	if err != nil {
		t.Fatalf("bytes: %v", err)
	}
	if !bytes.Equal(stored, written.Bytes) {
		t.Fatalf("stored %d bytes, wrote %d", len(stored), len(written.Bytes))
	}

	loaded, err := store.Load(ctx, key)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if !proto.Equal(loaded, document.New(school())) {
		t.Error("the document changed on its way through storage")
	}
}

// The same school stored twice costs one object.
func TestTheKeyIsTheHashOfTheBytes(t *testing.T) {
	ctx := context.Background()
	store := localStore(t)

	first, err := store.Put(ctx, encoded(t, school()))
	if err != nil {
		t.Fatalf("put: %v", err)
	}
	second, err := store.Put(ctx, encoded(t, school()))
	if err != nil {
		t.Fatalf("put again: %v", err)
	}
	if first != second {
		t.Errorf("keys differ: %s and %s", first, second)
	}
	if want := encoded(t, school()).SHA256; !strings.HasPrefix(first, documents.KeyPrefix+"/") || !strings.HasSuffix(first, want) {
		t.Errorf("key %q is not %s/.../%s", first, documents.KeyPrefix, want)
	}

	changed := school()
	changed.Name = "Inna Szkoła"
	third, err := store.Put(ctx, encoded(t, changed))
	if err != nil {
		t.Fatalf("put changed: %v", err)
	}
	if third == first {
		t.Error("two different schools share one key")
	}
}

// A row pointing at nothing is a repairable inconsistency, so it is a typed error.
func TestMissingObjectIsATypedError(t *testing.T) {
	ctx := context.Background()
	store := localStore(t)

	if _, err := store.Bytes(ctx, "plans/ab/cd/abcdef"); !errors.Is(err, documents.ErrNotFound) {
		t.Errorf("error = %v, want ErrNotFound", err)
	}
	if _, err := store.Load(ctx, "plans/ab/cd/abcdef"); !errors.Is(err, documents.ErrNotFound) {
		t.Errorf("load error = %v, want ErrNotFound", err)
	}
	if ok, err := store.Exists(ctx, "plans/ab/cd/abcdef"); err != nil || ok {
		t.Errorf("exists = %v, %v", ok, err)
	}
}

func TestPutRefusesNothing(t *testing.T) {
	if _, err := localStore(t).Put(context.Background(), document.Encoded{}); err == nil {
		t.Error("put accepted an empty document")
	}
}

// revivals records the keys a store revived before writing them.
type revivals struct {
	keys []string
	err  error
}

func (r *revivals) Revive(_ context.Context, key string) error {
	r.keys = append(r.keys, key)
	return r.err
}

// A put revives its key first, so a sweep cannot take the bytes between the
// write and the row that names them; a revive that fails stores nothing.
func TestPutRevivesItsKeyBeforeWriting(t *testing.T) {
	ctx := context.Background()
	garbage := &revivals{}
	store := localStore(t).WithGarbage(garbage)

	key, err := store.Put(ctx, encoded(t, school()))
	if err != nil {
		t.Fatalf("put: %v", err)
	}
	if len(garbage.keys) != 1 || garbage.keys[0] != key {
		t.Errorf("revived %v, want [%s]", garbage.keys, key)
	}

	garbage.err = errors.New("database is locked")
	other := school()
	other.Name = "Inna szkoła"
	if _, err := store.Put(ctx, encoded(t, other)); err == nil {
		t.Fatal("a put whose revive failed succeeded")
	}
	if ok, err := store.Exists(ctx, garbage.keys[1]); err != nil || ok {
		t.Errorf("a put whose revive failed stored its object: %v, %v", ok, err)
	}
}

func TestDeleteRemovesADocumentAndAcceptsOneAlreadyGone(t *testing.T) {
	ctx := context.Background()
	store := localStore(t)
	key, err := store.Put(ctx, encoded(t, school()))
	if err != nil {
		t.Fatalf("put: %v", err)
	}
	for attempt := 0; attempt < 2; attempt++ {
		if err := store.Delete(ctx, key); err != nil {
			t.Fatalf("delete %d: %v", attempt+1, err)
		}
	}
	if _, err := store.Bytes(ctx, key); !errors.Is(err, documents.ErrNotFound) {
		t.Errorf("bytes after delete = %v, want ErrNotFound", err)
	}
}

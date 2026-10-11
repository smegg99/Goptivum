// core/documents/infra_test.go

package documents_test

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/smegg99/s99ab/data/storage"
	"google.golang.org/protobuf/proto"

	"github.com/smegg99/goptivum/backend/core/documents"
	"github.com/smegg99/goptivum/backend/formats"
	"github.com/smegg99/goptivum/backend/formats/optivum/pla"
	planxml "github.com/smegg99/goptivum/backend/formats/optivum/xml"
	"github.com/smegg99/goptivum/backend/internal/testsupport"
	"github.com/smegg99/goptivum/document"
)

// bucketStore connects to the MinIO that "just infra-up" starts. Without it
// the test skips: the unit tests already cover the pipeline, and this one is
// here to prove the real backend behaves the same.
func bucketStore(t *testing.T) *documents.Store {
	t.Helper()

	endpoint := os.Getenv("GOPTIVUM_TEST_MINIO_ENDPOINT")
	if endpoint == "" {
		t.Skip("no GOPTIVUM_TEST_MINIO_ENDPOINT - run just infra-up and just test-infra")
	}
	bucket, err := storage.NewBucket(context.Background(), storage.BucketConfig{
		Endpoint:  endpoint,
		Bucket:    os.Getenv("GOPTIVUM_TEST_MINIO_BUCKET"),
		AccessKey: os.Getenv("GOPTIVUM_TEST_MINIO_ACCESS_KEY"),
		SecretKey: os.Getenv("GOPTIVUM_TEST_MINIO_SECRET_KEY"),
	})
	if err != nil {
		t.Fatalf("connect to minio at %s: %v", endpoint, err)
	}
	return documents.NewStore(bucket)
}

// fixtureSchool imports the committed Optivum fixture, so the document under
// test has the shape a real one has.
func fixtureSchool(t *testing.T) document.Encoded {
	t.Helper()

	body, err := os.ReadFile(testsupport.Fixture(t, filepath.Join("optivum", "plan-small.xml")))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	// The fixture is stored as utf-8 and declares it, so it is wrapped as it
	// is; the importer's own tests cover the iso-8859-2 path a real file takes.
	container, err := pla.Encode(pla.File{Magic: pla.Magic, XML: body, Compressed: true})
	if err != nil {
		t.Fatalf("wrap fixture: %v", err)
	}
	result, err := planxml.New().Import(context.Background(), formats.Open("plan-small.pla", container))
	if err != nil {
		t.Fatalf("import fixture: %v", err)
	}
	original := document.New(result.Model)
	doc, err := document.Encode(original)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	return doc
}

func TestInfraDocumentRoundTripsThroughMinIO(t *testing.T) {
	ctx := context.Background()
	store := bucketStore(t)
	doc := fixtureSchool(t)

	key, err := store.Put(ctx, doc)
	if err != nil {
		t.Fatalf("put: %v", err)
	}
	stored, err := store.Bytes(ctx, key)
	if err != nil {
		t.Fatalf("bytes: %v", err)
	}
	if !bytes.Equal(stored, doc.Bytes) {
		t.Fatalf("read %d bytes, wrote %d", len(stored), len(doc.Bytes))
	}

	model, err := store.Load(ctx, key)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	again, err := document.Encode(model)
	if err != nil {
		t.Fatalf("re-encode: %v", err)
	}
	if again.SHA256 != doc.SHA256 {
		t.Errorf("the document came back hashing to %s, stored as %s", again.SHA256, doc.SHA256)
	}

	// Writing the same school twice is one object, which is what content
	// addressing buys.
	if _, err := store.Put(ctx, doc); err != nil {
		t.Fatalf("put again: %v", err)
	}
	if ok, err := store.Exists(ctx, key); err != nil || !ok {
		t.Errorf("exists = %v, %v", ok, err)
	}
	if _, err := store.Bytes(ctx, "plans/00/00/notthere"); !errors.Is(err, documents.ErrNotFound) {
		t.Errorf("missing object = %v, want ErrNotFound", err)
	}
}

// The milestone's own gate: a real school, stored and read back byte for byte,
// through the bucket and through the directory.
func TestInfraRealSchoolRoundTripsThroughBothBackends(t *testing.T) {
	ctx := context.Background()
	bucket := bucketStore(t)
	local := documents.NewStore(storage.NewLocalDir(t.TempDir(), "/objects"))

	for _, path := range testsupport.Corpus(t, "*.pla") {
		// The file is named, never its contents.
		t.Run(filepath.Base(path), func(t *testing.T) {
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("read: %v", err)
			}
			result, err := planxml.New().Import(ctx, formats.Open(filepath.Base(path), data))
			if err != nil {
				t.Fatalf("import: %v", err)
			}
			original := document.New(result.Model)
			doc, err := document.Encode(original)
			if err != nil {
				t.Fatalf("encode: %v", err)
			}
			raw, err := proto.MarshalOptions{Deterministic: true}.Marshal(result.Model)
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			t.Logf("%d lessons, %d bytes of protobuf, %d compressed, %.1fx",
				len(result.Model.GetLessons()), len(raw), doc.Size,
				float64(len(raw))/float64(doc.Size))

			for name, store := range map[string]*documents.Store{"minio": bucket, "local": local} {
				t.Run(name, func(t *testing.T) {
					key, err := store.Put(ctx, doc)
					if err != nil {
						t.Fatalf("put: %v", err)
					}
					stored, err := store.Bytes(ctx, key)
					if err != nil {
						t.Fatalf("bytes: %v", err)
					}
					if !bytes.Equal(stored, doc.Bytes) {
						t.Fatal("the stored bytes differ from the written bytes")
					}
					model, err := store.Load(ctx, key)
					if err != nil {
						t.Fatalf("load: %v", err)
					}
					if !proto.Equal(original, model) {
						t.Error("the document envelope changed through storage")
					}
					read, err := proto.MarshalOptions{Deterministic: true}.Marshal(model.GetModel())
					if err != nil {
						t.Fatalf("marshal read: %v", err)
					}
					if !bytes.Equal(raw, read) {
						t.Error("the school changed on its way through storage")
					}
				})
			}
		})
	}
}

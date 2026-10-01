// core/exports/exports_test.go

package exports_test

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/smegg99/s99ab/data/db"
	"github.com/smegg99/s99ab/data/storage"

	arrangov1 "github.com/smegg99/arrango/backend/gen/arrangov1"

	"github.com/smegg99/goptivum/backend/core/documents"
	"github.com/smegg99/goptivum/backend/core/exports"
	"github.com/smegg99/goptivum/backend/core/plans"
	"github.com/smegg99/goptivum/backend/formats"
	"github.com/smegg99/goptivum/backend/migrations"
	"github.com/smegg99/goptivum/document"
)

// fake is an exporter that counts its builds, can hold one back, and names
// the key it encrypts to.
type fake struct {
	id    string
	built atomic.Int32
	gate  chan struct{}
	err   error

	mu      sync.Mutex
	variant string
}

func (f *fake) ID() string { return f.id }

func (f *fake) Export(_ context.Context, result *formats.Result) ([]byte, error) {
	f.built.Add(1)
	if f.gate != nil {
		<-f.gate
	}
	if f.err != nil {
		return nil, f.err
	}
	variant, _ := f.Variant(context.Background())
	return fmt.Appendf(nil, "%s|%s|%d|%s", f.id, result.Model.GetName(), len(result.Schedule.GetLessons()), variant), nil
}

func (f *fake) Variant(context.Context) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.variant, nil
}

func (f *fake) setVariant(variant string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.variant = variant
}

type fixture struct {
	conn    *sql.DB
	index   *plans.Repository
	objects storage.Storage
	service *exports.Service
	gpf     *fake
	html    *fake
	plan    plans.Plan
	version plans.Version
}

// setup keeps a plan whose version 1 is a small solved school.
func setup(t *testing.T) fixture {
	t.Helper()
	return setupOver(t, func(objects storage.Storage) storage.Storage { return objects })
}

// setupOver is setup with the object storage the service writes to wrapped.
func setupOver(t *testing.T, wrap func(storage.Storage) storage.Storage) fixture {
	t.Helper()
	ctx := context.Background()
	conn := db.TestDB(t, migrations.FS)
	index := plans.NewRepository(conn, db.SQLite, plans.Caps{BackupsKeep: 100, ResultsKeep: 20})
	objects := storage.NewLocalDir(t.TempDir(), "/objects")
	store := documents.NewStore(objects).WithGarbage(index)

	solved := document.New(&arrangov1.SchoolModel{
		Name:    "Szkoła Testowa",
		Lessons: []*arrangov1.LessonInstance{{Id: 5, Duration: 1}},
	})
	solved.Schedule = &arrangov1.ScheduleSnapshot{Lessons: []*arrangov1.ScheduledLesson{{LessonId: 5}}}
	encoded, err := document.Encode(solved)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	key, err := store.Put(ctx, encoded)
	if err != nil {
		t.Fatalf("put: %v", err)
	}
	plan, version, err := index.Create(ctx, plans.Plan{OwnerIdentityID: "identity-1", Name: "Plan 2027"},
		plans.Upload{ObjectKey: key, SizeBytes: encoded.Size, CreatedBy: "identity-1"})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	gpf := &fake{id: "goptivum.gpf"}
	html := &fake{id: "optivum.html"}
	service, err := exports.New(index, store, wrap(objects),
		exports.Format{Exporter: gpf, ContentType: "application/octet-stream"},
		exports.Format{Exporter: html, ContentType: "application/zip"})
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	return fixture{conn: conn, index: index, objects: objects, service: service, gpf: gpf, html: html, plan: plan, version: version}
}

func (f fixture) source() exports.Source {
	return exports.Source{PlanID: f.plan.ID, Version: f.version.Version, ObjectKey: f.version.ObjectKey}
}

func (f fixture) stored(t *testing.T, key string) []byte {
	t.Helper()
	reader, err := f.objects.Get(context.Background(), key)
	if err != nil {
		t.Fatalf("get %s: %v", key, err)
	}
	defer reader.Close()
	data, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("read %s: %v", key, err)
	}
	return data
}

// The first request builds the file and keeps it; the second answers from
// storage without building again.
func TestAFileIsBuiltOnceThenReadFromStorage(t *testing.T) {
	ctx := context.Background()
	f := setup(t)

	first, err := f.service.File(ctx, f.source(), "goptivum.gpf")
	if err != nil {
		t.Fatalf("first: %v", err)
	}
	if string(first.Bytes) != "goptivum.gpf|Szkoła Testowa|1|" || first.ContentType != "application/octet-stream" {
		t.Errorf("first = %q %s", first.Bytes, first.ContentType)
	}
	key := fmt.Sprintf("exports/%d/versions/1/goptivum.gpf", f.plan.ID)
	if got := exports.Key(f.source(), "goptivum.gpf"); got != key {
		t.Errorf("key = %s, want %s", got, key)
	}
	if kept := f.stored(t, key); !bytes.Equal(kept, first.Bytes) {
		t.Errorf("stored %q, answered %q", kept, first.Bytes)
	}
	row, err := f.index.Export(ctx, key)
	if err != nil || row.PlanID != f.plan.ID || row.Version != 1 || row.ResultID != 0 ||
		row.Format != "goptivum.gpf" || row.SizeBytes != int64(len(first.Bytes)) {
		t.Errorf("row = %+v, %v", row, err)
	}

	second, err := f.service.File(ctx, f.source(), "goptivum.gpf")
	if err != nil || !bytes.Equal(second.Bytes, first.Bytes) || second.ContentType != first.ContentType {
		t.Errorf("second = %q %s, %v", second.Bytes, second.ContentType, err)
	}
	if built := f.gpf.built.Load(); built != 1 {
		t.Errorf("built %d times, want once", built)
	}

	// Each format is its own file.
	html, err := f.service.File(ctx, f.source(), "optivum.html")
	if err != nil || html.ContentType != "application/zip" || f.html.built.Load() != 1 {
		t.Errorf("html = %q %s, %v; built %d", html.Bytes, html.ContentType, err, f.html.built.Load())
	}
}

// A result's files live under the result, not under its source version.
func TestAResultsFileIsKeptUnderTheResult(t *testing.T) {
	ctx := context.Background()
	f := setup(t)
	result, err := f.index.AddResult(ctx, plans.NewResult{
		PlanID: f.plan.ID, SourceVersion: 1, ObjectKey: f.version.ObjectKey, SizeBytes: f.version.SizeBytes,
		StartedBy: "identity-1", Device: "laptop", UploadKey: "result-upload-key-0001",
	})
	if err != nil {
		t.Fatalf("add result: %v", err)
	}
	source := exports.Source{PlanID: f.plan.ID, ResultID: result.ID, ObjectKey: result.ObjectKey}
	if _, err := f.service.File(ctx, source, "optivum.html"); err != nil {
		t.Fatalf("file: %v", err)
	}
	key := fmt.Sprintf("exports/%d/results/%d/optivum.html", f.plan.ID, result.ID)
	if row, err := f.index.Export(ctx, key); err != nil || row.ResultID != result.ID || row.Version != 0 {
		t.Errorf("row = %+v, %v", row, err)
	}
}

// A file kept from before the school had a key, or for another key, is built
// again and replaces the one kept.
func TestAFileBuiltForAnotherKeyIsBuiltAgain(t *testing.T) {
	ctx := context.Background()
	f := setup(t)
	if _, err := f.service.File(ctx, f.source(), "goptivum.gpf"); err != nil {
		t.Fatalf("plain: %v", err)
	}
	f.gpf.setVariant("age1school")
	sealed, err := f.service.File(ctx, f.source(), "goptivum.gpf")
	if err != nil || string(sealed.Bytes) != "goptivum.gpf|Szkoła Testowa|1|age1school" {
		t.Fatalf("sealed = %q, %v", sealed.Bytes, err)
	}
	if built := f.gpf.built.Load(); built != 2 {
		t.Errorf("built %d times, want twice", built)
	}
	key := exports.Key(f.source(), "goptivum.gpf")
	if row, err := f.index.Export(ctx, key); err != nil || row.Variant != "age1school" {
		t.Errorf("row = %+v, %v", row, err)
	}
	if again, err := f.service.File(ctx, f.source(), "goptivum.gpf"); err != nil || !bytes.Equal(again.Bytes, sealed.Bytes) || f.gpf.built.Load() != 2 {
		t.Errorf("again = %q, %v; built %d", again.Bytes, err, f.gpf.built.Load())
	}
}

// A kept row whose object is gone is repaired by building the file again, not
// reported as an outage.
func TestAFileWhoseObjectIsGoneIsBuiltAgain(t *testing.T) {
	ctx := context.Background()
	f := setup(t)
	if _, err := f.service.File(ctx, f.source(), "optivum.html"); err != nil {
		t.Fatalf("first: %v", err)
	}
	if err := f.objects.Delete(ctx, exports.Key(f.source(), "optivum.html")); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := f.service.File(ctx, f.source(), "optivum.html"); err != nil {
		t.Fatalf("second: %v", err)
	}
	if built := f.html.built.Load(); built != 2 {
		t.Errorf("built %d times, want twice", built)
	}
}

// A kept object shorter or longer than its row says, such as one cut short,
// is built again rather than served.
func TestATornFileIsBuiltAgain(t *testing.T) {
	ctx := context.Background()
	f := setup(t)
	first, err := f.service.File(ctx, f.source(), "goptivum.gpf")
	if err != nil {
		t.Fatalf("first: %v", err)
	}
	key := exports.Key(f.source(), "goptivum.gpf")
	if err := f.objects.Put(ctx, key, bytes.NewReader(first.Bytes[:3]), "application/octet-stream"); err != nil {
		t.Fatalf("tear: %v", err)
	}
	again, err := f.service.File(ctx, f.source(), "goptivum.gpf")
	if err != nil || !bytes.Equal(again.Bytes, first.Bytes) {
		t.Errorf("again = %q, %v; want the whole file", again.Bytes, err)
	}
	if built := f.gpf.built.Load(); built != 2 {
		t.Errorf("built %d times, want twice", built)
	}
}

// Requests for one file that arrive while it is being built wait for that
// build instead of starting their own.
func TestConcurrentRequestsBuildOnce(t *testing.T) {
	ctx := context.Background()
	f := setup(t)
	f.gpf.gate = make(chan struct{})

	const callers = 8
	var (
		started sync.WaitGroup
		done    sync.WaitGroup
		failed  atomic.Int32
	)
	started.Add(callers)
	done.Add(callers)
	for range callers {
		go func() {
			defer done.Done()
			started.Done()
			if _, err := f.service.File(ctx, f.source(), "goptivum.gpf"); err != nil {
				failed.Add(1)
			}
		}()
	}
	started.Wait()
	time.Sleep(50 * time.Millisecond)
	close(f.gpf.gate)
	done.Wait()
	if failed.Load() != 0 {
		t.Errorf("%d requests failed", failed.Load())
	}
	if built := f.gpf.built.Load(); built != 1 {
		t.Errorf("built %d times, want once", built)
	}
}

// A refusal from the exporter reaches the caller and keeps nothing.
func TestAnExportersRefusalKeepsNothing(t *testing.T) {
	ctx := context.Background()
	f := setup(t)
	f.html.err = formats.ErrNoTimetable
	if _, err := f.service.File(ctx, f.source(), "optivum.html"); !errors.Is(err, formats.ErrNoTimetable) {
		t.Errorf("err = %v, want ErrNoTimetable", err)
	}
	if _, err := f.index.Export(ctx, exports.Key(f.source(), "optivum.html")); !errors.Is(err, plans.ErrNoExport) {
		t.Errorf("a refused file was kept: %v", err)
	}
	if _, err := f.service.File(ctx, f.source(), "optivum.pdf"); !errors.Is(err, exports.ErrUnknownFormat) {
		t.Errorf("an unknown format: err = %v, want ErrUnknownFormat", err)
	}
}

// A file of a version that is gone is not kept, and what it stored waits for
// the sweep.
func TestAFileOfAVersionThatIsGoneIsNotKept(t *testing.T) {
	ctx := context.Background()
	f := setup(t)
	gone := exports.Source{PlanID: f.plan.ID, Version: 9, ObjectKey: f.version.ObjectKey}
	if _, err := f.service.File(ctx, gone, "goptivum.gpf"); !errors.Is(err, plans.ErrNoVersion) {
		t.Fatalf("err = %v, want ErrNoVersion", err)
	}
	stored, err := f.index.Stored(ctx)
	if err != nil || stored.Waiting != 1 {
		t.Errorf("stored = %+v, %v; want the stored file waiting for the sweep", stored, err)
	}
}

// sweeping is object storage whose every write is followed at once by an
// object sweep, the way an hourly sweep can land between a file's write and
// the row that names it.
type sweeping struct {
	storage.Storage
	sweep func(context.Context)
}

func (s sweeping) Put(ctx context.Context, key string, r io.Reader, mime string) error {
	if err := s.Storage.Put(ctx, key, r, mime); err != nil {
		return err
	}
	s.sweep(ctx)
	return nil
}

// A key an earlier removal left waiting for the sweep is revived before the
// file is written again, so a sweep between the write and its row cannot
// delete the new file.
func TestABuildRevivesItsKeyBeforeTheSweepCanTakeIt(t *testing.T) {
	ctx := context.Background()
	var f fixture
	f = setupOver(t, func(objects storage.Storage) storage.Storage {
		return sweeping{Storage: objects, sweep: func(ctx context.Context) {
			if _, err := f.index.Sweep(ctx, time.Now(), objects.Delete); err != nil {
				t.Errorf("sweep: %v", err)
			}
		}}
	})
	key := exports.Key(f.source(), "goptivum.gpf")
	if _, err := f.conn.Exec(`insert into object_garbage (object_key, since) values (?, ?)`,
		key, time.Now().UTC().Add(-2*time.Hour)); err != nil {
		t.Fatalf("wait: %v", err)
	}
	if _, err := f.service.File(ctx, f.source(), "goptivum.gpf"); err != nil {
		t.Fatalf("file: %v", err)
	}
	if ok, err := f.objects.Exists(ctx, key); err != nil || !ok {
		t.Errorf("the sweep deleted the file it had waited on: exists %v, %v", ok, err)
	}
}

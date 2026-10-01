// core/results/results_test.go

package results_test

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"google.golang.org/protobuf/proto"

	"github.com/smegg99/s99ab/data/db"
	"github.com/smegg99/s99ab/data/storage"

	arrangov1 "github.com/smegg99/arrango/backend/gen/arrangov1"

	"github.com/smegg99/goptivum/backend/arrango"
	"github.com/smegg99/goptivum/backend/core/documents"
	"github.com/smegg99/goptivum/backend/core/plans"
	"github.com/smegg99/goptivum/backend/core/results"
	"github.com/smegg99/goptivum/backend/migrations"
	"github.com/smegg99/goptivum/document"
)

func school() *arrangov1.SchoolModel {
	return &arrangov1.SchoolModel{
		Name:     "Szkoła Testowa",
		Days:     []*arrangov1.Day{{Id: 1, Name: "Poniedziałek", PeriodCount: 2}},
		Periods:  []*arrangov1.Period{{Id: 2, Name: "08:00-08:45"}},
		Teachers: []*arrangov1.Teacher{{Id: 3, Name: "Anna Kowalska"}},
		Lessons:  []*arrangov1.LessonInstance{{Id: 4, TeacherId: 3, Duration: 1}},
	}
}

func solved() *arrangov1.ScheduleSnapshot {
	return &arrangov1.ScheduleSnapshot{
		Lessons: []*arrangov1.ScheduledLesson{
			{LessonId: 4, Placement: &arrangov1.Placement{DayId: 1, StartPeriod: 2}},
		},
	}
}

type fixture struct {
	conn   *sql.DB
	index  *plans.Repository
	store  *documents.Store
	planID int64
}

// newFixture is a plan at version 1 holding one unsolved school.
func newFixture(t *testing.T) fixture {
	t.Helper()
	conn := db.TestDB(t, migrations.FS)
	index := plans.NewRepository(conn, db.SQLite, plans.Caps{BackupsKeep: 100, ResultsKeep: 20})
	store := documents.NewStore(storage.NewLocalDir(t.TempDir(), "/objects"))

	encoded, err := document.Encode(document.New(school()))
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	key, err := store.Put(t.Context(), encoded)
	if err != nil {
		t.Fatalf("put: %v", err)
	}
	plan, _, err := index.Create(t.Context(), plans.Plan{
		OwnerIdentityID: "owner", Name: "Plan 2027", SchoolYear: "2026/2027",
	}, plans.Upload{ObjectKey: key, SizeBytes: encoded.Size, CreatedBy: "owner"})
	if err != nil {
		t.Fatalf("create plan: %v", err)
	}
	return fixture{conn: conn, index: index, store: store, planID: plan.ID}
}

// job opens the solve a result belongs to.
func (f fixture) job(t *testing.T, version int64) int64 {
	t.Helper()
	if _, err := f.conn.Exec(`insert into arrango_nodes
		(id, label, address, fingerprint, paired_at, solves_at_once, workers_per_solve)
		values (1, 'node', '127.0.0.1:1', 'fingerprint', ?, 1, 4) on conflict (id) do nothing`, time.Now().UTC()); err != nil {
		t.Fatalf("pair: %v", err)
	}
	id := time.Now().UnixNano()
	if _, err := f.conn.Exec(`insert into solve_jobs
		(id, plan_id, plan_version, node_id, status, num_workers, started_by, created_at)
		values (?, ?, ?, 1, 'running', 4, 'owner', ?)`, id, f.planID, version, time.Now().UTC()); err != nil {
		t.Fatalf("job: %v", err)
	}
	return id
}

func (f fixture) found(t *testing.T, version int64) plans.NewResult {
	quality := 91.0
	return plans.NewResult{
		PlanID: f.planID, JobID: f.job(t, version), SourceVersion: version,
		StartedBy: "owner", StartedByName: "Anna Kowalska", Quality: &quality, Verdict: "TIER_PRISTINE",
	}
}

// A result is a whole document: the school the solve read with the timetable
// it found. The main file gains no version and the version solved is untouched.
func TestKeepStoresTheSchoolWithItsTimetable(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	kept, err := results.Keep(ctx, f.index, f.store, solved(), f.found(t, 1), 0)
	if err != nil {
		t.Fatalf("keep: %v", err)
	}
	if kept.SourceVersion != 1 || kept.ObjectKey == "" || kept.SizeBytes <= 0 ||
		kept.StartedByName != "Anna Kowalska" || kept.Quality == nil || *kept.Quality != 91 {
		t.Errorf("result = %+v", kept)
	}
	doc, err := f.store.Load(ctx, kept.ObjectKey)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(doc.GetSchedule().GetLessons()) != 1 || doc.GetModel().GetName() != "Szkoła Testowa" {
		t.Errorf("the result holds %d placements of %q", len(doc.GetSchedule().GetLessons()), doc.GetModel().GetName())
	}
	// The counter travels, so an edit of the result's school cannot reuse an id.
	if doc.GetNextArrangoId() <= 4 {
		t.Errorf("next_arrango_id = %d, want more than the ids in use", doc.GetNextArrangoId())
	}

	if current, err := f.index.Current(ctx, f.planID); err != nil || current != 1 {
		t.Errorf("current = %d, %v: a solve wrote the main file", current, err)
	}
	original, err := f.index.Version(ctx, f.planID, 1)
	if err != nil {
		t.Fatalf("version 1: %v", err)
	}
	if before, err := f.store.Load(ctx, original.ObjectKey); err != nil || before.GetSchedule() != nil {
		t.Errorf("the version solved was rewritten: %v", err)
	}
	if listed, err := f.index.Results(ctx, f.planID); err != nil || len(listed) != 1 || listed[0].ID != kept.ID {
		t.Errorf("results = %+v, %v", listed, err)
	}
}

// Editing while a solve runs loses nothing: the result carries the school it
// was solved from, whatever the main file became.
func TestKeepStoresAResultAfterThePlanMovedOn(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	add := f.found(t, 1)

	edited := school()
	edited.Name = "Szkoła Testowa, poprawiona"
	encoded, err := document.Encode(document.New(edited))
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	key, err := f.store.Put(ctx, encoded)
	if err != nil {
		t.Fatalf("put: %v", err)
	}
	if _, err := f.index.AddVersion(ctx, plans.Upload{
		PlanID: f.planID, Parent: 1, ObjectKey: key, SizeBytes: encoded.Size, CreatedBy: "owner",
	}); err != nil {
		t.Fatalf("the edit: %v", err)
	}

	kept, err := results.Keep(ctx, f.index, f.store, solved(), add, 0)
	if err != nil {
		t.Fatalf("keep after an edit: %v", err)
	}
	doc, err := f.store.Load(ctx, kept.ObjectKey)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if doc.GetModel().GetName() != "Szkoła Testowa" {
		t.Errorf("the result holds %q, want the school it was solved from", doc.GetModel().GetName())
	}
	if current, err := f.index.Current(ctx, f.planID); err != nil || current != 2 {
		t.Errorf("current = %d, %v, want the edit still newest", current, err)
	}
}

func TestKeepRefusesNothingOrAMissingVersion(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	if _, err := results.Keep(ctx, f.index, f.store, nil, f.found(t, 1), 0); err == nil {
		t.Error("a missing timetable was kept")
	}
	if _, err := results.Keep(ctx, f.index, f.store, solved(), f.found(t, 99), 0); err == nil {
		t.Error("a timetable was kept against a version that does not exist")
	}
	if _, err := results.Keep(ctx, f.index, f.store, solved(), f.found(t, 1), 99); err == nil {
		t.Error("a timetable was kept against a result that does not exist")
	}
	if listed, err := f.index.Results(ctx, f.planID); err != nil || len(listed) != 0 {
		t.Errorf("results = %+v, %v, want none", listed, err)
	}
}

// A solve that read a result keeps its own result from that result's document:
// the version the result came from may be gone, and the result may be in
// Trash by the time the solve ends.
func TestASolveOfAResultKeepsTheResultsSchool(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	source, err := results.Keep(ctx, f.index, f.store, solved(), f.found(t, 1), 0)
	if err != nil {
		t.Fatalf("keep the source: %v", err)
	}
	sourceDoc, err := f.store.Load(ctx, source.ObjectKey)
	if err != nil {
		t.Fatalf("load the source: %v", err)
	}

	// The backups cap removed version 1, and the owner moved the result to Trash.
	if _, err := f.conn.Exec(`delete from plan_versions where plan_id = ? and version = 1`, f.planID); err != nil {
		t.Fatalf("remove version 1: %v", err)
	}
	if err := f.index.TrashResult(ctx, f.planID, source.ID); err != nil {
		t.Fatalf("trash the source: %v", err)
	}

	improved := &arrangov1.ScheduleSnapshot{Lessons: []*arrangov1.ScheduledLesson{
		{LessonId: 4, Placement: &arrangov1.Placement{DayId: 1, StartPeriod: 1}},
	}}
	jobID := f.job(t, 1)
	id, err := results.Persist(f.index, f.store)(ctx, arrango.Job{
		ID: jobID, PlanID: f.planID, PlanVersion: 1, SourceResultID: &source.ID,
		StartedBy: arrango.Starter{ID: "owner", Name: "Anna Kowalska"},
	}, arrango.Result{HasSchedule: true, Snapshot: improved})
	if err != nil {
		t.Fatalf("persist: %v", err)
	}
	kept, err := f.index.Result(ctx, f.planID, id)
	if err != nil {
		t.Fatalf("result: %v", err)
	}
	if kept.SourceVersion != 1 || kept.JobID != jobID {
		t.Errorf("result = %+v, want version 1's school from job %d", kept, jobID)
	}
	doc, err := f.store.Load(ctx, kept.ObjectKey)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if !proto.Equal(doc.GetModel(), sourceDoc.GetModel()) || doc.GetNextArrangoId() != sourceDoc.GetNextArrangoId() {
		t.Errorf("the result holds %v, want the source result's school", doc.GetModel())
	}
	if !proto.Equal(doc.GetSchedule(), improved) {
		t.Errorf("the result holds %v, want the improved timetable", doc.GetSchedule())
	}
}

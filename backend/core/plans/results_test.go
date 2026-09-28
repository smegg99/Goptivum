// core/plans/results_test.go

package plans_test

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/smegg99/goptivum/backend/core/plans"
)

// solveJob opens a job row the way the solving client does, on a node it
// pairs on first use, and returns its id.
func solveJob(t *testing.T, conn *sql.DB, planID, version int64, status string) int64 {
	t.Helper()
	if _, err := conn.Exec(`insert into arrango_nodes
		(id, label, address, fingerprint, paired_at, solves_at_once, workers_per_solve)
		values (1, 'node', '127.0.0.1:1', 'fingerprint', ?, 1, 4) on conflict (id) do nothing`, time.Now().UTC()); err != nil {
		t.Fatalf("pair a node: %v", err)
	}
	id := time.Now().UnixNano()
	if _, err := conn.Exec(`insert into solve_jobs
		(id, plan_id, plan_version, node_id, status, num_workers, started_by, created_at)
		values (?, ?, ?, 1, ?, 4, 'identity-1', ?)`, id, planID, version, status, time.Now().UTC()); err != nil {
		t.Fatalf("open a job: %v", err)
	}
	return id
}

// addResult keeps a timetable of version 1 under its own object, the way a
// finished solve does.
func addResult(t *testing.T, repo *plans.Repository, conn *sql.DB, planID int64, key string) plans.Result {
	t.Helper()
	// Results are ordered by when they were kept, to the microsecond.
	time.Sleep(2 * time.Millisecond)
	quality := 87.5
	result, err := repo.AddResult(context.Background(), plans.NewResult{
		PlanID: planID, JobID: solveJob(t, conn, planID, 1, "done"), SourceVersion: 1,
		ObjectKey: key, SizeBytes: 42, StartedBy: "identity-1", StartedByName: "Anna Kowalska",
		Quality: &quality, Verdict: "TIER_WARNINGS",
	})
	if err != nil {
		t.Fatalf("add result %s: %v", key, err)
	}
	return result
}

func resultKeys(t *testing.T, repo *plans.Repository, planID int64) []string {
	t.Helper()
	listed, err := repo.Results(context.Background(), planID)
	if err != nil {
		t.Fatalf("results: %v", err)
	}
	out := []string{}
	for _, result := range listed {
		out = append(out, result.ObjectKey)
	}
	return out
}

func setResultsKeep(t *testing.T, repo *plans.Repository, planID int64, keep int) plans.Plan {
	t.Helper()
	plan, err := repo.Update(context.Background(), planID, plans.Change{ResultsKeep: &keep})
	if err != nil {
		t.Fatalf("set results cap %d: %v", keep, err)
	}
	return plan
}

func TestAResultRecordsWhatItCameFrom(t *testing.T) {
	repo, conn := repositoryWithConn(t)
	plan := newPlan(t, repo, "identity-1", "Plan 2027")

	kept := addResult(t, repo, conn, plan.ID, "plans/result-1")
	read, err := repo.Result(context.Background(), plan.ID, kept.ID)
	if err != nil {
		t.Fatalf("result: %v", err)
	}
	if !reflect.DeepEqual(read, kept) {
		t.Errorf("read back %+v, kept %+v", read, kept)
	}
	if read.SourceVersion != 1 || read.StartedBy != "identity-1" || read.StartedByName != "Anna Kowalska" ||
		read.Quality == nil || *read.Quality != 87.5 || read.Verdict != "TIER_WARNINGS" ||
		read.Pinned || read.Name != "" || read.CreatedAt.IsZero() {
		t.Errorf("result = %+v", read)
	}
	if _, err := repo.Result(context.Background(), plan.ID+1, kept.ID); !errors.Is(err, plans.ErrNoResult) {
		t.Errorf("another plan's result: err = %v, want ErrNoResult", err)
	}
}

// The cap keeps the pinned results and the newest unpinned ones; the rest go
// for good, with their objects recorded for the sweep.
func TestTheResultsCapKeepsThePinnedAndTheNewest(t *testing.T) {
	ctx := context.Background()
	repo, conn := repositoryWithConn(t)
	plan := newPlan(t, repo, "identity-1", "Plan 2027")
	if capped := setResultsKeep(t, repo, plan.ID, 2); capped.ResultsKeep != 2 {
		t.Fatalf("results cap = %d, want 2", capped.ResultsKeep)
	}

	first := addResult(t, repo, conn, plan.ID, "plans/r1")
	pinned := true
	if _, err := repo.UpdateResult(ctx, plan.ID, first.ID, plans.ResultChange{Pinned: &pinned}); err != nil {
		t.Fatalf("pin: %v", err)
	}
	addResult(t, repo, conn, plan.ID, "plans/r2")
	addResult(t, repo, conn, plan.ID, "plans/r3")
	addResult(t, repo, conn, plan.ID, "plans/r4")

	if got, want := resultKeys(t, repo, plan.ID), []string{"plans/r4", "plans/r3", "plans/r1"}; !reflect.DeepEqual(got, want) {
		t.Errorf("results = %v, want %v", got, want)
	}
	if got := garbage(t, conn); !reflect.DeepEqual(got, []string{"plans/r2"}) {
		t.Errorf("garbage = %v, want the removed result's object", got)
	}
}

// A result in Trash is neither counted nor removed by the cap, is listed in
// Trash while its plan is live, comes back on restore, and goes for good once
// its time in Trash is up.
func TestAResultGoesToTrashAndBack(t *testing.T) {
	ctx := context.Background()
	repo, conn := repositoryWithConn(t)
	plan := newPlan(t, repo, "identity-1", "Plan 2027")
	setResultsKeep(t, repo, plan.ID, 1)

	trashed := addResult(t, repo, conn, plan.ID, "plans/trashed")
	if err := repo.TrashResult(ctx, plan.ID, trashed.ID); err != nil {
		t.Fatalf("trash: %v", err)
	}
	if err := repo.TrashResult(ctx, plan.ID, trashed.ID); !errors.Is(err, plans.ErrNoResult) {
		t.Errorf("trash twice: err = %v, want ErrNoResult", err)
	}
	if _, err := repo.Result(ctx, plan.ID, trashed.ID); !errors.Is(err, plans.ErrNoResult) {
		t.Errorf("a result in Trash reads: %v", err)
	}
	addResult(t, repo, conn, plan.ID, "plans/newer")
	addResult(t, repo, conn, plan.ID, "plans/newest")
	if got, want := resultKeys(t, repo, plan.ID), []string{"plans/newest"}; !reflect.DeepEqual(got, want) {
		t.Errorf("results = %v, want %v", got, want)
	}

	listed, err := repo.TrashedResults(ctx, "identity-1")
	if err != nil {
		t.Fatalf("trashed results: %v", err)
	}
	if len(listed) != 1 || listed[0].Result.ID != trashed.ID || listed[0].Plan.ID != plan.ID ||
		listed[0].Plan.Name != "Plan 2027" || listed[0].Result.DeletedAt.IsZero() {
		t.Fatalf("trashed = %+v", listed)
	}
	if other, err := repo.TrashedResults(ctx, "identity-2"); err != nil || len(other) != 0 {
		t.Errorf("another owner's Trash = %+v, %v", other, err)
	}

	restored, err := repo.RestoreResult(ctx, plan.ID, trashed.ID)
	if err != nil || !restored.DeletedAt.IsZero() {
		t.Fatalf("restore = %+v, %v", restored, err)
	}
	if _, err := repo.RestoreResult(ctx, plan.ID, trashed.ID); err != nil {
		t.Errorf("a second restore: %v", err)
	}
	if got, want := resultKeys(t, repo, plan.ID), []string{"plans/newest", "plans/trashed"}; !reflect.DeepEqual(got, want) {
		t.Errorf("results after restore = %v, want %v", got, want)
	}

	if err := repo.TrashResult(ctx, plan.ID, trashed.ID); err != nil {
		t.Fatalf("trash again: %v", err)
	}
	if purged, err := repo.PurgeResults(ctx, time.Now().Add(29*24*time.Hour), 30); err != nil || purged != 0 {
		t.Errorf("purge before its time = %d, %v", purged, err)
	}
	if purged, err := repo.PurgeResults(ctx, time.Now().Add(31*24*time.Hour), 30); err != nil || purged != 1 {
		t.Errorf("purge = %d, %v, want 1", purged, err)
	}
	if !slices.Contains(garbage(t, conn), "plans/trashed") {
		t.Errorf("garbage = %v, want the purged result's object", garbage(t, conn))
	}
	if _, err := repo.RestoreResult(ctx, plan.ID, trashed.ID); !errors.Is(err, plans.ErrNoResult) {
		t.Errorf("restore after purge: err = %v, want ErrNoResult", err)
	}
}

// The results of a plan in Trash are part of that plan: not listed on their
// own, and removed with it, objects recorded.
func TestTheResultsOfATrashedPlanGoWithIt(t *testing.T) {
	ctx := context.Background()
	repo, conn := repositoryWithConn(t)
	plan := newPlan(t, repo, "identity-1", "Plan 2027")
	result := addResult(t, repo, conn, plan.ID, "plans/with-the-plan")
	if err := repo.TrashResult(ctx, plan.ID, result.ID); err != nil {
		t.Fatalf("trash result: %v", err)
	}
	if err := repo.Trash(ctx, plan.ID); err != nil {
		t.Fatalf("trash plan: %v", err)
	}
	if listed, err := repo.TrashedResults(ctx, "identity-1"); err != nil || len(listed) != 0 {
		t.Errorf("results of a trashed plan listed: %+v, %v", listed, err)
	}
	if purged, err := repo.Purge(ctx, time.Now().Add(31*24*time.Hour), 30); err != nil || purged != 1 {
		t.Fatalf("purge = %d, %v", purged, err)
	}
	if !slices.Contains(garbage(t, conn), "plans/with-the-plan") {
		t.Errorf("garbage = %v, want the result's object", garbage(t, conn))
	}
}

func TestAResultIsNamedAndPinned(t *testing.T) {
	ctx := context.Background()
	repo, conn := repositoryWithConn(t)
	plan := newPlan(t, repo, "identity-1", "Plan 2027")
	result := addResult(t, repo, conn, plan.ID, "plans/named")

	name, pinned := "  Wersja z piątkiem  ", true
	named, err := repo.UpdateResult(ctx, plan.ID, result.ID, plans.ResultChange{Name: &name, Pinned: &pinned})
	if err != nil || named.Name != "Wersja z piątkiem" || !named.Pinned {
		t.Fatalf("update = %+v, %v", named, err)
	}
	if read, err := repo.Result(ctx, plan.ID, result.ID); err != nil || read.Name != named.Name || !read.Pinned {
		t.Errorf("read back %+v, %v", read, err)
	}
	blank := "   "
	unnamed, err := repo.UpdateResult(ctx, plan.ID, result.ID, plans.ResultChange{Name: &blank})
	if err != nil || unnamed.Name != "" || !unnamed.Pinned {
		t.Errorf("a blank name = %+v, %v, want the name removed and the pin kept", unnamed, err)
	}
	long := strings.Repeat("ż", plans.MaxNameLength+1)
	var refusal *plans.Invalid
	if _, err := repo.UpdateResult(ctx, plan.ID, result.ID, plans.ResultChange{Name: &long}); !errors.As(err, &refusal) ||
		refusal.Field != "name" || refusal.Code != "too_long" {
		t.Errorf("a long name: err = %v", err)
	}
	if err := repo.TrashResult(ctx, plan.ID, result.ID); err != nil {
		t.Fatalf("trash: %v", err)
	}
	if _, err := repo.UpdateResult(ctx, plan.ID, result.ID, plans.ResultChange{Name: &name}); !errors.Is(err, plans.ErrNoResult) {
		t.Errorf("rename in Trash: err = %v, want ErrNoResult", err)
	}
}

func TestResultsKeepOutsideItsRangeIsRefused(t *testing.T) {
	repo := repository(t)
	plan := newPlan(t, repo, "identity-1", "Plan 2027")
	for _, keep := range []int{-1, plans.MaxResultsKeep + 1} {
		var refusal *plans.Invalid
		if _, err := repo.Update(context.Background(), plan.ID, plans.Change{ResultsKeep: &keep}); !errors.As(err, &refusal) ||
			refusal.Field != "results_keep" || refusal.Code != "invalid" {
			t.Errorf("results cap %d: err = %v", keep, err)
		}
	}
	if dropped := setResultsKeep(t, repo, plan.ID, 0); dropped.ResultsKeep != 0 || repo.DefaultResultsKeep() != 20 {
		t.Errorf("dropped cap = %d, default %d", dropped.ResultsKeep, repo.DefaultResultsKeep())
	}
}

// A result shares its object with any version or result of the same content,
// so the sweep leaves an object a result still names.
func TestTheSweepLeavesAnObjectAResultNames(t *testing.T) {
	ctx := context.Background()
	repo, conn := repositoryWithConn(t)
	plan := newPlan(t, repo, "identity-1", "Plan 2027")
	addResult(t, repo, conn, plan.ID, "plans/shared")
	wait(t, conn, "plans/shared", time.Now().Add(-2*time.Hour))

	store := &removals{}
	if deleted, err := repo.Sweep(ctx, time.Now(), store.remove); err != nil || deleted != 0 {
		t.Errorf("sweep = %d, %v, want nothing deleted", deleted, err)
	}
	if len(store.keys) != 0 {
		t.Errorf("the sweep deleted %v, which a result names", store.keys)
	}
}

// The backups cap leaves a version a pending or running solve reads, and
// removes it once the solve is over and the next version lands.
func TestTheBackupsCapLeavesAVersionASolveReads(t *testing.T) {
	repo, conn := repositoryWithConn(t)
	plan := newPlan(t, repo, "identity-1", "Plan 2027")
	setKeep(t, repo, plan.ID, 1)
	upload(t, repo, plan.ID, 1)
	upload(t, repo, plan.ID, 2)
	running := solveJob(t, conn, plan.ID, 2, "running")
	pending := solveJob(t, conn, plan.ID, 3, "pending")
	solveJob(t, conn, plan.ID, 1, "done")

	for parent := int64(3); parent < 5; parent++ {
		upload(t, repo, plan.ID, parent)
	}
	if got, want := numbers(t, repo, plan.ID), []int64{2, 3, 4, 5}; !reflect.DeepEqual(got, want) {
		t.Errorf("timeline = %v, want %v: versions 2 and 3 are being solved", got, want)
	}

	if _, err := conn.Exec(`update solve_jobs set status = 'done' where id in (?, ?)`, running, pending); err != nil {
		t.Fatalf("finish the jobs: %v", err)
	}
	upload(t, repo, plan.ID, 5)
	if got, want := numbers(t, repo, plan.ID), []int64{5, 6}; !reflect.DeepEqual(got, want) {
		t.Errorf("timeline = %v, want %v once the solves are over", got, want)
	}
	if !slices.Contains(garbage(t, conn), fmt.Sprintf("plans/%d/v2", plan.ID)) {
		t.Errorf("garbage = %v, want version 2's object", garbage(t, conn))
	}
}

// solveJobReading opens a job that reads a result instead of a version.
func solveJobReading(t *testing.T, conn *sql.DB, planID, resultID int64, status string) int64 {
	t.Helper()
	id := solveJob(t, conn, planID, 1, status)
	if _, err := conn.Exec(`update solve_jobs set source_result_id = ? where id = ?`, resultID, id); err != nil {
		t.Fatalf("name the result the job reads: %v", err)
	}
	return id
}

// A result a pending or running solve reads is counted by the cap but stays
// until the solve ends: the solve's own result is built from it.
func TestTheResultsCapLeavesAResultASolveReads(t *testing.T) {
	repo, conn := repositoryWithConn(t)
	plan := newPlan(t, repo, "identity-1", "Plan 2027")
	setResultsKeep(t, repo, plan.ID, 1)

	read := addResult(t, repo, conn, plan.ID, "plans/read")
	running := solveJobReading(t, conn, plan.ID, read.ID, "running")
	addResult(t, repo, conn, plan.ID, "plans/r2")
	addResult(t, repo, conn, plan.ID, "plans/r3")
	if got, want := resultKeys(t, repo, plan.ID), []string{"plans/r3", "plans/read"}; !reflect.DeepEqual(got, want) {
		t.Errorf("results = %v, want %v: the solve reads plans/read", got, want)
	}
	if got := garbage(t, conn); !reflect.DeepEqual(got, []string{"plans/r2"}) {
		t.Errorf("garbage = %v, want only plans/r2", got)
	}

	if _, err := conn.Exec(`update solve_jobs set status = 'failed' where id = ?`, running); err != nil {
		t.Fatalf("finish the job: %v", err)
	}
	addResult(t, repo, conn, plan.ID, "plans/r4")
	if got, want := resultKeys(t, repo, plan.ID), []string{"plans/r4"}; !reflect.DeepEqual(got, want) {
		t.Errorf("results = %v, want %v once the solve is over", got, want)
	}
	if !slices.Contains(garbage(t, conn), "plans/read") {
		t.Errorf("garbage = %v, want plans/read's object", garbage(t, conn))
	}
}

// Nor does the Trash purge remove a result a solve reads, and the solve still
// finds it there.
func TestThePurgeLeavesAResultASolveReads(t *testing.T) {
	ctx := context.Background()
	repo, conn := repositoryWithConn(t)
	plan := newPlan(t, repo, "identity-1", "Plan 2027")
	read := addResult(t, repo, conn, plan.ID, "plans/read")
	pending := solveJobReading(t, conn, plan.ID, read.ID, "pending")
	if err := repo.TrashResult(ctx, plan.ID, read.ID); err != nil {
		t.Fatalf("trash: %v", err)
	}

	later := time.Now().Add(31 * 24 * time.Hour)
	if purged, err := repo.PurgeResults(ctx, later, 30); err != nil || purged != 0 {
		t.Errorf("purge = %d, %v, want nothing while the solve reads it", purged, err)
	}
	source, err := repo.SourceResult(ctx, plan.ID, read.ID)
	if err != nil || source.ObjectKey != "plans/read" || source.DeletedAt.IsZero() {
		t.Errorf("source = %+v, %v, want the result in Trash", source, err)
	}
	if _, err := repo.SourceResult(ctx, plan.ID+1, read.ID); !errors.Is(err, plans.ErrNoResult) {
		t.Errorf("another plan's result: err = %v, want ErrNoResult", err)
	}

	if _, err := conn.Exec(`update solve_jobs set status = 'done' where id = ?`, pending); err != nil {
		t.Fatalf("finish the job: %v", err)
	}
	if purged, err := repo.PurgeResults(ctx, later, 30); err != nil || purged != 1 {
		t.Errorf("purge = %d, %v, want 1 once the solve is over", purged, err)
	}
	if _, err := repo.SourceResult(ctx, plan.ID, read.ID); !errors.Is(err, plans.ErrNoResult) {
		t.Errorf("a purged result: err = %v, want ErrNoResult", err)
	}
}

// A result solved on a device has no job: it records the device and when the
// device solved it, and reads back the same.
func TestAResultSolvedOnADeviceHasNoJob(t *testing.T) {
	repo, _ := repositoryWithConn(t)
	plan := newPlan(t, repo, "identity-1", "Plan 2027")
	solved := time.Date(2026, 9, 20, 10, 15, 0, 0, time.UTC)

	kept, err := repo.AddResult(context.Background(), plans.NewResult{
		PlanID: plan.ID, SourceVersion: 1, ObjectKey: "device-object", SizeBytes: 42,
		StartedBy: "identity-1", StartedByName: "Anna Kowalska",
		Device: "Laptop Anny", UploadKey: "device-result-000001", CreatedAt: solved,
	})
	if err != nil {
		t.Fatalf("add: %v", err)
	}
	read, err := repo.Result(context.Background(), plan.ID, kept.ID)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if read.JobID != 0 || read.Device != "Laptop Anny" || !read.CreatedAt.Equal(solved) {
		t.Errorf("read = %+v, want no job, the device and the solve time", read)
	}
	if !reflect.DeepEqual(read, kept) {
		t.Errorf("read %+v, kept %+v", read, kept)
	}
}

// An upload key answers a retry with the result it kept, and refuses the same
// key with another object or source version.
func TestAnUploadKeyKeepsOneResult(t *testing.T) {
	repo, _ := repositoryWithConn(t)
	plan := newPlan(t, repo, "identity-1", "Plan 2027")
	upload := plans.NewResult{
		PlanID: plan.ID, SourceVersion: 1, ObjectKey: "device-object", SizeBytes: 42,
		StartedBy: "identity-1", Device: "Laptop Anny", UploadKey: "device-result-000001",
	}
	first, err := repo.AddResult(context.Background(), upload)
	if err != nil {
		t.Fatalf("add: %v", err)
	}
	again, err := repo.AddResult(context.Background(), upload)
	if err != nil || again.ID != first.ID {
		t.Fatalf("retry = %+v, %v, want result %d", again, err, first.ID)
	}
	for _, changed := range []plans.NewResult{
		func() plans.NewResult { other := upload; other.ObjectKey = "another-object"; return other }(),
		func() plans.NewResult { other := upload; other.SourceVersion = 2; return other }(),
	} {
		var invalid *plans.Invalid
		if _, err := repo.AddResult(context.Background(), changed); !errors.As(err, &invalid) ||
			invalid.Field != "upload_key" || invalid.Code != "reused" {
			t.Errorf("reused key = %v, want upload_key: reused", err)
		}
	}
	if keys := resultKeys(t, repo, plan.ID); !slices.Equal(keys, []string{"device-object"}) {
		t.Errorf("results = %v, want one", keys)
	}

	// Another plan's key is its own.
	other := newPlan(t, repo, "identity-1", "Plan 2028")
	elsewhere := upload
	elsewhere.PlanID = other.ID
	if _, err := repo.AddResult(context.Background(), elsewhere); err != nil {
		t.Errorf("the same key on another plan: %v", err)
	}
}

// core/plans/storage_test.go

package plans_test

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/smegg99/goptivum/backend/core/plans"
)

// A new server default applies at once: to what a plan without its own cap
// reports, and to the next version it keeps.
func TestNewCapsApplyToPlansWithoutTheirOwnAtOnce(t *testing.T) {
	repo := repository(t)
	plan := newPlan(t, repo, "identity-1", "Plan 2027")

	repo.SetCaps(plans.Caps{BackupsKeep: 2, ResultsKeep: 3})
	if repo.DefaultBackupsKeep() != 2 || repo.DefaultResultsKeep() != 3 {
		t.Fatalf("defaults = %d, %d; want 2, 3", repo.DefaultBackupsKeep(), repo.DefaultResultsKeep())
	}
	for parent := int64(1); parent < 5; parent++ {
		upload(t, repo, plan.ID, parent)
	}
	if got, want := numbers(t, repo, plan.ID), []int64{3, 4, 5}; !reflect.DeepEqual(got, want) {
		t.Errorf("timeline = %v, want the main file and 2 backups %v", got, want)
	}
}

// Emptying Trash takes every owner's projects, checkpoints and results that
// are in Trash, whatever their date, and nothing else.
func TestPurgeTrashWithNoDaysEmptiesEveryOwnersTrash(t *testing.T) {
	ctx := context.Background()
	repo, conn := repositoryWithConn(t)
	mine := newPlan(t, repo, "identity-1", "Do usunięcia")
	theirs := newPlan(t, repo, "identity-2", "Cudzy")
	live := newPlan(t, repo, "identity-2", "Plan 2027")
	for _, id := range []int64{mine.ID, theirs.ID} {
		if err := repo.Trash(ctx, id); err != nil {
			t.Fatalf("trash: %v", err)
		}
	}
	upload(t, repo, live.ID, 1)
	if _, err := repo.NameCheckpoint(ctx, live.ID, 1, "przed zmianą"); err != nil {
		t.Fatalf("name: %v", err)
	}
	if err := repo.DeleteCheckpoint(ctx, live.ID, 1); err != nil {
		t.Fatalf("delete checkpoint: %v", err)
	}
	kept := addResult(t, repo, conn, live.ID, "plans/result-kept")
	gone := addResult(t, repo, conn, live.ID, "plans/result-gone")
	if err := repo.TrashResult(ctx, live.ID, gone.ID); err != nil {
		t.Fatalf("trash result: %v", err)
	}

	purged, err := repo.PurgeTrash(ctx, time.Now(), 0)
	if err != nil {
		t.Fatalf("purge: %v", err)
	}
	if purged != (plans.Purged{Plans: 2, Checkpoints: 1, Results: 1}) {
		t.Errorf("purged = %+v, want 2 plans, 1 checkpoint, 1 result", purged)
	}
	for _, owner := range []string{"identity-1", "identity-2"} {
		if left, err := repo.Trashed(ctx, owner); err != nil || len(left) != 0 {
			t.Errorf("%s still has %+v in Trash (%v)", owner, left, err)
		}
	}
	if results := resultKeys(t, repo, live.ID); !reflect.DeepEqual(results, []string{kept.ObjectKey}) {
		t.Errorf("results = %v, want only the one not in Trash", results)
	}
	if got := numbers(t, repo, live.ID); !reflect.DeepEqual(got, []int64{1, 2}) {
		t.Errorf("timeline = %v: the unnamed version stays under the cap", got)
	}
	if checkpoints, err := repo.TrashedCheckpoints(ctx, "identity-2"); err != nil || len(checkpoints) != 0 {
		t.Errorf("checkpoints in Trash = %+v, %v", checkpoints, err)
	}

	// With the server's days, nothing deleted just now is due.
	if err := repo.Trash(ctx, live.ID); err != nil {
		t.Fatalf("trash: %v", err)
	}
	if purged, err := repo.PurgeTrash(ctx, time.Now(), 30); err != nil || purged != (plans.Purged{}) {
		t.Errorf("purge after 30 days = %+v, %v; want nothing", purged, err)
	}
}

// Trimming applies each plan's cap in force now, every owner's and those in
// Trash too, and never touches a checkpoint or the main file.
func TestTrimBackupsAppliesEveryCapNow(t *testing.T) {
	ctx := context.Background()
	repo, conn := repositoryWithConn(t)
	mine := newPlan(t, repo, "identity-1", "Plan 2027")
	theirs := newPlan(t, repo, "identity-2", "Cudzy")
	own := newPlan(t, repo, "identity-2", "Z własnym")
	for _, plan := range []plans.Plan{mine, theirs, own} {
		for parent := int64(1); parent < 5; parent++ {
			upload(t, repo, plan.ID, parent)
		}
	}
	if _, err := repo.NameCheckpoint(ctx, mine.ID, 1, "początek"); err != nil {
		t.Fatalf("name: %v", err)
	}
	setKeep(t, repo, own.ID, 3)
	if err := repo.Trash(ctx, theirs.ID); err != nil {
		t.Fatalf("trash: %v", err)
	}
	before := len(garbage(t, conn))

	repo.SetCaps(plans.Caps{BackupsKeep: 1, ResultsKeep: 20})
	removed, err := repo.TrimBackups(ctx)
	if err != nil {
		t.Fatalf("trim: %v", err)
	}
	// mine: 2, 3 go (1 is a checkpoint, 4 the backup kept); theirs: 1, 2, 3
	// go; own keeps its own 3 backups, so only 1 goes.
	if removed != 6 {
		t.Errorf("removed = %d, want 6", removed)
	}
	if got := numbers(t, repo, mine.ID); !reflect.DeepEqual(got, []int64{1, 4, 5}) {
		t.Errorf("mine = %v, want the checkpoint, 1 backup and the main file", got)
	}
	if got := numbers(t, repo, own.ID); !reflect.DeepEqual(got, []int64{2, 3, 4, 5}) {
		t.Errorf("own = %v, want its own cap of 3 kept", got)
	}
	if after := len(garbage(t, conn)); after <= before {
		t.Errorf("garbage %d -> %d: the trimmed objects were not recorded", before, after)
	}
	if again, err := repo.TrimBackups(ctx); err != nil || again != 0 {
		t.Errorf("a second trim removed %d, %v; want 0", again, err)
	}
}

// Each project is split into its main file, its backups, its results and
// what it has in Trash; the totals count each stored object once.
func TestUsageSplitsEachProjectAndCountsObjectsOnce(t *testing.T) {
	ctx := context.Background()
	repo, conn := repositoryWithConn(t)
	plan := newPlan(t, repo, "identity-1", "Plan 2027")
	upload(t, repo, plan.ID, 1)
	upload(t, repo, plan.ID, 2)
	if _, err := repo.NameCheckpoint(ctx, plan.ID, 2, "przed zmianą"); err != nil {
		t.Fatalf("name: %v", err)
	}
	if err := repo.DeleteCheckpoint(ctx, plan.ID, 2); err != nil {
		t.Fatalf("delete checkpoint: %v", err)
	}
	addResult(t, repo, conn, plan.ID, "plans/result-1")
	trashed := addResult(t, repo, conn, plan.ID, "plans/result-2")
	if err := repo.TrashResult(ctx, plan.ID, trashed.ID); err != nil {
		t.Fatalf("trash result: %v", err)
	}
	// Another owner's project in Trash, created from the same first document.
	gone := newPlan(t, repo, "identity-2", "Cudzy")
	if err := repo.Trash(ctx, gone.ID); err != nil {
		t.Fatalf("trash: %v", err)
	}
	if err := repo.Discard(ctx, "plans/unnamed"); err != nil {
		t.Fatalf("discard: %v", err)
	}

	usage, err := repo.Usage(ctx)
	if err != nil {
		t.Fatalf("usage: %v", err)
	}
	if len(usage) != 2 {
		t.Fatalf("usage = %+v, want both owners' projects", usage)
	}
	first, second := usage[0], usage[1]
	if first.Plan.ID != plan.ID || second.Plan.ID != gone.ID {
		t.Fatalf("order = %d, %d; want the larger project first", first.Plan.ID, second.Plan.ID)
	}
	if first.Plan.OwnerIdentityID != "identity-1" || first.Plan.Name != "Plan 2027" || !first.Plan.DeletedAt.IsZero() {
		t.Errorf("first plan = %+v", first.Plan)
	}
	want := plans.Usage{
		Plan:     first.Plan,
		MainFile: plans.Files{Count: 1, Bytes: 10},
		Backups:  plans.Files{Count: 1, Bytes: 100},
		Results:  plans.Files{Count: 1, Bytes: 42},
		Trash:    plans.Files{Count: 2, Bytes: 52},
	}
	if first != want {
		t.Errorf("usage = %+v\nwant    %+v", first, want)
	}
	if second.Plan.DeletedAt.IsZero() || second.MainFile != (plans.Files{Count: 1, Bytes: 100}) ||
		second.Backups != (plans.Files{}) || second.Trash != (plans.Files{}) {
		t.Errorf("the project in Trash = %+v", second)
	}

	stored, err := repo.Stored(ctx)
	if err != nil {
		t.Fatalf("stored: %v", err)
	}
	// abcd (shared by both first versions), v2, v3 and the two results.
	if stored != (plans.Stored{Objects: 5, Bytes: 100 + 10 + 10 + 42 + 42, Waiting: 1}) {
		t.Errorf("stored = %+v", stored)
	}
}

// A key waiting in object_garbage that a row names again is not waiting.
func TestStoredCountsOnlyUnnamedObjectsAsWaiting(t *testing.T) {
	ctx := context.Background()
	repo := repository(t)
	newPlan(t, repo, "identity-1", "Plan 2027")
	if err := repo.Discard(ctx, firstUpload("identity-1").ObjectKey); err != nil {
		t.Fatalf("discard: %v", err)
	}
	if stored, err := repo.Stored(ctx); err != nil || stored.Waiting != 0 || stored.Objects != 1 {
		t.Errorf("stored = %+v, %v; want the named key not waiting", stored, err)
	}
}

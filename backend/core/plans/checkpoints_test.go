// core/plans/checkpoints_test.go

package plans_test

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/smegg99/goptivum/backend/core/plans"
)

func TestACheckpointIsNamedDeletedToTrashAndRestored(t *testing.T) {
	ctx := context.Background()
	repo := repository(t)
	plan := newPlan(t, repo, "identity-1", "Plan 2027")
	upload(t, repo, plan.ID, 1)

	named, err := repo.NameCheckpoint(ctx, plan.ID, 1, "  tydzień i klasy  ")
	if err != nil {
		t.Fatalf("name: %v", err)
	}
	if named.Version != 1 || named.Checkpoint != "tydzień i klasy" || !named.CheckpointDeletedAt.IsZero() {
		t.Errorf("named = %+v", named)
	}
	renamed, err := repo.NameCheckpoint(ctx, plan.ID, 1, "tydzień")
	if err != nil || renamed.Checkpoint != "tydzień" {
		t.Errorf("rename = %+v, %v", renamed, err)
	}

	if err := repo.DeleteCheckpoint(ctx, plan.ID, 1); err != nil {
		t.Fatalf("delete: %v", err)
	}
	inTrash, err := repo.Version(ctx, plan.ID, 1)
	if err != nil || inTrash.Checkpoint != "tydzień" || inTrash.CheckpointDeletedAt.IsZero() {
		t.Errorf("in Trash = %+v, %v; want the name kept and a deletion time", inTrash, err)
	}
	trashed, err := repo.TrashedCheckpoints(ctx, "identity-1")
	if err != nil || len(trashed) != 1 {
		t.Fatalf("trashed = %+v, %v", trashed, err)
	}
	if item := trashed[0]; item.Plan.ID != plan.ID || item.Plan.Name != "Plan 2027" ||
		item.Version != 1 || item.Name != "tydzień" || !item.DeletedAt.Equal(inTrash.CheckpointDeletedAt) {
		t.Errorf("trash item = %+v", item)
	}
	if theirs, err := repo.TrashedCheckpoints(ctx, "identity-2"); err != nil || len(theirs) != 0 {
		t.Errorf("another owner sees %+v, %v", theirs, err)
	}
	if err := repo.DeleteCheckpoint(ctx, plan.ID, 1); !errors.Is(err, plans.ErrNoVersion) {
		t.Errorf("a second delete = %v, want ErrNoVersion", err)
	}

	for attempt := 0; attempt < 2; attempt++ {
		restored, err := repo.RestoreCheckpoint(ctx, plan.ID, 1)
		if err != nil || restored.Checkpoint != "tydzień" || !restored.CheckpointDeletedAt.IsZero() {
			t.Errorf("restore %d = %+v, %v", attempt+1, restored, err)
		}
	}
	if trashed, err := repo.TrashedCheckpoints(ctx, "identity-1"); err != nil || len(trashed) != 0 {
		t.Errorf("after restore, Trash holds %+v, %v", trashed, err)
	}
}

func TestOnlyACheckpointCanBeDeletedOrRestored(t *testing.T) {
	ctx := context.Background()
	repo := repository(t)
	plan := newPlan(t, repo, "identity-1", "Plan 2027")

	if err := repo.DeleteCheckpoint(ctx, plan.ID, 1); !errors.Is(err, plans.ErrNoVersion) {
		t.Errorf("delete a plain version = %v, want ErrNoVersion", err)
	}
	if _, err := repo.RestoreCheckpoint(ctx, plan.ID, 1); !errors.Is(err, plans.ErrNoVersion) {
		t.Errorf("restore a plain version = %v, want ErrNoVersion", err)
	}
	if _, err := repo.NameCheckpoint(ctx, plan.ID, 9, "brak"); !errors.Is(err, plans.ErrNoVersion) {
		t.Errorf("name a missing version = %v, want ErrNoVersion", err)
	}
	for _, name := range []string{"", "   "} {
		_, err := repo.NameCheckpoint(ctx, plan.ID, 1, name)
		var refusal *plans.Invalid
		if !errors.As(err, &refusal) || refusal.Field != "name" || refusal.Code != "required" {
			t.Errorf("name %q = %v, want name: required", name, err)
		}
	}
}

// A plan in Trash takes its checkpoints with it: they are part of the project
// item, not items of their own.
func TestTheCheckpointsOfATrashedPlanAreNotListed(t *testing.T) {
	ctx := context.Background()
	repo := repository(t)
	plan := newPlan(t, repo, "identity-1", "Plan 2027")
	if _, err := repo.NameCheckpoint(ctx, plan.ID, 1, "start"); err != nil {
		t.Fatalf("name: %v", err)
	}
	if err := repo.DeleteCheckpoint(ctx, plan.ID, 1); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if err := repo.Trash(ctx, plan.ID); err != nil {
		t.Fatalf("trash: %v", err)
	}
	if trashed, err := repo.TrashedCheckpoints(ctx, "identity-1"); err != nil || len(trashed) != 0 {
		t.Errorf("trashed checkpoints = %+v, %v; want none while the plan is in Trash", trashed, err)
	}
}

// When a checkpoint's time in Trash is up its version becomes a plain backup,
// and the cap then removes it if it is older than the cap keeps.
func TestPurgeEndsExpiredCheckpointsAndTrimsTheirPlan(t *testing.T) {
	ctx := context.Background()
	repo, conn := repositoryWithConn(t)
	plan := newPlan(t, repo, "identity-1", "Plan 2027")
	setKeep(t, repo, plan.ID, 1)

	if _, err := repo.NameCheckpoint(ctx, plan.ID, 1, "start"); err != nil {
		t.Fatalf("name: %v", err)
	}
	upload(t, repo, plan.ID, 1)
	upload(t, repo, plan.ID, 2)
	if _, err := repo.NameCheckpoint(ctx, plan.ID, 2, "środek"); err != nil {
		t.Fatalf("name: %v", err)
	}
	upload(t, repo, plan.ID, 3)
	if err := repo.DeleteCheckpoint(ctx, plan.ID, 1); err != nil {
		t.Fatalf("delete: %v", err)
	}
	deleted, err := repo.Version(ctx, plan.ID, 1)
	if err != nil {
		t.Fatalf("version 1: %v", err)
	}
	if got, want := numbers(t, repo, plan.ID), []int64{1, 2, 3, 4}; !reflect.DeepEqual(got, want) {
		t.Fatalf("timeline = %v, want %v", got, want)
	}

	const days = 30
	removesAt := plans.RemovesAt(deleted.CheckpointDeletedAt, days)
	if ended, err := repo.PurgeCheckpoints(ctx, removesAt.Add(-time.Second), days); err != nil || ended != 0 {
		t.Errorf("a day early: ended %d, %v", ended, err)
	}
	ended, err := repo.PurgeCheckpoints(ctx, removesAt, days)
	if err != nil || ended != 1 {
		t.Fatalf("purge: ended %d, %v; want 1", ended, err)
	}

	// Version 1 lost its name and is beyond the cap of 1: 3 is the backup, 2
	// the live checkpoint, 4 the main file.
	if got, want := numbers(t, repo, plan.ID), []int64{2, 3, 4}; !reflect.DeepEqual(got, want) {
		t.Errorf("timeline = %v, want %v", got, want)
	}
	if got := garbage(t, conn); len(got) != 1 || got[0] != "plans/ab/cd/abcd" {
		t.Errorf("garbage = %v, want the key of version 1", got)
	}
	if trashed, err := repo.TrashedCheckpoints(ctx, "identity-1"); err != nil || len(trashed) != 0 {
		t.Errorf("Trash still holds %+v, %v", trashed, err)
	}
}

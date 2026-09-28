// core/plans/trash_test.go

package plans_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/smegg99/goptivum/backend/core/plans"
)

func TestATrashedPlanIsAbsentUntilRestored(t *testing.T) {
	ctx := context.Background()
	repo := repository(t)
	plan := newPlan(t, repo, "identity-1", "Do usunięcia")
	kept := newPlan(t, repo, "identity-1", "Plan 2027")

	if err := repo.Trash(ctx, plan.ID); err != nil {
		t.Fatalf("trash: %v", err)
	}
	if _, err := repo.Get(ctx, plan.ID); !errors.Is(err, plans.ErrNotFound) {
		t.Errorf("get a trashed plan = %v, want ErrNotFound", err)
	}
	if _, err := repo.Current(ctx, plan.ID); !errors.Is(err, plans.ErrNotFound) {
		t.Errorf("current of a trashed plan = %v, want ErrNotFound", err)
	}
	if listed, err := repo.List(ctx); err != nil || len(listed) != 1 || listed[0].ID != kept.ID {
		t.Errorf("list = %+v, %v; want only the live plan", listed, err)
	}
	name := "Nowa nazwa"
	if _, err := repo.Update(ctx, plan.ID, plans.Change{Name: &name}); !errors.Is(err, plans.ErrNotFound) {
		t.Errorf("rename a trashed plan = %v, want ErrNotFound", err)
	}
	if _, err := repo.AddVersion(ctx, plans.Upload{
		PlanID: plan.ID, Parent: 1, ObjectKey: "k", CreatedBy: "identity-1",
	}); !errors.Is(err, plans.ErrNotFound) {
		t.Errorf("upload to a trashed plan = %v, want ErrNotFound", err)
	}
	if err := repo.Trash(ctx, plan.ID); !errors.Is(err, plans.ErrNotFound) {
		t.Errorf("trash twice = %v, want ErrNotFound", err)
	}

	trashed, err := repo.Trashed(ctx, "identity-1")
	if err != nil {
		t.Fatalf("trashed: %v", err)
	}
	if len(trashed) != 1 || trashed[0].ID != plan.ID || trashed[0].Name != "Do usunięcia" {
		t.Fatalf("trashed = %+v", trashed)
	}
	if trashed[0].DeletedAt.IsZero() || trashed[0].DeletedAt.Before(plan.CreatedAt) {
		t.Errorf("deleted_at = %v", trashed[0].DeletedAt)
	}
	if theirs, err := repo.Trashed(ctx, "identity-2"); err != nil || len(theirs) != 0 {
		t.Errorf("another owner's trash = %+v, %v", theirs, err)
	}

	restored, err := repo.Restore(ctx, plan.ID, "identity-1")
	if err != nil {
		t.Fatalf("restore: %v", err)
	}
	if restored.ID != plan.ID || !restored.DeletedAt.IsZero() || restored.Name != plan.Name {
		t.Errorf("restored = %+v", restored)
	}
	if timeline, err := repo.Versions(ctx, plan.ID); err != nil || len(timeline) != 1 {
		t.Errorf("a restored plan has %d versions, %v", len(timeline), err)
	}
	if trashed, err := repo.Trashed(ctx, "identity-1"); err != nil || len(trashed) != 0 {
		t.Errorf("trash after restore = %+v, %v", trashed, err)
	}
}

// Restore is what undo calls, so asking twice lands where asking once did.
func TestRestoreIsOneOwnersAndRepeatable(t *testing.T) {
	ctx := context.Background()
	repo := repository(t)
	plan := newPlan(t, repo, "identity-1", "Plan 2027")

	if err := repo.Trash(ctx, plan.ID); err != nil {
		t.Fatalf("trash: %v", err)
	}
	if _, err := repo.Restore(ctx, plan.ID, "identity-2"); !errors.Is(err, plans.ErrNotFound) {
		t.Errorf("another owner restored it: %v", err)
	}
	if _, err := repo.Restore(ctx, plan.ID, "identity-1"); err != nil {
		t.Fatalf("restore: %v", err)
	}
	if again, err := repo.Restore(ctx, plan.ID, "identity-1"); err != nil || again.ID != plan.ID {
		t.Errorf("restoring a live plan = %+v, %v", again, err)
	}
	if _, err := repo.Restore(ctx, 404, "identity-1"); !errors.Is(err, plans.ErrNotFound) {
		t.Errorf("restore of an unknown plan = %v", err)
	}
}

func TestPurgeRemovesOnlyWhatOutlivedTheRetention(t *testing.T) {
	ctx := context.Background()
	repo, conn := repositoryWithConn(t)
	old := newPlan(t, repo, "identity-1", "Stary")
	recent := newPlan(t, repo, "identity-1", "Świeży")
	live := newPlan(t, repo, "identity-1", "Żywy")

	for _, plan := range []plans.Plan{old, recent} {
		if err := repo.Trash(ctx, plan.ID); err != nil {
			t.Fatalf("trash: %v", err)
		}
	}
	// Thirty days cannot pass in a test, so the older one is moved back.
	deletedAt := time.Now().UTC().Truncate(time.Microsecond).AddDate(0, 0, -31)
	if _, err := conn.ExecContext(ctx, `update plans set deleted_at = ? where id = ?`,
		deletedAt, old.ID); err != nil {
		t.Fatalf("backdate: %v", err)
	}

	const days = 30
	removesAt := plans.RemovesAt(deletedAt, days)
	if want := deletedAt.AddDate(0, 0, days); !removesAt.Equal(want) {
		t.Fatalf("removes at %v, want %v", removesAt, want)
	}

	// A moment before the removal date nothing goes.
	if purged, err := repo.Purge(ctx, removesAt.Add(-time.Second), days); err != nil || purged != 0 {
		t.Errorf("early purge removed %d, %v", purged, err)
	}
	purged, err := repo.Purge(ctx, removesAt, days)
	if err != nil || purged != 1 {
		t.Fatalf("purge removed %d, %v; want 1", purged, err)
	}

	if _, err := repo.Restore(ctx, old.ID, "identity-1"); !errors.Is(err, plans.ErrNotFound) {
		t.Errorf("a purged plan came back: %v", err)
	}
	if timeline, err := repo.Versions(ctx, old.ID); err != nil || len(timeline) != 0 {
		t.Errorf("a purged plan kept %d versions, %v", len(timeline), err)
	}
	if left, err := repo.Trashed(ctx, "identity-1"); err != nil || len(left) != 1 || left[0].ID != recent.ID {
		t.Errorf("trash after purge = %+v, %v", left, err)
	}
	if _, err := repo.Get(ctx, live.ID); err != nil {
		t.Errorf("the purge touched a live plan: %v", err)
	}
}

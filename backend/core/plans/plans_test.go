// core/plans/plans_test.go

package plans_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/smegg99/s99ab/data/db"

	"github.com/smegg99/goptivum/backend/core/plans"
	"github.com/smegg99/goptivum/backend/migrations"
)

func repository(t *testing.T) *plans.Repository {
	t.Helper()
	return plans.NewRepository(db.TestDB(t, migrations.FS), db.SQLite)
}

func newPlan(t *testing.T, repo *plans.Repository, owner, name string) plans.Plan {
	t.Helper()

	plan, err := repo.Create(context.Background(), plans.Plan{
		OwnerIdentityID: owner, Name: name, SchoolYear: "2026/2027",
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	return plan
}

func TestCreateGetListDelete(t *testing.T) {
	ctx := context.Background()
	repo := repository(t)

	plan := newPlan(t, repo, "identity-1", "Plan ZSEM")
	if plan.ID == 0 {
		t.Fatal("create returned no id")
	}
	if plan.CreatedAt.IsZero() || !plan.UpdatedAt.Equal(plan.CreatedAt) {
		t.Errorf("timestamps = %v / %v", plan.CreatedAt, plan.UpdatedAt)
	}

	read, err := repo.Get(ctx, plan.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if read != plan {
		t.Errorf("read back %+v, wrote %+v", read, plan)
	}

	other := newPlan(t, repo, "identity-2", "Inny plan")
	mine, err := repo.List(ctx, "identity-1")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(mine) != 1 || mine[0].ID != plan.ID {
		t.Errorf("list returned %d plans for one owner", len(mine))
	}
	if theirs, err := repo.List(ctx, "identity-2"); err != nil || len(theirs) != 1 ||
		theirs[0].ID != other.ID {
		t.Errorf("list for the other owner = %v, %v", theirs, err)
	}
	if none, err := repo.List(ctx, "identity-3"); err != nil || len(none) != 0 {
		t.Errorf("list for an owner with no plans = %v, %v", none, err)
	}

	if err := repo.Delete(ctx, plan.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := repo.Get(ctx, plan.ID); !errors.Is(err, plans.ErrNotFound) {
		t.Errorf("get after delete = %v, want ErrNotFound", err)
	}
	if err := repo.Delete(ctx, plan.ID); !errors.Is(err, plans.ErrNotFound) {
		t.Errorf("delete twice = %v, want ErrNotFound", err)
	}
}

func TestCreateRefusesAPlanWithoutAnOwnerOrName(t *testing.T) {
	ctx := context.Background()
	repo := repository(t)

	if _, err := repo.Create(ctx, plans.Plan{Name: "Plan"}); err == nil {
		t.Error("create accepted a plan with no owner")
	}
	if _, err := repo.Create(ctx, plans.Plan{OwnerIdentityID: "who"}); err == nil {
		t.Error("create accepted a plan with no name")
	}
}

func TestUnknownPlanIsNotFound(t *testing.T) {
	ctx := context.Background()
	repo := repository(t)

	if _, err := repo.Get(ctx, 404); !errors.Is(err, plans.ErrNotFound) {
		t.Errorf("get = %v", err)
	}
	if _, err := repo.Current(ctx, 404); !errors.Is(err, plans.ErrNotFound) {
		t.Errorf("current = %v", err)
	}
	if _, err := repo.AddVersion(ctx, plans.Upload{
		PlanID: 404, ObjectKey: "plans/ab/cd/abcd", CreatedBy: "who",
	}); !errors.Is(err, plans.ErrNotFound) {
		t.Errorf("add version = %v", err)
	}
}

func TestVersionTimeline(t *testing.T) {
	ctx := context.Background()
	repo := repository(t)
	plan := newPlan(t, repo, "identity-1", "Plan ZSEM")

	if current, err := repo.Current(ctx, plan.ID); err != nil || current != 0 {
		t.Errorf("a plan with no versions is at %d, %v", current, err)
	}
	if _, err := repo.Latest(ctx, plan.ID); !errors.Is(err, plans.ErrNoVersion) {
		t.Errorf("latest of an empty plan = %v, want ErrNoVersion", err)
	}

	var parent int64
	for step := 1; step <= 3; step++ {
		version, err := repo.AddVersion(ctx, plans.Upload{
			PlanID: plan.ID, Parent: parent,
			ObjectKey: "plans/ab/cd/abcd", SizeBytes: int64(step * 100),
			CreatedBy: "identity-1",
		})
		if err != nil {
			t.Fatalf("add version %d: %v", step, err)
		}
		if version.Version != int64(step) || version.Parent != parent {
			t.Fatalf("version %+v, want %d after %d", version, step, parent)
		}
		parent = version.Version
	}

	timeline, err := repo.Versions(ctx, plan.ID)
	if err != nil {
		t.Fatalf("versions: %v", err)
	}
	if len(timeline) != 3 {
		t.Fatalf("timeline holds %d versions", len(timeline))
	}
	for i, version := range timeline {
		if version.Version != int64(i+1) {
			t.Errorf("timeline[%d] is version %d", i, version.Version)
		}
	}
	if timeline[0].Parent != 0 {
		t.Errorf("the first version has parent %d, want none", timeline[0].Parent)
	}

	latest, err := repo.Latest(ctx, plan.ID)
	if err != nil || latest.Version != 3 {
		t.Errorf("latest = %+v, %v", latest, err)
	}
	if current, err := repo.Current(ctx, plan.ID); err != nil || current != 3 {
		t.Errorf("current = %d, %v", current, err)
	}
	if second, err := repo.Version(ctx, plan.ID, 2); err != nil || second.SizeBytes != 200 {
		t.Errorf("version 2 = %+v, %v", second, err)
	}
	if _, err := repo.Version(ctx, plan.ID, 9); !errors.Is(err, plans.ErrNoVersion) {
		t.Errorf("version 9 = %v, want ErrNoVersion", err)
	}

	// A version landing is what makes a plan change.
	touched, err := repo.Get(ctx, plan.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if !touched.UpdatedAt.After(touched.CreatedAt) && !touched.UpdatedAt.Equal(touched.CreatedAt) {
		t.Errorf("updated_at %v is before created_at %v", touched.UpdatedAt, touched.CreatedAt)
	}
	if touched.UpdatedAt.Before(latest.CreatedAt) {
		t.Errorf("updated_at %v predates the newest version %v", touched.UpdatedAt, latest.CreatedAt)
	}
}

// A stale parent is refused and the refusal carries the current version, so
// the client can say who saved first.
func TestStaleParentIsRefused(t *testing.T) {
	ctx := context.Background()
	repo := repository(t)
	plan := newPlan(t, repo, "identity-1", "Plan ZSEM")

	upload := plans.Upload{
		PlanID: plan.ID, ObjectKey: "plans/ab/cd/abcd", CreatedBy: "identity-1",
	}
	if _, err := repo.AddVersion(ctx, upload); err != nil {
		t.Fatalf("first version: %v", err)
	}

	cases := []struct {
		name   string
		parent int64
	}{
		{"the same first version twice", 0},
		{"a parent the plan never had", 7},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			upload.Parent = test.parent
			_, err := repo.AddVersion(ctx, upload)

			var conflict *plans.Conflict
			if !errors.As(err, &conflict) {
				t.Fatalf("error = %v, want a Conflict", err)
			}
			if conflict.Current != 1 || conflict.Parent != test.parent {
				t.Errorf("conflict = %+v, want current 1", conflict)
			}
			if conflict.Error() == "" {
				t.Error("a conflict with no message")
			}
		})
	}

	if timeline, err := repo.Versions(ctx, plan.ID); err != nil || len(timeline) != 1 {
		t.Errorf("a refused upload landed: %d versions, %v", len(timeline), err)
	}
}

// The test that matters: many uploads naming one parent, exactly one wins.
func TestConcurrentUploadsWithOneParentLeaveOneWinner(t *testing.T) {
	ctx := context.Background()
	repo := repository(t)
	plan := newPlan(t, repo, "identity-1", "Plan ZSEM")

	first, err := repo.AddVersion(ctx, plans.Upload{
		PlanID: plan.ID, ObjectKey: "plans/ab/cd/abcd", CreatedBy: "identity-1",
	})
	if err != nil {
		t.Fatalf("first version: %v", err)
	}

	const writers = 8
	var (
		wg        sync.WaitGroup
		mu        sync.Mutex
		winners   []plans.Version
		conflicts []*plans.Conflict
		failures  []error
	)
	start := make(chan struct{})
	for i := 0; i < writers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start

			version, err := repo.AddVersion(ctx, plans.Upload{
				PlanID: plan.ID, Parent: first.Version,
				ObjectKey: "plans/ab/cd/abcd", SizeBytes: int64(i),
				CreatedBy: "identity-1",
			})
			mu.Lock()
			defer mu.Unlock()

			var conflict *plans.Conflict
			switch {
			case err == nil:
				winners = append(winners, version)
			case errors.As(err, &conflict):
				conflicts = append(conflicts, conflict)
			default:
				failures = append(failures, err)
			}
		}(i)
	}
	close(start)
	wg.Wait()

	if len(failures) > 0 {
		t.Fatalf("%d uploads failed for another reason: %v", len(failures), failures[0])
	}
	if len(winners) != 1 {
		t.Fatalf("%d uploads won, want exactly 1", len(winners))
	}
	if len(conflicts) != writers-1 {
		t.Fatalf("%d conflicts, want %d", len(conflicts), writers-1)
	}
	for _, conflict := range conflicts {
		if conflict.Current != winners[0].Version {
			t.Errorf("a conflict named version %d, the winner is %d",
				conflict.Current, winners[0].Version)
		}
	}

	timeline, err := repo.Versions(ctx, plan.ID)
	if err != nil {
		t.Fatalf("versions: %v", err)
	}
	if len(timeline) != 2 {
		t.Errorf("the timeline holds %d versions, want 2", len(timeline))
	}
}

func TestValidityDatesRoundTrip(t *testing.T) {
	ctx := context.Background()
	repo := repository(t)
	plan := newPlan(t, repo, "identity-1", "Plan ZSEM")

	from := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	to := time.Date(2027, 1, 31, 0, 0, 0, 0, time.UTC)
	if _, err := repo.AddVersion(ctx, plans.Upload{
		PlanID: plan.ID, ObjectKey: "plans/ab/cd/abcd", CreatedBy: "identity-1",
		ValidFrom: from, ValidTo: to,
	}); err != nil {
		t.Fatalf("add version: %v", err)
	}
	// A school's timetable changes mid-year, so one plan holds a September
	// schedule and a February one.
	if _, err := repo.AddVersion(ctx, plans.Upload{
		PlanID: plan.ID, Parent: 1, ObjectKey: "plans/ef/12/ef12", CreatedBy: "identity-1",
		ValidFrom: to.AddDate(0, 0, 1),
	}); err != nil {
		t.Fatalf("add second version: %v", err)
	}

	timeline, err := repo.Versions(ctx, plan.ID)
	if err != nil {
		t.Fatalf("versions: %v", err)
	}
	if !timeline[0].ValidFrom.Equal(from) || !timeline[0].ValidTo.Equal(to) {
		t.Errorf("validity = %v..%v, want %v..%v",
			timeline[0].ValidFrom, timeline[0].ValidTo, from, to)
	}
	if !timeline[1].ValidTo.IsZero() {
		t.Errorf("an open-ended version has valid_to %v", timeline[1].ValidTo)
	}
}

func TestDeleteRemovesTheVersionIndex(t *testing.T) {
	ctx := context.Background()
	repo := repository(t)
	plan := newPlan(t, repo, "identity-1", "Plan ZSEM")

	if _, err := repo.AddVersion(ctx, plans.Upload{
		PlanID: plan.ID, ObjectKey: "plans/ab/cd/abcd", CreatedBy: "identity-1",
	}); err != nil {
		t.Fatalf("add version: %v", err)
	}
	if err := repo.Delete(ctx, plan.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if timeline, err := repo.Versions(ctx, plan.ID); err != nil || len(timeline) != 0 {
		t.Errorf("versions after deleting the plan = %d, %v", len(timeline), err)
	}
}

func TestUploadValidation(t *testing.T) {
	ctx := context.Background()
	repo := repository(t)
	plan := newPlan(t, repo, "identity-1", "Plan ZSEM")

	cases := []struct {
		name   string
		upload plans.Upload
	}{
		{"no object key", plans.Upload{PlanID: plan.ID}},
		{"a negative size", plans.Upload{PlanID: plan.ID, ObjectKey: "k", SizeBytes: -1}},
		{"a negative parent", plans.Upload{PlanID: plan.ID, ObjectKey: "k", Parent: -1}},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			if _, err := repo.AddVersion(ctx, test.upload); err == nil {
				t.Error("the upload was accepted")
			}
		})
	}
}

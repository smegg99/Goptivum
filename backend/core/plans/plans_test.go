// core/plans/plans_test.go

package plans_test

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/smegg99/s99ab/data/db"

	"github.com/smegg99/goptivum/backend/core/plans"
	"github.com/smegg99/goptivum/backend/migrations"
)

func repository(t *testing.T) *plans.Repository {
	t.Helper()
	repo, _ := repositoryWithConn(t)
	return repo
}

// backupsKeep is the server default, so a test that is not about the cap
// never meets it.
const backupsKeep = 100

func repositoryWithConn(t *testing.T) (*plans.Repository, *sql.DB) {
	t.Helper()
	conn := db.TestDB(t, migrations.FS)
	return plans.NewRepository(conn, db.SQLite, plans.Caps{BackupsKeep: backupsKeep, ResultsKeep: 20}), conn
}

// firstUpload is the document every plan is created with.
func firstUpload(owner string) plans.Upload {
	return plans.Upload{ObjectKey: "plans/ab/cd/abcd", SizeBytes: 100, CreatedBy: owner}
}

func newPlan(t *testing.T, repo *plans.Repository, owner, name string) plans.Plan {
	t.Helper()

	plan, _, err := repo.Create(context.Background(), plans.Plan{
		OwnerIdentityID: owner, Name: name, SchoolYear: "2026/2027",
	}, firstUpload(owner))
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	return plan
}

func TestCreateStoresThePlanWithItsFirstVersion(t *testing.T) {
	ctx := context.Background()
	repo := repository(t)

	plan, first, err := repo.Create(ctx, plans.Plan{
		OwnerIdentityID: "identity-1", Name: "  Plan ZSEM  ", SchoolYear: " 2026/2027 ",
	}, firstUpload("identity-1"))
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if plan.ID == 0 || plan.Name != "Plan ZSEM" || plan.SchoolYear != "2026/2027" {
		t.Fatalf("created %+v", plan)
	}
	if plan.CreatedAt.IsZero() || !plan.UpdatedAt.Equal(plan.CreatedAt) || !plan.DeletedAt.IsZero() {
		t.Errorf("timestamps = %v / %v / %v", plan.CreatedAt, plan.UpdatedAt, plan.DeletedAt)
	}
	if first.PlanID != plan.ID || first.Version != 1 || first.Parent != 0 ||
		first.ObjectKey != "plans/ab/cd/abcd" || first.CreatedBy != "identity-1" {
		t.Errorf("first version = %+v", first)
	}

	read, err := repo.Get(ctx, plan.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if read != plan {
		t.Errorf("read back %+v, wrote %+v", read, plan)
	}
	if current, err := repo.Current(ctx, plan.ID); err != nil || current != 1 {
		t.Errorf("current = %d, %v, want 1", current, err)
	}
}

// The plan and its first version land together or not at all, so no plan ever
// exists with nothing to open.
func TestCreateIsOneTransaction(t *testing.T) {
	ctx := context.Background()
	repo, conn := repositoryWithConn(t)

	if _, err := conn.ExecContext(ctx, `create trigger refuse_versions before insert on plan_versions
		begin select raise(abort, 'refused by the test'); end`); err != nil {
		t.Fatalf("trigger: %v", err)
	}
	if _, _, err := repo.Create(ctx, plans.Plan{
		OwnerIdentityID: "identity-1", Name: "Plan ZSEM",
	}, firstUpload("identity-1")); err == nil {
		t.Fatal("create succeeded although its version was refused")
	}

	var count int
	if err := conn.QueryRowContext(ctx, `select count(*) from plans`).Scan(&count); err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 0 {
		t.Errorf("%d plans left behind by a refused version", count)
	}
}

func TestCreateRefusesWhatCannotBeAPlan(t *testing.T) {
	ctx := context.Background()
	repo := repository(t)

	cases := []struct {
		name  string
		plan  plans.Plan
		first plans.Upload
		field string
		code  string
	}{
		{"a blank name", plans.Plan{OwnerIdentityID: "who", Name: "   "}, firstUpload("who"), "name", "required"},
		{"a long name", plans.Plan{OwnerIdentityID: "who", Name: strings.Repeat("a", 201)}, firstUpload("who"), "name", "too_long"},
		{"a long school year", plans.Plan{OwnerIdentityID: "who", Name: "Plan", SchoolYear: strings.Repeat("9", 33)}, firstUpload("who"), "school_year", "too_long"},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			_, _, err := repo.Create(ctx, test.plan, test.first)
			var invalid *plans.Invalid
			if !errors.As(err, &invalid) || invalid.Field != test.field || invalid.Code != test.code {
				t.Errorf("error = %v, want %s %s", err, test.field, test.code)
			}
		})
	}

	// A name counts letters, not bytes: 200 Polish letters are a valid name.
	if _, _, err := repo.Create(ctx, plans.Plan{
		OwnerIdentityID: "who", Name: strings.Repeat("ż", 200),
	}, firstUpload("who")); err != nil {
		t.Errorf("200 letters refused: %v", err)
	}
	if _, _, err := repo.Create(ctx, plans.Plan{Name: "Plan"}, firstUpload("who")); err == nil {
		t.Error("create accepted a plan with no owner")
	}
	if _, _, err := repo.Create(ctx, plans.Plan{OwnerIdentityID: "who", Name: "Plan"},
		plans.Upload{CreatedBy: "who"}); err == nil {
		t.Error("create accepted a first version with no object key")
	}
}

// Everyone at the school sees every live plan, newest first.
func TestListIsEveryOwnersLivePlans(t *testing.T) {
	ctx := context.Background()
	repo := repository(t)

	plan := newPlan(t, repo, "identity-1", "Plan ZSEM")
	time.Sleep(2 * time.Millisecond)
	other := newPlan(t, repo, "identity-2", "Inny plan")
	time.Sleep(2 * time.Millisecond)
	trashed := newPlan(t, repo, "identity-1", "W koszu")
	if err := repo.Trash(ctx, trashed.ID); err != nil {
		t.Fatalf("trash: %v", err)
	}

	listed, err := repo.List(ctx)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(listed) != 2 || listed[0].ID != other.ID || listed[1].ID != plan.ID {
		t.Errorf("list = %+v, want both owners' live plans, newest first", listed)
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
		PlanID: 404, Parent: 1, ObjectKey: "plans/ab/cd/abcd", CreatedBy: "who",
	}); !errors.Is(err, plans.ErrNotFound) {
		t.Errorf("add version = %v", err)
	}
	name := "Plan"
	if _, err := repo.Update(ctx, 404, plans.Change{Name: &name}); !errors.Is(err, plans.ErrNotFound) {
		t.Errorf("update = %v", err)
	}
}

func TestUpdateRenamesAndChangesTheSchoolYear(t *testing.T) {
	ctx := context.Background()
	repo := repository(t)
	plan := newPlan(t, repo, "identity-1", "Plan 2027")

	name := "  Plan 2027 test "
	renamed, err := repo.Update(ctx, plan.ID, plans.Change{Name: &name})
	if err != nil {
		t.Fatalf("rename: %v", err)
	}
	if renamed.Name != "Plan 2027 test" || renamed.SchoolYear != "2026/2027" {
		t.Errorf("renamed = %+v, want the name trimmed and the year kept", renamed)
	}
	if renamed.UpdatedAt.Before(plan.UpdatedAt) {
		t.Errorf("updated_at went back: %v before %v", renamed.UpdatedAt, plan.UpdatedAt)
	}

	year := "2027/2028"
	moved, err := repo.Update(ctx, plan.ID, plans.Change{SchoolYear: &year})
	if err != nil {
		t.Fatalf("change year: %v", err)
	}
	if moved.Name != "Plan 2027 test" || moved.SchoolYear != "2027/2028" {
		t.Errorf("moved = %+v, want the name kept", moved)
	}

	empty := ""
	cleared, err := repo.Update(ctx, plan.ID, plans.Change{SchoolYear: &empty})
	if err != nil || cleared.SchoolYear != "" {
		t.Errorf("clearing the school year = %+v, %v", cleared, err)
	}

	read, err := repo.Get(ctx, plan.ID)
	if err != nil || read != cleared {
		t.Errorf("read back %+v, %v; want %+v", read, err, cleared)
	}

	// Naming no field changes nothing, not even updated_at.
	untouched, err := repo.Update(ctx, plan.ID, plans.Change{})
	if err != nil || untouched != cleared {
		t.Errorf("an empty change = %+v, %v; want %+v", untouched, err, cleared)
	}
}

func TestUpdateRefusesABlankName(t *testing.T) {
	ctx := context.Background()
	repo := repository(t)
	plan := newPlan(t, repo, "identity-1", "Plan 2027")

	for _, blank := range []string{"", "   ", "\t\n"} {
		_, err := repo.Update(ctx, plan.ID, plans.Change{Name: &blank})
		var invalid *plans.Invalid
		if !errors.As(err, &invalid) || invalid.Field != "name" || invalid.Code != "required" {
			t.Errorf("rename to %q = %v, want name required", blank, err)
		}
	}
	if read, err := repo.Get(ctx, plan.ID); err != nil || read.Name != "Plan 2027" {
		t.Errorf("a refused rename changed the plan: %+v, %v", read, err)
	}
}

func TestVersionTimeline(t *testing.T) {
	ctx := context.Background()
	repo := repository(t)
	plan := newPlan(t, repo, "identity-1", "Plan ZSEM")

	parent := int64(1)
	for step := 2; step <= 3; step++ {
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
		PlanID: plan.ID, Parent: 1, ObjectKey: "plans/ab/cd/abcd", CreatedBy: "identity-1",
	}
	if _, err := repo.AddVersion(ctx, upload); err != nil {
		t.Fatalf("second version: %v", err)
	}

	cases := []struct {
		name   string
		parent int64
	}{
		{"the same parent twice", 1},
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
			if conflict.Current != 2 || conflict.Parent != test.parent {
				t.Errorf("conflict = %+v, want current 2", conflict)
			}
			if conflict.Error() == "" {
				t.Error("a conflict with no message")
			}
		})
	}

	if timeline, err := repo.Versions(ctx, plan.ID); err != nil || len(timeline) != 2 {
		t.Errorf("a refused upload landed: %d versions, %v", len(timeline), err)
	}
}

// The test that matters: many uploads naming one parent, exactly one wins.
func TestConcurrentUploadsWithOneParentLeaveOneWinner(t *testing.T) {
	ctx := context.Background()
	repo := repository(t)
	plan := newPlan(t, repo, "identity-1", "Plan ZSEM")

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
				PlanID: plan.ID, Parent: 1,
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

	from := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	to := time.Date(2027, 1, 31, 0, 0, 0, 0, time.UTC)
	first := firstUpload("identity-1")
	first.ValidFrom, first.ValidTo = &from, &to
	plan, _, err := repo.Create(ctx, plans.Plan{OwnerIdentityID: "identity-1", Name: "Plan ZSEM"}, first)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	// A school's timetable changes mid-year, so one plan holds a September
	// schedule and a February one.
	february := to.AddDate(0, 0, 1)
	if _, err := repo.AddVersion(ctx, plans.Upload{
		PlanID: plan.ID, Parent: 1, ObjectKey: "plans/ef/12/ef12", CreatedBy: "identity-1",
		ValidFrom: &february, ValidTo: &time.Time{},
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

func TestUploadValidation(t *testing.T) {
	ctx := context.Background()
	repo := repository(t)
	plan := newPlan(t, repo, "identity-1", "Plan ZSEM")

	cases := []struct {
		name   string
		upload plans.Upload
	}{
		{"no object key", plans.Upload{PlanID: plan.ID, Parent: 1}},
		{"a negative size", plans.Upload{PlanID: plan.ID, Parent: 1, ObjectKey: "k", SizeBytes: -1}},
		{"a negative parent", plans.Upload{PlanID: plan.ID, ObjectKey: "k", Parent: -1}},
		// Every plan is born with version 1, so a later version always names one.
		{"no parent", plans.Upload{PlanID: plan.ID, ObjectKey: "k"}},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			if _, err := repo.AddVersion(ctx, test.upload); err == nil {
				t.Error("the upload was accepted")
			}
		})
	}
}

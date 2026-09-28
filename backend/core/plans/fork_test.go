// core/plans/fork_test.go

package plans_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/smegg99/goptivum/backend/core/plans"
)

// secondVersion adds a version with its own document and validity dates.
func secondVersion(t *testing.T, repo *plans.Repository, plan plans.Plan) plans.Version {
	t.Helper()
	from := time.Date(2027, time.February, 1, 0, 0, 0, 0, time.UTC)
	to := time.Date(2027, time.June, 26, 0, 0, 0, 0, time.UTC)
	version, err := repo.AddVersion(context.Background(), plans.Upload{
		PlanID: plan.ID, Parent: 1, ObjectKey: "plans/ef/12/ef12", SizeBytes: 250,
		CreatedBy: plan.OwnerIdentityID, UploadKey: "second-version-key-0001",
		ValidFrom: &from, ValidTo: &to,
	})
	if err != nil {
		t.Fatalf("second version: %v", err)
	}
	return version
}

// A fork is a new plan of its own owner whose one version is the origin's
// chosen version: the same stored document, its validity dates, nothing else.
func TestAForkCopiesOneVersionAndRecordsItsOrigin(t *testing.T) {
	ctx := context.Background()
	repo := repository(t)
	origin := newPlan(t, repo, "identity-1", "Plan 2027")
	second := secondVersion(t, repo, origin)
	if _, err := repo.NameCheckpoint(ctx, origin.ID, 1, "wrzesień"); err != nil {
		t.Fatalf("checkpoint: %v", err)
	}
	keep := 7
	if _, err := repo.Update(ctx, origin.ID, plans.Change{BackupsKeep: &keep}); err != nil {
		t.Fatalf("cap: %v", err)
	}

	fork, first, err := repo.Fork(ctx, plans.ForkOf{
		Origin: origin.ID, Version: second.Version, Owner: "identity-2", Name: "  Plan 2027 (fork)  ",
	})
	if err != nil {
		t.Fatalf("fork: %v", err)
	}
	if fork.ID == 0 || fork.ID == origin.ID || fork.OwnerIdentityID != "identity-2" ||
		fork.Name != "Plan 2027 (fork)" || fork.SchoolYear != origin.SchoolYear {
		t.Errorf("fork = %+v", fork)
	}
	if fork.Origin != (plans.Origin{PlanID: origin.ID, Version: second.Version}) {
		t.Errorf("origin = %+v, want plan %d version %d", fork.Origin, origin.ID, second.Version)
	}
	if fork.BackupsKeep != 0 || fork.ResultsKeep != 0 {
		t.Errorf("caps = %d / %d, want the server's", fork.BackupsKeep, fork.ResultsKeep)
	}
	if first.PlanID != fork.ID || first.Version != 1 || first.Parent != 0 ||
		first.ObjectKey != second.ObjectKey || first.SizeBytes != second.SizeBytes ||
		first.CreatedBy != "identity-2" || !first.ValidFrom.Equal(second.ValidFrom) ||
		!first.ValidTo.Equal(second.ValidTo) || first.Checkpoint != "" || first.UploadKey != "" {
		t.Errorf("version 1 = %+v, want version %d's document", first, second.Version)
	}

	read, err := repo.Get(ctx, fork.ID)
	if err != nil || read != fork {
		t.Errorf("read back %+v, %v; wrote %+v", read, err, fork)
	}
	timeline, err := repo.Versions(ctx, fork.ID)
	if err != nil || len(timeline) != 1 {
		t.Fatalf("the fork's timeline = %+v, %v; want one version", timeline, err)
	}
	if results, err := repo.Results(ctx, fork.ID); err != nil || len(results) != 0 {
		t.Errorf("the fork's results = %+v, %v; want none", results, err)
	}

	// No version named: the newest.
	newest, first, err := repo.Fork(ctx, plans.ForkOf{Origin: origin.ID, Owner: "identity-1", Name: "Kopia"})
	if err != nil {
		t.Fatalf("duplicate: %v", err)
	}
	if newest.Origin.Version != second.Version || first.ObjectKey != second.ObjectKey || newest.OwnerIdentityID != "identity-1" {
		t.Errorf("duplicate = %+v with %+v, want the newest version", newest, first)
	}
}

func TestAForkRefusesWhatIsNotThere(t *testing.T) {
	ctx := context.Background()
	repo := repository(t)
	origin := newPlan(t, repo, "identity-1", "Plan 2027")
	trashed := newPlan(t, repo, "identity-1", "W koszu")
	if err := repo.Trash(ctx, trashed.ID); err != nil {
		t.Fatalf("trash: %v", err)
	}

	for _, attempt := range []struct {
		name string
		fork plans.ForkOf
		want error
	}{
		{"a version the origin lacks", plans.ForkOf{Origin: origin.ID, Version: 9, Owner: "identity-2", Name: "Kopia"}, plans.ErrNoVersion},
		{"an origin in Trash", plans.ForkOf{Origin: trashed.ID, Owner: "identity-2", Name: "Kopia"}, plans.ErrNotFound},
		{"no origin", plans.ForkOf{Origin: 424242, Owner: "identity-2", Name: "Kopia"}, plans.ErrNotFound},
	} {
		if _, _, err := repo.Fork(ctx, attempt.fork); !errors.Is(err, attempt.want) {
			t.Errorf("%s: err = %v, want %v", attempt.name, err, attempt.want)
		}
	}
	var refusal *plans.Invalid
	if _, _, err := repo.Fork(ctx, plans.ForkOf{Origin: origin.ID, Owner: "identity-2", Name: "   "}); !errors.As(err, &refusal) ||
		refusal.Field != "name" || refusal.Code != "required" {
		t.Errorf("a blank name: err = %v, want name: required", err)
	}
	if _, _, err := repo.Fork(ctx, plans.ForkOf{Origin: origin.ID, Name: "Kopia"}); err == nil {
		t.Error("a fork without an owner was stored")
	}
	if listed, err := repo.List(ctx); err != nil || len(listed) != 1 {
		t.Errorf("after refused forks the plans are %+v, %v", listed, err)
	}
}

// The copy says where it came from after the origin is removed for good.
func TestTheOriginOutlivesThePurgeOfItsPlan(t *testing.T) {
	ctx := context.Background()
	repo := repository(t)
	origin := newPlan(t, repo, "identity-1", "Plan 2027")
	fork, _, err := repo.Fork(ctx, plans.ForkOf{Origin: origin.ID, Owner: "identity-2", Name: "Kopia"})
	if err != nil {
		t.Fatalf("fork: %v", err)
	}
	if err := repo.Trash(ctx, origin.ID); err != nil {
		t.Fatalf("trash: %v", err)
	}
	if purged, err := repo.Purge(ctx, time.Now().Add(time.Hour), 0); err != nil || purged != 1 {
		t.Fatalf("purge = %d, %v", purged, err)
	}
	read, err := repo.Get(ctx, fork.ID)
	if err != nil || read.Origin != (plans.Origin{PlanID: origin.ID, Version: 1}) {
		t.Errorf("fork after its origin's purge = %+v, %v", read, err)
	}
}

// A fork names a stored object again, so a sweep that is waiting to delete it
// has to wait another hour.
func TestAForkRevivesTheObjectItNamesAgain(t *testing.T) {
	ctx := context.Background()
	repo, conn := repositoryWithConn(t)
	origin := newPlan(t, repo, "identity-1", "Plan 2027")
	old := time.Now().UTC().Add(-2 * time.Hour)
	wait(t, conn, "plans/ab/cd/abcd", old)

	if _, _, err := repo.Fork(ctx, plans.ForkOf{Origin: origin.ID, Owner: "identity-2", Name: "Kopia"}); err != nil {
		t.Fatalf("fork: %v", err)
	}
	var since time.Time
	if err := conn.QueryRow(`select since from object_garbage where object_key = ?`, "plans/ab/cd/abcd").Scan(&since); err != nil {
		t.Fatalf("read since: %v", err)
	}
	if !since.After(old.Add(time.Hour)) {
		t.Errorf("since = %v, want the time of the fork", since)
	}
}

func TestTransferGivesALivePlanToAnotherOwner(t *testing.T) {
	ctx := context.Background()
	repo := repository(t)
	plan := newPlan(t, repo, "identity-1", "Plan 2027")

	moved, err := repo.Transfer(ctx, plan.ID, "identity-2")
	if err != nil {
		t.Fatalf("transfer: %v", err)
	}
	if moved.OwnerIdentityID != "identity-2" || moved.Name != plan.Name || moved.UpdatedAt.Before(plan.UpdatedAt) {
		t.Errorf("moved = %+v", moved)
	}
	if read, err := repo.Get(ctx, plan.ID); err != nil || read != moved {
		t.Errorf("read back %+v, %v; want %+v", read, err, moved)
	}
	if trashed, err := repo.Trashed(ctx, "identity-2"); err != nil || len(trashed) != 0 {
		t.Errorf("trash of the new owner = %+v, %v", trashed, err)
	}

	if err := repo.Trash(ctx, plan.ID); err != nil {
		t.Fatalf("trash: %v", err)
	}
	if _, err := repo.Transfer(ctx, plan.ID, "identity-1"); !errors.Is(err, plans.ErrNotFound) {
		t.Errorf("transfer of a plan in Trash: err = %v, want ErrNotFound", err)
	}
	if _, err := repo.Transfer(ctx, 424242, "identity-1"); !errors.Is(err, plans.ErrNotFound) {
		t.Errorf("transfer of no plan: err = %v, want ErrNotFound", err)
	}
	if _, err := repo.Transfer(ctx, plan.ID, ""); err == nil {
		t.Error("a transfer to nobody was accepted")
	}
}

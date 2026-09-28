// core/plans/infra_test.go

package plans_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/smegg99/s99ab/data/db"

	"github.com/smegg99/goptivum/backend/core/plans"
	"github.com/smegg99/goptivum/backend/migrations"
)

// postgresRepository connects to the Postgres that "just infra-up" starts.
// SQLite and Postgres differ in how a concurrent write is refused, so the
// version check is exercised on both rather than on the one that is easier.
func postgresRepository(t *testing.T) *plans.Repository {
	t.Helper()

	dsn := os.Getenv("GOPTIVUM_TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("no GOPTIVUM_TEST_POSTGRES_DSN - run just infra-up and just test-infra")
	}
	conn, err := db.Open(db.Config{Driver: db.Postgres, DSN: dsn})
	if err != nil {
		t.Fatalf("connect to postgres: %v", err)
	}
	t.Cleanup(func() { conn.Close() })

	if err := db.Migrate(conn, migrations.FS, db.Postgres); err != nil {
		t.Fatalf("migrate postgres: %v", err)
	}
	return plans.NewRepository(conn, db.Postgres, plans.Caps{BackupsKeep: backupsKeep, ResultsKeep: 20})
}

// remove trashes a plan and purges everything trashed, which in the test
// database is only what earlier runs left behind.
func remove(ctx context.Context, repo *plans.Repository, id int64) {
	_ = repo.Trash(ctx, id)
	_, _ = repo.Purge(ctx, time.Now().AddDate(0, 0, 1), 0)
}

// owner is unique per run, so a shared development database does not make one
// run depend on another.
// listedOnce reports whether id is among plans exactly once. The database is
// shared with other runs, so the listing may hold their plans too.
func listedOnce(listed []plans.Plan, id int64) bool {
	seen := 0
	for _, plan := range listed {
		if plan.ID == id {
			seen++
		}
	}
	return seen == 1
}

func owner(t *testing.T) string {
	t.Helper()
	return fmt.Sprintf("infra-%s-%d", t.Name(), os.Getpid())
}

func TestInfraPlansOnPostgres(t *testing.T) {
	ctx := context.Background()
	repo := postgresRepository(t)
	identity := owner(t)

	plan, first, err := repo.Create(ctx, plans.Plan{
		OwnerIdentityID: identity, Name: "Plan ZSEM", SchoolYear: "2026/2027",
	}, plans.Upload{ObjectKey: "plans/ab/cd/abcd", SizeBytes: 22339, CreatedBy: identity})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	t.Cleanup(func() { remove(ctx, repo, plan.ID) })

	read, err := repo.Get(ctx, plan.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if read != plan {
		t.Errorf("read back %+v, wrote %+v", read, plan)
	}
	if listed, err := repo.List(ctx); err != nil || !listedOnce(listed, plan.ID) {
		t.Errorf("list = %d plans, %v; want the new plan once", len(listed), err)
	}

	if _, err := repo.AddVersion(ctx, plans.Upload{
		PlanID: plan.ID, Parent: first.Version, ObjectKey: "plans/ef/12/ef12",
		SizeBytes: 22500, CreatedBy: identity,
	}); err != nil {
		t.Fatalf("second version: %v", err)
	}

	var conflict *plans.Conflict
	_, err = repo.AddVersion(ctx, plans.Upload{
		PlanID: plan.ID, Parent: first.Version, ObjectKey: "plans/ab/cd/abcd",
		CreatedBy: identity,
	})
	if !errors.As(err, &conflict) {
		t.Fatalf("stale parent = %v, want a Conflict", err)
	}
	if conflict.Current != 2 {
		t.Errorf("conflict names version %d, want 2", conflict.Current)
	}

	if timeline, err := repo.Versions(ctx, plan.ID); err != nil || len(timeline) != 2 {
		t.Errorf("timeline = %d versions, %v", len(timeline), err)
	}
	if err := repo.Trash(ctx, plan.ID); err != nil {
		t.Fatalf("trash: %v", err)
	}
	if _, err := repo.Purge(ctx, time.Now().AddDate(0, 0, 1), 0); err != nil {
		t.Fatalf("purge: %v", err)
	}
	if timeline, err := repo.Versions(ctx, plan.ID); err != nil || len(timeline) != 0 {
		t.Errorf("the version index survived the plan: %d, %v", len(timeline), err)
	}
}

// The test that matters, on the engine production uses: many uploads naming
// one parent, exactly one wins.
func TestInfraConcurrentUploadsOnPostgres(t *testing.T) {
	ctx := context.Background()
	repo := postgresRepository(t)
	identity := owner(t)

	plan, first, err := repo.Create(ctx, plans.Plan{OwnerIdentityID: identity, Name: "Plan ZSEM"},
		plans.Upload{ObjectKey: "plans/ab/cd/abcd", CreatedBy: identity})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	t.Cleanup(func() { remove(ctx, repo, plan.ID) })

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
				ObjectKey: "plans/ab/cd/abcd", SizeBytes: int64(i), CreatedBy: identity,
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
	if len(winners) != 1 || len(conflicts) != writers-1 {
		t.Fatalf("%d winners and %d conflicts, want 1 and %d",
			len(winners), len(conflicts), writers-1)
	}
	for _, conflict := range conflicts {
		if conflict.Current != winners[0].Version {
			t.Errorf("a conflict named version %d, the winner is %d",
				conflict.Current, winners[0].Version)
		}
	}
	if timeline, err := repo.Versions(ctx, plan.ID); err != nil || len(timeline) != 2 {
		t.Errorf("timeline = %d versions, %v", len(timeline), err)
	}
}

// core/plans/infra_test.go

package plans_test

import (
	"context"
	"crypto/rand"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/smegg99/s99ab/data/db"

	"github.com/smegg99/goptivum/backend/core/plans"
	"github.com/smegg99/goptivum/backend/core/profiles"
	"github.com/smegg99/goptivum/backend/migrations"
)

func TestInfraProfileNameRacesOnPostgres(t *testing.T) {
	_, conn := postgresState(t)
	repo := profiles.NewRepository(conn, db.Postgres)
	start, outcomes := make(chan struct{}), make(chan error, 8)
	for range 8 {
		go func() {
			<-start
			_, err := repo.Create(t.Context(), profiles.NewProfile{Name: "Same name", CreatedBy: "synthetic"})
			outcomes <- err
		}()
	}
	close(start)
	winners := 0
	for range 8 {
		err := <-outcomes
		var invalid *profiles.Invalid
		if err == nil {
			winners++
		} else if !errors.As(err, &invalid) || invalid.Field != "name" || invalid.Code != "taken" {
			t.Fatalf("concurrent create: %v", err)
		}
	}
	if winners != 1 {
		t.Fatalf("create had %d winners", winners)
	}
	first, err := repo.Create(t.Context(), profiles.NewProfile{Name: "First", CreatedBy: "synthetic"})
	if err != nil {
		t.Fatal(err)
	}
	second, err := repo.Create(t.Context(), profiles.NewProfile{Name: "Second", CreatedBy: "synthetic"})
	if err != nil {
		t.Fatal(err)
	}
	start = make(chan struct{})
	name := "Renamed"
	for _, profile := range []profiles.Profile{first, second} {
		go func() {
			<-start
			_, err := repo.Update(t.Context(), profile.ID, profiles.Editor{ID: "synthetic"}, profiles.Change{Name: &name})
			outcomes <- err
		}()
	}
	close(start)
	winners = 0
	for range 2 {
		err := <-outcomes
		var invalid *profiles.Invalid
		if err == nil {
			winners++
		} else if !errors.As(err, &invalid) || invalid.Field != "name" || invalid.Code != "taken" {
			t.Fatalf("concurrent rename: %v", err)
		}
	}
	if winners != 1 {
		t.Fatalf("rename had %d winners", winners)
	}
}

func TestInfraIndependentPlanPatchesOnPostgres(t *testing.T) {
	repo := postgresRepository(t)
	plan, _, err := repo.Create(t.Context(), plans.Plan{OwnerIdentityID: "synthetic", Name: "Initial"}, plans.Upload{ObjectKey: "source"})
	if err != nil {
		t.Fatal(err)
	}
	for round := range 20 {
		start, outcomes := make(chan struct{}), make(chan error, 2)
		name, year := fmt.Sprintf("Name %d", round), fmt.Sprintf("Year %d", round)
		for _, change := range []plans.Change{{Name: &name}, {SchoolYear: &year}} {
			go func() {
				<-start
				_, err := repo.Update(t.Context(), plan.ID, change)
				outcomes <- err
			}()
		}
		close(start)
		for range 2 {
			if err := <-outcomes; err != nil {
				t.Fatal(err)
			}
		}
		got, err := repo.Get(t.Context(), plan.ID)
		if err != nil || got.Name != name || got.SchoolYear != year {
			t.Fatalf("patches lost a field: %+v, %v", got, err)
		}
	}
}

// postgresRepository connects to the Postgres that "just infra-up" starts.
// SQLite and Postgres differ in how a concurrent write is refused, so the
// version check is exercised on both rather than on the one that is easier.
func postgresRepository(t *testing.T) *plans.Repository {
	repo, _ := postgresState(t)
	return repo
}

func postgresState(t *testing.T) (*plans.Repository, *sql.DB) {
	t.Helper()

	dsn := os.Getenv("GOPTIVUM_TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("no GOPTIVUM_TEST_POSTGRES_DSN - run just infra-up and just test-infra")
	}
	admin, err := db.Open(db.Config{Driver: db.Postgres, DSN: dsn})
	if err != nil {
		t.Fatalf("connect to postgres: %v", err)
	}
	t.Cleanup(func() { admin.Close() })
	var random [8]byte
	if _, err := rand.Read(random[:]); err != nil {
		t.Fatal(err)
	}
	schema := fmt.Sprintf("goptivum_test_%x", random)
	if _, err := admin.ExecContext(t.Context(), `create schema `+schema); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if _, err := admin.ExecContext(ctx, `drop schema `+schema+` cascade`); err != nil {
			t.Errorf("remove owned schema: %v", err)
		}
	})
	configuration, err := pgx.ParseConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	configuration.RuntimeParams["search_path"] = schema
	configuration.RuntimeParams["application_name"] = schema
	conn := stdlib.OpenDB(*configuration)
	t.Cleanup(func() { conn.Close() })

	if err := db.Migrate(conn, migrations.FS, db.Postgres); err != nil {
		t.Fatalf("migrate postgres: %v", err)
	}
	return plans.NewRepository(conn, db.Postgres, plans.Caps{BackupsKeep: backupsKeep, ResultsKeep: 20}), conn
}

// remove purges inside the schema owned by this test.
func remove(ctx context.Context, repo *plans.Repository, id int64) {
	_ = repo.Trash(ctx, id)
	_, _ = repo.Purge(ctx, time.Now().AddDate(0, 0, 1), 0)
}

// listedOnce reports whether id is among plans exactly once. The database is
// isolated per test.
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

func TestInfraConcurrentResultRetentionOnPostgres(t *testing.T) {
	repo := postgresRepository(t)
	plan, _, err := repo.Create(t.Context(), plans.Plan{OwnerIdentityID: "synthetic", Name: "Retention"}, plans.Upload{ObjectKey: "source"})
	if err != nil {
		t.Fatal(err)
	}
	keep := 2
	if _, err := repo.Update(t.Context(), plan.ID, plans.Change{ResultsKeep: &keep}); err != nil {
		t.Fatal(err)
	}
	start, outcomes := make(chan struct{}), make(chan error, 8)
	for n := range 8 {
		go func() {
			<-start
			_, err := repo.AddResult(t.Context(), plans.NewResult{PlanID: plan.ID, SourceVersion: 1, Device: "synthetic", ObjectKey: fmt.Sprintf("result-%d", n)})
			outcomes <- err
		}()
	}
	close(start)
	for range 8 {
		err := <-outcomes
		var invalid *plans.Invalid
		if err != nil && !(errors.As(err, &invalid) && invalid.Field == "solved_at") {
			t.Fatal(err)
		}
	}
	results, err := repo.Results(t.Context(), plan.ID)
	if err != nil || len(results) != keep {
		t.Fatalf("retention: %d results, %v", len(results), err)
	}
}

// Hold the restored row uncommitted until purge has selected its old deletion
// state and is blocked trying to delete it. Committing must make DELETE recheck.
func TestInfraPurgeRechecksRestoredRowsOnPostgres(t *testing.T) {
	for _, result := range []bool{false, true} {
		t.Run(fmt.Sprintf("result=%v", result), func(t *testing.T) {
			repo, conn := postgresState(t)
			plan, _, err := repo.Create(t.Context(), plans.Plan{OwnerIdentityID: "synthetic", Name: "Restore"}, plans.Upload{ObjectKey: "source"})
			if err != nil {
				t.Fatal(err)
			}
			table, id, field := "plans", plan.ID, "id"
			if result {
				added, err := repo.AddResult(t.Context(), plans.NewResult{PlanID: plan.ID, SourceVersion: 1, Device: "synthetic", ObjectKey: "result"})
				if err != nil {
					t.Fatal(err)
				}
				table, id = "solve_results", added.ID
				if err := repo.TrashResult(t.Context(), plan.ID, id); err != nil {
					t.Fatal(err)
				}
			} else if err := repo.Trash(t.Context(), id); err != nil {
				t.Fatal(err)
			}
			tx, err := conn.BeginTx(t.Context(), nil)
			if err != nil {
				t.Fatal(err)
			}
			defer tx.Rollback()
			if _, err := tx.ExecContext(t.Context(), `update `+table+` set deleted_at = null where `+field+` = $1`, id); err != nil {
				t.Fatal(err)
			}
			finished := make(chan error, 1)
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()
			go func() {
				if result {
					_, err := repo.PurgeResults(ctx, time.Now().AddDate(0, 0, 1), 0)
					finished <- err
				} else {
					_, err := repo.Purge(ctx, time.Now().AddDate(0, 0, 1), 0)
					finished <- err
				}
			}()
			ticker := time.NewTicker(10 * time.Millisecond)
			defer ticker.Stop()
			for {
				var blocked bool
				err := conn.QueryRowContext(ctx, `select exists (select 1 from pg_stat_activity where application_name = current_setting('application_name') and wait_event_type = 'Lock')`).Scan(&blocked)
				if err != nil {
					t.Fatal(err)
				}
				if blocked {
					break
				}
				select {
				case err := <-finished:
					t.Fatalf("purge did not wait for restore: %v", err)
				case <-ctx.Done():
					t.Fatal(ctx.Err())
				case <-ticker.C:
				}
			}
			if err := tx.Commit(); err != nil {
				t.Fatal(err)
			}
			if err := <-finished; err != nil {
				t.Fatal(err)
			}
			if result {
				if _, err := repo.Result(t.Context(), plan.ID, id); err != nil {
					t.Fatal(err)
				}
			} else {
				if _, err := repo.Get(t.Context(), id); err != nil {
					t.Fatal(err)
				}
			}
		})
	}
}

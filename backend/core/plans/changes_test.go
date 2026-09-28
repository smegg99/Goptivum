// core/plans/changes_test.go

package plans_test

import (
	"context"
	"errors"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/smegg99/s99ab/data/db"

	"github.com/smegg99/goptivum/backend/core/plans"
)

// point is where the changes stand now, as a desktop starting out learns it.
func point(t *testing.T, repo *plans.Repository) plans.Cursor {
	t.Helper()
	changes, err := repo.Changes(context.Background(), nil, plans.ChangesPage)
	if err != nil {
		t.Fatalf("changes: %v", err)
	}
	if !changes.Reset || len(changes.Plans) != 0 {
		t.Fatalf("a first call = %+v, want a reset and no plans", changes)
	}
	return changes.Cursor
}

// changedSince is every plan that changed after a point, by id, read to the
// end, and the point it ends at.
func changedSince(t *testing.T, repo *plans.Repository, since plans.Cursor) (map[int64]plans.PlanChange, plans.Cursor) {
	t.Helper()
	out := map[int64]plans.PlanChange{}
	for {
		changes, err := repo.Changes(context.Background(), &since, plans.ChangesPage)
		if err != nil {
			t.Fatalf("changes since %s: %v", since, err)
		}
		if changes.Reset {
			t.Fatalf("changes since %s asked to start over", since)
		}
		for _, change := range changes.Plans {
			out[change.PlanID] = merged(out[change.PlanID], change)
		}
		since = changes.Cursor
		if !changes.More {
			return out, since
		}
	}
}

// merged joins two listings of one plan across pages.
func merged(before, after plans.PlanChange) plans.PlanChange {
	if before.PlanID == 0 {
		return after
	}
	seen := map[plans.Part]bool{}
	var parts []plans.Part
	for _, part := range append(before.Parts, after.Parts...) {
		if !seen[part] {
			seen[part] = true
			parts = append(parts, part)
		}
	}
	after.Parts = parts
	return after
}

// expectChanges asserts exactly these plans changed after a point, each with
// these parts and in this state, and returns the point after them.
func expectChanges(t *testing.T, repo *plans.Repository, since plans.Cursor, want ...plans.PlanChange) plans.Cursor {
	t.Helper()
	got, next := changedSince(t, repo, since)
	wanted := map[int64]plans.PlanChange{}
	for _, change := range want {
		wanted[change.PlanID] = change
	}
	if !reflect.DeepEqual(got, wanted) {
		t.Errorf("changed = %+v, want %+v", got, wanted)
	}
	return next
}

func change(planID int64, state plans.State, parts ...plans.Part) plans.PlanChange {
	return plans.PlanChange{PlanID: planID, State: state, Parts: parts}
}

const (
	project  = plans.PartProject
	versions = plans.PartVersions
	results  = plans.PartResults
)

// Every change the desktop mirrors stamps the parts it changed, and nothing
// else: a mirror reads again exactly what moved.
func TestEveryChangeStampsWhatItChanged(t *testing.T) {
	ctx := context.Background()
	repo, conn := repositoryWithConn(t)
	origin := newPlan(t, repo, "identity-1", "Plan 2027")
	name := func(s string) *string { return &s }
	yes := true

	for _, one := range []struct {
		name string
		// act changes something and returns the plan it expects listed, or
		// 0 when nothing should be.
		act  func(t *testing.T) int64
		want func(id int64) []plans.PlanChange
	}{
		{"create", func(t *testing.T) int64 {
			return newPlan(t, repo, "identity-1", "Nowy").ID
		}, func(id int64) []plans.PlanChange { return only(change(id, plans.StateLive, project, versions)) }},
		{"fork", func(t *testing.T) int64 {
			fork, _, err := repo.Fork(ctx, plans.ForkOf{Origin: origin.ID, Owner: "identity-2", Name: "Kopia"})
			if err != nil {
				t.Fatalf("fork: %v", err)
			}
			return fork.ID
		}, func(id int64) []plans.PlanChange { return only(change(id, plans.StateLive, project, versions)) }},
		{"rename", func(t *testing.T) int64 {
			if _, err := repo.Update(ctx, origin.ID, plans.Change{Name: name("Plan 2027 test")}); err != nil {
				t.Fatalf("update: %v", err)
			}
			return origin.ID
		}, func(id int64) []plans.PlanChange { return only(change(id, plans.StateLive, project)) }},
		{"an update that names nothing", func(t *testing.T) int64 {
			if _, err := repo.Update(ctx, origin.ID, plans.Change{}); err != nil {
				t.Fatalf("update: %v", err)
			}
			return 0
		}, nothing},
		{"transfer", func(t *testing.T) int64 {
			if _, err := repo.Transfer(ctx, origin.ID, "identity-2"); err != nil {
				t.Fatalf("transfer: %v", err)
			}
			return origin.ID
		}, func(id int64) []plans.PlanChange { return only(change(id, plans.StateLive, project)) }},
		{"a new version", func(t *testing.T) int64 {
			upload(t, repo, origin.ID, current(t, repo, origin.ID))
			return origin.ID
		}, func(id int64) []plans.PlanChange { return only(change(id, plans.StateLive, project, versions)) }},
		{"name a checkpoint", func(t *testing.T) int64 {
			if _, err := repo.NameCheckpoint(ctx, origin.ID, 1, "tydzień i klasy"); err != nil {
				t.Fatalf("name: %v", err)
			}
			return origin.ID
		}, func(id int64) []plans.PlanChange { return only(change(id, plans.StateLive, versions)) }},
		{"delete a checkpoint", func(t *testing.T) int64 {
			if err := repo.DeleteCheckpoint(ctx, origin.ID, 1); err != nil {
				t.Fatalf("delete: %v", err)
			}
			return origin.ID
		}, func(id int64) []plans.PlanChange { return only(change(id, plans.StateLive, versions)) }},
		{"restore a checkpoint", func(t *testing.T) int64 {
			if _, err := repo.RestoreCheckpoint(ctx, origin.ID, 1); err != nil {
				t.Fatalf("restore: %v", err)
			}
			return origin.ID
		}, func(id int64) []plans.PlanChange { return only(change(id, plans.StateLive, versions)) }},
		{"delete what is no checkpoint", func(t *testing.T) int64 {
			if err := repo.DeleteCheckpoint(ctx, origin.ID, 2); !errors.Is(err, plans.ErrNoVersion) {
				t.Fatalf("delete = %v, want ErrNoVersion", err)
			}
			return 0
		}, nothing},
		{"keep a result", func(t *testing.T) int64 {
			addResult(t, repo, conn, origin.ID, "plans/re/su/lt-1")
			return origin.ID
		}, func(id int64) []plans.PlanChange { return only(change(id, plans.StateLive, results)) }},
		{"pin a result", func(t *testing.T) int64 {
			if _, err := repo.UpdateResult(ctx, origin.ID, firstResult(t, repo, origin.ID),
				plans.ResultChange{Pinned: &yes}); err != nil {
				t.Fatalf("pin: %v", err)
			}
			return origin.ID
		}, func(id int64) []plans.PlanChange { return only(change(id, plans.StateLive, results)) }},
		{"a result change that names nothing", func(t *testing.T) int64 {
			if _, err := repo.UpdateResult(ctx, origin.ID, firstResult(t, repo, origin.ID),
				plans.ResultChange{}); err != nil {
				t.Fatalf("update: %v", err)
			}
			return 0
		}, nothing},
		{"trash a result", func(t *testing.T) int64 {
			if err := repo.TrashResult(ctx, origin.ID, firstResult(t, repo, origin.ID)); err != nil {
				t.Fatalf("trash: %v", err)
			}
			return origin.ID
		}, func(id int64) []plans.PlanChange { return only(change(id, plans.StateLive, results)) }},
		{"trash a result already in Trash", func(t *testing.T) int64 {
			if err := repo.TrashResult(ctx, origin.ID, trashedResult(t, repo)); !errors.Is(err, plans.ErrNoResult) {
				t.Fatalf("trash = %v, want ErrNoResult", err)
			}
			return 0
		}, nothing},
		{"restore a result", func(t *testing.T) int64 {
			if _, err := repo.RestoreResult(ctx, origin.ID, trashedResult(t, repo)); err != nil {
				t.Fatalf("restore: %v", err)
			}
			return origin.ID
		}, func(id int64) []plans.PlanChange { return only(change(id, plans.StateLive, results)) }},
		{"trash the plan", func(t *testing.T) int64 {
			if err := repo.Trash(ctx, origin.ID); err != nil {
				t.Fatalf("trash: %v", err)
			}
			return origin.ID
		}, func(id int64) []plans.PlanChange { return only(change(id, plans.StateTrash, project)) }},
		{"trash a plan already in Trash", func(t *testing.T) int64 {
			if err := repo.Trash(ctx, origin.ID); !errors.Is(err, plans.ErrNotFound) {
				t.Fatalf("trash = %v, want ErrNotFound", err)
			}
			return 0
		}, nothing},
		{"restore the plan", func(t *testing.T) int64 {
			if _, err := repo.Restore(ctx, origin.ID, "identity-2"); err != nil {
				t.Fatalf("restore: %v", err)
			}
			return origin.ID
		}, func(id int64) []plans.PlanChange { return only(change(id, plans.StateLive, project)) }},
		{"restore another owner's plan", func(t *testing.T) int64 {
			if _, err := repo.Restore(ctx, origin.ID, "identity-3"); !errors.Is(err, plans.ErrNotFound) {
				t.Fatalf("restore = %v, want ErrNotFound", err)
			}
			return 0
		}, nothing},
	} {
		t.Run(one.name, func(t *testing.T) {
			since := point(t, repo)
			id := one.act(t)
			expectChanges(t, repo, since, one.want(id)...)
		})
	}
}

func only(change plans.PlanChange) []plans.PlanChange { return []plans.PlanChange{change} }

func nothing(int64) []plans.PlanChange { return nil }

// current is the newest version of a plan.
func current(t *testing.T, repo *plans.Repository, planID int64) int64 {
	t.Helper()
	newest, err := repo.Current(context.Background(), planID)
	if err != nil {
		t.Fatalf("current: %v", err)
	}
	return newest
}

// firstResult is a plan's newest live result.
func firstResult(t *testing.T, repo *plans.Repository, planID int64) int64 {
	t.Helper()
	listed, err := repo.Results(context.Background(), planID)
	if err != nil || len(listed) == 0 {
		t.Fatalf("results = %v, %v", listed, err)
	}
	return listed[0].ID
}

// trashedResult is the one result in Trash of identity-2's plans.
func trashedResult(t *testing.T, repo *plans.Repository) int64 {
	t.Helper()
	trashed, err := repo.TrashedResults(context.Background(), "identity-2")
	if err != nil || len(trashed) != 1 {
		t.Fatalf("trashed results = %+v, %v", trashed, err)
	}
	return trashed[0].Result.ID
}

// The purge and the caps change what a mirror holds without anyone asking,
// so they stamp too. A plan removed for good stays listed as removed, with
// only its project part: its versions and results went with it.
func TestPurgesAndCapsStampWhatTheyRemove(t *testing.T) {
	ctx := context.Background()
	repo, conn := repositoryWithConn(t)
	plan := newPlan(t, repo, "identity-1", "Plan 2027")
	untouched := newPlan(t, repo, "identity-1", "Bez zmian")
	upload(t, repo, plan.ID, 1)
	upload(t, repo, plan.ID, 2)
	upload(t, repo, untouched.ID, 1)
	addResult(t, repo, conn, plan.ID, "plans/re/su/lt-a")
	if _, err := repo.NameCheckpoint(ctx, plan.ID, 1, "początek"); err != nil {
		t.Fatalf("name: %v", err)
	}
	if err := repo.DeleteCheckpoint(ctx, plan.ID, 1); err != nil {
		t.Fatalf("delete checkpoint: %v", err)
	}
	if err := repo.TrashResult(ctx, plan.ID, firstResult(t, repo, plan.ID)); err != nil {
		t.Fatalf("trash result: %v", err)
	}

	t.Run("purged results", func(t *testing.T) {
		since := point(t, repo)
		if purged, err := repo.PurgeResults(ctx, time.Now(), 0); err != nil || purged != 1 {
			t.Fatalf("purge results = %d, %v", purged, err)
		}
		expectChanges(t, repo, since, change(plan.ID, plans.StateLive, results))
	})
	t.Run("ended checkpoints", func(t *testing.T) {
		since := point(t, repo)
		if ended, err := repo.PurgeCheckpoints(ctx, time.Now(), 0); err != nil || ended != 1 {
			t.Fatalf("purge checkpoints = %d, %v", ended, err)
		}
		expectChanges(t, repo, since, change(plan.ID, plans.StateLive, versions))
	})
	t.Run("backups trimmed now", func(t *testing.T) {
		setKeep(t, repo, plan.ID, 1)
		since := point(t, repo)
		if removed, err := repo.TrimBackups(ctx); err != nil || removed == 0 {
			t.Fatalf("trim = %d, %v", removed, err)
		}
		// The untouched plan had nothing past its cap, so it is not listed.
		expectChanges(t, repo, since, change(plan.ID, plans.StateLive, versions))
	})
	t.Run("a plan removed for good", func(t *testing.T) {
		if err := repo.Trash(ctx, plan.ID); err != nil {
			t.Fatalf("trash: %v", err)
		}
		since := point(t, repo)
		if purged, err := repo.Purge(ctx, time.Now(), 0); err != nil || purged != 1 {
			t.Fatalf("purge = %d, %v", purged, err)
		}
		expectChanges(t, repo, since, change(plan.ID, plans.StateRemoved, project))

		// From the very start, the removed plan is one tombstone.
		everything, _ := changedSince(t, repo, plans.Cursor{Epoch: since.Epoch})
		if got := everything[plan.ID]; !reflect.DeepEqual(got, change(plan.ID, plans.StateRemoved, project)) {
			t.Errorf("from the start the removed plan is %+v", got)
		}
	})
}

// A part stamped twice is listed once, at its later point: a poll from the
// point after it lists nothing, and one from before it lists it once.
func TestAPlanChangedTwiceIsListedOnce(t *testing.T) {
	ctx := context.Background()
	repo := repository(t)
	plan := newPlan(t, repo, "identity-1", "Plan 2027")
	since := point(t, repo)

	upload(t, repo, plan.ID, 1)
	middle := point(t, repo)
	upload(t, repo, plan.ID, 2)

	changes, err := repo.Changes(ctx, &since, plans.ChangesPage)
	if err != nil {
		t.Fatalf("changes: %v", err)
	}
	if len(changes.Plans) != 1 || changes.More || changes.Reset {
		t.Fatalf("changes = %+v, want the plan once", changes)
	}
	if !reflect.DeepEqual(changes.Plans[0], change(plan.ID, plans.StateLive, project, versions)) {
		t.Errorf("listed %+v", changes.Plans[0])
	}
	if changes.Cursor.Seq <= middle.Seq {
		t.Errorf("cursor %s is not after %s", changes.Cursor, middle)
	}

	// Nothing changed since the answer's cursor.
	after, err := repo.Changes(ctx, &changes.Cursor, plans.ChangesPage)
	if err != nil || len(after.Plans) != 0 || after.Reset || after.More || after.Cursor != changes.Cursor {
		t.Errorf("a poll with nothing new = %+v, %v", after, err)
	}
}

// Pages end on a stamped part, never inside one transaction's stamps, so
// walking them loses nothing.
func TestPagesLoseNoChange(t *testing.T) {
	ctx := context.Background()
	repo := repository(t)
	since := point(t, repo)
	var created []int64
	for range 5 {
		created = append(created, newPlan(t, repo, "identity-1", "Plan").ID)
	}

	seen := map[int64][]plans.Part{}
	for page := 0; ; page++ {
		changes, err := repo.Changes(ctx, &since, 3)
		if err != nil {
			t.Fatalf("changes: %v", err)
		}
		parts := 0
		for _, change := range changes.Plans {
			seen[change.PlanID] = append(seen[change.PlanID], change.Parts...)
			parts += len(change.Parts)
		}
		if parts > 3 {
			t.Fatalf("page %d holds %d parts, over the limit of 3", page, parts)
		}
		if changes.More && changes.Cursor.Seq <= since.Seq {
			t.Fatalf("page %d did not move the cursor", page)
		}
		since = changes.Cursor
		if !changes.More {
			break
		}
		if page > 10 {
			t.Fatal("the pages never end")
		}
	}
	for _, id := range created {
		if got := seen[id]; !reflect.DeepEqual(got, []plans.Part{project, versions}) {
			t.Errorf("plan %d listed with %v", id, got)
		}
	}
}

// A point from another database, one this database never handed out, or none
// at all, is answered with the current point and a reset: the caller reads
// everything again.
func TestAPointThisDatabaseNeverHandedOutStartsOver(t *testing.T) {
	ctx := context.Background()
	repo := repository(t)
	newPlan(t, repo, "identity-1", "Plan 2027")
	now := point(t, repo)
	if now.Seq == 0 || len(now.Epoch) != 16 {
		t.Fatalf("point = %+v", now)
	}

	for _, since := range []plans.Cursor{
		{Epoch: "0123456789abcdef", Seq: 1},
		{Epoch: now.Epoch, Seq: now.Seq + 1},
	} {
		changes, err := repo.Changes(ctx, &since, plans.ChangesPage)
		if err != nil {
			t.Fatalf("changes since %s: %v", since, err)
		}
		if !changes.Reset || len(changes.Plans) != 0 || changes.Cursor != now {
			t.Errorf("since %s = %+v, want a reset at %s", since, changes, now)
		}
	}
}

// The clock is made once per database and survives a new repository over it,
// as it survives a restart.
func TestThePointSurvivesARestart(t *testing.T) {
	repo, conn := repositoryWithConn(t)
	plan := newPlan(t, repo, "identity-1", "Plan 2027")
	before := point(t, repo)

	restarted := plans.NewRepository(conn, db.SQLite, plans.Caps{BackupsKeep: backupsKeep, ResultsKeep: 20})
	if after := point(t, restarted); after != before {
		t.Fatalf("after a restart the point is %s, was %s", after, before)
	}
	upload(t, restarted, plan.ID, 1)
	expectChanges(t, restarted, before, change(plan.ID, plans.StateLive, project, versions))
}

// A changed thing that is rolled back leaves no stamp: the stamp is part of
// the change's own transaction.
func TestAFailedChangeLeavesNoStamp(t *testing.T) {
	ctx := context.Background()
	repo, conn := repositoryWithConn(t)
	plan := newPlan(t, repo, "identity-1", "Plan 2027")
	since := point(t, repo)

	// A stale parent is refused inside the version's transaction.
	upload(t, repo, plan.ID, 1)
	after := point(t, repo)
	if _, err := repo.AddVersion(ctx, plans.Upload{
		PlanID: plan.ID, Parent: 1, ObjectKey: "plans/st/al/e", SizeBytes: 1, CreatedBy: "identity-1",
	}); err == nil {
		t.Fatal("a stale parent was accepted")
	}
	expectChanges(t, repo, after)
	expectChanges(t, repo, since, change(plan.ID, plans.StateLive, project, versions))

	var stamped int
	if err := conn.QueryRowContext(ctx, `select count(*) from plan_changes`).Scan(&stamped); err != nil {
		t.Fatalf("count: %v", err)
	}
	if stamped != 2 {
		t.Errorf("plan_changes holds %d rows, want 2 (project and versions of one plan)", stamped)
	}
}

func TestACursorReadsBackAsItWasWritten(t *testing.T) {
	cursor := plans.Cursor{Epoch: "0123456789abcdef", Seq: 42}
	parsed, err := plans.ParseCursor(cursor.String())
	if err != nil || parsed != cursor {
		t.Fatalf("parse %q = %+v, %v", cursor.String(), parsed, err)
	}
	for _, raw := range []string{
		"", "42", "0123456789abcdef", "0123456789abcdef.", "0123456789abcdef.-1",
		"0123456789ABCDEF.1", "0123456789abcde.1", "0123456789abcdef.1.2", "0123456789abcdef.x",
		"0123456789abcdef.99999999999999999999",
	} {
		var invalid *plans.Invalid
		if _, err := plans.ParseCursor(raw); !errors.As(err, &invalid) ||
			invalid.Field != "since" || invalid.Code != "invalid" {
			t.Errorf("parse %q = %v, want since: invalid", raw, err)
		}
	}
}

// Changes to different plans at once each get their own point on the clock:
// none is lost and none shares a point with another.
func TestConcurrentChangesAreEachStamped(t *testing.T) {
	ctx := context.Background()
	repo, conn := repositoryWithConn(t)
	var created []plans.Plan
	for range 8 {
		created = append(created, newPlan(t, repo, "identity-1", "Plan"))
	}
	since := point(t, repo)

	var wait sync.WaitGroup
	failures := make(chan error, len(created))
	for _, plan := range created {
		wait.Go(func() {
			if _, err := repo.AddVersion(ctx, plans.Upload{
				PlanID: plan.ID, Parent: 1, ObjectKey: "plans/co/nc/" + plan.Name, SizeBytes: 1, CreatedBy: "identity-1",
			}); err != nil {
				failures <- err
			}
		})
	}
	wait.Wait()
	close(failures)
	for err := range failures {
		t.Fatalf("a concurrent upload failed: %v", err)
	}

	var want []plans.PlanChange
	for _, plan := range created {
		want = append(want, change(plan.ID, plans.StateLive, project, versions))
	}
	expectChanges(t, repo, since, want...)

	var rows, points int
	if err := conn.QueryRowContext(ctx, `select count(*), count(distinct seq) from plan_changes`).Scan(&rows, &points); err != nil {
		t.Fatalf("count: %v", err)
	}
	if rows != points {
		t.Errorf("%d stamps share %d points", rows, points)
	}
}

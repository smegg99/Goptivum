// core/plans/garbage_test.go

package plans_test

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"testing"
	"time"

	"github.com/smegg99/goptivum/backend/core/plans"
)

// wait puts a key into object_garbage as if its row went at since.
func wait(t *testing.T, conn *sql.DB, key string, since time.Time) {
	t.Helper()
	if _, err := conn.Exec(`insert into object_garbage (object_key, since) values (?, ?)`,
		key, since.UTC()); err != nil {
		t.Fatalf("record %s: %v", key, err)
	}
}

// removals records what the sweep asked the store to delete.
type removals struct {
	keys []string
	fail map[string]bool
}

func (r *removals) remove(_ context.Context, key string) error {
	if r.fail[key] {
		return errors.New("storage: connection refused")
	}
	r.keys = append(r.keys, key)
	return nil
}

func TestSweepDeletesOnlyOldKeysNoRowNames(t *testing.T) {
	ctx := context.Background()
	repo, conn := repositoryWithConn(t)
	plan := newPlan(t, repo, "identity-1", "Plan 2027")
	now := time.Now().UTC()

	wait(t, conn, "plans/old", now.Add(-2*time.Hour))
	wait(t, conn, "plans/young", now.Add(-10*time.Minute))
	// A key a version names again, as when an undo brings back a document
	// whose older copy the cap had removed.
	wait(t, conn, "plans/ab/cd/abcd", now.Add(-2*time.Hour))
	wait(t, conn, "plans/broken", now.Add(-2*time.Hour))

	store := &removals{fail: map[string]bool{"plans/broken": true}}
	deleted, err := repo.Sweep(ctx, now, store.remove)
	if err == nil {
		t.Error("a failed delete was not reported")
	}
	if deleted != 1 || !reflect.DeepEqual(store.keys, []string{"plans/old"}) {
		t.Errorf("swept %d: %v, want only plans/old", deleted, store.keys)
	}
	// The named key leaves the table without its object being touched, the
	// young one waits, and the failed one stays for the next sweep.
	if got, want := garbage(t, conn), []string{"plans/broken", "plans/young"}; !reflect.DeepEqual(got, want) {
		t.Errorf("garbage = %v, want %v", got, want)
	}
	if _, err := repo.Version(ctx, plan.ID, 1); err != nil {
		t.Errorf("the named version: %v", err)
	}

	// An hour later the young key is due too.
	store.fail = nil
	deleted, err = repo.Sweep(ctx, now.Add(time.Hour), store.remove)
	if err != nil || deleted != 2 {
		t.Errorf("second sweep deleted %d, %v; want 2", deleted, err)
	}
	if got := garbage(t, conn); len(got) != 0 {
		t.Errorf("garbage = %v, want none", got)
	}
}

// An upload about to store a key the sweep is waiting on pushes the sweep
// back, so the object cannot go between its write and its row.
func TestReviveHoldsOffTheSweep(t *testing.T) {
	ctx := context.Background()
	repo, conn := repositoryWithConn(t)
	now := time.Now().UTC()
	wait(t, conn, "plans/again", now.Add(-2*time.Hour))

	if err := repo.Revive(ctx, "plans/again"); err != nil {
		t.Fatalf("revive: %v", err)
	}
	if err := repo.Revive(ctx, "plans/never-waited"); err != nil {
		t.Errorf("reviving a key that was not waiting: %v", err)
	}
	store := &removals{}
	if deleted, err := repo.Sweep(ctx, now.Add(30*time.Minute), store.remove); err != nil || deleted != 0 {
		t.Errorf("swept %d, %v; want nothing within the hour after a revive", deleted, err)
	}
	if got := garbage(t, conn); !reflect.DeepEqual(got, []string{"plans/again"}) {
		t.Errorf("garbage = %v", got)
	}
}

// A purged plan's objects wait for the sweep instead of staying for ever.
func TestPurgeRecordsTheObjectsOfWhatItRemoves(t *testing.T) {
	ctx := context.Background()
	repo, conn := repositoryWithConn(t)
	gone := newPlan(t, repo, "identity-1", "Do usunięcia")
	upload(t, repo, gone.ID, 1)
	kept := newPlan(t, repo, "identity-1", "Plan 2027")
	upload(t, repo, kept.ID, 1)

	if err := repo.Trash(ctx, gone.ID); err != nil {
		t.Fatalf("trash: %v", err)
	}
	if purged, err := repo.Purge(ctx, time.Now().AddDate(0, 0, 31), 30); err != nil || purged != 1 {
		t.Fatalf("purge: %d, %v", purged, err)
	}
	want := []string{"plans/ab/cd/abcd", fmt.Sprintf("plans/%d/v2", gone.ID)}
	sort.Strings(want)
	if got := garbage(t, conn); !reflect.DeepEqual(got, want) {
		t.Errorf("garbage = %v, want %v", got, want)
	}

	// The first version's object is shared with the plan that stayed, so the
	// sweep drops it from the list and deletes only the other.
	store := &removals{}
	if deleted, err := repo.Sweep(ctx, time.Now().Add(2*time.Hour), store.remove); err != nil || deleted != 1 {
		t.Errorf("sweep deleted %d, %v; want 1", deleted, err)
	}
	if !reflect.DeepEqual(store.keys, []string{fmt.Sprintf("plans/%d/v2", gone.ID)}) {
		t.Errorf("deleted %v", store.keys)
	}
	if _, err := repo.Version(ctx, kept.ID, 1); err != nil {
		t.Errorf("the plan that stayed lost version 1: %v", err)
	}
	if _, err := repo.Current(ctx, gone.ID); !errors.Is(err, plans.ErrNotFound) {
		t.Errorf("current of the purged plan = %v, want ErrNotFound", err)
	}
}

// A refused upload stored its object first; discarding the key lets the sweep
// take it an hour later, unless a version names the same bytes.
func TestDiscardedKeysAreSweptUnlessNamed(t *testing.T) {
	ctx := context.Background()
	repo, conn := repositoryWithConn(t)
	newPlan(t, repo, "identity-1", "Plan 2027")

	for _, key := range []string{"plans/refused", "plans/ab/cd/abcd"} {
		if err := repo.Discard(ctx, key); err != nil {
			t.Fatalf("discard %s: %v", key, err)
		}
	}
	if err := repo.Discard(ctx, "plans/refused"); err != nil {
		t.Errorf("discarding a key twice: %v", err)
	}
	store := &removals{}
	if deleted, err := repo.Sweep(ctx, time.Now().Add(30*time.Minute), store.remove); err != nil || deleted != 0 {
		t.Errorf("within the hour, swept %d, %v", deleted, err)
	}
	if deleted, err := repo.Sweep(ctx, time.Now().Add(2*time.Hour), store.remove); err != nil || deleted != 1 {
		t.Errorf("after the hour, swept %d, %v; want 1", deleted, err)
	}
	if !reflect.DeepEqual(store.keys, []string{"plans/refused"}) {
		t.Errorf("deleted %v, want only the unnamed key", store.keys)
	}
	if got := garbage(t, conn); len(got) != 0 {
		t.Errorf("garbage = %v, want none", got)
	}
}

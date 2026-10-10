// core/plans/trash.go

package plans

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// RemovesAt is when a plan trashed at deletedAt is removed for good. Purge
// uses the same arithmetic, so the date Trash shows is the date it happens.
func RemovesAt(deletedAt time.Time, trashDays int) time.Time {
	return deletedAt.AddDate(0, 0, trashDays)
}

// Trash moves a live plan to Trash. Its versions, results and jobs stay, so a
// restore brings back everything.
func (r *Repository) Trash(ctx context.Context, id int64) error {
	const update = `update plans set deleted_at = ? where id = ? and deleted_at is null`
	return r.changePlan(ctx, id, update, now(), id)
}

// Trashed returns one owner's plans in Trash, most recently deleted first.
func (r *Repository) Trashed(ctx context.Context, ownerIdentityID string) ([]Plan, error) {
	query := `select ` + planColumns + ` from plans
		where owner_identity_id = ? and deleted_at is not null
		order by deleted_at desc, id desc`
	return r.plans(ctx, query, ownerIdentityID)
}

// Restore takes one owner's plan out of Trash. Restoring a live plan changes
// nothing and succeeds, because undo may ask twice.
func (r *Repository) Restore(ctx context.Context, id int64, ownerIdentityID string) (Plan, error) {
	const update = `update plans set deleted_at = null where id = ? and owner_identity_id = ?`
	if err := r.changePlan(ctx, id, update, id, ownerIdentityID); err != nil {
		return Plan{}, err
	}
	return r.Get(ctx, id)
}

// Purged is what one purge removed from Trash.
type Purged struct {
	Plans       int64
	Checkpoints int64
	Results     int64
}

// PurgeTrash removes for good what has been in Trash for trashDays: plans
// first, which takes their checkpoints and results with them, then the
// checkpoints and results of live plans. With 0 days it empties Trash.
func (r *Repository) PurgeTrash(ctx context.Context, at time.Time, trashDays int) (Purged, error) {
	plans, err := r.Purge(ctx, at, trashDays)
	checkpoints, checkpointErr := r.PurgeCheckpoints(ctx, at, trashDays)
	results, resultErr := r.PurgeResults(ctx, at, trashDays)
	return Purged{Plans: plans, Checkpoints: checkpoints, Results: results},
		errors.Join(err, checkpointErr, resultErr)
}

// Purge removes for good every plan whose removal date has come, with its
// versions, results, exported files and jobs by cascade. Their objects stay,
// since other plans may share them: their keys go to object_garbage in the
// same transaction, and the sweep deletes each once no row names it.
func (r *Repository) Purge(ctx context.Context, at time.Time, trashDays int) (int64, error) {
	cutoff := at.UTC().AddDate(0, 0, -trashDays)
	tx, err := r.conn.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("plans: purge: %w", err)
	}
	defer tx.Rollback()

	// The write comes first, so the transaction holds the lock before the
	// delete reads which plans are due.
	for _, table := range []string{"plan_versions", "solve_results", "plan_exports"} {
		record := `insert into object_garbage (object_key, since)
			select object_key, ? from ` + table + `
			where plan_id in (select id from plans where deleted_at is not null and deleted_at <= ?)
			on conflict (object_key) do nothing`
		if _, err := tx.ExecContext(ctx, r.rebind(record), now(), cutoff); err != nil {
			return 0, fmt.Errorf("plans: purge: record objects of %s: %w", table, err)
		}
	}
	// Removed by id, so exactly the plans read here are the ones entombed.
	due, err := planIDsIn(ctx, tx, r.rebind(`select id from plans where deleted_at is not null and deleted_at <= ?`), cutoff)
	if err != nil {
		return 0, fmt.Errorf("plans: purge: %w", err)
	}
	var removed []int64
	for _, id := range due {
		result, err := tx.ExecContext(ctx, r.rebind(`delete from plans where id = ? and deleted_at is not null and deleted_at <= ?`), id, cutoff)
		if err != nil {
			return 0, fmt.Errorf("plans: purge %d: %w", id, err)
		}
		count, err := result.RowsAffected()
		if err != nil {
			return 0, fmt.Errorf("plans: purge %d: %w", id, err)
		}
		if count > 0 {
			removed = append(removed, id)
		}
	}
	purged := int64(len(removed))
	if err := r.entomb(ctx, tx, removed); err != nil {
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("plans: purge: %w", err)
	}
	return purged, nil
}

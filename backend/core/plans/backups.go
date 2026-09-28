// core/plans/backups.go

package plans

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// trim removes for good the oldest plain versions of a plan beyond keep,
// counting only versions below newest, and returns how many it removed.
// Checkpoints, live or in Trash, are neither counted nor removed, and newest
// itself never is. A version a pending or running solve reads is counted but
// stays until the solve ends: its result is built from it. The object keys of
// the removed versions and of the files exported from them go to
// object_garbage in the same transaction, which must already hold the write
// lock.
func (r *Repository) trim(ctx context.Context, tx *sql.Tx, planID, newest int64, keep int) (int64, error) {
	const plain = `select version from plan_versions
		where plan_id = ? and version < ? and checkpoint_name is null
		order by version desc`
	rows, err := tx.QueryContext(ctx, r.rebind(plain), planID, newest)
	if err != nil {
		return 0, fmt.Errorf("plans: backups of %d: %w", planID, err)
	}
	var backups []int64
	for rows.Next() {
		var version int64
		if err := rows.Scan(&version); err != nil {
			rows.Close()
			return 0, fmt.Errorf("plans: backups of %d: %w", planID, err)
		}
		backups = append(backups, version)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, fmt.Errorf("plans: backups of %d: %w", planID, err)
	}
	if len(backups) <= keep {
		return 0, nil
	}
	// Every plain version at or below the newest one past the cap goes.
	cutoff := backups[keep]

	const record = `insert into object_garbage (object_key, since)
		select object_key, ? from plan_versions
		where plan_id = ? and version <= ? and checkpoint_name is null and ` + notSolving + `
		on conflict (object_key) do nothing`
	if _, err := tx.ExecContext(ctx, r.rebind(record), now(), planID, cutoff, planID); err != nil {
		return 0, fmt.Errorf("plans: record trimmed objects of %d: %w", planID, err)
	}
	if err := r.recordExports(ctx, tx, `plan_id = ? and version in (select version from plan_versions
		where plan_id = ? and version <= ? and checkpoint_name is null and `+notSolving+`)`,
		planID, planID, cutoff, planID); err != nil {
		return 0, err
	}
	const remove = `delete from plan_versions
		where plan_id = ? and version <= ? and checkpoint_name is null and ` + notSolving
	result, err := tx.ExecContext(ctx, r.rebind(remove), planID, cutoff, planID)
	if err != nil {
		return 0, fmt.Errorf("plans: trim backups of %d: %w", planID, err)
	}
	removed, err := result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("plans: trim backups of %d: %w", planID, err)
	}
	return removed, nil
}

// TrimBackups applies the backups cap in force to every plan now, every
// owner's and those in Trash too, as a new version would, and returns how many
// versions it removed. Each plan is its own transaction, so one that fails
// leaves the others trimmed.
func (r *Repository) TrimBackups(ctx context.Context) (int64, error) {
	ids, err := r.planIDs(ctx, `select id from plans order by id`)
	if err != nil {
		return 0, fmt.Errorf("plans: trim backups: %w", err)
	}
	var (
		removed  int64
		failures []error
	)
	for _, id := range ids {
		count, err := r.trimPlan(ctx, id)
		if err != nil {
			failures = append(failures, err)
			continue
		}
		removed += count
	}
	return removed, errors.Join(failures...)
}

// trimPlan applies one plan's cap in one transaction. Its first statement is a
// write that changes nothing, so the transaction holds the write lock before
// trim reads the timeline, as every other trim does.
func (r *Repository) trimPlan(ctx context.Context, planID int64) (int64, error) {
	tx, err := r.conn.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("plans: trim backups of %d: %w", planID, err)
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, r.rebind(`update plans set id = id where id = ?`), planID); err != nil {
		return 0, fmt.Errorf("plans: trim backups of %d: %w", planID, err)
	}
	var newest sql.NullInt64
	if err := tx.QueryRowContext(ctx, r.rebind(`select max(version) from plan_versions where plan_id = ?`),
		planID).Scan(&newest); err != nil {
		return 0, fmt.Errorf("plans: trim backups of %d: %w", planID, err)
	}
	if !newest.Valid {
		return 0, nil
	}
	keep, err := r.planKeep(ctx, tx, planID)
	if err != nil {
		return 0, err
	}
	removed, err := r.trim(ctx, tx, planID, newest.Int64, keep)
	if err != nil || removed == 0 {
		return 0, err
	}
	if err := r.stamp(ctx, tx, parts(planID, PartVersions)); err != nil {
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("plans: trim backups of %d: %w", planID, err)
	}
	return removed, nil
}

// planIDs reads a list of plan ids.
func (r *Repository) planIDs(ctx context.Context, query string, args ...any) ([]int64, error) {
	return planIDsIn(ctx, r.conn, r.rebind(query), args...)
}

type querier interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
}

// planIDsIn reads a list of ids through a pool or a transaction, from a query
// already rebound.
func planIDsIn(ctx context.Context, on querier, query string, args ...any) ([]int64, error) {
	rows, err := on.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// notSolving leaves out the versions a pending or running solve of the plan
// reads, named by the plan id it binds. The statuses are the solving client's
// two open ones.
const notSolving = `version not in (select plan_version from solve_jobs
	where plan_id = ? and status in ('pending', 'running'))`

// planKeep reads the backups cap in force for a plan, live or in Trash, inside
// a transaction.
func (r *Repository) planKeep(ctx context.Context, tx *sql.Tx, planID int64) (int, error) {
	var keep sql.NullInt64
	if err := tx.QueryRowContext(ctx, r.rebind(`select backups_keep from plans where id = ?`),
		planID).Scan(&keep); err != nil {
		return 0, fmt.Errorf("plans: backups cap of %d: %w", planID, err)
	}
	return r.keepFor(Plan{BackupsKeep: int(keep.Int64)}), nil
}

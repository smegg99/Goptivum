// core/plans/garbage.go

package plans

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// SweepAge is how long a key waits in object_garbage before the sweep may
// delete its object. It covers an upload that stores its object before the
// row that names it.
const SweepAge = time.Hour

// Revive pushes back the sweep of a key that is about to be stored again.
// Called before the object is written, it waits for a sweep that is deleting
// that key, so the write lands after the delete and never before it.
func (r *Repository) Revive(ctx context.Context, key string) error {
	const touch = `update object_garbage set since = ? where object_key = ?`
	if _, err := r.conn.ExecContext(ctx, r.rebind(touch), now(), key); err != nil {
		return fmt.Errorf("plans: revive %s: %w", key, err)
	}
	return nil
}

// Discard puts a key whose upload was refused into object_garbage, so the
// object it stored before the refusal does not stay for ever. A key another
// row names is only dropped from the list by the sweep.
func (r *Repository) Discard(ctx context.Context, key string) error {
	const record = `insert into object_garbage (object_key, since) values (?, ?)
		on conflict (object_key) do update set since = excluded.since`
	if _, err := r.conn.ExecContext(ctx, r.rebind(record), key, now()); err != nil {
		return fmt.Errorf("plans: discard %s: %w", key, err)
	}
	return nil
}

// Sweep deletes the objects in object_garbage that waited SweepAge and that no
// version, result or exported file names, through remove, and returns how many
// it deleted. A key a row names again leaves the table without its object
// being touched; it comes back when that row goes. Each key is its own
// transaction that holds the key's row while remove runs, so a Revive waits
// for it. A key whose remove fails stays for the next sweep.
func (r *Repository) Sweep(ctx context.Context, at time.Time,
	remove func(context.Context, string) error) (int, error) {
	cutoff := at.UTC().Add(-SweepAge)
	rows, err := r.conn.QueryContext(ctx,
		r.rebind(`select object_key from object_garbage where since <= ?`), cutoff)
	if err != nil {
		return 0, fmt.Errorf("plans: sweep: %w", err)
	}
	var keys []string
	for rows.Next() {
		var key string
		if err := rows.Scan(&key); err != nil {
			rows.Close()
			return 0, fmt.Errorf("plans: sweep: %w", err)
		}
		keys = append(keys, key)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, fmt.Errorf("plans: sweep: %w", err)
	}

	deleted := 0
	var failures []error
	for _, key := range keys {
		gone, err := r.sweepOne(ctx, key, cutoff, remove)
		if err != nil {
			failures = append(failures, err)
			continue
		}
		if gone {
			deleted++
		}
	}
	return deleted, errors.Join(failures...)
}

// sweepOne settles one key and reports whether its object was deleted.
func (r *Repository) sweepOne(ctx context.Context, key string, cutoff time.Time,
	remove func(context.Context, string) error) (bool, error) {
	tx, err := r.conn.BeginTx(ctx, nil)
	if err != nil {
		return false, fmt.Errorf("plans: sweep %s: %w", key, err)
	}
	defer tx.Rollback()

	// The write comes first, so this transaction holds the lock before it
	// reads, and a Revive since the listing leaves nothing to take.
	const take = `delete from object_garbage where object_key = ? and since <= ?`
	result, err := tx.ExecContext(ctx, r.rebind(take), key, cutoff)
	if err != nil {
		return false, fmt.Errorf("plans: sweep %s: %w", key, err)
	}
	if taken, err := result.RowsAffected(); err != nil || taken == 0 {
		return false, err
	}

	var named int
	const naming = `select (select count(*) from plan_versions where object_key = ?)
		+ (select count(*) from solve_results where object_key = ?)
		+ (select count(*) from plan_exports where object_key = ?)`
	if err := tx.QueryRowContext(ctx, r.rebind(naming), key, key, key).Scan(&named); err != nil {
		return false, fmt.Errorf("plans: sweep %s: %w", key, err)
	}
	if named > 0 {
		return false, commit(tx, key)
	}
	if err := remove(ctx, key); err != nil {
		return false, fmt.Errorf("plans: sweep %s: %w", key, err)
	}
	return true, commit(tx, key)
}

func commit(tx *sql.Tx, key string) error {
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("plans: sweep %s: commit: %w", key, err)
	}
	return nil
}

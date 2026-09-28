// core/plans/storage.go

package plans

import (
	"context"
	"fmt"
	"slices"
	"strings"
)

// Files is a part of a project: how many stored files and their sizes.
type Files struct {
	Count int
	Bytes int64
}

// Usage is what one plan stores. The main file is the newest version; the
// backups are the older ones, checkpoints in Trash not; the results are those
// not in Trash; Trash is the checkpoints and results in Trash. A plan in Trash
// keeps its parts where they are and says so with its DeletedAt.
type Usage struct {
	Plan     Plan
	MainFile Files
	Backups  Files
	Results  Files
	Trash    Files
}

func (u Usage) bytes() int64 {
	return u.MainFile.Bytes + u.Backups.Bytes + u.Results.Bytes + u.Trash.Bytes
}

// Stored is what the object store holds for the index: every object a version,
// a result or an exported file names, each counted once, and the objects
// nothing names that wait for the sweep.
type Stored struct {
	Objects int
	Bytes   int64
	Waiting int
}

// Usage splits every owner's plans, those in Trash included, into their parts,
// largest first. Sizes are the sizes each row names: objects are keyed by
// content, so plans that share bytes each count them.
func (r *Repository) Usage(ctx context.Context) ([]Usage, error) {
	every, err := r.plans(ctx, `select `+planColumns+` from plans`)
	if err != nil {
		return nil, fmt.Errorf("plans: usage: %w", err)
	}
	byID := make(map[int64]*Usage, len(every))
	for _, plan := range every {
		byID[plan.ID] = &Usage{Plan: plan}
	}

	// The newest version is the main file; below it, a checkpoint in Trash is
	// in Trash and every other version is a backup.
	const newest = `(select max(version) from plan_versions n where n.plan_id = v.plan_id)`
	parts := []struct {
		query string
		part  func(*Usage) *Files
	}{
		{`select plan_id, count(*), ` + sum + ` from plan_versions v
			where version = ` + newest + ` group by plan_id`,
			func(u *Usage) *Files { return &u.MainFile }},
		{`select plan_id, count(*), ` + sum + ` from plan_versions v
			where version < ` + newest + ` and checkpoint_deleted_at is null group by plan_id`,
			func(u *Usage) *Files { return &u.Backups }},
		{`select plan_id, count(*), ` + sum + ` from plan_versions v
			where version < ` + newest + ` and checkpoint_deleted_at is not null group by plan_id`,
			func(u *Usage) *Files { return &u.Trash }},
		{`select plan_id, count(*), ` + sum + ` from solve_results
			where deleted_at is null group by plan_id`,
			func(u *Usage) *Files { return &u.Results }},
		{`select plan_id, count(*), ` + sum + ` from solve_results
			where deleted_at is not null group by plan_id`,
			func(u *Usage) *Files { return &u.Trash }},
	}
	for _, part := range parts {
		if err := r.addFiles(ctx, part.query, byID, part.part); err != nil {
			return nil, err
		}
	}

	out := make([]Usage, 0, len(every))
	for _, plan := range every {
		out = append(out, *byID[plan.ID])
	}
	slices.SortStableFunc(out, func(a, b Usage) int {
		if a.bytes() != b.bytes() {
			if a.bytes() > b.bytes() {
				return -1
			}
			return 1
		}
		return strings.Compare(a.Plan.Name, b.Plan.Name)
	})
	return out, nil
}

// sum is a size total that reads as an integer on both engines: Postgres
// sums a bigint into a numeric.
const sum = `cast(coalesce(sum(size_bytes), 0) as bigint)`

// addFiles adds one grouped count and size per plan onto a part of its usage.
func (r *Repository) addFiles(ctx context.Context, query string, byID map[int64]*Usage,
	part func(*Usage) *Files) error {
	rows, err := r.conn.QueryContext(ctx, query)
	if err != nil {
		return fmt.Errorf("plans: usage: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var (
			planID int64
			files  Files
		)
		if err := rows.Scan(&planID, &files.Count, &files.Bytes); err != nil {
			return fmt.Errorf("plans: usage: %w", err)
		}
		if usage, ok := byID[planID]; ok {
			target := part(usage)
			target.Count += files.Count
			target.Bytes += files.Bytes
		}
	}
	return rows.Err()
}

// Stored counts the objects the index names, each once, and those waiting for
// the sweep that no row names.
func (r *Repository) Stored(ctx context.Context) (Stored, error) {
	var stored Stored
	const named = `select count(*), ` + sum + ` from (
		select object_key, max(size_bytes) as size_bytes from (
			select object_key, size_bytes from plan_versions
			union all
			select object_key, size_bytes from solve_results
			union all
			select object_key, size_bytes from plan_exports
		) as named_rows group by object_key
	) as objects`
	if err := r.conn.QueryRowContext(ctx, named).Scan(&stored.Objects, &stored.Bytes); err != nil {
		return Stored{}, fmt.Errorf("plans: stored objects: %w", err)
	}
	waiting, err := r.Waiting(ctx)
	if err != nil {
		return Stored{}, err
	}
	stored.Waiting = waiting
	return stored, nil
}

// Waiting counts the keys in object_garbage that no version, result or
// exported file names, which the sweep deletes once they have waited
// SweepAge.
func (r *Repository) Waiting(ctx context.Context) (int, error) {
	const unnamed = `select count(*) from object_garbage g
		where not exists (select 1 from plan_versions v where v.object_key = g.object_key)
		and not exists (select 1 from solve_results s where s.object_key = g.object_key)
		and not exists (select 1 from plan_exports e where e.object_key = g.object_key)`
	var waiting int
	if err := r.conn.QueryRowContext(ctx, unnamed).Scan(&waiting); err != nil {
		return 0, fmt.Errorf("plans: waiting objects: %w", err)
	}
	return waiting, nil
}

// core/plans/fork.go

package plans

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// ForkOf is an own copy of a live plan: a fork of another owner's plan, or a
// duplicate of one's own.
type ForkOf struct {
	Origin int64
	// Version is the origin's version to copy, 0 for its newest.
	Version int64
	Owner   string
	Name    string
}

// Fork stores a new plan of fork.Owner whose version 1 is the origin's chosen
// version: the same stored object, with that version's validity dates. The
// origin's school year comes with it; its other versions, checkpoints,
// results, jobs and caps do not. The copy records its origin.
func (r *Repository) Fork(ctx context.Context, fork ForkOf) (Plan, Version, error) {
	if fork.Owner == "" {
		return Plan{}, Version{}, fmt.Errorf("plans: a plan needs an owner")
	}
	name, _, err := Clean(fork.Name, "")
	if err != nil {
		return Plan{}, Version{}, err
	}
	id, err := newID()
	if err != nil {
		return Plan{}, Version{}, err
	}

	tx, err := r.conn.BeginTx(ctx, nil)
	if err != nil {
		return Plan{}, Version{}, fmt.Errorf("plans: begin fork: %w", err)
	}
	defer tx.Rollback()

	origin, err := scanPlan(tx.QueryRowContext(ctx,
		r.rebind(`select `+planColumns+` from plans where id = ? and deleted_at is null`), fork.Origin))
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return Plan{}, Version{}, fmt.Errorf("%w: %d", ErrNotFound, fork.Origin)
	case err != nil:
		return Plan{}, Version{}, fmt.Errorf("plans: fork %d: %w", fork.Origin, err)
	}
	number := fork.Version
	if number == 0 {
		var newest sql.NullInt64
		if err := tx.QueryRowContext(ctx, r.rebind(`select max(version) from plan_versions where plan_id = ?`),
			origin.ID).Scan(&newest); err != nil {
			return Plan{}, Version{}, fmt.Errorf("plans: fork %d: %w", origin.ID, err)
		}
		number = newest.Int64
	}
	// Revived before the version is read: a sweep that already took the key
	// found no row naming it, so the read below finds none either.
	const revive = `update object_garbage set since = ?
		where object_key = (select object_key from plan_versions where plan_id = ? and version = ?)`
	if _, err := tx.ExecContext(ctx, r.rebind(revive), now(), origin.ID, number); err != nil {
		return Plan{}, Version{}, fmt.Errorf("plans: fork %d: revive: %w", origin.ID, err)
	}
	source, err := scanVersion(tx.QueryRowContext(ctx,
		r.rebind(`select `+versionColumns+` from plan_versions where plan_id = ? and version = ?`),
		origin.ID, number))
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return Plan{}, Version{}, fmt.Errorf("%w: plan %d version %d", ErrNoVersion, origin.ID, number)
	case err != nil:
		return Plan{}, Version{}, fmt.Errorf("plans: fork %d version %d: %w", origin.ID, number, err)
	}

	stamp := now()
	plan := Plan{
		ID: id, OwnerIdentityID: fork.Owner, Name: name, SchoolYear: origin.SchoolYear,
		CreatedAt: stamp, UpdatedAt: stamp, Origin: Origin{PlanID: origin.ID, Version: source.Version},
	}
	version := Version{
		PlanID: id, Version: 1, ObjectKey: source.ObjectKey, SizeBytes: source.SizeBytes,
		CreatedAt: stamp, CreatedBy: fork.Owner, ValidFrom: source.ValidFrom, ValidTo: source.ValidTo,
	}
	const insertPlan = `insert into plans
		(id, owner_identity_id, name, school_year, created_at, updated_at, origin_plan_id, origin_version)
		values (?, ?, ?, ?, ?, ?, ?, ?)`
	if _, err := tx.ExecContext(ctx, r.rebind(insertPlan),
		plan.ID, plan.OwnerIdentityID, plan.Name, plan.SchoolYear, plan.CreatedAt, plan.UpdatedAt,
		plan.Origin.PlanID, plan.Origin.Version); err != nil {
		return Plan{}, Version{}, fmt.Errorf("plans: fork %d: %w", origin.ID, err)
	}
	const insertFirst = `insert into plan_versions
		(plan_id, version, parent, object_key, size_bytes, created_at, created_by, valid_from, valid_to)
		values (?, ?, ?, ?, ?, ?, ?, ?, ?)`
	if _, err := tx.ExecContext(ctx, r.rebind(insertFirst),
		version.PlanID, version.Version, nil, version.ObjectKey, version.SizeBytes,
		version.CreatedAt, version.CreatedBy,
		nullableDate(version.ValidFrom), nullableDate(version.ValidTo)); err != nil {
		return Plan{}, Version{}, fmt.Errorf("plans: fork %d: first version: %w", origin.ID, err)
	}
	if err := r.stamp(ctx, tx, parts(plan.ID, PartProject, PartVersions)); err != nil {
		return Plan{}, Version{}, err
	}
	if err := tx.Commit(); err != nil {
		return Plan{}, Version{}, fmt.Errorf("plans: commit fork of %d: %w", origin.ID, err)
	}
	return plan, version, nil
}

// Transfer gives a live plan to another owner, with its results, checkpoints
// and Trash items. It moves updated_at like any other change.
func (r *Repository) Transfer(ctx context.Context, id int64, owner string) (Plan, error) {
	if owner == "" {
		return Plan{}, fmt.Errorf("plans: a plan needs an owner")
	}
	const update = `update plans set owner_identity_id = ?, updated_at = ?
		where id = ? and deleted_at is null`
	if err := r.changePlan(ctx, id, update, owner, now(), id); err != nil {
		return Plan{}, err
	}
	return r.Get(ctx, id)
}

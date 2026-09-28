// core/plans/checkpoints.go

package plans

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"
)

// TrashedCheckpoint is a checkpoint in Trash, with the live plan it names a
// version of.
type TrashedCheckpoint struct {
	Plan      Plan
	Version   int64
	Name      string
	DeletedAt time.Time
}

// CleanCheckpointName trims a checkpoint name and refuses what the contract
// refuses, with an *Invalid.
func CleanCheckpointName(name string) (string, error) {
	name = strings.TrimSpace(name)
	switch {
	case name == "":
		return "", &Invalid{Field: "name", Code: "required"}
	case utf8.RuneCountInString(name) > MaxNameLength:
		return "", &Invalid{Field: "name", Code: "too_long"}
	}
	return name, nil
}

// NameCheckpoint names a version, which makes it a checkpoint the backups cap
// never removes. Naming a checkpoint again renames it, and naming one in Trash
// takes it out. The plan is the caller's to check.
func (r *Repository) NameCheckpoint(ctx context.Context, planID, version int64, name string) (Version, error) {
	name, err := CleanCheckpointName(name)
	if err != nil {
		return Version{}, err
	}
	const update = `update plan_versions set checkpoint_name = ?, checkpoint_deleted_at = null
		where plan_id = ? and version = ?`
	if err := r.changeVersion(ctx, planID, version, update, name, planID, version); err != nil {
		return Version{}, err
	}
	return r.Version(ctx, planID, version)
}

// DeleteCheckpoint moves a live checkpoint to Trash. The version keeps its
// name there, so the backups cap still leaves it alone and a restore names it
// again. A version that is not a live checkpoint is ErrNoVersion.
func (r *Repository) DeleteCheckpoint(ctx context.Context, planID, version int64) error {
	const update = `update plan_versions set checkpoint_deleted_at = ?
		where plan_id = ? and version = ? and checkpoint_name is not null and checkpoint_deleted_at is null`
	return r.changeVersion(ctx, planID, version, update, now(), planID, version)
}

// RestoreCheckpoint takes a checkpoint out of Trash. Restoring a live one
// changes nothing and succeeds, because undo may ask twice. A version that is
// no checkpoint is ErrNoVersion.
func (r *Repository) RestoreCheckpoint(ctx context.Context, planID, version int64) (Version, error) {
	const update = `update plan_versions set checkpoint_deleted_at = null
		where plan_id = ? and version = ? and checkpoint_name is not null`
	if err := r.changeVersion(ctx, planID, version, update, planID, version); err != nil {
		return Version{}, err
	}
	return r.Version(ctx, planID, version)
}

// changeVersion runs one checkpoint update and stamps the plan's versions, in
// one transaction. An update that matches no row is ErrNoVersion and stamps
// nothing.
func (r *Repository) changeVersion(ctx context.Context, planID, version int64, update string, args ...any) error {
	tx, err := r.conn.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("plans: begin: %w", err)
	}
	defer tx.Rollback()

	result, err := tx.ExecContext(ctx, r.rebind(update), args...)
	if err != nil {
		return fmt.Errorf("plans: checkpoint %d of %d: %w", version, planID, err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("plans: checkpoint %d of %d: %w", version, planID, err)
	}
	if affected == 0 {
		return fmt.Errorf("%w: plan %d version %d", ErrNoVersion, planID, version)
	}
	if err := r.stamp(ctx, tx, parts(planID, PartVersions)); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("plans: commit checkpoint %d of %d: %w", version, planID, err)
	}
	return nil
}

// TrashedCheckpoints returns one owner's checkpoints in Trash, most recently
// deleted first. Those of a plan in Trash are part of that plan and are not
// listed.
func (r *Repository) TrashedCheckpoints(ctx context.Context, ownerIdentityID string) ([]TrashedCheckpoint, error) {
	const query = `select p.id, p.owner_identity_id, p.name, p.school_year, p.created_at,
		p.updated_at, p.deleted_at, p.backups_keep,
		v.version, v.checkpoint_name, v.checkpoint_deleted_at
		from plan_versions v join plans p on p.id = v.plan_id
		where p.owner_identity_id = ? and p.deleted_at is null
		and v.checkpoint_deleted_at is not null
		order by v.checkpoint_deleted_at desc, p.id desc, v.version desc`
	rows, err := r.conn.QueryContext(ctx, r.rebind(query), ownerIdentityID)
	if err != nil {
		return nil, fmt.Errorf("plans: trashed checkpoints: %w", err)
	}
	defer rows.Close()

	out := []TrashedCheckpoint{}
	for rows.Next() {
		var (
			item    TrashedCheckpoint
			deleted sql.NullTime
			keep    sql.NullInt64
		)
		if err := rows.Scan(&item.Plan.ID, &item.Plan.OwnerIdentityID, &item.Plan.Name,
			&item.Plan.SchoolYear, &item.Plan.CreatedAt, &item.Plan.UpdatedAt, &deleted, &keep,
			&item.Version, &item.Name, &item.DeletedAt); err != nil {
			return nil, fmt.Errorf("plans: trashed checkpoints: %w", err)
		}
		item.Plan.CreatedAt = item.Plan.CreatedAt.UTC()
		item.Plan.UpdatedAt = item.Plan.UpdatedAt.UTC()
		item.Plan.BackupsKeep = int(keep.Int64)
		item.DeletedAt = item.DeletedAt.UTC()
		out = append(out, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("plans: trashed checkpoints: %w", err)
	}
	return out, nil
}

// PurgeCheckpoints ends every checkpoint whose time in Trash is up: its
// version loses the name and becomes a plain backup, and the backups cap is
// applied to its plan at once, so a version older than the cap goes for good.
// It returns how many checkpoints ended.
func (r *Repository) PurgeCheckpoints(ctx context.Context, at time.Time, trashDays int) (int64, error) {
	cutoff := at.UTC().AddDate(0, 0, -trashDays)
	rows, err := r.conn.QueryContext(ctx, r.rebind(`select distinct plan_id from plan_versions
		where checkpoint_deleted_at is not null and checkpoint_deleted_at <= ?`), cutoff)
	if err != nil {
		return 0, fmt.Errorf("plans: purge checkpoints: %w", err)
	}
	var planIDs []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return 0, fmt.Errorf("plans: purge checkpoints: %w", err)
		}
		planIDs = append(planIDs, id)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, fmt.Errorf("plans: purge checkpoints: %w", err)
	}

	var (
		ended    int64
		failures []error
	)
	for _, planID := range planIDs {
		count, err := r.purgeCheckpointsOf(ctx, planID, cutoff)
		if err != nil {
			failures = append(failures, err)
			continue
		}
		ended += count
	}
	return ended, errors.Join(failures...)
}

// purgeCheckpointsOf ends one plan's expired checkpoints and trims its
// backups, in one transaction that starts with the write.
func (r *Repository) purgeCheckpointsOf(ctx context.Context, planID int64, cutoff time.Time) (int64, error) {
	tx, err := r.conn.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("plans: purge checkpoints of %d: %w", planID, err)
	}
	defer tx.Rollback()

	const unname = `update plan_versions set checkpoint_name = null, checkpoint_deleted_at = null
		where plan_id = ? and checkpoint_deleted_at is not null and checkpoint_deleted_at <= ?`
	result, err := tx.ExecContext(ctx, r.rebind(unname), planID, cutoff)
	if err != nil {
		return 0, fmt.Errorf("plans: purge checkpoints of %d: %w", planID, err)
	}
	ended, err := result.RowsAffected()
	if err != nil || ended == 0 {
		return 0, err
	}

	var newest int64
	if err := tx.QueryRowContext(ctx, r.rebind(`select max(version) from plan_versions where plan_id = ?`),
		planID).Scan(&newest); err != nil {
		return 0, fmt.Errorf("plans: purge checkpoints of %d: %w", planID, err)
	}
	keep, err := r.planKeep(ctx, tx, planID)
	if err != nil {
		return 0, err
	}
	if _, err := r.trim(ctx, tx, planID, newest, keep); err != nil {
		return 0, err
	}
	if err := r.stamp(ctx, tx, parts(planID, PartVersions)); err != nil {
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("plans: purge checkpoints of %d: %w", planID, err)
	}
	return ended, nil
}

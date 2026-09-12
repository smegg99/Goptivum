// core/plans/versions.go

package plans

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// ErrNoVersion means the plan exists but that version does not.
var ErrNoVersion = errors.New("plans: no such version")

// errRefused is internal: the conditional insert matched nothing, so the
// parent the upload named is not the newest version.
var errRefused = errors.New("plans: upload refused")

// Version is one immutable document in a plan's timeline. Parent is 0 for the
// first version. ValidFrom and ValidTo are zero when the plan does not say.
type Version struct {
	PlanID    int64
	Version   int64
	Parent    int64
	ObjectKey string
	SizeBytes int64
	CreatedAt time.Time
	CreatedBy string
	ValidFrom time.Time
	ValidTo   time.Time
}

// Upload is a new version derived from Parent. Parent is 0 for the first one.
type Upload struct {
	PlanID    int64
	Parent    int64
	ObjectKey string
	SizeBytes int64
	CreatedBy string
	ValidFrom time.Time
	ValidTo   time.Time
}

// Conflict is a refused upload. It carries the version that is current, so a
// client can say who saved first instead of overwriting their work.
type Conflict struct {
	PlanID  int64
	Parent  int64
	Current int64
}

func (c *Conflict) Error() string {
	return fmt.Sprintf("plans: plan %d is at version %d, not %d",
		c.PlanID, c.Current, c.Parent)
}

// AddVersion stores a new version if the parent it names is still the newest.
//
// There is no lock. A stale parent is refused and the refusal carries the
// current version; a lease would buy only the courtesy of "someone has this
// open" at the cost of TTLs, heartbeats and stale-lock recovery.
func (r *Repository) AddVersion(ctx context.Context, upload Upload) (Version, error) {
	if upload.ObjectKey == "" {
		return Version{}, fmt.Errorf("plans: a version needs an object key")
	}
	if upload.SizeBytes < 0 {
		return Version{}, fmt.Errorf("plans: a version cannot be %d bytes", upload.SizeBytes)
	}
	if upload.Parent < 0 {
		return Version{}, fmt.Errorf("plans: a parent cannot be version %d", upload.Parent)
	}

	// The plan is checked before the transaction opens. A transaction that
	// reads before it writes cannot upgrade its lock while another holds a
	// read lock on the same rows, which SQLite reports as a busy database
	// instead of waiting; starting with the write makes the loser wait.
	if _, err := r.Get(ctx, upload.PlanID); err != nil {
		return Version{}, err
	}

	version, err := r.insertVersion(ctx, upload)
	if err == nil {
		return version, nil
	}

	// A refused insert and a lost race are the same answer to the caller: the
	// parent it named is no longer current. The race surfaces as a duplicate
	// primary key, which every engine spells differently, so the current
	// version decides rather than the driver's error text.
	if current, readErr := r.Current(ctx, upload.PlanID); readErr == nil {
		if errors.Is(err, errRefused) || current > upload.Parent {
			return Version{}, &Conflict{
				PlanID: upload.PlanID, Parent: upload.Parent, Current: current,
			}
		}
	}
	return Version{}, err
}

// insertVersion inserts only while the parent is still the newest version, in
// one statement, so two uploads cannot both pass the check.
func (r *Repository) insertVersion(ctx context.Context, upload Upload) (Version, error) {
	version := Version{
		PlanID:    upload.PlanID,
		Version:   upload.Parent + 1,
		Parent:    upload.Parent,
		ObjectKey: upload.ObjectKey,
		SizeBytes: upload.SizeBytes,
		CreatedAt: now(),
		CreatedBy: upload.CreatedBy,
		ValidFrom: upload.ValidFrom,
		ValidTo:   upload.ValidTo,
	}

	tx, err := r.conn.BeginTx(ctx, nil)
	if err != nil {
		return Version{}, fmt.Errorf("plans: begin: %w", err)
	}
	defer tx.Rollback()

	const columns = `insert into plan_versions
		(plan_id, version, parent, object_key, size_bytes, created_at, created_by, valid_from, valid_to)
		select ?, ?, ?, ?, ?, ?, ?, ?, ? where `
	// The first version is the one with nothing before it; a later one must
	// name the newest version there is.
	const firstOnly = `not exists (select 1 from plan_versions where plan_id = ?)`
	const afterParent = `exists (select 1 from plan_versions where plan_id = ? and version = ?)
		and not exists (select 1 from plan_versions where plan_id = ? and version > ?)`

	args := []any{
		version.PlanID, version.Version, nullableParent(version.Parent),
		version.ObjectKey, version.SizeBytes, version.CreatedAt, version.CreatedBy,
		nullableDate(version.ValidFrom), nullableDate(version.ValidTo),
	}
	query := columns + firstOnly
	if version.Parent == 0 {
		args = append(args, version.PlanID)
	} else {
		query = columns + afterParent
		args = append(args, version.PlanID, version.Parent, version.PlanID, version.Parent)
	}

	result, err := tx.ExecContext(ctx, r.rebind(query), args...)
	if err != nil {
		return Version{}, fmt.Errorf("plans: add version: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return Version{}, fmt.Errorf("plans: add version: %w", err)
	}
	if affected == 0 {
		return Version{}, errRefused
	}

	if _, err := tx.ExecContext(ctx, r.rebind(`update plans set updated_at = ? where id = ?`),
		version.CreatedAt, version.PlanID); err != nil {
		return Version{}, fmt.Errorf("plans: touch plan: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return Version{}, fmt.Errorf("plans: commit version: %w", err)
	}
	return version, nil
}

// Current is the newest version of a plan, or 0 when it has none.
func (r *Repository) Current(ctx context.Context, planID int64) (int64, error) {
	var exists int
	err := r.conn.QueryRowContext(ctx, r.rebind(`select 1 from plans where id = ?`), planID).Scan(&exists)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return 0, fmt.Errorf("%w: %d", ErrNotFound, planID)
	case err != nil:
		return 0, fmt.Errorf("plans: current version of %d: %w", planID, err)
	}

	var current sql.NullInt64
	if err := r.conn.QueryRowContext(ctx,
		r.rebind(`select max(version) from plan_versions where plan_id = ?`),
		planID).Scan(&current); err != nil {
		return 0, fmt.Errorf("plans: current version of %d: %w", planID, err)
	}
	return current.Int64, nil
}

// Version returns one version of a plan.
func (r *Repository) Version(ctx context.Context, planID, version int64) (Version, error) {
	const query = `select plan_id, version, parent, object_key, size_bytes,
		created_at, created_by, valid_from, valid_to
		from plan_versions where plan_id = ? and version = ?`

	row := r.conn.QueryRowContext(ctx, r.rebind(query), planID, version)
	found, err := scanVersion(row)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return Version{}, fmt.Errorf("%w: plan %d version %d", ErrNoVersion, planID, version)
	case err != nil:
		return Version{}, fmt.Errorf("plans: version %d of %d: %w", version, planID, err)
	}
	return found, nil
}

// Latest returns the newest version of a plan.
func (r *Repository) Latest(ctx context.Context, planID int64) (Version, error) {
	const query = `select plan_id, version, parent, object_key, size_bytes,
		created_at, created_by, valid_from, valid_to
		from plan_versions where plan_id = ? order by version desc limit 1`

	row := r.conn.QueryRowContext(ctx, r.rebind(query), planID)
	found, err := scanVersion(row)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return Version{}, fmt.Errorf("%w: plan %d has none", ErrNoVersion, planID)
	case err != nil:
		return Version{}, fmt.Errorf("plans: latest version of %d: %w", planID, err)
	}
	return found, nil
}

// Versions returns a plan's timeline, oldest first.
func (r *Repository) Versions(ctx context.Context, planID int64) ([]Version, error) {
	const query = `select plan_id, version, parent, object_key, size_bytes,
		created_at, created_by, valid_from, valid_to
		from plan_versions where plan_id = ? order by version`

	rows, err := r.conn.QueryContext(ctx, r.rebind(query), planID)
	if err != nil {
		return nil, fmt.Errorf("plans: versions of %d: %w", planID, err)
	}
	defer rows.Close()

	var out []Version
	for rows.Next() {
		version, err := scanVersion(rows)
		if err != nil {
			return nil, fmt.Errorf("plans: versions of %d: %w", planID, err)
		}
		out = append(out, version)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("plans: versions of %d: %w", planID, err)
	}
	return out, nil
}

// scanner is a row or a rows cursor, which scan identically.
type scanner interface {
	Scan(dest ...any) error
}

func scanVersion(row scanner) (Version, error) {
	var (
		version   Version
		parent    sql.NullInt64
		validFrom sql.NullTime
		validTo   sql.NullTime
	)
	if err := row.Scan(&version.PlanID, &version.Version, &parent, &version.ObjectKey,
		&version.SizeBytes, &version.CreatedAt, &version.CreatedBy,
		&validFrom, &validTo); err != nil {
		return Version{}, err
	}
	version.Parent = parent.Int64
	version.CreatedAt = version.CreatedAt.UTC()
	if validFrom.Valid {
		version.ValidFrom = validFrom.Time.UTC()
	}
	if validTo.Valid {
		version.ValidTo = validTo.Time.UTC()
	}
	return version, nil
}

// nullableParent keeps the first version's parent null, which is what says it
// has none.
func nullableParent(parent int64) any {
	if parent == 0 {
		return nil
	}
	return parent
}

func nullableDate(date time.Time) any {
	if date.IsZero() {
		return nil
	}
	return date
}

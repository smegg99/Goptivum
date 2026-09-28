// core/plans/exports.go

package plans

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// ErrNoExport means no file is kept under that key.
var ErrNoExport = errors.New("plans: no such export")

// Export is a file built from one version or one result and kept in object
// storage, so a second request for it is answered without building it again.
type Export struct {
	ObjectKey string
	PlanID    int64
	// Version is set for a file built from a version, ResultID for one built
	// from a result; the other is zero.
	Version  int64
	ResultID int64
	Format   string
	// Variant is what the bytes depend on besides the source, such as the
	// recipient a .gpf is encrypted to; a file built with another is stale.
	Variant   string
	SizeBytes int64
	CreatedAt time.Time
}

const exportColumns = `object_key, plan_id, version, result_id, format, variant, size_bytes, created_at`

// Export is the file kept under key.
func (r *Repository) Export(ctx context.Context, key string) (Export, error) {
	query := `select ` + exportColumns + ` from plan_exports where object_key = ?`
	var (
		export   Export
		version  sql.NullInt64
		resultID sql.NullInt64
	)
	err := r.conn.QueryRowContext(ctx, r.rebind(query), key).Scan(&export.ObjectKey, &export.PlanID,
		&version, &resultID, &export.Format, &export.Variant, &export.SizeBytes, &export.CreatedAt)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return Export{}, fmt.Errorf("%w: %s", ErrNoExport, key)
	case err != nil:
		return Export{}, fmt.Errorf("plans: export %s: %w", key, err)
	}
	export.Version, export.ResultID = version.Int64, resultID.Int64
	export.CreatedAt = export.CreatedAt.UTC()
	return export, nil
}

// KeepExport records a file stored at its key, replacing what the key named
// before. The version or the result it was built from must still be the
// plan's: otherwise it is ErrNoVersion or ErrNoResult and nothing is kept, so
// no row names a file that nothing would remove. A source removed after the
// check fails the insert on its foreign key.
func (r *Repository) KeepExport(ctx context.Context, export Export) (Export, error) {
	var (
		source  string
		args    []any
		missing error
	)
	switch {
	case export.Version > 0 && export.ResultID == 0:
		source = `select count(*) from plan_versions where plan_id = ? and version = ?`
		args = []any{export.PlanID, export.Version}
		missing = fmt.Errorf("%w: plan %d version %d", ErrNoVersion, export.PlanID, export.Version)
	case export.ResultID > 0 && export.Version == 0:
		source = `select count(*) from solve_results where plan_id = ? and id = ?`
		args = []any{export.PlanID, export.ResultID}
		missing = fmt.Errorf("%w: plan %d result %d", ErrNoResult, export.PlanID, export.ResultID)
	default:
		return Export{}, fmt.Errorf("plans: export %s names neither one version nor one result", export.ObjectKey)
	}

	tx, err := r.conn.BeginTx(ctx, nil)
	if err != nil {
		return Export{}, fmt.Errorf("plans: keep export %s: %w", export.ObjectKey, err)
	}
	defer tx.Rollback()
	var found int
	if err := tx.QueryRowContext(ctx, r.rebind(source), args...).Scan(&found); err != nil {
		return Export{}, fmt.Errorf("plans: keep export %s: %w", export.ObjectKey, err)
	}
	if found == 0 {
		return Export{}, missing
	}

	export.CreatedAt = now()
	const keep = `insert into plan_exports (` + exportColumns + `)
		values (?, ?, ?, ?, ?, ?, ?, ?)
		on conflict (object_key) do update set
			plan_id = excluded.plan_id, version = excluded.version, result_id = excluded.result_id,
			format = excluded.format, variant = excluded.variant,
			size_bytes = excluded.size_bytes, created_at = excluded.created_at`
	if _, err := tx.ExecContext(ctx, r.rebind(keep), export.ObjectKey, export.PlanID,
		nullableID(export.Version), nullableID(export.ResultID), export.Format, export.Variant,
		export.SizeBytes, export.CreatedAt); err != nil {
		return Export{}, fmt.Errorf("plans: keep export %s: %w", export.ObjectKey, err)
	}
	if err := tx.Commit(); err != nil {
		return Export{}, fmt.Errorf("plans: keep export %s: %w", export.ObjectKey, err)
	}
	return export, nil
}

// nullableID stores zero as null.
func nullableID(id int64) any {
	if id == 0 {
		return nil
	}
	return id
}

// recordExports puts the keys of the files the condition selects into
// object_garbage, inside a transaction about to remove what they were built
// from; the rows then go by cascade and the sweep deletes the files.
func (r *Repository) recordExports(ctx context.Context, tx *sql.Tx, condition string, args ...any) error {
	record := `insert into object_garbage (object_key, since)
		select object_key, ? from plan_exports where ` + condition + `
		on conflict (object_key) do nothing`
	if _, err := tx.ExecContext(ctx, r.rebind(record), append([]any{now()}, args...)...); err != nil {
		return fmt.Errorf("plans: record exported files: %w", err)
	}
	return nil
}

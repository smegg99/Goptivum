// core/plans/versions.go

package plans

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"regexp"
	"time"

	"github.com/smegg99/goptivum/backend/common/messages"
)

// ErrNoVersion means the plan exists but that version does not.
var ErrNoVersion = errors.New("plans: no such version")

// errRefused is internal: the conditional insert matched nothing, so the
// parent the upload named is not the newest version.
var errRefused = errors.New("plans: upload refused")

// Version is one immutable document in a plan's timeline. Parent is 0 for the
// first version, which Create stores with the plan. ValidFrom and ValidTo are
// zero when the plan does not say.
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
	// Summary counts what changed from the parent, nil when none was sent.
	Summary Summary
	// UploadKey is the client's key for the upload, empty when the server
	// made the version itself.
	UploadKey string
	// Checkpoint is the version's checkpoint name, empty when it is none.
	Checkpoint string
	// CheckpointDeletedAt is when the checkpoint went to Trash, zero while it
	// is live.
	CheckpointDeletedAt time.Time
}

// Upload is a new version derived from Parent. Create takes the first one
// without a parent; AddVersion always needs one.
type Upload struct {
	PlanID    int64
	Parent    int64
	ObjectKey string
	SizeBytes int64
	CreatedBy string
	// ValidFrom and ValidTo are nil to keep the parent's date and a zero time
	// to clear it. Create has no parent, so nil there means no date.
	ValidFrom *time.Time
	ValidTo   *time.Time
	Summary   Summary
	// UploadKey makes the upload repeatable: AddVersion answers a key the
	// plan already stored with that version. Empty for versions the server
	// makes itself.
	UploadKey string
}

// uploadKeyPattern is what a client's key looks like: random, URL-safe, long
// enough not to collide.
var uploadKeyPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{16,64}$`)

// CheckUploadKey refuses a client's upload key with an *Invalid.
func CheckUploadKey(key string) error {
	switch {
	case key == "":
		return &Invalid{Field: "upload_key", Code: "required"}
	case !uploadKeyPattern.MatchString(key):
		return &Invalid{Field: "upload_key", Code: "invalid"}
	}
	return nil
}

// Conflict is a refused upload. It carries the version that is current, so a
// client can say who saved first instead of overwriting their work.
type Conflict struct {
	PlanID  int64
	Parent  int64
	Current int64
}

func (c *Conflict) Error() string { return c.fault().Error() }
func (c *Conflict) Unwrap() error { return c.fault() }

// fault carries the versions, because "somebody saved first" is useless without
// saying whose version is current.
func (c *Conflict) fault() *messages.Fault {
	return messages.NewFault(messages.KeyPlansVersionConflict,
		map[string]any{"Current": c.Current, "Parent": c.Parent})
}

// AddVersion stores a new version if the parent it names is still the newest,
// then removes the oldest plain versions beyond the plan's backups cap.
//
// There is no lock. A stale parent is refused and the refusal carries the
// current version; a lease would buy only the courtesy of "someone has this
// open" at the cost of TTLs, heartbeats and stale-lock recovery. An upload
// key the plan already stored answers with that version before the parent is
// looked at, because a retried upload's parent is stale by construction.
func (r *Repository) AddVersion(ctx context.Context, upload Upload) (Version, error) {
	if upload.ObjectKey == "" {
		return Version{}, fmt.Errorf("plans: a version needs an object key")
	}
	if upload.SizeBytes < 0 {
		return Version{}, fmt.Errorf("plans: a version cannot be %d bytes", upload.SizeBytes)
	}
	// Every plan is born with version 1, so a later version names the one it
	// came from.
	if upload.Parent < 1 {
		return Version{}, fmt.Errorf("plans: a later version needs a parent, not version %d", upload.Parent)
	}
	if upload.UploadKey != "" {
		if err := CheckUploadKey(upload.UploadKey); err != nil {
			return Version{}, err
		}
	}
	normal, summary, err := summaryText(upload.Summary)
	if err != nil {
		return Version{}, err
	}
	upload.Summary = normal

	// The plan is checked before the transaction opens. A transaction that
	// reads before it writes cannot upgrade its lock while another holds a
	// read lock on the same rows, which SQLite reports as a busy database
	// instead of waiting; starting with the write makes the loser wait.
	plan, err := r.Get(ctx, upload.PlanID)
	if err != nil {
		return Version{}, err
	}
	if stored, found, err := r.replay(ctx, upload); found || err != nil {
		return stored, err
	}
	validFrom, validTo, err := r.validity(ctx, upload)
	if err != nil {
		return Version{}, err
	}

	version, err := r.insertVersion(ctx, Version{
		PlanID:    upload.PlanID,
		Version:   upload.Parent + 1,
		Parent:    upload.Parent,
		ObjectKey: upload.ObjectKey,
		SizeBytes: upload.SizeBytes,
		CreatedAt: now(),
		CreatedBy: upload.CreatedBy,
		ValidFrom: validFrom,
		ValidTo:   validTo,
		Summary:   upload.Summary,
		UploadKey: upload.UploadKey,
	}, summary, r.keepFor(plan))
	if err == nil {
		return version, nil
	}
	// Two sends of one upload racing: the loser answers with the winner.
	if stored, found, replayErr := r.replay(ctx, upload); found || replayErr != nil {
		if !found {
			return Version{}, errors.Join(err, replayErr)
		}
		return stored, replayErr
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

// replay finds the version an earlier send of the same upload stored. The
// same key with another document or parent is a client bug, refused rather
// than answered with a version that is not what was sent.
func (r *Repository) replay(ctx context.Context, upload Upload) (Version, bool, error) {
	if upload.UploadKey == "" {
		return Version{}, false, nil
	}
	query := `select ` + versionColumns + ` from plan_versions where plan_id = ? and upload_key = ?`
	stored, err := scanVersion(r.conn.QueryRowContext(ctx, r.rebind(query), upload.PlanID, upload.UploadKey))
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return Version{}, false, nil
	case err != nil:
		return Version{}, false, fmt.Errorf("plans: upload key of %d: %w", upload.PlanID, err)
	case stored.ObjectKey != upload.ObjectKey || stored.Parent != upload.Parent:
		return Version{}, true, &Invalid{Field: "upload_key", Code: "reused"}
	}
	return stored, true, nil
}

// validity resolves the dates a new version carries: the parent's unless the
// upload says otherwise. A parent that is not there leaves them empty, and the
// insert then refuses the upload anyway.
func (r *Repository) validity(ctx context.Context, upload Upload) (time.Time, time.Time, error) {
	if upload.ValidFrom != nil && upload.ValidTo != nil {
		return upload.ValidFrom.UTC(), upload.ValidTo.UTC(), nil
	}
	parent, err := r.Version(ctx, upload.PlanID, upload.Parent)
	if err != nil && !errors.Is(err, ErrNoVersion) {
		return time.Time{}, time.Time{}, err
	}
	return dateOr(upload.ValidFrom, parent.ValidFrom), dateOr(upload.ValidTo, parent.ValidTo), nil
}

// dateOr is the date an upload sent, or fallback when it sent none.
func dateOr(date *time.Time, fallback time.Time) time.Time {
	if date == nil {
		return fallback
	}
	if date.IsZero() {
		return time.Time{}
	}
	return date.UTC()
}

// insertVersion inserts only while the parent is still the newest version, in
// one statement, so two uploads cannot both pass the check. The backups cap is
// applied in the same transaction, so no reader sees more than keep backups.
func (r *Repository) insertVersion(ctx context.Context, version Version, summary any, keep int) (Version, error) {
	tx, err := r.conn.BeginTx(ctx, nil)
	if err != nil {
		return Version{}, fmt.Errorf("plans: begin: %w", err)
	}
	defer tx.Rollback()

	// The parent must be the newest version there is.
	const query = `insert into plan_versions
		(plan_id, version, parent, object_key, size_bytes, created_at, created_by, valid_from, valid_to,
		 summary, upload_key)
		select ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?
		where exists (select 1 from plan_versions where plan_id = ? and version = ?)
		and not exists (select 1 from plan_versions where plan_id = ? and version > ?)`

	result, err := tx.ExecContext(ctx, r.rebind(query),
		version.PlanID, version.Version, version.Parent,
		version.ObjectKey, version.SizeBytes, version.CreatedAt, version.CreatedBy,
		nullableDate(version.ValidFrom), nullableDate(version.ValidTo),
		summary, nullableText(version.UploadKey),
		version.PlanID, version.Parent, version.PlanID, version.Parent)
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
	if _, err := r.trim(ctx, tx, version.PlanID, version.Version, keep); err != nil {
		return Version{}, err
	}

	if _, err := tx.ExecContext(ctx, r.rebind(`update plans set updated_at = ? where id = ?`),
		version.CreatedAt, version.PlanID); err != nil {
		return Version{}, fmt.Errorf("plans: touch plan: %w", err)
	}
	// The plan's updated_at moved too, so its project part changed with it.
	if err := r.stamp(ctx, tx, parts(version.PlanID, PartProject, PartVersions)); err != nil {
		return Version{}, err
	}
	if err := tx.Commit(); err != nil {
		return Version{}, fmt.Errorf("plans: commit version: %w", err)
	}
	return version, nil
}

// Current is the newest version of a live plan.
func (r *Repository) Current(ctx context.Context, planID int64) (int64, error) {
	var exists int
	err := r.conn.QueryRowContext(ctx,
		r.rebind(`select 1 from plans where id = ? and deleted_at is null`), planID).Scan(&exists)
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

// versionColumns is what scanVersion reads, in its order.
const versionColumns = `plan_id, version, parent, object_key, size_bytes,
	created_at, created_by, valid_from, valid_to,
	summary, upload_key, checkpoint_name, checkpoint_deleted_at`

// Version returns one version of a plan.
func (r *Repository) Version(ctx context.Context, planID, version int64) (Version, error) {
	query := `select ` + versionColumns + ` from plan_versions where plan_id = ? and version = ?`

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
	query := `select ` + versionColumns + ` from plan_versions where plan_id = ? order by version desc limit 1`

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

// Versions returns a plan's timeline, oldest first. The backups cap leaves
// gaps in the numbering.
func (r *Repository) Versions(ctx context.Context, planID int64) ([]Version, error) {
	query := `select ` + versionColumns + ` from plan_versions where plan_id = ? order by version`

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
		version      Version
		parent       sql.NullInt64
		validFrom    sql.NullTime
		validTo      sql.NullTime
		summary      sql.NullString
		uploadKey    sql.NullString
		checkpoint   sql.NullString
		checkpointAt sql.NullTime
	)
	if err := row.Scan(&version.PlanID, &version.Version, &parent, &version.ObjectKey,
		&version.SizeBytes, &version.CreatedAt, &version.CreatedBy,
		&validFrom, &validTo, &summary, &uploadKey, &checkpoint, &checkpointAt); err != nil {
		return Version{}, err
	}
	version.Parent = parent.Int64
	version.UploadKey = uploadKey.String
	version.Checkpoint = checkpoint.String
	if checkpointAt.Valid {
		version.CheckpointDeletedAt = checkpointAt.Time.UTC()
	}
	if summary.Valid {
		parsed, err := ParseSummary([]byte(summary.String))
		if err != nil {
			return Version{}, fmt.Errorf("plans: stored summary: %w", err)
		}
		version.Summary = parsed
	}
	version.CreatedAt = version.CreatedAt.UTC()
	if validFrom.Valid {
		version.ValidFrom = validFrom.Time.UTC()
	}
	if validTo.Valid {
		version.ValidTo = validTo.Time.UTC()
	}
	return version, nil
}

func nullableDate(date time.Time) any {
	if date.IsZero() {
		return nil
	}
	return date
}

func nullableText(text string) any {
	if text == "" {
		return nil
	}
	return text
}

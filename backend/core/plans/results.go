// core/plans/results.go

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

// ErrNoResult means the plan has no such result, or none out of Trash where
// only a live one will do.
var ErrNoResult = errors.New("plans: no such result")

// Result is one timetable a solve kept: a whole document of the school the
// solve read, a version's or another result's, with the timetable. Everything but Name, Pinned and DeletedAt is
// written once.
type Result struct {
	ID     int64
	PlanID int64
	// JobID is the server solve that kept it, 0 for a result solved on a device.
	JobID int64
	// Device is the device that solved it and uploaded it, empty for a server solve.
	Device        string
	SourceVersion int64
	ObjectKey     string
	SizeBytes     int64
	CreatedAt     time.Time
	StartedBy     string
	StartedByName string
	// RuleProfile is the profile the solve used in place of the plan's rules, nil for the plan's own.
	RuleProfile *RuleProfile
	// Quality is the node's 0-100 rating, nil when it sent none.
	Quality *float64
	Verdict string
	// Name is empty until someone names the result.
	Name   string
	Pinned bool
	// DeletedAt is when the result went to Trash, zero while it is listed.
	DeletedAt time.Time
}

// RuleProfile is the rule profile a result's solve used, as the solve's job recorded it.
type RuleProfile struct {
	// Name is a built-in's name, or a school profile's name when the solve started.
	Name string
	// ID is the school profile's id, 0 for a built-in.
	ID int64
	// Rules is the rule_config the solve request carried, as the solving client encoded it.
	Rules string
}

// NewResult is a result to store. Its id is the repository's.
type NewResult struct {
	PlanID int64
	// JobID is the server solve that kept it, 0 for a result solved on a device.
	JobID int64
	// Device and UploadKey name a result solved on a device: its name, and the
	// key its upload carries so a retry keeps one result.
	Device        string
	UploadKey     string
	SourceVersion int64
	ObjectKey     string
	SizeBytes     int64
	// CreatedAt is when its solve ended; zero is now.
	CreatedAt     time.Time
	StartedBy     string
	StartedByName string
	RuleProfile   *RuleProfile
	Quality       *float64
	Verdict       string
}

// ResultChange names or pins a result. A nil field is left as it is; an empty
// name removes the name.
type ResultChange struct {
	Name   *string
	Pinned *bool
}

// TrashedResult is a result in Trash, with the live plan it belongs to.
type TrashedResult struct {
	Plan   Plan
	Result Result
}

// AddResult stores a result, then removes for good the oldest unpinned results
// beyond the plan's results cap, in one transaction that starts with the
// write. A plan in Trash takes the result too: it comes back with the plan.
//
// An upload key the plan already stored answers with that result and changes
// nothing, so a device's retry keeps one result; the same key with another
// object or source version is refused with upload_key: reused.
func (r *Repository) AddResult(ctx context.Context, add NewResult) (Result, error) {
	if add.ObjectKey == "" {
		return Result{}, fmt.Errorf("plans: a result needs an object key")
	}
	if add.UploadKey != "" {
		if err := CheckUploadKey(add.UploadKey); err != nil {
			return Result{}, err
		}
	}
	id, err := newID()
	if err != nil {
		return Result{}, err
	}
	created := now()
	if !add.CreatedAt.IsZero() {
		created = add.CreatedAt.UTC().Truncate(time.Microsecond)
	}
	result := Result{
		ID: id, PlanID: add.PlanID, JobID: add.JobID, Device: add.Device, SourceVersion: add.SourceVersion,
		ObjectKey: add.ObjectKey, SizeBytes: add.SizeBytes, CreatedAt: created,
		StartedBy: add.StartedBy, StartedByName: add.StartedByName, RuleProfile: add.RuleProfile,
		Quality: add.Quality, Verdict: add.Verdict,
	}
	stored, err := r.insertResult(ctx, result, add.UploadKey)
	if err != nil {
		// The unique upload key refused a second send of one upload, a retry or
		// a race: it is answered with the first.
		if replayed, found, replayErr := r.replayResult(ctx, add); found || replayErr != nil {
			if !found {
				return Result{}, errors.Join(err, replayErr)
			}
			return replayed, replayErr
		}
		return Result{}, err
	}
	return stored, nil
}

// replayResult finds the result an earlier send of the same upload kept.
func (r *Repository) replayResult(ctx context.Context, add NewResult) (Result, bool, error) {
	if add.UploadKey == "" {
		return Result{}, false, nil
	}
	query := `select ` + resultColumns + ` from solve_results where plan_id = ? and upload_key = ?`
	stored, err := scanResult(r.conn.QueryRowContext(ctx, r.rebind(query), add.PlanID, add.UploadKey))
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return Result{}, false, nil
	case err != nil:
		return Result{}, false, fmt.Errorf("plans: upload key of %d: %w", add.PlanID, err)
	case stored.ObjectKey != add.ObjectKey || stored.SourceVersion != add.SourceVersion:
		return Result{}, true, &Invalid{Field: "upload_key", Code: "reused"}
	}
	return stored, true, nil
}

// insertResult writes one result and applies the results cap in the same
// transaction, so no reader sees more than the cap keeps.
func (r *Repository) insertResult(ctx context.Context, result Result, uploadKey string) (Result, error) {
	tx, err := r.conn.BeginTx(ctx, nil)
	if err != nil {
		return Result{}, fmt.Errorf("plans: begin: %w", err)
	}
	defer tx.Rollback()
	locked, err := tx.ExecContext(ctx, r.rebind(`update plans set id = id where id = ?`), result.PlanID)
	if err != nil {
		return Result{}, fmt.Errorf("plans: lock result owner: %w", err)
	}
	if err := changedOne(locked, result.PlanID); err != nil {
		return Result{}, err
	}

	const insert = `insert into solve_results
		(id, plan_id, job_id, device, upload_key, source_version, object_key, size_bytes, created_at,
		 started_by, started_by_name, rule_profile_name, rule_profile_id, rule_config,
		 quality, verdict, pinned)
		values (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`
	var quality, profileID, jobID any
	if result.Quality != nil {
		quality = *result.Quality
	}
	if result.JobID != 0 {
		jobID = result.JobID
	}
	var profileName, rules string
	if profile := result.RuleProfile; profile != nil {
		profileName, rules = profile.Name, profile.Rules
		if profile.ID != 0 {
			profileID = profile.ID
		}
	}
	if _, err := tx.ExecContext(ctx, r.rebind(insert),
		result.ID, result.PlanID, jobID, nullableText(result.Device), nullableText(uploadKey),
		result.SourceVersion, result.ObjectKey,
		result.SizeBytes, result.CreatedAt, result.StartedBy, result.StartedByName,
		profileName, profileID, rules, quality, result.Verdict, false); err != nil {
		return Result{}, fmt.Errorf("plans: add result to %d: %w", result.PlanID, err)
	}
	keep, err := r.planResultsKeep(ctx, tx, result.PlanID)
	if err != nil {
		return Result{}, err
	}
	if err := r.trimResults(ctx, tx, result.PlanID, keep); err != nil {
		return Result{}, err
	}
	var retained int
	if err := tx.QueryRowContext(ctx, r.rebind(`select count(*) from solve_results where id = ?`), result.ID).Scan(&retained); err != nil {
		return Result{}, fmt.Errorf("plans: retained result: %w", err)
	}
	if retained == 0 {
		return Result{}, &Invalid{Field: "solved_at", Code: "invalid"}
	}
	if err := r.stamp(ctx, tx, parts(result.PlanID, PartResults)); err != nil {
		return Result{}, err
	}
	if err := tx.Commit(); err != nil {
		return Result{}, fmt.Errorf("plans: commit result: %w", err)
	}
	return result, nil
}

// trimResults removes for good the oldest unpinned results of a plan beyond
// keep, recording their objects in object_garbage. Pinned results and results
// in Trash are neither counted nor removed. A result a pending or running
// solve reads is counted but stays until the solve ends: its result is built
// from it.
func (r *Repository) trimResults(ctx context.Context, tx *sql.Tx, planID int64, keep int) error {
	const unpinned = `select id, id in ` + solvingResults + ` from solve_results
		where plan_id = ? and deleted_at is null and pinned = ?
		order by created_at desc, id desc`
	rows, err := tx.QueryContext(ctx, r.rebind(unpinned), planID, false)
	if err != nil {
		return fmt.Errorf("plans: results of %d: %w", planID, err)
	}
	type counted struct {
		id      int64
		solving bool
	}
	var listed []counted
	for rows.Next() {
		var one counted
		if err := rows.Scan(&one.id, &one.solving); err != nil {
			rows.Close()
			return fmt.Errorf("plans: results of %d: %w", planID, err)
		}
		listed = append(listed, one)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return fmt.Errorf("plans: results of %d: %w", planID, err)
	}
	for _, one := range listed[min(keep, len(listed)):] {
		if one.solving {
			continue
		}
		if err := r.removeResult(ctx, tx, one.id); err != nil {
			return err
		}
	}
	return nil
}

// solvingResults is the results a pending or running solve reads. The statuses
// are the solving client's two open ones.
const solvingResults = `(select source_result_id from solve_jobs
	where source_result_id is not null and status in ('pending', 'running'))`

// removeResult deletes one result row and records its object, and the files
// exported from it, for the sweep.
func (r *Repository) removeResult(ctx context.Context, tx *sql.Tx, id int64) error {
	const record = `insert into object_garbage (object_key, since)
		select object_key, ? from solve_results where id = ?
		on conflict (object_key) do nothing`
	if _, err := tx.ExecContext(ctx, r.rebind(record), now(), id); err != nil {
		return fmt.Errorf("plans: record the object of result %d: %w", id, err)
	}
	if err := r.recordExports(ctx, tx, `result_id = ?`, id); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, r.rebind(`delete from solve_results where id = ?`), id); err != nil {
		return fmt.Errorf("plans: remove result %d: %w", id, err)
	}
	return nil
}

// planResultsKeep reads the results cap in force for a plan, live or in
// Trash, inside a transaction.
func (r *Repository) planResultsKeep(ctx context.Context, tx *sql.Tx, planID int64) (int, error) {
	var keep sql.NullInt64
	err := tx.QueryRowContext(ctx, r.rebind(`select results_keep from plans where id = ?`), planID).Scan(&keep)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, fmt.Errorf("%w: %d", ErrNotFound, planID)
	}
	if err != nil {
		return 0, fmt.Errorf("plans: results cap of %d: %w", planID, err)
	}
	return r.resultsKeepFor(Plan{ResultsKeep: int(keep.Int64)}), nil
}

const resultColumns = `id, plan_id, job_id, device, source_version, object_key, size_bytes, created_at,
	started_by, started_by_name, rule_profile_name, rule_profile_id, rule_config,
	quality, verdict, name, pinned, deleted_at`

func scanResult(row scanner) (Result, error) {
	var (
		result      Result
		jobID       sql.NullInt64
		device      sql.NullString
		quality     sql.NullFloat64
		name        sql.NullString
		deleted     sql.NullTime
		profileID   sql.NullInt64
		profileName string
		rules       string
	)
	if err := row.Scan(&result.ID, &result.PlanID, &jobID, &device, &result.SourceVersion,
		&result.ObjectKey, &result.SizeBytes, &result.CreatedAt, &result.StartedBy,
		&result.StartedByName, &profileName, &profileID, &rules,
		&quality, &result.Verdict, &name, &result.Pinned, &deleted); err != nil {
		return Result{}, err
	}
	result.JobID, result.Device = jobID.Int64, device.String
	if profileName != "" {
		result.RuleProfile = &RuleProfile{Name: profileName, ID: profileID.Int64, Rules: rules}
	}
	result.CreatedAt = result.CreatedAt.UTC()
	if quality.Valid {
		result.Quality = &quality.Float64
	}
	result.Name = name.String
	if deleted.Valid {
		result.DeletedAt = deleted.Time.UTC()
	}
	return result, nil
}

// Results is a plan's results out of Trash, newest first.
func (r *Repository) Results(ctx context.Context, planID int64) ([]Result, error) {
	query := `select ` + resultColumns + ` from solve_results
		where plan_id = ? and deleted_at is null order by created_at desc, id desc`
	rows, err := r.conn.QueryContext(ctx, r.rebind(query), planID)
	if err != nil {
		return nil, fmt.Errorf("plans: results of %d: %w", planID, err)
	}
	defer rows.Close()

	out := []Result{}
	for rows.Next() {
		result, err := scanResult(rows)
		if err != nil {
			return nil, fmt.Errorf("plans: results of %d: %w", planID, err)
		}
		out = append(out, result)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("plans: results of %d: %w", planID, err)
	}
	return out, nil
}

// SourceResult is one of a plan's results, out of Trash or in it, for the solve that reads it.
func (r *Repository) SourceResult(ctx context.Context, planID, id int64) (Result, error) {
	query := `select ` + resultColumns + ` from solve_results where id = ? and plan_id = ?`
	result, err := scanResult(r.conn.QueryRowContext(ctx, r.rebind(query), id, planID))
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return Result{}, fmt.Errorf("%w: plan %d result %d", ErrNoResult, planID, id)
	case err != nil:
		return Result{}, fmt.Errorf("plans: result %d of %d: %w", id, planID, err)
	}
	return result, nil
}

// Result is one of a plan's results out of Trash.
func (r *Repository) Result(ctx context.Context, planID, id int64) (Result, error) {
	query := `select ` + resultColumns + ` from solve_results
		where id = ? and plan_id = ? and deleted_at is null`
	result, err := scanResult(r.conn.QueryRowContext(ctx, r.rebind(query), id, planID))
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return Result{}, fmt.Errorf("%w: plan %d result %d", ErrNoResult, planID, id)
	case err != nil:
		return Result{}, fmt.Errorf("plans: result %d of %d: %w", id, planID, err)
	}
	return result, nil
}

// CleanResultName trims a result name; empty means no name. Longer than
// MaxNameLength letters is an *Invalid.
func CleanResultName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if utf8.RuneCountInString(name) > MaxNameLength {
		return "", &Invalid{Field: "name", Code: "too_long"}
	}
	return name, nil
}

// UpdateResult names, renames or pins a result out of Trash. A change that
// names no field writes nothing.
func (r *Repository) UpdateResult(ctx context.Context, planID, id int64, change ResultChange) (Result, error) {
	result, err := r.Result(ctx, planID, id)
	if err != nil || (change.Name == nil && change.Pinned == nil) {
		return result, err
	}
	sets, args := []string{}, []any{}
	if change.Name != nil {
		name, err := CleanResultName(*change.Name)
		if err != nil {
			return Result{}, err
		}
		sets, args = append(sets, "name = ?"), append(args, nullableText(name))
	}
	if change.Pinned != nil {
		sets, args = append(sets, "pinned = ?"), append(args, *change.Pinned)
	}
	update := `update solve_results set ` + strings.Join(sets, ", ") + ` where id = ? and plan_id = ? and deleted_at is null`
	if err := r.changeResult(ctx, planID, id, update, append(args, id, planID)...); err != nil {
		return Result{}, err
	}
	return r.Result(ctx, planID, id)
}

// TrashResult moves a result out of Trash into it. One already there is
// ErrNoResult.
func (r *Repository) TrashResult(ctx context.Context, planID, id int64) error {
	const update = `update solve_results set deleted_at = ?
		where id = ? and plan_id = ? and deleted_at is null`
	return r.changeResult(ctx, planID, id, update, now(), id, planID)
}

// RestoreResult takes a result out of Trash. Restoring one that is not there
// changes nothing and succeeds, because undo may ask twice. The results cap
// applies again at the plan's next result.
func (r *Repository) RestoreResult(ctx context.Context, planID, id int64) (Result, error) {
	const update = `update solve_results set deleted_at = null where id = ? and plan_id = ?`
	if err := r.changeResult(ctx, planID, id, update, id, planID); err != nil {
		return Result{}, err
	}
	return r.Result(ctx, planID, id)
}

// changeResult runs one result update and stamps the plan's results, in one
// transaction. An update that matches no row is ErrNoResult and stamps
// nothing.
func (r *Repository) changeResult(ctx context.Context, planID, id int64, update string, args ...any) error {
	tx, err := r.conn.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("plans: begin: %w", err)
	}
	defer tx.Rollback()

	changed, err := tx.ExecContext(ctx, r.rebind(update), args...)
	if err != nil {
		return fmt.Errorf("plans: result %d of %d: %w", id, planID, err)
	}
	affected, err := changed.RowsAffected()
	if err != nil {
		return fmt.Errorf("plans: result %d of %d: %w", id, planID, err)
	}
	if affected == 0 {
		return fmt.Errorf("%w: plan %d result %d", ErrNoResult, planID, id)
	}
	if err := r.stamp(ctx, tx, parts(planID, PartResults)); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("plans: commit result %d of %d: %w", id, planID, err)
	}
	return nil
}

// TrashedResults returns one owner's results in Trash, most recently deleted
// first. Those of a plan in Trash are part of that plan and are not listed.
func (r *Repository) TrashedResults(ctx context.Context, ownerIdentityID string) ([]TrashedResult, error) {
	query := `select p.id, p.name, p.school_year, ` + prefixed("r.", resultColumns) + `
		from solve_results r join plans p on p.id = r.plan_id
		where p.owner_identity_id = ? and p.deleted_at is null and r.deleted_at is not null
		order by r.deleted_at desc, r.id desc`
	rows, err := r.conn.QueryContext(ctx, r.rebind(query), ownerIdentityID)
	if err != nil {
		return nil, fmt.Errorf("plans: trashed results: %w", err)
	}
	defer rows.Close()

	out := []TrashedResult{}
	for rows.Next() {
		var item TrashedResult
		result, err := scanResult(prefixScanner{row: rows, head: []any{
			&item.Plan.ID, &item.Plan.Name, &item.Plan.SchoolYear,
		}})
		if err != nil {
			return nil, fmt.Errorf("plans: trashed results: %w", err)
		}
		item.Plan.OwnerIdentityID = ownerIdentityID
		item.Result = result
		out = append(out, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("plans: trashed results: %w", err)
	}
	return out, nil
}

// PurgeResults removes for good every result whose time in Trash is up,
// recording its object and its exported files for the sweep, and returns how many it removed. A
// result a pending or running solve reads stays until the solve ends.
func (r *Repository) PurgeResults(ctx context.Context, at time.Time, trashDays int) (int64, error) {
	cutoff := at.UTC().AddDate(0, 0, -trashDays)
	tx, err := r.conn.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("plans: purge results: %w", err)
	}
	defer tx.Rollback()

	// The write comes first, so the transaction holds the lock before the
	// delete reads which results are due.
	const due = `deleted_at is not null and deleted_at <= ? and id not in ` + solvingResults
	const record = `insert into object_garbage (object_key, since)
		select object_key, ? from solve_results where ` + due + `
		on conflict (object_key) do nothing`
	if _, err := tx.ExecContext(ctx, r.rebind(record), now(), cutoff); err != nil {
		return 0, fmt.Errorf("plans: purge results: record objects: %w", err)
	}
	if err := r.recordExports(ctx, tx, `result_id in (select id from solve_results where `+due+`)`, cutoff); err != nil {
		return 0, err
	}
	// DELETE rechecks eligibility after waiting on a concurrent restore. Only
	// rows actually removed contribute to change stamps.
	rows, err := tx.QueryContext(ctx, r.rebind(`delete from solve_results where `+due+` returning plan_id`), cutoff)
	if err != nil {
		return 0, fmt.Errorf("plans: purge results: %w", err)
	}
	var purged int64
	touched := map[int64]bool{}
	for rows.Next() {
		var planID int64
		if err := rows.Scan(&planID); err != nil {
			rows.Close()
			return 0, fmt.Errorf("plans: purge results: %w", err)
		}
		purged++
		touched[planID] = true
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, fmt.Errorf("plans: purge results: %w", err)
	}
	var changed []stamped
	for planID := range touched {
		changed = append(changed, parts(planID, PartResults)...)
	}
	if err := r.stamp(ctx, tx, changed); err != nil {
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("plans: purge results: %w", err)
	}
	return purged, nil
}

// prefixed qualifies every column in a list with a table alias.
func prefixed(alias, columns string) string {
	parts := strings.Split(columns, ",")
	for i, part := range parts {
		parts[i] = alias + strings.TrimSpace(part)
	}
	return strings.Join(parts, ", ")
}

// prefixScanner scans leading columns into head, then hands the rest to the
// caller's scan function.
type prefixScanner struct {
	row  scanner
	head []any
}

func (p prefixScanner) Scan(dest ...any) error {
	return p.row.Scan(append(append([]any{}, p.head...), dest...)...)
}

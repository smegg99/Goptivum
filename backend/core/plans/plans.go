// core/plans/plans.go

// Package plans owns the index: which plans exist, who owns them, and the
// timeline of versions each one has. The documents themselves live in object
// storage; a row here references one by key.
//
// The database is the index and the object store never is. Scanning a bucket
// to discover what exists is how silent data loss happens.
package plans

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/binary"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/smegg99/s99ab/data/db"
)

// ErrNotFound means no live plan has that id. A plan in Trash is not found
// everywhere except Trash itself and restore.
var ErrNotFound = errors.New("plans: no such plan")

// The limits the contract states. The repository enforces them, so every route
// that names a plan refuses the same things.
const (
	MaxNameLength       = 200
	MaxSchoolYearLength = 32
	MaxBackupsKeep      = 10000
	MaxResultsKeep      = 10000
)

// Invalid is a field the repository refused, named the way the error envelope
// names it: the field path and a code, never a sentence.
type Invalid struct {
	Field string
	Code  string
}

func (e *Invalid) Error() string { return fmt.Sprintf("plans: %s: %s", e.Field, e.Code) }

// Plan is one school timetable project.
type Plan struct {
	ID              int64
	OwnerIdentityID string
	Name            string
	SchoolYear      string
	CreatedAt       time.Time
	UpdatedAt       time.Time
	// DeletedAt is when the plan went to Trash, zero while it is live.
	DeletedAt time.Time
	// BackupsKeep is the plan's own backups cap, 0 when it follows the
	// server's default.
	BackupsKeep int
	// ResultsKeep is the plan's own results cap, 0 when it follows the
	// server's default.
	ResultsKeep int
	// Origin is the plan and version this one was forked from, zero when it
	// was not.
	Origin Origin
}

// Origin is where a fork or a duplicate was copied from. It is kept when that
// plan goes to Trash or is removed for good.
type Origin struct {
	PlanID  int64
	Version int64
}

// Change is a rename, a new school year or a new cap. A nil field is left as
// it is; a cap of 0 drops the plan's own.
type Change struct {
	Name        *string
	SchoolYear  *string
	BackupsKeep *int
	ResultsKeep *int
}

// Caps are the server's projects.backups_keep and projects.results_keep, each
// at least 1, in force for plans without their own.
type Caps struct {
	BackupsKeep int
	ResultsKeep int
}

// Repository reads and writes the index.
type Repository struct {
	conn   *sql.DB
	driver string

	mu   sync.RWMutex
	caps Caps
}

// NewRepository binds the index to one pool. The driver decides the
// placeholder dialect, so every query here is written with "?".
func NewRepository(conn *sql.DB, driver string, caps Caps) *Repository {
	return &Repository{conn: conn, driver: driver, caps: caps}
}

// SetCaps replaces the server's caps. They apply from the next read of a cap:
// what a plan reports and the next version or result it keeps.
func (r *Repository) SetCaps(caps Caps) {
	r.mu.Lock()
	r.caps = caps
	r.mu.Unlock()
}

func (r *Repository) currentCaps() Caps {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.caps
}

// DefaultBackupsKeep is the backups cap of a plan without its own.
func (r *Repository) DefaultBackupsKeep() int { return r.currentCaps().BackupsKeep }

// DefaultResultsKeep is the results cap of a plan without its own.
func (r *Repository) DefaultResultsKeep() int { return r.currentCaps().ResultsKeep }

// keepFor is the backups cap in force for a plan.
func (r *Repository) keepFor(plan Plan) int {
	if plan.BackupsKeep > 0 {
		return plan.BackupsKeep
	}
	return r.DefaultBackupsKeep()
}

// resultsKeepFor is the results cap in force for a plan.
func (r *Repository) resultsKeepFor(plan Plan) int {
	if plan.ResultsKeep > 0 {
		return plan.ResultsKeep
	}
	return r.DefaultResultsKeep()
}

func (r *Repository) rebind(query string) string {
	return db.Rebind(r.driver, query)
}

// now is stored at microsecond precision, which is what Postgres keeps. A
// value that changes when it round-trips is a value tests cannot compare.
func now() time.Time {
	return time.Now().UTC().Truncate(time.Microsecond)
}

// Clean trims a name and a school year and refuses what the contract refuses,
// with an *Invalid. Lengths count letters, because a Polish name is not
// measured in bytes.
func Clean(name, schoolYear string) (string, string, error) {
	name, schoolYear = strings.TrimSpace(name), strings.TrimSpace(schoolYear)
	switch {
	case name == "":
		return "", "", &Invalid{Field: "name", Code: "required"}
	case utf8.RuneCountInString(name) > MaxNameLength:
		return "", "", &Invalid{Field: "name", Code: "too_long"}
	case utf8.RuneCountInString(schoolYear) > MaxSchoolYearLength:
		return "", "", &Invalid{Field: "school_year", Code: "too_long"}
	}
	return name, schoolYear, nil
}

// Create stores a plan together with its first version, in one transaction,
// so no plan exists with nothing to open. The first upload's PlanID and Parent
// are ignored: it becomes version 1 of the new plan.
func (r *Repository) Create(ctx context.Context, plan Plan, first Upload) (Plan, Version, error) {
	if plan.OwnerIdentityID == "" {
		return Plan{}, Version{}, fmt.Errorf("plans: a plan needs an owner")
	}
	name, schoolYear, err := Clean(plan.Name, plan.SchoolYear)
	if err != nil {
		return Plan{}, Version{}, err
	}
	if first.ObjectKey == "" {
		return Plan{}, Version{}, fmt.Errorf("plans: a version needs an object key")
	}
	if first.SizeBytes < 0 {
		return Plan{}, Version{}, fmt.Errorf("plans: a version cannot be %d bytes", first.SizeBytes)
	}

	id, err := newID()
	if err != nil {
		return Plan{}, Version{}, err
	}
	stamp := now()
	plan = Plan{
		ID: id, OwnerIdentityID: plan.OwnerIdentityID, Name: name, SchoolYear: schoolYear,
		CreatedAt: stamp, UpdatedAt: stamp,
	}
	version := Version{
		PlanID: id, Version: 1, ObjectKey: first.ObjectKey, SizeBytes: first.SizeBytes,
		CreatedAt: stamp, CreatedBy: first.CreatedBy,
		ValidFrom: dateOr(first.ValidFrom, time.Time{}), ValidTo: dateOr(first.ValidTo, time.Time{}),
	}

	tx, err := r.conn.BeginTx(ctx, nil)
	if err != nil {
		return Plan{}, Version{}, fmt.Errorf("plans: begin: %w", err)
	}
	defer tx.Rollback()

	const insertPlan = `insert into plans
		(id, owner_identity_id, name, school_year, created_at, updated_at)
		values (?, ?, ?, ?, ?, ?)`
	if _, err := tx.ExecContext(ctx, r.rebind(insertPlan),
		plan.ID, plan.OwnerIdentityID, plan.Name, plan.SchoolYear,
		plan.CreatedAt, plan.UpdatedAt); err != nil {
		return Plan{}, Version{}, fmt.Errorf("plans: create: %w", err)
	}
	const insertFirst = `insert into plan_versions
		(plan_id, version, parent, object_key, size_bytes, created_at, created_by, valid_from, valid_to)
		values (?, ?, ?, ?, ?, ?, ?, ?, ?)`
	if _, err := tx.ExecContext(ctx, r.rebind(insertFirst),
		version.PlanID, version.Version, nil, version.ObjectKey, version.SizeBytes,
		version.CreatedAt, version.CreatedBy,
		nullableDate(version.ValidFrom), nullableDate(version.ValidTo)); err != nil {
		return Plan{}, Version{}, fmt.Errorf("plans: create first version: %w", err)
	}
	if err := r.stamp(ctx, tx, parts(plan.ID, PartProject, PartVersions)); err != nil {
		return Plan{}, Version{}, err
	}
	if err := tx.Commit(); err != nil {
		return Plan{}, Version{}, fmt.Errorf("plans: commit create: %w", err)
	}
	return plan, version, nil
}

const planColumns = `id, owner_identity_id, name, school_year, created_at, updated_at, deleted_at,
	backups_keep, results_keep, origin_plan_id, origin_version`

func scanPlan(row scanner) (Plan, error) {
	var (
		plan          Plan
		deleted       sql.NullTime
		keep          sql.NullInt64
		resultsKeep   sql.NullInt64
		originPlan    sql.NullInt64
		originVersion sql.NullInt64
	)
	if err := row.Scan(&plan.ID, &plan.OwnerIdentityID, &plan.Name, &plan.SchoolYear,
		&plan.CreatedAt, &plan.UpdatedAt, &deleted, &keep, &resultsKeep,
		&originPlan, &originVersion); err != nil {
		return Plan{}, err
	}
	plan.BackupsKeep = int(keep.Int64)
	plan.ResultsKeep = int(resultsKeep.Int64)
	plan.Origin = Origin{PlanID: originPlan.Int64, Version: originVersion.Int64}
	plan.CreatedAt = plan.CreatedAt.UTC()
	plan.UpdatedAt = plan.UpdatedAt.UTC()
	if deleted.Valid {
		plan.DeletedAt = deleted.Time.UTC()
	}
	return plan, nil
}

// Get returns one live plan.
func (r *Repository) Get(ctx context.Context, id int64) (Plan, error) {
	query := `select ` + planColumns + ` from plans where id = ? and deleted_at is null`
	plan, err := scanPlan(r.conn.QueryRowContext(ctx, r.rebind(query), id))
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return Plan{}, fmt.Errorf("%w: %d", ErrNotFound, id)
	case err != nil:
		return Plan{}, fmt.Errorf("plans: get %d: %w", id, err)
	}
	return plan, nil
}

// Live reports whether a plan exists and is out of Trash.
func (r *Repository) Live(ctx context.Context, id int64) (bool, error) {
	_, err := r.Get(ctx, id)
	if errors.Is(err, ErrNotFound) {
		return false, nil
	}
	return err == nil, err
}

// List returns every owner's live plans, newest first: everyone at the school
// reads every plan.
func (r *Repository) List(ctx context.Context) ([]Plan, error) {
	query := `select ` + planColumns + ` from plans
		where deleted_at is null
		order by created_at desc, id desc`
	return r.plans(ctx, query)
}

// Lookup is each plan with one of these ids, whoever owns it and in Trash or
// not; an id with no plan is left out.
func (r *Repository) Lookup(ctx context.Context, ids []int64) (map[int64]Plan, error) {
	found := make(map[int64]Plan, len(ids))
	if len(ids) == 0 {
		return found, nil
	}
	args := make([]any, 0, len(ids))
	for _, id := range ids {
		args = append(args, id)
	}
	query := `select ` + planColumns + ` from plans where id in (?` + strings.Repeat(`, ?`, len(ids)-1) + `)`
	listed, err := r.plans(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("plans: lookup: %w", err)
	}
	for _, plan := range listed {
		found[plan.ID] = plan
	}
	return found, nil
}

func (r *Repository) plans(ctx context.Context, query string, args ...any) ([]Plan, error) {
	rows, err := r.conn.QueryContext(ctx, r.rebind(query), args...)
	if err != nil {
		return nil, fmt.Errorf("plans: list: %w", err)
	}
	defer rows.Close()

	out := []Plan{}
	for rows.Next() {
		plan, err := scanPlan(rows)
		if err != nil {
			return nil, fmt.Errorf("plans: list scan: %w", err)
		}
		out = append(out, plan)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("plans: list: %w", err)
	}
	return out, nil
}

// Update renames a live plan, changes its school year or sets one of its caps.
// A blank name and a cap outside 0 to its maximum are refused, and a change
// moves updated_at like any other edit; a change that names no field writes
// nothing. A lower cap removes nothing now: it applies from the next version
// or the next result.
func (r *Repository) Update(ctx context.Context, id int64, change Change) (Plan, error) {
	plan, err := r.Get(ctx, id)
	if err != nil || (change.Name == nil && change.SchoolYear == nil &&
		change.BackupsKeep == nil && change.ResultsKeep == nil) {
		return plan, err
	}
	name, schoolYear := plan.Name, plan.SchoolYear
	if change.Name != nil {
		name = *change.Name
	}
	if change.SchoolYear != nil {
		schoolYear = *change.SchoolYear
	}
	if plan.Name, plan.SchoolYear, err = Clean(name, schoolYear); err != nil {
		return Plan{}, err
	}
	if change.BackupsKeep != nil {
		if *change.BackupsKeep < 0 || *change.BackupsKeep > MaxBackupsKeep {
			return Plan{}, &Invalid{Field: "backups_keep", Code: "invalid"}
		}
		plan.BackupsKeep = *change.BackupsKeep
	}
	if change.ResultsKeep != nil {
		if *change.ResultsKeep < 0 || *change.ResultsKeep > MaxResultsKeep {
			return Plan{}, &Invalid{Field: "results_keep", Code: "invalid"}
		}
		plan.ResultsKeep = *change.ResultsKeep
	}
	plan.UpdatedAt = now()

	const update = `update plans set name = ?, school_year = ?, backups_keep = ?, results_keep = ?,
		updated_at = ? where id = ? and deleted_at is null`
	err = r.changePlan(ctx, id, update,
		plan.Name, plan.SchoolYear, nullableKeep(plan.BackupsKeep), nullableKeep(plan.ResultsKeep),
		plan.UpdatedAt, id)
	if err != nil {
		return Plan{}, err
	}
	return plan, nil
}

// changePlan runs one update of one plan row and stamps its project part, in
// one transaction. An update that matches no row is ErrNotFound and stamps
// nothing.
func (r *Repository) changePlan(ctx context.Context, id int64, update string, args ...any) error {
	tx, err := r.conn.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("plans: begin: %w", err)
	}
	defer tx.Rollback()

	result, err := tx.ExecContext(ctx, r.rebind(update), args...)
	if err != nil {
		return fmt.Errorf("plans: change %d: %w", id, err)
	}
	if err := changedOne(result, id); err != nil {
		return err
	}
	if err := r.stamp(ctx, tx, parts(id, PartProject)); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("plans: commit change of %d: %w", id, err)
	}
	return nil
}

// nullableKeep stores a plan without its own cap as null.
func nullableKeep(keep int) any {
	if keep == 0 {
		return nil
	}
	return keep
}

// changedOne turns an update that matched nothing into ErrNotFound.
func changedOne(result sql.Result, id int64) error {
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("plans: %d: %w", id, err)
	}
	if affected == 0 {
		return fmt.Errorf("%w: %d", ErrNotFound, id)
	}
	return nil
}

// newID is a random 63-bit id. The application allocates ids because the
// schema stays dialect-neutral: no sequences and no identity columns, so the
// same migration runs on SQLite in tests and on Postgres in production.
func newID() (int64, error) {
	for attempt := 0; attempt < 4; attempt++ {
		var buf [8]byte
		if _, err := rand.Read(buf[:]); err != nil {
			return 0, fmt.Errorf("plans: allocate id: %w", err)
		}
		if id := int64(binary.BigEndian.Uint64(buf[:]) >> 1); id != 0 {
			return id, nil
		}
	}
	return 0, fmt.Errorf("plans: allocate id: no nonzero value")
}

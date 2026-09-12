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
	"time"

	"github.com/smegg99/s99ab/data/db"
)

// ErrNotFound means no plan has that id.
var ErrNotFound = errors.New("plans: no such plan")

// Plan is one school timetable project.
type Plan struct {
	ID              int64
	OwnerIdentityID string
	Name            string
	SchoolYear      string
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

// Repository reads and writes the index.
type Repository struct {
	conn   *sql.DB
	driver string
}

// NewRepository binds the index to one pool. The driver decides the
// placeholder dialect, so every query here is written with "?".
func NewRepository(conn *sql.DB, driver string) *Repository {
	return &Repository{conn: conn, driver: driver}
}

func (r *Repository) rebind(query string) string {
	return db.Rebind(r.driver, query)
}

// now is stored at microsecond precision, which is what Postgres keeps. A
// value that changes when it round-trips is a value tests cannot compare.
func now() time.Time {
	return time.Now().UTC().Truncate(time.Microsecond)
}

// Create allocates an id and stores the plan.
func (r *Repository) Create(ctx context.Context, plan Plan) (Plan, error) {
	if plan.OwnerIdentityID == "" {
		return Plan{}, fmt.Errorf("plans: a plan needs an owner")
	}
	if plan.Name == "" {
		return Plan{}, fmt.Errorf("plans: a plan needs a name")
	}

	id, err := newID()
	if err != nil {
		return Plan{}, err
	}
	plan.ID = id
	plan.CreatedAt = now()
	plan.UpdatedAt = plan.CreatedAt

	const insert = `insert into plans
		(id, owner_identity_id, name, school_year, created_at, updated_at)
		values (?, ?, ?, ?, ?, ?)`
	if _, err := r.conn.ExecContext(ctx, r.rebind(insert),
		plan.ID, plan.OwnerIdentityID, plan.Name, plan.SchoolYear,
		plan.CreatedAt, plan.UpdatedAt); err != nil {
		return Plan{}, fmt.Errorf("plans: create: %w", err)
	}
	return plan, nil
}

// Get returns one plan.
func (r *Repository) Get(ctx context.Context, id int64) (Plan, error) {
	const query = `select id, owner_identity_id, name, school_year, created_at, updated_at
		from plans where id = ?`

	var plan Plan
	err := r.conn.QueryRowContext(ctx, r.rebind(query), id).Scan(
		&plan.ID, &plan.OwnerIdentityID, &plan.Name, &plan.SchoolYear,
		&plan.CreatedAt, &plan.UpdatedAt)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return Plan{}, fmt.Errorf("%w: %d", ErrNotFound, id)
	case err != nil:
		return Plan{}, fmt.Errorf("plans: get %d: %w", id, err)
	}
	plan.CreatedAt = plan.CreatedAt.UTC()
	plan.UpdatedAt = plan.UpdatedAt.UTC()
	return plan, nil
}

// List returns one owner's plans, newest first.
func (r *Repository) List(ctx context.Context, ownerIdentityID string) ([]Plan, error) {
	const query = `select id, owner_identity_id, name, school_year, created_at, updated_at
		from plans where owner_identity_id = ?
		order by created_at desc, id desc`

	rows, err := r.conn.QueryContext(ctx, r.rebind(query), ownerIdentityID)
	if err != nil {
		return nil, fmt.Errorf("plans: list: %w", err)
	}
	defer rows.Close()

	var out []Plan
	for rows.Next() {
		var plan Plan
		if err := rows.Scan(&plan.ID, &plan.OwnerIdentityID, &plan.Name,
			&plan.SchoolYear, &plan.CreatedAt, &plan.UpdatedAt); err != nil {
			return nil, fmt.Errorf("plans: list scan: %w", err)
		}
		plan.CreatedAt = plan.CreatedAt.UTC()
		plan.UpdatedAt = plan.UpdatedAt.UTC()
		out = append(out, plan)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("plans: list: %w", err)
	}
	return out, nil
}

// Delete removes a plan and, by cascade, its version index. The objects it
// referenced become garbage a sweep can collect; an orphaned row would be a
// bug, an orphaned object is not.
func (r *Repository) Delete(ctx context.Context, id int64) error {
	result, err := r.conn.ExecContext(ctx, r.rebind(`delete from plans where id = ?`), id)
	if err != nil {
		return fmt.Errorf("plans: delete %d: %w", id, err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("plans: delete %d: %w", id, err)
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

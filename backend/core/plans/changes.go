// core/plans/changes.go

package plans

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strconv"
)

// Part is what of a plan changed: the plan row, its versions or its results.
type Part string

const (
	PartProject  Part = "project"
	PartVersions Part = "versions"
	PartResults  Part = "results"
)

// partOrder is the order a plan's parts are listed in.
var partOrder = []Part{PartProject, PartVersions, PartResults}

// State is where a changed plan is now.
type State string

const (
	StateLive    State = "live"
	StateTrash   State = "trash"
	StateRemoved State = "removed"
)

// ChangesPage is the most stamped parts one listing answers with.
const ChangesPage = 500

// Cursor is a point in one database's changes: its clock's epoch and value.
type Cursor struct {
	Epoch string
	Seq   int64
}

// String is the cursor as a client carries it.
func (c Cursor) String() string { return c.Epoch + "." + strconv.FormatInt(c.Seq, 10) }

var cursorPattern = regexp.MustCompile(`^([0-9a-f]{16})\.([0-9]{1,19})$`)

// ParseCursor reads what String wrote. Anything else is an *Invalid for since.
func ParseCursor(raw string) (Cursor, error) {
	match := cursorPattern.FindStringSubmatch(raw)
	if match == nil {
		return Cursor{}, &Invalid{Field: "since", Code: "invalid"}
	}
	seq, err := strconv.ParseInt(match[2], 10, 64)
	if err != nil {
		return Cursor{}, &Invalid{Field: "since", Code: "invalid"}
	}
	return Cursor{Epoch: match[1], Seq: seq}, nil
}

// PlanChange is one plan changed after a point: where it is now and which of
// its parts changed, in partOrder.
type PlanChange struct {
	PlanID int64
	State  State
	Parts  []Part
}

// Changes is what changed after a point.
type Changes struct {
	// Cursor is the point to ask from next.
	Cursor Cursor
	// Reset means there was no point, or not one this database handed out:
	// the caller reads everything again, then asks from Cursor.
	Reset bool
	// More means further changes wait after Cursor.
	More  bool
	Plans []PlanChange
}

// Changes lists each plan with a part stamped after since, once, reading at
// most limit stamped parts. A nil since, or one this database never handed
// out, is answered with a reset at the current point.
func (r *Repository) Changes(ctx context.Context, since *Cursor, limit int) (Changes, error) {
	if limit < 1 {
		limit = ChangesPage
	}
	now, err := r.clock(ctx)
	if err != nil {
		return Changes{}, err
	}
	if since == nil || since.Epoch != now.Epoch || since.Seq > now.Seq {
		return Changes{Cursor: now, Reset: true, Plans: []PlanChange{}}, nil
	}
	// The poll that finds nothing, which is most of them, stops at the clock.
	if since.Seq == now.Seq {
		return Changes{Cursor: *since, Plans: []PlanChange{}}, nil
	}

	const query = `select c.plan_id, c.part, c.seq, p.id, p.deleted_at
		from plan_changes c left join plans p on p.id = c.plan_id
		where c.seq > ? order by c.seq limit ?`
	rows, err := r.conn.QueryContext(ctx, r.rebind(query), since.Seq, limit+1)
	if err != nil {
		return Changes{}, fmt.Errorf("plans: changes: %w", err)
	}
	defer rows.Close()

	out := Changes{Cursor: *since, Plans: []PlanChange{}}
	listed := map[int64]int{}
	read := 0
	for rows.Next() {
		var (
			planID  int64
			part    Part
			seq     int64
			found   sql.NullInt64
			deleted sql.NullTime
		)
		if err := rows.Scan(&planID, &part, &seq, &found, &deleted); err != nil {
			return Changes{}, fmt.Errorf("plans: changes: %w", err)
		}
		if read++; read > limit {
			out.More = true
			break
		}
		out.Cursor.Seq = seq
		index, ok := listed[planID]
		if !ok {
			index = len(out.Plans)
			listed[planID] = index
			out.Plans = append(out.Plans, PlanChange{PlanID: planID, State: stateOf(found, deleted)})
		}
		out.Plans[index].Parts = append(out.Plans[index].Parts, part)
	}
	if err := rows.Err(); err != nil {
		return Changes{}, fmt.Errorf("plans: changes: %w", err)
	}
	for i := range out.Plans {
		slices.SortFunc(out.Plans[i].Parts, func(a, b Part) int {
			return slices.Index(partOrder, a) - slices.Index(partOrder, b)
		})
	}
	return out, nil
}

// stateOf reads where a plan is from its row, if it still has one.
func stateOf(found sql.NullInt64, deleted sql.NullTime) State {
	switch {
	case !found.Valid:
		return StateRemoved
	case deleted.Valid:
		return StateTrash
	default:
		return StateLive
	}
}

// clock is the current point, writing the clock first if this database has
// none yet.
func (r *Repository) clock(ctx context.Context) (Cursor, error) {
	const read = `select epoch, value from change_clock where id = 1`
	var now Cursor
	err := r.conn.QueryRowContext(ctx, r.rebind(read)).Scan(&now.Epoch, &now.Seq)
	if errors.Is(err, sql.ErrNoRows) {
		if err := r.startClock(ctx, r.conn); err != nil {
			return Cursor{}, err
		}
		err = r.conn.QueryRowContext(ctx, r.rebind(read)).Scan(&now.Epoch, &now.Seq)
	}
	if err != nil {
		return Cursor{}, fmt.Errorf("plans: change clock: %w", err)
	}
	return now, nil
}

type execer interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

// startClock writes the clock row with a new random epoch, unless it exists.
// The epoch is made here because SQL has no portable random value.
func (r *Repository) startClock(ctx context.Context, on execer) error {
	var buf [8]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return fmt.Errorf("plans: change clock epoch: %w", err)
	}
	const insert = `insert into change_clock (id, epoch, value) values (1, ?, 0)
		on conflict (id) do nothing`
	if _, err := on.ExecContext(ctx, r.rebind(insert), hex.EncodeToString(buf[:])); err != nil {
		return fmt.Errorf("plans: start the change clock: %w", err)
	}
	return nil
}

// stamped is one part of one plan that changed.
type stamped struct {
	planID int64
	part   Part
}

// parts stamps these parts of one plan.
func parts(planID int64, changed ...Part) []stamped {
	out := make([]stamped, 0, len(changed))
	for _, part := range changed {
		out = append(out, stamped{planID: planID, part: part})
	}
	return out
}

// stamp records that these parts changed, each at the clock's next value. It
// is the transaction's last write: the clock row stays locked until commit,
// which orders the stamps by commit on Postgres, and a transaction holding it
// must not then wait on another row.
func (r *Repository) stamp(ctx context.Context, tx *sql.Tx, changed []stamped) error {
	first, err := r.advance(ctx, tx, len(changed))
	if err != nil {
		return err
	}
	for i, one := range changed {
		if err := r.upsertStamp(ctx, tx, one, first+int64(i)); err != nil {
			return err
		}
	}
	return nil
}

// entomb records that plans were removed for good: each keeps one project
// row, listed as removed, and loses its other parts. Like stamp, it is the
// transaction's last write.
func (r *Repository) entomb(ctx context.Context, tx *sql.Tx, planIDs []int64) error {
	first, err := r.advance(ctx, tx, len(planIDs))
	if err != nil {
		return err
	}
	for i, id := range planIDs {
		if _, err := tx.ExecContext(ctx, r.rebind(`delete from plan_changes where plan_id = ? and part <> ?`),
			id, PartProject); err != nil {
			return fmt.Errorf("plans: entomb %d: %w", id, err)
		}
		if err := r.upsertStamp(ctx, tx, stamped{planID: id, part: PartProject}, first+int64(i)); err != nil {
			return err
		}
	}
	return nil
}

// advance moves the clock on by n and returns the first of the n values it
// passed. The clock's update comes before any plan_changes write, so every
// transaction that writes a stamp holds the clock first.
func (r *Repository) advance(ctx context.Context, tx *sql.Tx, n int) (int64, error) {
	if n == 0 {
		return 0, nil
	}
	if err := r.startClock(ctx, tx); err != nil {
		return 0, err
	}
	if _, err := tx.ExecContext(ctx, r.rebind(`update change_clock set value = value + ? where id = 1`), n); err != nil {
		return 0, fmt.Errorf("plans: advance the change clock: %w", err)
	}
	var value int64
	if err := tx.QueryRowContext(ctx, r.rebind(`select value from change_clock where id = 1`)).Scan(&value); err != nil {
		return 0, fmt.Errorf("plans: read the change clock: %w", err)
	}
	return value - int64(n) + 1, nil
}

func (r *Repository) upsertStamp(ctx context.Context, tx *sql.Tx, one stamped, seq int64) error {
	const upsert = `insert into plan_changes (plan_id, part, seq) values (?, ?, ?)
		on conflict (plan_id, part) do update set seq = excluded.seq`
	if _, err := tx.ExecContext(ctx, r.rebind(upsert), one.planID, one.part, seq); err != nil {
		return fmt.Errorf("plans: stamp %s of %d: %w", one.part, one.planID, err)
	}
	return nil
}

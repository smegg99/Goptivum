// core/audit/audit.go

// Package audit keeps the audit trail: who changed which project, account,
// node or setting, and when. An entry copies every name as it was then, so it
// still reads after what it names was renamed or removed.
package audit

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"time"

	"github.com/smegg99/s99ab/data/db"
)

// Kind is what an entry changed.
type Kind string

const (
	Project Kind = "project"
	User    Kind = "user"
	Node    Kind = "node"
	Setting Kind = "setting"
	Storage Kind = "storage"
)

// Kinds are every kind an entry can have.
var Kinds = []Kind{Project, User, Node, Setting, Storage}

// Action is what was done, as the key the desktop words.
type Action string

const (
	ProjectCreated  Action = "project.created"
	ProjectRenamed  Action = "project.renamed"
	ProjectChanged  Action = "project.changed"
	ProjectDeleted  Action = "project.deleted"
	ProjectRestored Action = "project.restored"
	// ProjectTransferred is an administrator giving a project to another
	// account.
	ProjectTransferred Action = "project.transferred"

	UserCreated       Action = "user.created"
	UserChanged       Action = "user.changed"
	UserPasswordReset Action = "user.passwordReset"
	UserSignedOut     Action = "user.signedOut"
	UserSessionEnded  Action = "user.sessionEnded"

	NodePaired  Action = "node.paired"
	NodeChanged Action = "node.changed"
	NodeRevoked Action = "node.revoked"

	SettingChanged Action = "setting.changed"

	StorageTrashEmptied   Action = "storage.trashEmptied"
	StorageBackupsTrimmed Action = "storage.backupsTrimmed"
	StorageSwept          Action = "storage.swept"
)

// Actor is the account that did it, with its display name then.
type Actor struct {
	ID   string
	Name string
}

// Target is what it was done to, with its name then.
type Target struct {
	Kind Kind
	ID   string
	Name string
}

// Entry is one line of the trail.
type Entry struct {
	ID      int64
	At      time.Time
	Actor   Actor
	Action  Action
	Target  Target
	Details map[string]string
}

// Log reads and writes the trail.
type Log struct {
	conn   *sql.DB
	driver string
	now    func() time.Time
}

// New binds the trail to one pool.
func New(conn *sql.DB, driver string) *Log {
	return &Log{conn: conn, driver: driver, now: time.Now}
}

// WithClock replaces the time each entry is stamped with, for tests.
func (l *Log) WithClock(now func() time.Time) *Log {
	l.now = now
	return l
}

// Record writes one entry. Details may be nil.
func (l *Log) Record(ctx context.Context, actor Actor, action Action, target Target, details map[string]string) error {
	if details == nil {
		details = map[string]string{}
	}
	encoded, err := json.Marshal(details)
	if err != nil {
		return fmt.Errorf("audit: %s: %w", action, err)
	}
	id, err := newID()
	if err != nil {
		return err
	}
	// Microseconds, which is what Postgres keeps.
	at := l.now().UTC().Truncate(time.Microsecond)
	const insert = `insert into audit_log
		(id, at, actor_id, actor_name, action, target_kind, target_id, target_name, details)
		values (?, ?, ?, ?, ?, ?, ?, ?, ?)`
	if _, err := l.conn.ExecContext(ctx, db.Rebind(l.driver, insert),
		id, at, actor.ID, actor.Name, string(action),
		string(target.Kind), target.ID, target.Name, string(encoded)); err != nil {
		return fmt.Errorf("audit: record %s: %w", action, err)
	}
	return nil
}

// Page reads one page of the trail, newest first, narrowed to one kind unless
// kind is empty, and how many entries match in all. page is 1-indexed.
func (l *Log) Page(ctx context.Context, kind Kind, page, limit int) ([]Entry, int, error) {
	where, args := "", []any{}
	if kind != "" {
		where, args = ` where target_kind = ?`, append(args, string(kind))
	}
	var total int
	if err := l.conn.QueryRowContext(ctx, db.Rebind(l.driver, `select count(*) from audit_log`+where),
		args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("audit: count: %w", err)
	}

	query := `select id, at, actor_id, actor_name, action, target_kind, target_id, target_name, details
		from audit_log` + where + ` order by at desc, id desc limit ? offset ?`
	rows, err := l.conn.QueryContext(ctx, db.Rebind(l.driver, query),
		append(args, limit, (page-1)*limit)...)
	if err != nil {
		return nil, 0, fmt.Errorf("audit: page: %w", err)
	}
	defer rows.Close()
	entries := []Entry{}
	for rows.Next() {
		var (
			entry        Entry
			action, kind string
			details      string
		)
		if err := rows.Scan(&entry.ID, &entry.At, &entry.Actor.ID, &entry.Actor.Name, &action,
			&kind, &entry.Target.ID, &entry.Target.Name, &details); err != nil {
			return nil, 0, fmt.Errorf("audit: page: %w", err)
		}
		entry.At = entry.At.UTC()
		entry.Action, entry.Target.Kind = Action(action), Kind(kind)
		if err := json.Unmarshal([]byte(details), &entry.Details); err != nil {
			return nil, 0, fmt.Errorf("audit: entry %d: %w", entry.ID, err)
		}
		if entry.Details == nil {
			entry.Details = map[string]string{}
		}
		entries = append(entries, entry)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("audit: page: %w", err)
	}
	return entries, total, nil
}

// newID is a random positive 63-bit id, as the other tables have.
func newID() (int64, error) {
	for attempt := 0; attempt < 4; attempt++ {
		var buf [8]byte
		if _, err := rand.Read(buf[:]); err != nil {
			return 0, fmt.Errorf("audit: allocate id: %w", err)
		}
		if id := int64(binary.BigEndian.Uint64(buf[:]) >> 1); id != 0 {
			return id, nil
		}
	}
	return 0, fmt.Errorf("audit: allocate id: no nonzero value")
}

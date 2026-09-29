// core/profiles/profiles.go

// Package profiles stores the school's own rule profiles: sets of rule overrides a solve can use in place of a plan's own rules.
package profiles

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/binary"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/smegg99/s99ab/data/db"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"

	arrangov1 "github.com/smegg99/arrango/backend/gen/arrangov1"

	"github.com/smegg99/goptivum/backend/common/messages"
)

// The contract's bounds on a profile.
const (
	MaxNameLength = 200
	MaxOverrides  = 64
	maxRuleLength = 64
)

var (
	// ErrNotFound means no school profile has that id.
	ErrNotFound = errors.New("profiles: no such rule profile")
	// ErrNotYours is a change asked by an account that neither created the profile nor administers the school.
	ErrNotYours = messages.NewFault(messages.KeyRulesProfileNotYours, nil)
)

// rulePattern is a RuleDefinition key; whether the solver knows it is the node's answer.
var rulePattern = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)

// Invalid is a field the repository refused, named the way the error envelope names it.
type Invalid struct {
	Field string
	Code  string
}

func (e *Invalid) Error() string { return fmt.Sprintf("profiles: %s: %s", e.Field, e.Code) }

// Profile is one school rule profile.
type Profile struct {
	ID   int64
	Name string
	// Overrides are unscoped and name each rule at most once.
	Overrides     []*arrangov1.RuleOverride
	CreatedBy     string
	CreatedByName string
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

// Rules is the rule_config a solve that picks the profile sends: its overrides over the solver's defaults.
func (p Profile) Rules() *arrangov1.RuleConfig {
	rules := &arrangov1.RuleConfig{}
	for _, override := range p.Overrides {
		rules.Overrides = append(rules.Overrides, proto.CloneOf(override))
	}
	return rules
}

// Editor is the account asking to change a profile.
type Editor struct {
	ID    string
	Admin bool
}

// EditableBy reports whether an account may change or delete the profile: its creator or an administrator.
func (p Profile) EditableBy(editor Editor) bool {
	return editor.Admin || (editor.ID != "" && editor.ID == p.CreatedBy)
}

// NewProfile is a profile to store; its id and times are the repository's.
type NewProfile struct {
	Name          string
	Overrides     []*arrangov1.RuleOverride
	CreatedBy     string
	CreatedByName string
}

// Change renames a profile or replaces its overrides; a nil field is left as it is.
type Change struct {
	Name      *string
	Overrides *[]*arrangov1.RuleOverride
}

// Repository is the rule_profiles table.
type Repository struct {
	conn   *sql.DB
	driver string
}

// NewRepository binds the table to one pool.
func NewRepository(conn *sql.DB, driver string) *Repository {
	return &Repository{conn: conn, driver: driver}
}

func (r *Repository) rebind(query string) string { return db.Rebind(r.driver, query) }

const columns = `id, name, rules, created_by, created_by_name, created_at, updated_at`

// List is every school profile, oldest first, so a rename does not move one.
func (r *Repository) List(ctx context.Context) ([]Profile, error) {
	rows, err := r.conn.QueryContext(ctx, `select `+columns+` from rule_profiles order by created_at, id`)
	if err != nil {
		return nil, fmt.Errorf("profiles: list: %w", err)
	}
	defer rows.Close()

	out := []Profile{}
	for rows.Next() {
		profile, err := scan(rows)
		if err != nil {
			return nil, fmt.Errorf("profiles: list: %w", err)
		}
		out = append(out, profile)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("profiles: list: %w", err)
	}
	return out, nil
}

// Get is one school profile.
func (r *Repository) Get(ctx context.Context, id int64) (Profile, error) {
	profile, err := scan(r.conn.QueryRowContext(ctx, r.rebind(`select `+columns+` from rule_profiles where id = ?`), id))
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return Profile{}, fmt.Errorf("%w: %d", ErrNotFound, id)
	case err != nil:
		return Profile{}, fmt.Errorf("profiles: %d: %w", id, err)
	}
	return profile, nil
}

// Create stores a profile; a name another profile has is an *Invalid.
func (r *Repository) Create(ctx context.Context, add NewProfile) (Profile, error) {
	name, err := cleanName(add.Name)
	if err != nil {
		return Profile{}, err
	}
	if err := CheckOverrides(add.Overrides); err != nil {
		return Profile{}, err
	}
	id, err := newID()
	if err != nil {
		return Profile{}, err
	}
	stamp := now()
	profile := Profile{
		ID: id, Name: name, Overrides: add.Overrides, CreatedBy: add.CreatedBy,
		CreatedByName: add.CreatedByName, CreatedAt: stamp, UpdatedAt: stamp,
	}
	rules, err := encode(profile)
	if err != nil {
		return Profile{}, err
	}
	// The name check and the write are one statement; the unique index is what decides a race.
	const insert = `insert into rule_profiles (` + columns + `)
		select ?, ?, ?, ?, ?, ?, ?
		where not exists (select 1 from rule_profiles where name = ?)`
	written, err := r.conn.ExecContext(ctx, r.rebind(insert), profile.ID, profile.Name, rules,
		profile.CreatedBy, profile.CreatedByName, profile.CreatedAt, profile.UpdatedAt, profile.Name)
	if err != nil {
		return Profile{}, fmt.Errorf("profiles: create: %w", err)
	}
	if affected, err := written.RowsAffected(); err != nil {
		return Profile{}, fmt.Errorf("profiles: create: %w", err)
	} else if affected == 0 {
		return Profile{}, &Invalid{Field: "name", Code: "taken"}
	}
	return profile, nil
}

// Update renames a profile or replaces its overrides for its creator or an administrator.
func (r *Repository) Update(ctx context.Context, id int64, editor Editor, change Change) (Profile, error) {
	profile, err := r.Get(ctx, id)
	if err != nil {
		return Profile{}, err
	}
	if !profile.EditableBy(editor) {
		return Profile{}, ErrNotYours
	}
	if change.Name != nil {
		if profile.Name, err = cleanName(*change.Name); err != nil {
			return Profile{}, err
		}
	}
	if change.Overrides != nil {
		if err := CheckOverrides(*change.Overrides); err != nil {
			return Profile{}, err
		}
		profile.Overrides = *change.Overrides
	}
	if change.Name == nil && change.Overrides == nil {
		return profile, nil
	}
	rules, err := encode(profile)
	if err != nil {
		return Profile{}, err
	}
	profile.UpdatedAt = now()
	const update = `update rule_profiles set name = ?, rules = ?, updated_at = ?
		where id = ? and not exists (select 1 from rule_profiles where name = ? and id <> ?)`
	written, err := r.conn.ExecContext(ctx, r.rebind(update), profile.Name, rules, profile.UpdatedAt,
		profile.ID, profile.Name, profile.ID)
	if err != nil {
		return Profile{}, fmt.Errorf("profiles: update %d: %w", id, err)
	}
	if affected, err := written.RowsAffected(); err != nil {
		return Profile{}, fmt.Errorf("profiles: update %d: %w", id, err)
	} else if affected == 0 {
		// The row was there a moment ago, so either the name is taken or the profile went meanwhile.
		if _, err := r.Get(ctx, id); err != nil {
			return Profile{}, err
		}
		return Profile{}, &Invalid{Field: "name", Code: "taken"}
	}
	return profile, nil
}

// Delete removes a profile for good for its creator or an administrator; jobs and results keep what they were sent.
func (r *Repository) Delete(ctx context.Context, id int64, editor Editor) error {
	profile, err := r.Get(ctx, id)
	if err != nil {
		return err
	}
	if !profile.EditableBy(editor) {
		return ErrNotYours
	}
	removed, err := r.conn.ExecContext(ctx, r.rebind(`delete from rule_profiles where id = ?`), id)
	if err != nil {
		return fmt.Errorf("profiles: delete %d: %w", id, err)
	}
	if affected, err := removed.RowsAffected(); err != nil {
		return fmt.Errorf("profiles: delete %d: %w", id, err)
	} else if affected == 0 {
		return fmt.Errorf("%w: %d", ErrNotFound, id)
	}
	return nil
}

type scanner interface{ Scan(...any) error }

func scan(row scanner) (Profile, error) {
	var profile Profile
	var rules string
	if err := row.Scan(&profile.ID, &profile.Name, &rules, &profile.CreatedBy, &profile.CreatedByName,
		&profile.CreatedAt, &profile.UpdatedAt); err != nil {
		return Profile{}, err
	}
	var decoded arrangov1.RuleConfig
	if err := protojson.Unmarshal([]byte(rules), &decoded); err != nil {
		return Profile{}, fmt.Errorf("profiles: the rules of %d: %w", profile.ID, err)
	}
	profile.Overrides = decoded.GetOverrides()
	profile.CreatedAt, profile.UpdatedAt = profile.CreatedAt.UTC(), profile.UpdatedAt.UTC()
	return profile, nil
}

// encode is the rules column: the profile's rule_config in protobuf JSON.
func encode(profile Profile) (string, error) {
	encoded, err := protojson.Marshal(profile.Rules())
	if err != nil {
		return "", fmt.Errorf("profiles: encode the rules of %d: %w", profile.ID, err)
	}
	return string(encoded), nil
}

// cleanName trims a name and refuses an empty or long one; lengths count letters.
func cleanName(name string) (string, error) {
	name = strings.TrimSpace(name)
	switch {
	case name == "":
		return "", &Invalid{Field: "name", Code: "required"}
	case utf8.RuneCountInString(name) > MaxNameLength:
		return "", &Invalid{Field: "name", Code: "too_long"}
	}
	return name, nil
}

// CheckOverrides refuses what a profile cannot hold: too many, a malformed or scoped one, or one rule twice, as an *Invalid.
func CheckOverrides(overrides []*arrangov1.RuleOverride) error {
	if len(overrides) > MaxOverrides {
		return &Invalid{Field: "overrides", Code: "too_many"}
	}
	seen := make(map[string]bool, len(overrides))
	for _, override := range overrides {
		if !wellFormed(override) {
			return &Invalid{Field: "overrides", Code: "invalid"}
		}
		if seen[override.GetRule()] {
			return &Invalid{Field: "overrides", Code: "duplicate"}
		}
		seen[override.GetRule()] = true
	}
	return nil
}

func wellFormed(override *arrangov1.RuleOverride) bool {
	rule := override.GetRule()
	if len(rule) > maxRuleLength || !rulePattern.MatchString(rule) || override.GetWeight() < 0 {
		return false
	}
	if _, known := arrangov1.RuleMode_name[int32(override.GetMode())]; !known {
		return false
	}
	return override.GetYearId() == 0 && override.GetDivisionId() == 0 &&
		override.GetSubjectId() == 0 && override.GetTeacherId() == 0
}

// now is stored at microsecond precision, which is what Postgres keeps.
func now() time.Time { return time.Now().UTC().Truncate(time.Microsecond) }

// newID is a random nonzero 63-bit id; the schema has no sequences, so the application allocates ids.
func newID() (int64, error) {
	for range 4 {
		var buf [8]byte
		if _, err := rand.Read(buf[:]); err != nil {
			return 0, fmt.Errorf("profiles: allocate id: %w", err)
		}
		if id := int64(binary.BigEndian.Uint64(buf[:]) >> 1); id != 0 {
			return id, nil
		}
	}
	return 0, fmt.Errorf("profiles: allocate id: no nonzero value")
}

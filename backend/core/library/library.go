// core/library/library.go

// Package library keeps how each account arranges its Library: folders of projects, their order, pins and Recent, one versioned document per account.
package library

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/smegg99/s99ab/data/db"

	"github.com/smegg99/goptivum/backend/core/plans"
)

// The limits the contract states.
const (
	// MaxBytes bounds the document as stored.
	MaxBytes      = 256 << 10
	MaxFolders    = 500
	MaxProjects   = 5000
	MaxRecent     = 20
	MaxDepth      = 8
	MaxNameLength = 200
)

// Folder is one of an account's folders; it holds project ids, never the projects.
type Folder struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	// Parent is the folder this one sits in, empty at the top level.
	Parent   string  `json:"parent,omitempty"`
	Accent   string  `json:"accent,omitempty"`
	Pinned   bool    `json:"pinned,omitempty"`
	Projects []int64 `json:"projects"`
}

// Library is one account's arrangement; every list keeps the order shown.
type Library struct {
	// Version counts the account's saves, 0 before the first; a save carries the version it read.
	Version  int64    `json:"-"`
	Folders  []Folder `json:"folders"`
	Projects []int64  `json:"projects"`
	Pinned   []int64  `json:"pinned"`
	Recent   []int64  `json:"recent"`
}

// Invalid is a field the repository refused, named the way the error envelope names it.
type Invalid struct {
	Field string
	Code  string
}

func (e *Invalid) Error() string { return fmt.Sprintf("library: %s: %s", e.Field, e.Code) }

// Stale refuses a save whose version is not the stored one and carries what is stored.
type Stale struct {
	Current Library
}

func (e *Stale) Error() string {
	return fmt.Sprintf("library: the stored arrangement is at version %d", e.Current.Version)
}

// Plans finds the plans with these ids, in Trash or not; an id with no plan is left out.
type Plans interface {
	Lookup(ctx context.Context, ids []int64) (map[int64]plans.Plan, error)
}

// Repository is the account_libraries table.
type Repository struct {
	conn   *sql.DB
	driver string
	plans  Plans
}

// NewRepository binds the table to one pool and to the plan index that says which projects still exist.
func NewRepository(conn *sql.DB, driver string, index Plans) *Repository {
	return &Repository{conn: conn, driver: driver, plans: index}
}

func (r *Repository) rebind(query string) string { return db.Rebind(r.driver, query) }

// Get is the account's arrangement without the projects removed for good; one never saved is version 0.
func (r *Repository) Get(ctx context.Context, account string) (Library, error) {
	var version int64
	var document string
	err := r.conn.QueryRowContext(ctx,
		r.rebind(`select version, document from account_libraries where identity_id = ?`), account).
		Scan(&version, &document)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return normal(Library{}), nil
	case err != nil:
		return Library{}, fmt.Errorf("library: read %s: %w", account, err)
	}
	var stored Library
	if err := json.Unmarshal([]byte(document), &stored); err != nil {
		return Library{}, fmt.Errorf("library: decode %s: %w", account, err)
	}
	stored.Version = version
	return r.visible(ctx, normal(stored))
}

// Save stores next for the account when next.Version is the stored version, and answers what it stored as Get would.
func (r *Repository) Save(ctx context.Context, account string, next Library) (Library, error) {
	next = normal(next)
	if err := check(&next); err != nil {
		return Library{}, err
	}
	document, err := json.Marshal(next)
	if err != nil {
		return Library{}, fmt.Errorf("library: encode %s: %w", account, err)
	}
	if len(document) > MaxBytes {
		return Library{}, &Invalid{Field: "body", Code: "too_large"}
	}

	// The version check and the write are one statement, so two saves from one version cannot both land.
	var written sql.Result
	if next.Version == 0 {
		written, err = r.conn.ExecContext(ctx, r.rebind(`insert into account_libraries (identity_id, version, document)
			values (?, 1, ?) on conflict (identity_id) do nothing`), account, string(document))
	} else {
		written, err = r.conn.ExecContext(ctx, r.rebind(`update account_libraries set version = version + 1, document = ?
			where identity_id = ? and version = ?`), string(document), account, next.Version)
	}
	if err != nil {
		return Library{}, fmt.Errorf("library: save %s: %w", account, err)
	}
	affected, err := written.RowsAffected()
	if err != nil {
		return Library{}, fmt.Errorf("library: save %s: %w", account, err)
	}
	if affected == 0 {
		current, err := r.Get(ctx, account)
		if err != nil {
			return Library{}, err
		}
		return Library{}, &Stale{Current: current}
	}
	next.Version++
	return r.visible(ctx, next)
}

// visible leaves out the projects removed for good; one in Trash stays, so a restore finds its place.
func (r *Repository) visible(ctx context.Context, arrangement Library) (Library, error) {
	named := names(arrangement)
	if len(named) == 0 {
		return arrangement, nil
	}
	found, err := r.plans.Lookup(ctx, named)
	if err != nil {
		return Library{}, fmt.Errorf("library: look up projects: %w", err)
	}
	exists := func(ids []int64) []int64 {
		kept := make([]int64, 0, len(ids))
		for _, id := range ids {
			if _, ok := found[id]; ok {
				kept = append(kept, id)
			}
		}
		return kept
	}
	folders := make([]Folder, len(arrangement.Folders))
	for i, folder := range arrangement.Folders {
		folder.Projects = exists(folder.Projects)
		folders[i] = folder
	}
	arrangement.Folders = folders
	arrangement.Projects = exists(arrangement.Projects)
	arrangement.Pinned = exists(arrangement.Pinned)
	arrangement.Recent = exists(arrangement.Recent)
	return arrangement, nil
}

// names is every project id the arrangement names, each once.
func names(arrangement Library) []int64 {
	seen := map[int64]bool{}
	var out []int64
	add := func(ids []int64) {
		for _, id := range ids {
			if !seen[id] {
				seen[id] = true
				out = append(out, id)
			}
		}
	}
	for _, folder := range arrangement.Folders {
		add(folder.Projects)
	}
	add(arrangement.Projects)
	add(arrangement.Pinned)
	add(arrangement.Recent)
	return out
}

// normal gives every list a value, so an answer carries [] rather than null, and copies the folders it may change.
func normal(arrangement Library) Library {
	orEmpty := func(ids []int64) []int64 {
		if ids == nil {
			return []int64{}
		}
		return ids
	}
	folders := make([]Folder, len(arrangement.Folders))
	for i, folder := range arrangement.Folders {
		folder.Projects = orEmpty(folder.Projects)
		folders[i] = folder
	}
	arrangement.Folders = folders
	arrangement.Projects = orEmpty(arrangement.Projects)
	arrangement.Pinned = orEmpty(arrangement.Pinned)
	arrangement.Recent = orEmpty(arrangement.Recent)
	return arrangement
}

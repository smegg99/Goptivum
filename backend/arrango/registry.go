// arrango/registry.go

// Package arrango is this server's policy over its Arrango Nodes: the registry
// of nodes it has paired with, each node's limits, the job table, admission,
// and dispatching and following a solve. The client itself (identity, pairing,
// pinned connections, a solve's stream) is the solveclient module, which the
// desktop uses as well.
package arrango

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/binary"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/smegg99/s99ab/data/db"

	"github.com/smegg99/goptivum/solveclient"
)

// ErrNoNode means no node has that id, or it is revoked.
var ErrNoNode = errors.New("arrango: no such node")

// ErrRevoked is a node that was paired and is not trusted any more.
var ErrRevoked = errors.New("arrango: the node is revoked")

// Node is one paired Arrango Node.
type Node struct {
	ID          int64
	Label       string
	Address     string
	Fingerprint string
	PairedAt    time.Time
	RevokedAt   *time.Time
	// Limits is this server's own cap on the node.
	Limits Limits
}

// Revoked reports whether this node may still be dispatched to.
func (n Node) Revoked() bool { return n.RevokedAt != nil }

// Registry is the arrango_nodes table.
type Registry struct {
	conn   *sql.DB
	driver string
}

// NewRegistry binds the registry to one pool.
func NewRegistry(conn *sql.DB, driver string) *Registry {
	return &Registry{conn: conn, driver: driver}
}

func (r *Registry) rebind(query string) string { return db.Rebind(r.driver, query) }

// now matches the index's precision, which is what Postgres keeps.
func now() time.Time { return time.Now().UTC().Truncate(time.Microsecond) }

// newID allocates a random 63-bit id. The schema stays dialect-neutral, so
// there are no sequences and no identity columns to lean on.
func newID() (int64, error) {
	var buffer [8]byte
	if _, err := rand.Read(buffer[:]); err != nil {
		return 0, fmt.Errorf("arrango: read random bytes: %w", err)
	}
	return int64(binary.BigEndian.Uint64(buffer[:]) >> 1), nil
}

// Record stores a pairing. Pairing the same node again replaces the address,
// clears a revocation and keeps the id, because the fingerprint is the identity
// and a second row for it would be two registry entries racing over one peer.
// An empty label keeps an existing node's name; a new node needs one.
//
// A new node starts at DefaultLimits of the node's ceilings; an existing one
// keeps its limits, lowered to the ceilings when the node reported them.
func (r *Registry) Record(ctx context.Context, label string, paired solveclient.Paired) (Node, error) {
	fingerprint := paired.Fingerprint.String()

	existing, err := r.ByFingerprint(ctx, fingerprint)
	if err == nil {
		if label == "" {
			label = existing.Label
		}
		limits := existing.Limits
		if paired.Ceilings != nil {
			limits = limits.Within(*paired.Ceilings)
		}
		update := r.rebind(`update arrango_nodes
			set label = ?, address = ?, paired_at = ?, revoked_at = null,
				solves_at_once = ?, workers_per_solve = ? where id = ?`)
		stamp := now()
		if _, err := r.conn.ExecContext(ctx, update, label, paired.Address, stamp,
			limits.Solves, limits.Workers, existing.ID); err != nil {
			return Node{}, fmt.Errorf("arrango: update node: %w", err)
		}
		existing.Label, existing.Address, existing.PairedAt, existing.RevokedAt = label, paired.Address, stamp, nil
		existing.Limits = limits
		return existing, nil
	}
	if !errors.Is(err, ErrNoNode) {
		return Node{}, err
	}
	if label == "" {
		return Node{}, fmt.Errorf("arrango: a node needs a label")
	}

	id, err := newID()
	if err != nil {
		return Node{}, err
	}
	node := Node{
		ID: id, Label: label, Address: paired.Address,
		Fingerprint: fingerprint, PairedAt: now(), Limits: DefaultLimits(paired.Ceilings),
	}
	insert := r.rebind(`insert into arrango_nodes
		(id, label, address, fingerprint, paired_at, solves_at_once, workers_per_solve)
		values (?, ?, ?, ?, ?, ?, ?)`)
	if _, err := r.conn.ExecContext(ctx, insert, node.ID, node.Label, node.Address,
		node.Fingerprint, node.PairedAt, node.Limits.Solves, node.Limits.Workers); err != nil {
		return Node{}, fmt.Errorf("arrango: record node: %w", err)
	}
	return node, nil
}

// NodeChange is a rename, new limits, or both; a nil field is left as it is.
type NodeChange struct {
	Label  *string
	Limits *Limits
}

// Update renames a node or sets its limits in one statement. The caller has
// checked the limits against the node's ceilings.
func (r *Registry) Update(ctx context.Context, id int64, change NodeChange) (Node, error) {
	var sets []string
	var args []any
	if change.Label != nil {
		sets, args = append(sets, "label = ?"), append(args, *change.Label)
	}
	if change.Limits != nil {
		sets = append(sets, "solves_at_once = ?", "workers_per_solve = ?")
		args = append(args, change.Limits.Solves, change.Limits.Workers)
	}
	if len(sets) > 0 {
		query := r.rebind(`update arrango_nodes set ` + strings.Join(sets, ", ") + ` where id = ?`)
		if _, err := r.conn.ExecContext(ctx, query, append(args, id)...); err != nil {
			return Node{}, fmt.Errorf("arrango: update node: %w", err)
		}
	}
	return r.Get(ctx, id)
}

const nodeColumns = `id, label, address, fingerprint, paired_at, revoked_at, solves_at_once, workers_per_solve`

func scanNode(row interface{ Scan(...any) error }) (Node, error) {
	var node Node
	var revoked sql.NullTime
	if err := row.Scan(&node.ID, &node.Label, &node.Address,
		&node.Fingerprint, &node.PairedAt, &revoked, &node.Limits.Solves, &node.Limits.Workers); err != nil {
		return Node{}, err
	}
	if revoked.Valid {
		stamp := revoked.Time.UTC()
		node.RevokedAt = &stamp
	}
	node.PairedAt = node.PairedAt.UTC()
	return node, nil
}

// Get is one node by id, revoked or not. Dispatch checks the revocation
// itself, because a job that names a revoked node still has to read.
func (r *Registry) Get(ctx context.Context, id int64) (Node, error) {
	query := r.rebind(`select ` + nodeColumns + ` from arrango_nodes where id = ?`)
	node, err := scanNode(r.conn.QueryRowContext(ctx, query, id))
	if errors.Is(err, sql.ErrNoRows) {
		return Node{}, ErrNoNode
	}
	if err != nil {
		return Node{}, fmt.Errorf("arrango: get node: %w", err)
	}
	return node, nil
}

// ByFingerprint finds a node by the identity it is pinned to.
func (r *Registry) ByFingerprint(ctx context.Context, fingerprint string) (Node, error) {
	query := r.rebind(`select ` + nodeColumns + ` from arrango_nodes where fingerprint = ?`)
	node, err := scanNode(r.conn.QueryRowContext(ctx, query, fingerprint))
	if errors.Is(err, sql.ErrNoRows) {
		return Node{}, ErrNoNode
	}
	if err != nil {
		return Node{}, fmt.Errorf("arrango: get node by fingerprint: %w", err)
	}
	return node, nil
}

// List is every node, revoked ones included, newest pairing first.
func (r *Registry) List(ctx context.Context) ([]Node, error) {
	query := r.rebind(`select ` + nodeColumns + ` from arrango_nodes order by paired_at desc, id`)
	rows, err := r.conn.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("arrango: list nodes: %w", err)
	}
	defer rows.Close()

	nodes := []Node{}
	for rows.Next() {
		node, err := scanNode(rows)
		if err != nil {
			return nil, fmt.Errorf("arrango: scan node: %w", err)
		}
		nodes = append(nodes, node)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("arrango: list nodes: %w", err)
	}
	return nodes, nil
}

// Active is every node that is not revoked, paired earliest first.
func (r *Registry) Active(ctx context.Context) ([]Node, error) {
	query := r.rebind(`select ` + nodeColumns + ` from arrango_nodes
		where revoked_at is null order by paired_at, id`)
	rows, err := r.conn.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("arrango: active nodes: %w", err)
	}
	defer rows.Close()

	nodes := []Node{}
	for rows.Next() {
		node, err := scanNode(rows)
		if err != nil {
			return nil, fmt.Errorf("arrango: scan node: %w", err)
		}
		nodes = append(nodes, node)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("arrango: active nodes: %w", err)
	}
	return nodes, nil
}

// First is the node paired earliest that is not revoked. ErrNoNode means there
// is none.
func (r *Registry) First(ctx context.Context) (Node, error) {
	query := r.rebind(`select ` + nodeColumns + ` from arrango_nodes
		where revoked_at is null order by paired_at, id limit 1`)
	node, err := scanNode(r.conn.QueryRowContext(ctx, query))
	if errors.Is(err, sql.ErrNoRows) {
		return Node{}, ErrNoNode
	}
	if err != nil {
		return Node{}, fmt.Errorf("arrango: first node: %w", err)
	}
	return node, nil
}

// Revoke stops this server dispatching to a node. Revoking is one side only:
// the node forgets this deployment when an operator revokes it there.
func (r *Registry) Revoke(ctx context.Context, id int64) error {
	query := r.rebind(`update arrango_nodes set revoked_at = ? where id = ? and revoked_at is null`)
	result, err := r.conn.ExecContext(ctx, query, now(), id)
	if err != nil {
		return fmt.Errorf("arrango: revoke node: %w", err)
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("arrango: revoke node: %w", err)
	}
	if changed == 0 {
		// Either it does not exist or it was already revoked, and the caller
		// wanted the same end state in both cases.
		if _, err := r.Get(ctx, id); err != nil {
			return err
		}
	}
	return nil
}

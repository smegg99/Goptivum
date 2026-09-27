// solveclient/dial.go

// Package solveclient is the client half of the link to an Arrango Node,
// shared by the Goptivum server and the desktop: the caller's own identity,
// pairing, pinned connections, the node's status, checks, and one solve's
// stream read to its best timetable.
//
// It holds no policy of its own caller: which node to use, how many solves a
// node may run and where a result is kept belong to the program that imports
// it.
//
// The mTLS client is imported from the Arrango repo rather than rebuilt here.
// Its Dial verifies the presented certificate against the pin before anything
// leaves this process, which is the property the whole pairing design rests on,
// and a second copy of that logic is a second chance to get it wrong.
package solveclient

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	arrangoid "github.com/smegg99/arrango/backend/identity"
	"github.com/smegg99/arrango/backend/transport"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"

	arrangorpcv1 "github.com/smegg99/arrango/backend/gen/arrangorpcv1"
)

// dialTimeout bounds reaching a node. It is not the solve budget: a solve gets
// its own deadline, computed from the time the caller asked for.
const dialTimeout = 15 * time.Second

// Identity is the caller's own keypair. A node pins it the same way the caller
// pins the node's, so it is created once and kept.
type Identity struct {
	self *arrangoid.Identity
}

// LoadIdentity reads the caller's keypair, creating it on first use.
//
// The fingerprint of this certificate is what a node records when it enrols
// the caller, so replacing the directory means every node must pair again.
func LoadIdentity(dir string) (*Identity, error) {
	if dir == "" {
		return nil, fmt.Errorf("solveclient: no identity directory")
	}
	if arrangoid.Exists(dir) {
		self, err := arrangoid.Load(dir)
		if err != nil {
			return nil, fmt.Errorf("solveclient: load identity: %w", err)
		}
		return &Identity{self: self}, nil
	}
	// Create writes into the directory but does not make one, and 0700 because
	// a private key lives here.
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("solveclient: create identity directory: %w", err)
	}
	self, err := arrangoid.Create(dir)
	if err != nil {
		return nil, fmt.Errorf("solveclient: create identity: %w", err)
	}
	return &Identity{self: self}, nil
}

// Fingerprint is what a node records for the caller.
func (i *Identity) Fingerprint() arrangoid.Fingerprint { return i.self.Fingerprint }

// Conn is a connection to one node, plus the clients on it.
type Conn struct {
	conn   *grpc.ClientConn
	label  string
	Node   arrangorpcv1.NodeServiceClient
	Solver arrangorpcv1.SolverServiceClient
}

// Close releases the connection.
func (c *Conn) Close() error { return c.conn.Close() }

// Dialer opens pinned connections to nodes.
type Dialer struct {
	identity *Identity
	version  string
}

// NewDialer binds the identity and the version the caller announces. Every
// call carries the version; a node refuses a caller that does not send one.
func NewDialer(identity *Identity, clientVersion string) (*Dialer, error) {
	if identity == nil {
		return nil, fmt.Errorf("solveclient: no identity")
	}
	if clientVersion == "" {
		return nil, fmt.Errorf("solveclient: no client version to announce")
	}
	return &Dialer{identity: identity, version: clientVersion}, nil
}

// Dial connects to a node, pinned to its fingerprint.
//
// A fingerprint that does not match is a transport.PinMismatch and never a
// retry: a regenerated node certificate is indistinguishable from an impostor,
// so it demands a deliberate re-pair.
func (d *Dialer) Dial(ctx context.Context, address string, pin arrangoid.Fingerprint) (*Conn, error) {
	dialCtx, cancel := context.WithTimeout(ctx, dialTimeout)
	defer cancel()

	conn, err := transport.Dial(dialCtx, address, pin, d.identity.self, transport.Announce(d.version)...)
	if err != nil {
		return nil, err
	}
	return &Conn{
		conn:   conn,
		label:  address,
		Node:   arrangorpcv1.NewNodeServiceClient(conn),
		Solver: arrangorpcv1.NewSolverServiceClient(conn),
	}, nil
}

// Reach connects to a paired node by the fingerprint stored for it, and names
// it by label in every error. A node it cannot reach is ErrUnreachable, and one
// presenting another certificate is ErrPinMismatch as well.
func (d *Dialer) Reach(ctx context.Context, label, address, fingerprint string) (*Conn, error) {
	pin, err := arrangoid.ParseFingerprint(fingerprint)
	if err != nil {
		return nil, fmt.Errorf("solveclient: node %s has an unreadable fingerprint: %w", label, err)
	}
	conn, err := d.Dial(ctx, address, pin)
	if err != nil {
		var mismatch *transport.PinMismatch
		if errors.As(err, &mismatch) {
			return nil, fmt.Errorf("%w: %w: %s: %w", ErrUnreachable, ErrPinMismatch, label, err)
		}
		return nil, fmt.Errorf("%w: %s: %w", ErrUnreachable, label, err)
	}
	conn.label = label
	return conn, nil
}

// withToken attaches the one-time pairing token. A node enrols the caller on
// the first call that carries a valid one and records its fingerprint for good.
func withToken(ctx context.Context, token string) context.Context {
	return metadata.AppendToOutgoingContext(ctx, transport.MetadataToken, token)
}

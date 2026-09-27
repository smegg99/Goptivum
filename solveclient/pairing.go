// solveclient/pairing.go

package solveclient

import (
	"context"
	"errors"
	"fmt"

	arrangoid "github.com/smegg99/arrango/backend/identity"
	"github.com/smegg99/arrango/backend/pairing"
	"github.com/smegg99/arrango/backend/transport"

	arrangorpcv1 "github.com/smegg99/arrango/backend/gen/arrangorpcv1"
)

// Paired is what one successful pairing learned.
type Paired struct {
	Address     string
	Fingerprint arrangoid.Fingerprint
	NodeAPI     string
	MinClient   string
	Solver      string
	// Ceilings is the policy the node gave the caller, nil when it reported none.
	Ceilings *Ceilings
}

// Pair redeems a pairing URI.
//
// The order is the whole design: parse, dial with the fingerprint pinned, and
// only once the presented certificate matches does the token travel. An
// impostor at that address fails the pin and learns nothing, not even that a
// token existed.
//
// A refusal is ErrNotALink, ErrUnreachable, ErrPinMismatch, ErrLinkSpent,
// ErrClientTooOld, or the node's own error.
func (d *Dialer) Pair(ctx context.Context, uri string) (Paired, error) {
	parsed, err := pairing.ParseURI(uri)
	if err != nil {
		return Paired{}, fmt.Errorf("%w: %w", ErrNotALink, err)
	}

	conn, err := d.Dial(ctx, parsed.Address, parsed.Fingerprint)
	if err != nil {
		var mismatch *transport.PinMismatch
		if errors.As(err, &mismatch) {
			return Paired{}, fmt.Errorf("%w: %w", ErrPinMismatch, err)
		}
		return Paired{}, fmt.Errorf("%w: %s: %w", ErrUnreachable, parsed.Address, err)
	}
	defer conn.Close()

	// The first call carrying a valid token is what enrols the caller, so the
	// cheapest one doubles as the pairing handshake and as proof the node
	// speaks a version the caller can use.
	info, err := conn.Node.GetNodeInfo(withToken(ctx, string(parsed.Token)), &arrangorpcv1.NodeInfoRequest{})
	if err != nil {
		refused := refusal(parsed.Address, "pair", err)
		if errors.Is(refused, ErrNotPaired) {
			// Not on the node's list and the token did not put it there. With
			// no token pending the node closes its handshake to strangers, so
			// an expired or used link reads the same as a wrong one.
			return Paired{}, fmt.Errorf("%w: %s: %v", ErrLinkSpent, parsed.Address, err)
		}
		return Paired{}, refused
	}

	paired := Paired{
		Address:     parsed.Address,
		Fingerprint: parsed.Fingerprint,
		NodeAPI:     info.GetNodeApiVersion(),
		MinClient:   info.GetMinClientVersion(),
		Solver:      info.GetSolverVersion(),
	}
	// Enrolled by now, so the status carries the policy the operator gave the
	// caller. A status that fails leaves the ceilings unknown: the pairing
	// itself has happened, and a caller reads the policy live anyway.
	if status, err := conn.Node.GetNodeStatus(ctx, &arrangorpcv1.NodeStatusRequest{}); err == nil {
		if ceilings, ok := CeilingsOf(status); ok {
			paired.Ceilings = &ceilings
		}
	}
	return paired, nil
}

// arrango/admission.go

package arrango

import (
	"context"
	"errors"
	"fmt"

	"github.com/smegg99/goptivum/solveclient"

	arrangorpcv1 "github.com/smegg99/arrango/backend/gen/arrangorpcv1"
	arrangov1 "github.com/smegg99/arrango/backend/gen/arrangov1"
)

// DefaultConcurrentSolves is a newly paired node's solves at once when the node
// reports no ceiling of its own.
//
// One, because that is what a node gives a newly paired peer:
// config.DefaultPolicy() on the node is MaxConcurrentSolves: 1. Asking for a
// second does not fail fast, which would be fine. The node QUEUES it, polls for
// a slot, and then re-checks the caller's remaining deadline after the wait, so
// the second solve sits silently for as long as the first one runs and then
// fails on a deadline it can no longer meet. A local refusal is a far better
// answer than that, which is why admission also holds to the node's own
// ceiling, read live.
const DefaultConcurrentSolves = 1

// ErrNoCapacity is a node that cannot take this solve: at this server's solves
// at once on it or the node's own ceiling, or out of the memory a single worker
// needs.
var ErrNoCapacity = errors.New("arrango: the node cannot take another solve")

// Admission is what a dispatch has to pass before anything is sent.
type Admission struct {
	Node    Node
	Workers uint32
	// Capacity is what the node said about itself, just now.
	Capacity solveclient.Capacity
}

// Admit checks the request and a node before anything is dispatched and reports what the solve gets.
//
// nodeID 0 picks the node paired earliest that is unrevoked, below the
// concurrency ceiling and answering; when none is, the refusal is
// ErrNoCapacity if one was busy, else solveclient.ErrUnreachable if one was silent, else
// ErrNoNode.
func (d *Dispatcher) Admit(ctx context.Context, nodeID int64,
	model *arrangov1.SchoolModel, config *arrangorpcv1.SolverConfig) (Admission, error) {

	if err := solveclient.CheckBaseline(model, config); err != nil {
		return Admission{}, err
	}
	if nodeID != 0 {
		node, err := d.nodes.Get(ctx, nodeID)
		if err != nil {
			return Admission{}, err
		}
		if node.Revoked() {
			return Admission{}, fmt.Errorf("%w: %s", ErrRevoked, node.Label)
		}
		return d.admitOn(ctx, node, model, config)
	}

	candidates, err := d.nodes.Active(ctx)
	if err != nil {
		return Admission{}, err
	}
	var busy, silent error
	var limit *solveclient.ErrLimitTooLong
	var outdated *solveclient.ErrOutdated
	for _, node := range candidates {
		admission, err := d.admitOn(ctx, node, model, config)
		switch {
		case err == nil:
			return admission, nil
		case errors.As(err, &outdated):
			// An older node must not prevent another eligible node being tried.
		case isLimitRefusal(err):
			var tooLong *solveclient.ErrLimitTooLong
			errors.As(err, &tooLong)
			if limit == nil || tooLong.Seconds > limit.Seconds {
				limit = tooLong
			}
		case errors.Is(err, ErrNoCapacity):
			busy = err
		case errors.Is(err, solveclient.ErrUnreachable):
			silent = err
		default:
			return Admission{}, err
		}
	}
	switch {
	case busy != nil:
		return Admission{}, busy
	case silent != nil:
		return Admission{}, silent
	case limit != nil:
		return Admission{}, limit
	case outdated != nil:
		return Admission{}, outdated
	}
	return Admission{}, fmt.Errorf("%w: none is paired and unrevoked", ErrNoNode)
}

// admitOn admits the solve on one unrevoked node, within this server's limits
// on it and the node's own ceilings for this server.
func (d *Dispatcher) admitOn(ctx context.Context, node Node,
	model *arrangov1.SchoolModel, config *arrangorpcv1.SolverConfig) (Admission, error) {

	running, err := d.jobs.CountActive(ctx, node.ID)
	if err != nil {
		return Admission{}, err
	}

	if running >= node.Limits.Solves {
		return Admission{}, fmt.Errorf("%w: %s", ErrNoCapacity, node.Label)
	}

	// A silent node is refused here, in seconds, rather than failed in the
	// background; solveclient.ErrModelInvalid is a baseline the node checked and refused.
	capacity, err := d.preflight(ctx, node, model, solveclient.BaselineSnapshot(model, config), config.GetStallStopSeconds() > 0)
	if err != nil {
		return Admission{}, err
	}
	// Read live, because the operator may have paired this server again with a
	// lower one since its limits were set, and a solve above it queues on the
	// node instead of being refused.
	ceilings := capacity.Ceilings
	if err := solveclient.CheckTimeLimit(node.Label, capacity, config); err != nil {
		return Admission{}, err
	}
	if ceilings != nil && ceilings.Solves > 0 && running >= ceilings.Solves {
		return Admission{}, fmt.Errorf("%w: %s already runs %d, the node allows this server %d",
			ErrNoCapacity, node.Label, running, ceilings.Solves)
	}
	if capacity.AffordableWorkers == 0 {
		return Admission{}, fmt.Errorf(
			"%w: %s free on %s, and one worker needs %s on top of a %s base",
			ErrNoCapacity, humanBytes(capacity.FreeBytes), node.Label,
			humanBytes(solveclient.PerWorkerBytes), humanBytes(solveclient.BaseBytes))
	}
	// The node's workers per solve is what a solve gets and the most it may
	// ask for; the node's ceiling and its free memory can lower it.
	ceiling := min(uint32(node.Limits.Workers), capacity.AffordableWorkers)
	if ceilings != nil && ceilings.Workers > 0 {
		ceiling = min(ceiling, uint32(ceilings.Workers))
	}
	requested := config.GetNumWorkers()
	if requested == 0 {
		requested = uint32(node.Limits.Workers)
	}
	return Admission{
		Node:     node,
		Workers:  solveclient.ClampWorkers(requested, ceiling),
		Capacity: capacity,
	}, nil
}

// humanBytes is for a refusal a person reads, never for arithmetic.
func humanBytes(n uint64) string {
	switch {
	case n >= 1<<30:
		return fmt.Sprintf("%.1f GB", float64(n)/float64(1<<30))
	case n >= 1<<20:
		return fmt.Sprintf("%.0f MB", float64(n)/float64(1<<20))
	default:
		return fmt.Sprintf("%d bytes", n)
	}
}

func isLimitRefusal(err error) bool {
	var limit *solveclient.ErrLimitTooLong
	return errors.As(err, &limit)
}

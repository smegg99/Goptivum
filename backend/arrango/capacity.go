// arrango/capacity.go

package arrango

import (
	"context"
	"fmt"
	"slices"
	"time"

	"github.com/smegg99/goptivum/solveclient"

	arrangorpcv1 "github.com/smegg99/arrango/backend/gen/arrangorpcv1"
	arrangov1 "github.com/smegg99/arrango/backend/gen/arrangov1"
)

// statusTimeout bounds the capacity question, and is short because it sits on
// the synchronous path of the request that accepts the job.
//
// A node on a LAN answers this in milliseconds, and one across the internet in
// well under a second. One that does not answer in three seconds is refused
// as unreachable: accepting it only moves the same failure into the
// background, where the planner learns of it later.
const statusTimeout = 3 * time.Second

// preflight asks a node what it can do and, when there is a schedule to check,
// whether that schedule holds, on one connection. A node that cannot be dialled
// or does not answer the first question is ErrUnreachable.
//
// Its own connection, not the solve's. The solve is dispatched from a
// background goroutine that outlives the request, and holding a connection
// across that boundary means leaking one whenever the answer is a refusal.
//
// Both questions together because they are one round trip to the same node.
func (d *Dispatcher) preflight(ctx context.Context, node Node,
	model *arrangov1.SchoolModel,
	snapshot *arrangov1.ScheduleSnapshot, stall bool) (solveclient.Capacity, error) {
	askCtx, cancel := context.WithTimeout(ctx, statusTimeout)
	defer cancel()

	conn, err := d.dialer.Reach(askCtx, node.Label, node.Address, node.Fingerprint)
	if err != nil {
		return solveclient.Capacity{}, err
	}
	defer conn.Close()

	status, err := conn.Node.GetNodeStatus(askCtx, &arrangorpcv1.NodeStatusRequest{})
	if err != nil {
		return solveclient.Capacity{}, fmt.Errorf("%w: %s status: %w", solveclient.ErrUnreachable, node.Label, err)
	}
	capacity := solveclient.CapacityOf(status)
	info, err := conn.Node.GetNodeInfo(askCtx, &arrangorpcv1.NodeInfoRequest{})
	if err != nil {
		return capacity, fmt.Errorf("%w: %s info: %w", solveclient.ErrUnreachable, node.Label, err)
	}
	if !slices.Contains(info.GetCapabilities(), solveclient.CapabilityRuleMetadata) {
		return capacity, &solveclient.ErrOutdated{Node: node.Label, Capability: solveclient.CapabilityRuleMetadata}
	}
	if stall && !slices.Contains(info.GetCapabilities(), "stall_stop") {
		return capacity, &solveclient.ErrOutdated{Node: node.Label, Capability: "stall_stop"}
	}

	// Validate reports on a SCHEDULE, so it is only asked when there is one.
	// Asked with no snapshot it checks the model against an empty timetable,
	// where every lesson is KIND_MISSING_LESSON and every day breaks its load
	// rule: that refused every solve of a plan that had never been placed,
	// which is every imported plan. The inputs themselves are checked by the
	// node's own RunPreflight at the start of Solve, which refuses in seconds
	// and names the culprit.
	//
	// Validation runs a solver child on the node, so it is not free. It is
	// still seconds against a budget of minutes, and the alternative is a
	// school watching a full search fail on a baseline that never held.
	if snapshot != nil {
		report, err := conn.Solver.Validate(askCtx,
			&arrangorpcv1.ValidateRequest{Model: model, Snapshot: snapshot})
		if err != nil {
			// Not a refusal. The node answered its status a moment ago, so a
			// failed check is a transient, and turning it into a lost job is
			// the worse trade.
			return capacity, nil
		}
		if report != nil && !report.GetValid() && len(report.GetConflicts()) > 0 {
			return capacity, &solveclient.ErrModelInvalid{Conflicts: report.GetConflicts()}
		}
	}
	return capacity, nil
}

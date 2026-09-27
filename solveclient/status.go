// solveclient/status.go

package solveclient

import (
	"context"
	"slices"

	arrangorpcv1 "github.com/smegg99/arrango/backend/gen/arrangorpcv1"
)

// CapabilityPeerPolicy is the node's name for reporting the caller's own policy in GetNodeStatus.
const CapabilityPeerPolicy = "peer_policy"

// PerWorkerBytes and BaseBytes are the node's memory cost of one worker and of
// a solve's base: its own arithmetic, mirrored rather than imported, because
// importing its capacity package would pull the whole node service into a
// client. Mega peaked at 15.9 GB on 16 workers and was OOM-killed at 20 on a
// 23 GB box, which is where these two numbers come from.
const (
	PerWorkerBytes uint64 = 900 << 20
	BaseBytes      uint64 = 1 << 30
)

// Ceilings is the node's own policy for the caller, set by its operator at pairing.
type Ceilings struct {
	// Solves is the most solves at once, 0 when the node sets no limit of its own.
	Solves int
	// Workers is the node's worker ceiling for the caller, or its cores when it sets none; 0 when neither is known.
	Workers int
	// Seconds is the maximum search time granted to this peer.
	Seconds float64
}

// CeilingsOf reads the caller's policy out of a node's status; false when the node's build reports none.
func CeilingsOf(status *arrangorpcv1.NodeStatusResponse) (Ceilings, bool) {
	policy := status.GetPolicy()
	if policy == nil {
		return Ceilings{}, false
	}
	workers := policy.GetWorkerCeiling()
	if workers == 0 {
		// Zero is "the host's cores decide", and the node clamps to them.
		workers = status.GetCores()
	}
	return Ceilings{Solves: int(policy.GetMaxConcurrentSolves()), Workers: int(workers), Seconds: policy.GetMaxSolveSeconds()}, true
}

// Capacity is what a node said about itself, just now.
//
// Zero value means it was not asked, and Known is how a caller tells that
// apart from a node that answered "nothing available".
type Capacity struct {
	Known             bool
	Cores             uint32
	FreeBytes         uint64
	AffordableWorkers uint32
	RunningSolves     uint32
	LoadAverage1m     float64
	// Speed above 1 is faster than the host the built-in figures were measured
	// on. Carried for a caller that wants to explain why a solve took as long
	// as it did; never an input to the clamp.
	Speed float64
	// Ceilings is the node's own policy for the caller, nil when its build
	// reports none.
	Ceilings *Ceilings
}

// AffordableWorkers is how many workers the node's free memory covers.
//
// This mirrors capacity.Host.AffordableWorkers on the node. The node runs it
// again on arrival and is the authority; running it here is what turns "the
// solve was queued or refused after a 1272 lesson model was sent" into an
// answer the caller gets before anything is sent.
func AffordableWorkers(free uint64, cores uint32) uint32 {
	if free <= BaseBytes {
		return 0
	}
	workers := (free - BaseBytes) / PerWorkerBytes
	if cores > 0 && workers > uint64(cores) {
		workers = uint64(cores)
	}
	return uint32(workers)
}

// CapacityOf reads the node's status into the caller's view of it.
func CapacityOf(status *arrangorpcv1.NodeStatusResponse) Capacity {
	free := uint64(0)
	if available := status.GetAvailableBytes(); available > status.GetCommittedBytes() {
		free = available - status.GetCommittedBytes()
	}
	capacity := Capacity{
		Known:             true,
		Cores:             status.GetCores(),
		FreeBytes:         free,
		AffordableWorkers: AffordableWorkers(free, status.GetCores()),
		RunningSolves:     status.GetRunningSolves(),
		LoadAverage1m:     status.GetLoadAverage_1M(),
		Speed:             status.GetCalibration().GetSpeed(),
	}
	if ceilings, ok := CeilingsOf(status); ok {
		capacity.Ceilings = &ceilings
	}
	return capacity
}

// Probe is a node as it answered just now.
type Probe struct {
	NodeAPI      string
	Solver       string
	MinClient    string
	Capabilities []string
	Capacity     Capacity
}

// Can reports whether the node's build has a capability.
func (p Probe) Can(capability string) bool { return slices.Contains(p.Capabilities, capability) }

// Probe asks the node GetNodeInfo and GetNodeStatus. A failure is
// ErrUnreachable, ErrNotPaired or ErrClientTooOld; the caller bounds the time.
func (c *Conn) Probe(ctx context.Context) (Probe, error) {
	info, err := c.Node.GetNodeInfo(ctx, &arrangorpcv1.NodeInfoRequest{})
	if err != nil {
		return Probe{}, refusal(c.label, "node info", err)
	}
	answer, err := c.Node.GetNodeStatus(ctx, &arrangorpcv1.NodeStatusRequest{})
	if err != nil {
		return Probe{}, refusal(c.label, "status", err)
	}
	return Probe{
		NodeAPI:      info.GetNodeApiVersion(),
		Solver:       info.GetSolverVersion(),
		MinClient:    info.GetMinClientVersion(),
		Capabilities: info.GetCapabilities(),
		Capacity:     CapacityOf(answer),
	}, nil
}

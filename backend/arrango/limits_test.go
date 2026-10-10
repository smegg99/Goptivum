// arrango/limits_test.go

package arrango_test

import (
	"context"
	"errors"
	"testing"
	"time"

	arrangorpcv1 "github.com/smegg99/arrango/backend/gen/arrangorpcv1"

	"github.com/smegg99/goptivum/backend/arrango"
)

// limitedNode records a fake node and sets this server's limits on it.
func limitedNode(t *testing.T, registry *arrango.Registry, fake *fakeNode, limits arrango.Limits) arrango.Node {
	t.Helper()
	recorded, err := registry.Record(t.Context(), "node", fake.paired())
	if err != nil {
		t.Fatalf("record: %v", err)
	}
	node, err := registry.Update(t.Context(), recorded.ID, arrango.NodeChange{Solves: &limits.Solves, Workers: &limits.Workers})
	if err != nil {
		t.Fatalf("set limits: %v", err)
	}
	return node
}

func policyStatus(solves, workers uint32) *arrangorpcv1.NodeStatusResponse {
	return &arrangorpcv1.NodeStatusResponse{
		Cores: 8, AvailableBytes: 16 << 30,
		Policy: &arrangorpcv1.PeerPolicy{MaxConcurrentSolves: solves, WorkerCeiling: workers, MaxSolveSeconds: 900},
	}
}

// Solves at once is this server's limit on each node, not one number for all.
func TestAdmissionHoldsToTheNodesSolvesAtOnce(t *testing.T) {
	registry, jobs, repository, store := newRegistry(t)
	ctx := t.Context()
	node := limitedNode(t, registry, startFakeNode(t, &fakeNode{}), arrango.Limits{Solves: 2, Workers: 4})
	plan := newPlan(t, repository, store)
	dispatcher := newDispatcher(t, registry, jobs)
	config := &arrangorpcv1.SolverConfig{IgnorePreviousPlacements: true}

	if _, err := jobs.Create(ctx, arrango.NewJob{PlanID: plan.ID, Version: 1, NodeID: node.ID, Workers: 4, StartedBy: owner}); err != nil {
		t.Fatalf("create job: %v", err)
	}
	if _, err := dispatcher.Admit(ctx, node.ID, lessons(1, 1), config); err != nil {
		t.Fatalf("the second of two solves at once: %v", err)
	}
	if _, err := jobs.Create(ctx, arrango.NewJob{PlanID: plan.ID, Version: 1, NodeID: node.ID, Workers: 4, StartedBy: owner}); err != nil {
		t.Fatalf("create job: %v", err)
	}
	if _, err := dispatcher.Admit(ctx, node.ID, lessons(1, 1), config); !errors.Is(err, arrango.ErrNoCapacity) {
		t.Errorf("a third solve: err = %v, want ErrNoCapacity", err)
	}
}

// The node's own ceiling for this server is read at every admission, so a
// solve never goes above what its operator allows, even after the operator
// lowered it below the limit this server keeps.
func TestAdmissionHoldsToTheNodesLiveCeiling(t *testing.T) {
	registry, jobs, repository, store := newRegistry(t)
	ctx := t.Context()
	fake := startFakeNode(t, &fakeNode{status: policyStatus(1, 12)})
	node := limitedNode(t, registry, fake, arrango.Limits{Solves: 3, Workers: 4})
	plan := newPlan(t, repository, store)
	if _, err := jobs.Create(ctx, arrango.NewJob{PlanID: plan.ID, Version: 1, NodeID: node.ID, Workers: 4, StartedBy: owner}); err != nil {
		t.Fatalf("create job: %v", err)
	}

	dispatcher := newDispatcher(t, registry, jobs)
	_, err := dispatcher.Admit(ctx, node.ID, lessons(1, 1), &arrangorpcv1.SolverConfig{IgnorePreviousPlacements: true})
	if !errors.Is(err, arrango.ErrNoCapacity) {
		t.Errorf("err = %v, want ErrNoCapacity at the node's ceiling of 1", err)
	}
}

// A solve gets the node's workers per solve, fewer when it asks for fewer, and
// never more than the node allows this server.
func TestEachSolveGetsTheNodesWorkersPerSolve(t *testing.T) {
	for _, one := range []struct {
		name      string
		status    *arrangorpcv1.NodeStatusResponse
		workers   int
		requested uint32
		want      uint32
	}{
		{"nothing asked", nil, 2, 0, 2},
		{"nothing asked of a node above the default", nil, 6, 0, 6},
		{"more asked", nil, 2, 8, 2},
		{"fewer asked", nil, 2, 1, 1},
		{"the node's ceiling is lower", policyStatus(1, 1), 2, 0, 1},
		{"the node sets no ceiling", policyStatus(0, 0), 2, 0, 2},
	} {
		t.Run(one.name, func(t *testing.T) {
			registry, jobs, _, _ := newRegistry(t)
			node := limitedNode(t, registry, startFakeNode(t, &fakeNode{status: one.status}), arrango.Limits{Solves: 1, Workers: one.workers})
			dispatcher := newDispatcher(t, registry, jobs)
			admission, err := dispatcher.Admit(t.Context(), node.ID, lessons(1, 1),
				&arrangorpcv1.SolverConfig{NumWorkers: one.requested, IgnorePreviousPlacements: true})
			if err != nil {
				t.Fatalf("admit: %v", err)
			}
			if admission.Workers != one.want {
				t.Errorf("workers = %d, want %d", admission.Workers, one.want)
			}
		})
	}
}

// Revoking a node stops its running solves and answers once their rows are
// closed; nothing is admitted on it afterwards.
func TestRevokingANodeStopsItsSolvesAndWaits(t *testing.T) {
	node := startFakeNode(t, &fakeNode{hold: true})
	runner, registry, jobs, repository, store := newRunner(t)
	ctx := t.Context()
	recorded, err := registry.Record(ctx, "streaming", node.paired())
	if err != nil {
		t.Fatalf("record: %v", err)
	}
	plan := newPlan(t, repository, store)
	job, err := runner.Start(ctx, order(plan.ID, recorded.ID, lessons(2, 2)))
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	waitFor(t, "the solve to run", func() bool {
		row, err := jobs.Get(ctx, job.ID)
		return err == nil && row.Status == arrango.StatusRunning
	})

	if err := runner.Revoke(ctx, recorded.ID); err != nil {
		t.Fatalf("revoke: %v", err)
	}
	closed, err := jobs.Get(ctx, job.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if closed.Status != arrango.StatusCancelled || closed.FinishedAt == nil {
		t.Errorf("the node's job is %q, finished %v, want cancelled before Revoke answered", closed.Status, closed.FinishedAt)
	}
	stored, err := registry.Get(ctx, recorded.ID)
	if err != nil || !stored.Revoked() {
		t.Errorf("node = %+v, %v, want it revoked", stored, err)
	}
	if _, err := runner.Start(ctx, order(plan.ID, recorded.ID, lessons(2, 2))); !errors.Is(err, arrango.ErrRevoked) {
		t.Errorf("a solve after the revoke: err = %v, want ErrRevoked", err)
	}
	if err := runner.Revoke(ctx, 999); !errors.Is(err, arrango.ErrNoNode) {
		t.Errorf("revoking a node that does not exist = %v, want ErrNoNode", err)
	}
}

// A solve admitted just before its node was revoked, and started only after
// the revoke's cancel, never reaches the node.
func TestASolveOnANodeRevokedAfterAdmissionNeverReachesIt(t *testing.T) {
	node := startFakeNode(t, &fakeNode{hold: true})
	registry, jobs, repository, store := newRegistry(t)
	ctx := t.Context()
	recorded, err := registry.Record(ctx, "streaming", node.paired())
	if err != nil {
		t.Fatalf("record: %v", err)
	}
	plan := newPlan(t, repository, store)
	dispatcher := newDispatcher(t, registry, jobs)

	solve := order(plan.ID, recorded.ID, lessons(2, 2))
	admission, job, err := dispatcher.Begin(ctx, solve)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	if err := registry.Revoke(ctx, recorded.ID); err != nil {
		t.Fatalf("revoke: %v", err)
	}
	// Bounded, so a solve that did reach the held node ends as a failure here
	// rather than at its deadline.
	finishCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if _, err := dispatcher.Finish(finishCtx, admission, job, solve.Request, nil); err == nil {
		t.Fatal("a solve on a revoked node finished without an error")
	}

	closed, err := jobs.Get(ctx, job.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if closed.Status != arrango.StatusCancelled {
		t.Errorf("status = %q, want cancelled", closed.Status)
	}
	if calls := node.solveCalls(); calls != 0 {
		t.Errorf("the revoked node was asked to solve %d times", calls)
	}
}

// A node's history is the jobs this server ran on it, whoever started them,
// newest first and paged.
func TestANodesHistoryIsItsOwnJobsNewestFirst(t *testing.T) {
	registry, jobs, repository, store := newRegistry(t)
	ctx := t.Context()
	node, err := registry.Record(ctx, "node", paired(t, "127.0.0.1:1"))
	if err != nil {
		t.Fatalf("record: %v", err)
	}
	other, err := registry.Record(ctx, "other", paired(t, "127.0.0.1:2"))
	if err != nil {
		t.Fatalf("record: %v", err)
	}
	plan := newPlan(t, repository, store)

	var ids []int64
	for i := range 3 {
		time.Sleep(2 * time.Millisecond)
		job, err := jobs.Create(ctx, arrango.NewJob{PlanID: plan.ID, Version: 1, NodeID: node.ID, Workers: uint32(i + 1),
			StartedBy: arrango.Starter{ID: "someone-" + string(rune('a'+i))}})
		if err != nil {
			t.Fatalf("create job: %v", err)
		}
		ids = append(ids, job.ID)
	}
	if _, err := jobs.Create(ctx, arrango.NewJob{PlanID: plan.ID, Version: 1, NodeID: other.ID, Workers: 4, StartedBy: owner}); err != nil {
		t.Fatalf("create job: %v", err)
	}

	first, total, err := jobs.ForNode(ctx, node.ID, 0, 2)
	if err != nil {
		t.Fatalf("for node: %v", err)
	}
	if total != 3 || len(first) != 2 || first[0].ID != ids[2] || first[1].ID != ids[1] {
		t.Errorf("first page = %d of %d, want the two newest of 3", len(first), total)
	}
	rest, _, err := jobs.ForNode(ctx, node.ID, 2, 2)
	if err != nil || len(rest) != 1 || rest[0].ID != ids[0] {
		t.Errorf("second page = %+v, %v, want the oldest", rest, err)
	}
	for _, job := range append(first, rest...) {
		if job.NodeID != node.ID || job.NodeLabel != "node" {
			t.Errorf("job %d is on node %d (%q)", job.ID, job.NodeID, job.NodeLabel)
		}
	}
}

// arrango/pick_test.go

package arrango_test

import (
	"errors"
	"testing"
	"time"

	arrangorpcv1 "github.com/smegg99/arrango/backend/gen/arrangorpcv1"

	"github.com/smegg99/goptivum/backend/arrango"
	"github.com/smegg99/goptivum/solveclient"
)

// A node that does not answer is refused at admission, in seconds, rather than
// accepted and failed in the background.
func TestAnUnreachableNodeIsRefusedAtAdmission(t *testing.T) {
	runner, registry, jobs, repository, store := newRunner(t)
	ctx := t.Context()
	// Port 1 on loopback refuses at once.
	node, err := registry.Record(ctx, "silent", paired(t, "127.0.0.1:1"))
	if err != nil {
		t.Fatalf("record: %v", err)
	}
	plan := newPlan(t, repository, store)

	_, err = runner.Start(ctx, order(plan.ID, node.ID, lessons(2, 2)))
	if !errors.Is(err, solveclient.ErrUnreachable) {
		t.Fatalf("err = %v, want ErrUnreachable", err)
	}
	if listed, err := jobs.ForPlan(ctx, plan.ID); err != nil || len(listed) != 0 {
		t.Errorf("jobs = %+v, %v: a refused solve opened a row", listed, err)
	}
}

// Without a node named, the dispatcher takes the node paired earliest that is
// not revoked, has a free slot and answers.
func TestTheDispatcherPicksTheFirstNodeThatCanTakeTheSolve(t *testing.T) {
	registry, jobs, repository, store := newRegistry(t)
	dispatcher := newDispatcher(t, registry, jobs)
	ctx := t.Context()
	plan := newPlan(t, repository, store)
	answering := startFakeNode(t, &fakeNode{})

	record := func(label string, pair solveclient.Paired) arrango.Node {
		t.Helper()
		// Pairing order is by time, to the microsecond.
		time.Sleep(2 * time.Millisecond)
		node, err := registry.Record(ctx, label, pair)
		if err != nil {
			t.Fatalf("record %s: %v", label, err)
		}
		return node
	}
	revoked := record("revoked", paired(t, "127.0.0.1:2"))
	if err := registry.Revoke(ctx, revoked.ID); err != nil {
		t.Fatalf("revoke: %v", err)
	}
	busy := record("busy", paired(t, "127.0.0.1:3"))
	if _, err := jobs.Create(ctx, arrango.NewJob{PlanID: plan.ID, Version: 1, NodeID: busy.ID, Workers: 4,
		StartedBy: arrango.Starter{ID: "owner"}}); err != nil {
		t.Fatalf("fill the busy node: %v", err)
	}
	record("silent", paired(t, "127.0.0.1:1"))
	chosen := record("answering", answering.paired())

	admission, err := dispatcher.Admit(ctx, 0, lessons(2, 2), &arrangorpcv1.SolverConfig{IgnorePreviousPlacements: true})
	if err != nil {
		t.Fatalf("admit: %v", err)
	}
	if admission.Node.ID != chosen.ID {
		t.Errorf("picked %q, want %q", admission.Node.Label, chosen.Label)
	}
	if admission.Workers == 0 || !admission.Capacity.Known {
		t.Errorf("admission = %+v", admission)
	}
}

// When no node can take the solve, the refusal says why: none paired, every
// one busy, or none that had room answered.
func TestWhenNoNodeCanTakeASolveTheRefusalSaysWhy(t *testing.T) {
	registry, jobs, repository, store := newRegistry(t)
	dispatcher := newDispatcher(t, registry, jobs)
	ctx := t.Context()
	plan := newPlan(t, repository, store)
	config := &arrangorpcv1.SolverConfig{IgnorePreviousPlacements: true}

	if _, err := dispatcher.Admit(ctx, 0, lessons(2, 2), config); !errors.Is(err, arrango.ErrNoNode) {
		t.Errorf("no node paired: err = %v, want ErrNoNode", err)
	}

	silent, err := registry.Record(ctx, "silent", paired(t, "127.0.0.1:1"))
	if err != nil {
		t.Fatalf("record: %v", err)
	}
	if _, err := dispatcher.Admit(ctx, 0, lessons(2, 2), config); !errors.Is(err, solveclient.ErrUnreachable) {
		t.Errorf("the only node silent: err = %v, want ErrUnreachable", err)
	}

	if _, err := jobs.Create(ctx, arrango.NewJob{PlanID: plan.ID, Version: 1, NodeID: silent.ID, Workers: 4,
		StartedBy: arrango.Starter{ID: "owner"}}); err != nil {
		t.Fatalf("fill the node: %v", err)
	}
	if _, err := dispatcher.Admit(ctx, 0, lessons(2, 2), config); !errors.Is(err, arrango.ErrNoCapacity) {
		t.Errorf("the only node busy: err = %v, want ErrNoCapacity", err)
	}

	if err := registry.Revoke(ctx, silent.ID); err != nil {
		t.Fatalf("revoke: %v", err)
	}
	if _, err := dispatcher.Admit(ctx, 0, lessons(2, 2), config); !errors.Is(err, arrango.ErrNoNode) {
		t.Errorf("the only node revoked: err = %v, want ErrNoNode", err)
	}
}

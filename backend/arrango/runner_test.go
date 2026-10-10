// arrango/runner_test.go

package arrango_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/smegg99/s99ab/data/db"
	"github.com/smegg99/s99ab/data/storage"

	arrangorpcv1 "github.com/smegg99/arrango/backend/gen/arrangorpcv1"
	arrangov1 "github.com/smegg99/arrango/backend/gen/arrangov1"

	"github.com/smegg99/goptivum/backend/arrango"
	"github.com/smegg99/goptivum/backend/core/documents"
	"github.com/smegg99/goptivum/backend/core/plans"
	"github.com/smegg99/goptivum/backend/core/results"
	"github.com/smegg99/goptivum/backend/migrations"
	"github.com/smegg99/goptivum/solveclient"
)

// owner is who starts every solve here.
var owner = arrango.Starter{ID: "owner", Name: "Anna Kowalska"}

// order asks for a from-scratch solve of a plan's version 1 on a node.
func order(planID, nodeID int64, model *arrangov1.SchoolModel) arrango.Order {
	return arrango.Order{
		PlanID: planID, Version: 1, NodeID: nodeID, StartedBy: owner,
		Request: &arrangorpcv1.SolveRequest{
			Model:  model,
			Config: &arrangorpcv1.SolverConfig{IgnorePreviousPlacements: true},
		},
	}
}

func newRunner(t *testing.T) (*arrango.Runner, *arrango.Registry, *arrango.Jobs, *plans.Repository, *documents.Store) {
	t.Helper()
	conn := db.TestDB(t, migrations.FS)
	registry := arrango.NewRegistry(conn, db.SQLite)
	jobs := arrango.NewJobs(conn, db.SQLite)
	repository := plans.NewRepository(conn, db.SQLite, plans.Caps{BackupsKeep: 100, ResultsKeep: 20})

	self, err := solveclient.LoadIdentity(t.TempDir())
	if err != nil {
		t.Fatalf("identity: %v", err)
	}
	dialer, err := solveclient.NewDialer(self, "0.1.0")
	if err != nil {
		t.Fatalf("dialer: %v", err)
	}
	// A real persist, so a test can assert on what a solve actually stored
	// rather than on a flag saying it would have.
	store := documents.NewStore(storage.NewLocalDir(t.TempDir(), "/objects"))
	dispatcher, err := arrango.NewDispatcher(dialer, registry, jobs,
		results.Persist(repository, store), repository.Live)
	if err != nil {
		t.Fatalf("dispatcher: %v", err)
	}
	runner := arrango.NewRunner(dispatcher, jobs)
	t.Cleanup(func() { _ = runner.Close() })
	return runner, registry, jobs, repository, store
}

// Admission is synchronous, so a caller answering an HTTP request can refuse
// before anything runs in the background.
func TestRunnerRefusesBeforeStarting(t *testing.T) {
	runner, registry, jobs, repository, store := newRunner(t)
	ctx := t.Context()

	node, err := registry.Record(ctx, "node", paired(t, "127.0.0.1:1"))
	if err != nil {
		t.Fatalf("record: %v", err)
	}
	if err := registry.Revoke(ctx, node.ID); err != nil {
		t.Fatalf("revoke: %v", err)
	}
	plan := newPlan(t, repository, store)

	_, err = runner.Start(ctx, order(plan.ID, node.ID, lessons(1, 1)))
	if !errors.Is(err, arrango.ErrRevoked) {
		t.Fatalf("err = %v, want ErrRevoked", err)
	}
	if runner.Running() != 0 {
		t.Error("a refused solve is running")
	}
	// Nothing was opened, so nothing holds the node's capacity.
	active, err := jobs.CountActive(ctx, node.ID)
	if err != nil || active != 0 {
		t.Errorf("CountActive = %d, %v, want 0", active, err)
	}
}

// The stream breaks, so the solve fails. What matters is that it fails as a
// closed job rather than a row stuck in running for ever.
func TestRunnerClosesTheRowWhenTheSolveFails(t *testing.T) {
	node := startFakeNode(t, &fakeNode{solveErr: errors.New("the solver process died")})
	runner, registry, jobs, repository, store := newRunner(t)
	ctx := t.Context()

	recorded, err := registry.Record(ctx, "node", node.paired())
	if err != nil {
		t.Fatalf("record: %v", err)
	}
	plan := newPlan(t, repository, store)

	solve := order(plan.ID, recorded.ID, lessons(2, 2))
	solve.Request.Config.NumWorkers = 0
	job, err := runner.Start(ctx, solve)
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	if job.PlanVersion != 1 {
		t.Errorf("plan_version = %d, want the version that was asked for", job.PlanVersion)
	}
	// The clamp happened before the row was written.
	if job.NumWorkers == 0 {
		t.Error("the job recorded zero workers")
	}

	waitForTerminal(t, jobs, job.ID)

	closed, err := jobs.Get(ctx, job.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if closed.Status != arrango.StatusFailed {
		t.Errorf("status = %q, want failed", closed.Status)
	}
	if closed.FinishedAt == nil {
		t.Error("a closed job has no finish time")
	}
	if closed.ResultID != nil {
		t.Error("a solve whose stream broke kept a result")
	}
	active, err := jobs.CountActive(ctx, recorded.ID)
	if err != nil || active != 0 {
		t.Errorf("CountActive = %d, %v, want 0 once the job is terminal", active, err)
	}
}

// A subscriber attached while the solve runs sees the terminal frame, so a
// client never has to guess that the stream is over.
func TestRunnerPublishesATerminalFrame(t *testing.T) {
	fake := startFakeNode(t, &fakeNode{hold: true})
	runner, registry, _, repository, store := newRunner(t)
	ctx := t.Context()

	node, err := registry.Record(ctx, "node", fake.paired())
	if err != nil {
		t.Fatalf("record: %v", err)
	}
	plan := newPlan(t, repository, store)

	job, err := runner.Start(ctx, order(plan.ID, node.ID, lessons(1, 1)))
	if err != nil {
		t.Fatalf("start: %v", err)
	}

	broker, live := runner.Broker(job.ID)
	if !live {
		t.Fatal("a held solve is not running")
	}
	events, cancel := broker.Subscribe()
	defer cancel()
	runner.Cancel(job.ID)

	deadline := time.After(10 * time.Second)
	for {
		select {
		case event, open := <-events:
			if !open {
				t.Fatal("the stream closed with no terminal frame")
			}
			var update arrango.Update
			if err := event.Unmarshal(&update); err != nil {
				t.Fatalf("decode event: %v", err)
			}
			if update.Terminal {
				if update.JobID == "" {
					t.Error("the terminal frame names no job")
				}
				return
			}
		case <-deadline:
			t.Fatal("no terminal frame arrived")
		}
	}
}

// Close is a lifecycle Closer: it stops the work and waits for the rows, so a
// shutdown does not leave a job counted against its node for ever.
func TestRunnerCloseWaitsForTheRows(t *testing.T) {
	fake := startFakeNode(t, &fakeNode{hold: true})
	runner, registry, jobs, repository, store := newRunner(t)
	ctx := t.Context()

	node, err := registry.Record(ctx, "node", fake.paired())
	if err != nil {
		t.Fatalf("record: %v", err)
	}
	plan := newPlan(t, repository, store)
	job, err := runner.Start(ctx, order(plan.ID, node.ID, lessons(1, 1)))
	if err != nil {
		t.Fatalf("start: %v", err)
	}

	if err := runner.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	if runner.Running() != 0 {
		t.Error("a solve is still running after Close returned")
	}
	closed, err := jobs.Get(ctx, job.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if closed.FinishedAt == nil {
		t.Errorf("the job is still open after Close: %+v", closed)
	}
	// Closing twice is the same end state.
	if err := runner.Close(); err != nil {
		t.Errorf("closing twice: %v", err)
	}
}

// Cancelling a job that already finished is not an error: the caller wanted it
// stopped and it is stopped.
func TestCancelAFinishedJobIsNotAnError(t *testing.T) {
	runner, _, _, _, _ := newRunner(t)
	if runner.Cancel(12345) {
		t.Error("cancelling a job that was never running reported that it was")
	}
}

// waitForTerminal polls the row, because the work runs on its own goroutine.
func waitForTerminal(t *testing.T, jobs *arrango.Jobs, id int64) {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		job, err := jobs.Get(t.Context(), id)
		if err != nil {
			t.Fatalf("get: %v", err)
		}
		if job.FinishedAt != nil {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("the job never reached a terminal state")
}

// A solve outlives its request, so it must not hold the request's context.
func TestARunningSolveDoesNotHoldItsCallersContext(t *testing.T) {
	fake := startFakeNode(t, &fakeNode{frames: []*arrangorpcv1.SolveUpdate{
		doneFrame(arrangorpcv1.SolveStatus_SOLVE_STATUS_FEASIBLE, placed(1, 1)),
	}})
	runner, registry, jobs, repository, store := newRunner(t)

	node, err := registry.Record(t.Context(), "node", fake.paired())
	if err != nil {
		t.Fatalf("record: %v", err)
	}
	plan := newPlan(t, repository, store)

	// A context that is cancelled the moment Start returns, the way a request's
	// is. The solve must still run and still close its row.
	caller, cancel := context.WithCancel(t.Context())
	job, err := runner.Start(caller, order(plan.ID, node.ID, lessons(1, 1)))
	cancel()
	if err != nil {
		t.Fatalf("start: %v", err)
	}

	waitForTerminal(t, jobs, job.ID)
	closed, err := jobs.Get(t.Context(), job.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if closed.FinishedAt == nil || closed.Status != arrango.StatusDone {
		t.Errorf("the job is %q, finished %v, after its caller went away", closed.Status, closed.FinishedAt)
	}
}

// Deleting a project stops its solves first, and the answer waits until their
// rows are closed: a solve left running on a deleted project keeps its node busy
// while this server counts the node free.
func TestCancelPlanStopsItsSolvesAndWaitsForTheRows(t *testing.T) {
	node := startFakeNode(t, &fakeNode{
		frames: []*arrangorpcv1.SolveUpdate{solutionFrame(1, 100)},
		hold:   true,
	})
	runner, registry, jobs, repository, store := newRunner(t)
	ctx := t.Context()
	recorded := limitedNode(t, registry, node, arrango.Limits{Solves: 2, Workers: 4})
	deleted := newPlan(t, repository, store)
	kept := newPlan(t, repository, store)

	start := func(planID int64) arrango.Job {
		t.Helper()
		job, err := runner.Start(ctx, order(planID, recorded.ID, lessons(2, 2)))
		if err != nil {
			t.Fatalf("start: %v", err)
		}
		return job
	}
	stopped, running := start(deleted.ID), start(kept.ID)
	waitFor(t, "both solves to run", func() bool {
		first, err1 := jobs.Get(ctx, stopped.ID)
		second, err2 := jobs.Get(ctx, running.ID)
		return err1 == nil && err2 == nil &&
			first.Status == arrango.StatusRunning && second.Status == arrango.StatusRunning
	})
	waitFor(t, "received schedule", func() bool { preview, ok := runner.Preview(stopped.ID); return ok && preview.Received > 0 })

	if err := runner.CancelPlan(ctx, deleted.ID); err != nil {
		t.Fatalf("cancel plan: %v", err)
	}
	// No polling: the row is closed by the time CancelPlan returns.
	closed, err := jobs.Get(ctx, stopped.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if closed.Status != arrango.StatusCancelled || closed.FinishedAt == nil {
		t.Errorf("the deleted plan's job is %q, finished %v", closed.Status, closed.FinishedAt)
	}
	if closed.ResultID == nil {
		t.Error("the schedule the cancelled solve had was not stored before CancelPlan returned")
	}
	other, err := jobs.Get(ctx, running.ID)
	if err != nil || other.Status != arrango.StatusRunning {
		t.Errorf("another plan's solve = %+v, %v; want it still running", other, err)
	}
	if runner.Running() != 1 {
		t.Errorf("%d solves running, want the other plan's one", runner.Running())
	}

	// A plan with nothing running answers at once.
	if err := runner.CancelPlan(ctx, deleted.ID); err != nil {
		t.Errorf("cancel a plan with nothing running: %v", err)
	}
}

// Revoking a node stops what runs on it, the same way.
func TestCancelNodeStopsEverySolveOnTheNode(t *testing.T) {
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

	if err := runner.CancelNode(ctx, recorded.ID); err != nil {
		t.Fatalf("cancel node: %v", err)
	}
	closed, err := jobs.Get(ctx, job.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if closed.FinishedAt == nil || closed.Status != arrango.StatusCancelled {
		t.Errorf("the node's job is %q, finished %v", closed.Status, closed.FinishedAt)
	}
	if runner.Running() != 0 {
		t.Errorf("%d solves still running", runner.Running())
	}
}

// A solve admitted just before its plan went to Trash, and registered only
// after the delete's last cancel, never reaches the node: it is refused once
// the runner holds it, so every solve is either cancelled or refused.
func TestASolveOfAPlanInTrashNeverReachesTheNode(t *testing.T) {
	node := startFakeNode(t, &fakeNode{hold: true})
	runner, registry, jobs, repository, store := newRunner(t)
	ctx := t.Context()
	recorded, err := registry.Record(ctx, "streaming", node.paired())
	if err != nil {
		t.Fatalf("record: %v", err)
	}
	plan := newPlan(t, repository, store)
	if err := repository.Trash(ctx, plan.ID); err != nil {
		t.Fatalf("trash: %v", err)
	}

	job, err := runner.Start(ctx, order(plan.ID, recorded.ID, lessons(2, 2)))
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	waitForTerminal(t, jobs, job.ID)

	closed, err := jobs.Get(ctx, job.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if closed.Status != arrango.StatusCancelled {
		t.Errorf("status = %q, want cancelled", closed.Status)
	}
	if calls := node.solveCalls(); calls != 0 {
		t.Errorf("the node was asked to solve %d times for a plan in Trash", calls)
	}
	active, err := jobs.CountActive(ctx, recorded.ID)
	if err != nil || active != 0 {
		t.Errorf("CountActive = %d, %v, want 0", active, err)
	}
}

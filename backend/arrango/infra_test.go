// arrango/infra_test.go

package arrango_test

import (
	"os"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"github.com/smegg99/s99ab/data/db"
	"github.com/smegg99/s99ab/data/storage"

	arrangorpcv1 "github.com/smegg99/arrango/backend/gen/arrangorpcv1"

	"github.com/smegg99/goptivum/backend/arrango"
	"github.com/smegg99/goptivum/backend/core/documents"
	"github.com/smegg99/goptivum/backend/core/plans"
	"github.com/smegg99/goptivum/backend/core/results"
	"github.com/smegg99/goptivum/backend/migrations"
	"github.com/smegg99/goptivum/backend/version"
	"github.com/smegg99/goptivum/document"
	"github.com/smegg99/goptivum/solveclient"
)

// demoSolveBudget is the default search budget. The test does not require a
// schedule: a timeout is a real outcome of a bounded search on a loaded host,
// not a fault in this client. Raise it with GOPTIVUM_TEST_SOLVE_SECONDS to
// exercise the path that stores one.
const demoSolveBudget = 120.0

func solveBudget(t *testing.T) float64 {
	t.Helper()
	raw := os.Getenv("GOPTIVUM_TEST_SOLVE_SECONDS")
	if raw == "" {
		return demoSolveBudget
	}
	seconds, err := strconv.ParseFloat(raw, 64)
	if err != nil || seconds <= 0 {
		t.Fatalf("GOPTIVUM_TEST_SOLVE_SECONDS = %q, want a positive number", raw)
	}
	return seconds
}

// realNode pairs with the node named by a fresh pairing URI.
//
// A URI rather than a stored identity, because a token is one-time: the test
// mints its own peer entry on the node each run and is self-contained. Revoke
// the label afterwards on the node if a run is not wanted in its peer list.
func realNode(t *testing.T) (*arrango.Dispatcher, *arrango.Registry, *plans.Repository,
	*documents.Store, int64) {
	t.Helper()

	uri := os.Getenv("GOPTIVUM_TEST_ARRANGO_URI")
	if uri == "" {
		t.Skip("no GOPTIVUM_TEST_ARRANGO_URI - see just test-arrango")
	}

	self, err := solveclient.LoadIdentity(t.TempDir())
	if err != nil {
		t.Fatalf("identity: %v", err)
	}
	dialer, err := solveclient.NewDialer(self, version.Client())
	if err != nil {
		t.Fatalf("dialer: %v", err)
	}

	ctx := t.Context()
	paired, err := dialer.Pair(ctx, uri)
	if err != nil {
		t.Fatalf("pair: %v", err)
	}
	t.Logf("paired with %s: node api %s, solver %s, clients >= %s",
		paired.Address, paired.NodeAPI, paired.Solver, paired.MinClient)

	conn := db.TestDB(t, migrations.FS)
	registry := arrango.NewRegistry(conn, db.SQLite)
	jobs := arrango.NewJobs(conn, db.SQLite)
	repository := plans.NewRepository(conn, db.SQLite, plans.Caps{BackupsKeep: 100, ResultsKeep: 20})
	store := documents.NewStore(storage.NewLocalDir(t.TempDir(), "/objects"))

	node, err := registry.Record(ctx, "test-node", paired)
	if err != nil {
		t.Fatalf("record: %v", err)
	}
	// The real hook, not a stand-in: a solved schedule that is never stored is
	// a job the user cannot open, and this is the run that proves otherwise.
	dispatcher, err := arrango.NewDispatcher(dialer, registry, jobs,
		results.Persist(repository, store), nil)
	if err != nil {
		t.Fatalf("dispatcher: %v", err)
	}
	return dispatcher, registry, repository, store, node.ID
}

// The milestone's gate: a real node, paired by fingerprint, accepts a
// dispatched solve and streams updates back through this server.
func TestInfraSolveOnARealNode(t *testing.T) {
	dispatcher, registry, repository, store, nodeID := realNode(t)
	ctx := t.Context()

	node, err := registry.Get(ctx, nodeID)
	if err != nil {
		t.Fatalf("get node: %v", err)
	}
	// The node ships the demo school, so the test needs no fixture of its own
	// and no school's real data ever leaves this machine.
	dialer := dispatcherDialer(t, dispatcher)
	conn, err := dialer.Reach(ctx, node.Label, node.Address, node.Fingerprint)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	model, err := conn.Solver.GetDemoSchool(ctx, &arrangorpcv1.DemoRequest{})
	if err != nil {
		conn.Close()
		t.Fatalf("demo school: %v", err)
	}
	conn.Close()
	t.Logf("demo school: %d lessons", len(model.GetLessons()))

	// Stored as a version, because a result is the document it was solved
	// from with the timetable, and there has to be one.
	encoded, err := document.Encode(document.New(model))
	if err != nil {
		t.Fatalf("encode the demo school: %v", err)
	}
	key, err := store.Put(ctx, encoded)
	if err != nil {
		t.Fatalf("store the demo school: %v", err)
	}
	plan, _, err := repository.Create(ctx, plans.Plan{
		OwnerIdentityID: "test", Name: "demo", SchoolYear: "2026/2027",
	}, plans.Upload{ObjectKey: key, SizeBytes: encoded.Size, CreatedBy: "test"})
	if err != nil {
		t.Fatalf("create plan: %v", err)
	}

	// Capacity negotiation: the node answers with its real host, and admission
	// clamps on that rather than on what this server was configured to assume.
	admission, err := dispatcher.Admit(ctx, nodeID, model,
		&arrangorpcv1.SolverConfig{NumWorkers: 0, IgnorePreviousPlacements: true})
	if err != nil {
		t.Fatalf("admit: %v", err)
	}
	if !admission.Capacity.Known {
		t.Fatal("the node did not answer the capacity question")
	}
	if admission.Capacity.Cores == 0 {
		t.Error("the node reports no cores")
	}
	if admission.Capacity.AffordableWorkers == 0 {
		t.Skip("the node has no memory free for a solve right now")
	}
	if admission.Workers > admission.Capacity.AffordableWorkers {
		t.Errorf("admitted %d workers on a node that affords %d",
			admission.Workers, admission.Capacity.AffordableWorkers)
	}
	t.Logf("node capacity: %d cores, %.1f GB free, affords %d workers, %d running, load %.2f, speed %.2f",
		admission.Capacity.Cores, float64(admission.Capacity.FreeBytes)/(1<<30),
		admission.Capacity.AffordableWorkers, admission.Capacity.RunningSolves,
		admission.Capacity.LoadAverage1m, admission.Capacity.Speed)

	var updates, solutions atomic.Int64
	var lastPhase atomic.Value
	request := &arrangorpcv1.SolveRequest{
		Model: model,
		Config: &arrangorpcv1.SolverConfig{
			TotalTimeSeconds: solveBudget(t),
			// Deliberately zero: admission must clamp it before it reaches the
			// node, because zero means every core on a 24 core box.
			NumWorkers:               0,
			IgnorePreviousPlacements: true,
		},
	}

	started := time.Now()
	solve := arrango.Order{PlanID: plan.ID, Version: 1, NodeID: nodeID,
		StartedBy: arrango.Starter{ID: "test", Name: "test"}, Request: request}
	result, err := dispatcher.Solve(ctx, solve, func(update *arrangorpcv1.SolveUpdate) {
		updates.Add(1)
		lastPhase.Store(update.GetPhase())
		if update.GetPhase() == arrangorpcv1.SolvePhase_SOLVE_PHASE_SOLUTION {
			solutions.Add(1)
		}
	})
	if err != nil {
		t.Fatalf("solve: %v", err)
	}
	t.Logf("solve took %s, %d updates, %d solution frames, status %v",
		time.Since(started).Round(time.Millisecond),
		updates.Load(), solutions.Load(), result.Status)

	// What this layer owns: the stream was consumed to its terminal frame and
	// the deadline covered the node's whole budget.
	if updates.Load() == 0 {
		t.Error("the stream carried no updates at all")
	}
	if phase, _ := lastPhase.Load().(arrangorpcv1.SolvePhase); phase != arrangorpcv1.SolvePhase_SOLVE_PHASE_DONE {
		t.Errorf("last phase = %v, want DONE: the terminal frame was not read", phase)
	}
	if result.Status == arrangorpcv1.SolveStatus_SOLVE_STATUS_UNSPECIFIED {
		t.Error("no terminal status, so the DONE frame was never absorbed")
	}

	// The clamp is applied to the outgoing message, not merely checked.
	if request.GetConfig().GetNumWorkers() == 0 {
		t.Error("num_workers was still zero on the outgoing request")
	}
	if result.Job.NumWorkers == 0 {
		t.Error("the job recorded zero workers")
	}

	// A schedule is claimed only from a solution frame that carried a snapshot,
	// and the row must say the same thing the result does.
	if result.HasSchedule != (result.Snapshot != nil) {
		t.Errorf("HasSchedule = %v with snapshot %v", result.HasSchedule, result.Snapshot != nil)
	}
	if result.Job.ResultID != nil && !result.HasSchedule {
		t.Error("the job names a stored result for a solve that produced no snapshot")
	}

	// Done means stored, not merely solved: a schedule nobody can open is not
	// a job that worked.
	wantStatus := arrango.StatusFailed
	if result.Job.ResultID != nil {
		wantStatus = arrango.StatusDone
	}
	if result.Job.Status != wantStatus {
		t.Errorf("job status = %q, want %q for result %v",
			result.Job.Status, wantStatus, result.Job.ResultID)
	}
	// What the solve cost, from the node's final frame.
	t.Logf("cost: %d workers, peak %v bytes, %v cpu seconds",
		result.Job.NumWorkers, result.Job.PeakRSSBytes, result.Job.CPUSeconds)
	if result.Job.FinishedAt == nil {
		t.Error("a finished job has no finish time")
	}
	if !result.HasSchedule {
		// Not a failure of this client: a bounded search on a loaded host is
		// allowed to run out of time, and the layer's job is to report it.
		t.Logf("no schedule this run: status %v, %q", result.Status, result.Message)
		return
	}
	if result.Job.ResultID == nil {
		t.Fatalf("a snapshot arrived and nothing was stored: %q", result.Job.Detail)
	}

	// The whole point of solving: the schedule is a result somebody can open,
	// not a message that says one existed for a moment.
	saved, err := repository.Result(ctx, result.Job.PlanID, *result.Job.ResultID)
	if err != nil {
		t.Fatalf("the job names result %d and the index does not have it: %v",
			*result.Job.ResultID, err)
	}
	written, err := store.Load(ctx, saved.ObjectKey)
	if err != nil {
		t.Fatalf("load the result: %v", err)
	}
	if len(written.GetSchedule().GetLessons()) == 0 {
		t.Error("the result carries no schedule")
	}
	if len(written.GetModel().GetLessons()) != len(model.GetLessons()) {
		t.Errorf("the result has %d lessons and the school solved had %d",
			len(written.GetModel().GetLessons()), len(model.GetLessons()))
	}
	if written.GetNextArrangoId() == 0 {
		t.Error("the result lost its id counter")
	}
	t.Logf("solved schedule kept as result %d: %d placements",
		saved.ID, len(written.GetSchedule().GetLessons()))
}

// dispatcherDialer reaches the dialer the dispatcher holds, so the test uses
// the same identity the node just enrolled rather than a second one.
func dispatcherDialer(t *testing.T, dispatcher *arrango.Dispatcher) *solveclient.Dialer {
	t.Helper()
	dialer := dispatcher.Dialer()
	if dialer == nil {
		t.Fatal("the dispatcher has no dialer")
	}
	return dialer
}

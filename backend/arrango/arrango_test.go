// arrango/arrango_test.go

package arrango_test

import (
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	arrangoid "github.com/smegg99/arrango/backend/identity"
	"github.com/smegg99/s99ab/data/db"
	"github.com/smegg99/s99ab/data/storage"

	arrangorpcv1 "github.com/smegg99/arrango/backend/gen/arrangorpcv1"
	arrangov1 "github.com/smegg99/arrango/backend/gen/arrangov1"

	"github.com/smegg99/goptivum/backend/arrango"
	"github.com/smegg99/goptivum/backend/common/messages"
	"github.com/smegg99/goptivum/backend/core/documents"
	"github.com/smegg99/goptivum/backend/core/plans"
	"github.com/smegg99/goptivum/backend/migrations"
	"github.com/smegg99/goptivum/document"
	"github.com/smegg99/goptivum/solveclient"
)

// lessons builds a model whose first `placed` lessons carry a previous
// placement and the rest do not.
func lessons(total, placed int) *arrangov1.SchoolModel {
	model := &arrangov1.SchoolModel{}
	for index := range total {
		model.Lessons = append(model.Lessons, &arrangov1.LessonInstance{
			Id:          uint32(index + 1),
			HasPrevious: index < placed,
		})
	}
	return model
}

func newRegistry(t *testing.T) (*arrango.Registry, *arrango.Jobs, *plans.Repository, *documents.Store) {
	t.Helper()
	conn := db.TestDB(t, migrations.FS)
	return arrango.NewRegistry(conn, db.SQLite),
		arrango.NewJobs(conn, db.SQLite),
		plans.NewRepository(conn, db.SQLite, plans.Caps{BackupsKeep: 100, ResultsKeep: 20}),
		documents.NewStore(storage.NewLocalDir(t.TempDir(), "/objects"))
}

// paired builds a Paired with a real fingerprint, since the registry stores and
// reparses it.
func paired(t *testing.T, address string) solveclient.Paired {
	t.Helper()
	self, err := arrangoid.Create(t.TempDir())
	if err != nil {
		t.Fatalf("create identity: %v", err)
	}
	return solveclient.Paired{Address: address, Fingerprint: self.Fingerprint}
}

func TestRegistryRecordsAndRevokes(t *testing.T) {
	registry, _, _, _ := newRegistry(t)
	ctx := t.Context()

	node, err := registry.Record(ctx, "school-node", paired(t, "127.0.0.1:19443"))
	if err != nil {
		t.Fatalf("record: %v", err)
	}
	if node.Revoked() {
		t.Error("a freshly paired node is revoked")
	}
	if _, err := arrangoid.ParseFingerprint(node.Fingerprint); err != nil {
		t.Errorf("the stored fingerprint does not parse back: %v", err)
	}

	found, err := registry.ByFingerprint(ctx, node.Fingerprint)
	if err != nil || found.ID != node.ID {
		t.Fatalf("ByFingerprint = %+v, %v", found, err)
	}

	if err := registry.Revoke(ctx, node.ID); err != nil {
		t.Fatalf("revoke: %v", err)
	}
	after, err := registry.Get(ctx, node.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if !after.Revoked() {
		t.Error("the node is not revoked after being revoked")
	}
	// Revoking twice is the same end state, not an error.
	if err := registry.Revoke(ctx, node.ID); err != nil {
		t.Errorf("revoking twice: %v", err)
	}
	if err := registry.Revoke(ctx, 999); !errors.Is(err, arrango.ErrNoNode) {
		t.Errorf("revoking a node that does not exist = %v", err)
	}
}

// The fingerprint is the identity, so pairing the same node again updates the
// row rather than opening a second one racing over the same peer.
func TestPairingTheSameNodeAgainReplacesTheRow(t *testing.T) {
	registry, _, _, _ := newRegistry(t)
	ctx := t.Context()

	first := paired(t, "127.0.0.1:19443")
	node, err := registry.Record(ctx, "old-label", first)
	if err != nil {
		t.Fatalf("record: %v", err)
	}
	if err := registry.Revoke(ctx, node.ID); err != nil {
		t.Fatalf("revoke: %v", err)
	}

	moved := solveclient.Paired{Address: "10.0.0.5:9443", Fingerprint: first.Fingerprint}
	again, err := registry.Record(ctx, "new-label", moved)
	if err != nil {
		t.Fatalf("record again: %v", err)
	}
	if again.ID != node.ID {
		t.Errorf("a second row was created: %d then %d", node.ID, again.ID)
	}
	if again.Revoked() {
		t.Error("re-pairing left the node revoked")
	}
	if again.Address != "10.0.0.5:9443" || again.Label != "new-label" {
		t.Errorf("node = %+v, want the new address and label", again)
	}

	all, err := registry.List(ctx)
	if err != nil || len(all) != 1 {
		t.Fatalf("List = %d nodes, %v, want 1", len(all), err)
	}
}

func newPlan(t *testing.T, repository *plans.Repository, store *documents.Store) plans.Plan {
	t.Helper()
	// With a stored version: a solved schedule is written as the next version
	// of the document it was solved from, so there has to be one.
	encoded, err := document.Encode(document.New(lessons(2, 2)))
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	key, err := store.Put(t.Context(), encoded)
	if err != nil {
		t.Fatalf("put: %v", err)
	}
	plan, _, err := repository.Create(t.Context(), plans.Plan{
		OwnerIdentityID: "owner", Name: "wrzesien", SchoolYear: "2026/2027",
	}, plans.Upload{ObjectKey: key, SizeBytes: encoded.Size, CreatedBy: "owner"})
	if err != nil {
		t.Fatalf("create plan: %v", err)
	}
	return plan
}

func TestJobsLifecycle(t *testing.T) {
	registry, jobs, repository, store := newRegistry(t)
	ctx := t.Context()
	node, err := registry.Record(ctx, "node", paired(t, "127.0.0.1:19443"))
	if err != nil {
		t.Fatalf("record: %v", err)
	}
	plan := newPlan(t, repository, store)

	job, err := jobs.Create(ctx, arrango.NewJob{PlanID: plan.ID, Version: 1, NodeID: node.ID, Workers: 6, StartedBy: owner})
	if err != nil {
		t.Fatalf("create job: %v", err)
	}
	active, err := jobs.CountActive(ctx, node.ID)
	if err != nil || active != 1 {
		t.Fatalf("CountActive = %d, %v, want 1: a pending job holds capacity", active, err)
	}

	if err := jobs.Start(ctx, job.ID); err != nil {
		t.Fatalf("start: %v", err)
	}
	if err := jobs.Finish(ctx, job.ID, arrango.Outcome{Status: "nonsense"}); err == nil {
		t.Error("a status that is not terminal was accepted")
	}
	stored := int64(4242)
	quality := 87.5
	if err := jobs.Finish(ctx, job.ID, arrango.Outcome{Status: arrango.StatusDone, Detail: "solved",
		ResultID: &stored, Quality: &quality, Verdict: "TIER_PRISTINE", Workers: 5,
		PeakRSSBytes: 2 << 30, CPUSeconds: 12.5}); err != nil {
		t.Fatalf("finish: %v", err)
	}

	after, err := jobs.Get(ctx, job.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if after.Status != arrango.StatusDone || after.Detail != "solved" || after.Reason != "" ||
		after.StartedBy != owner || after.NodeLabel != "node" {
		t.Errorf("job = %+v", after)
	}
	// The schedule is a result, so what the job records is where to find it
	// rather than a flag saying it exists somewhere.
	if after.ResultID == nil || *after.ResultID != stored {
		t.Errorf("result_id = %v, want %d", after.ResultID, stored)
	}
	// What the node ran it with and what it cost.
	if after.NumWorkers != 5 || after.PeakRSSBytes == nil || *after.PeakRSSBytes != 2<<30 ||
		after.CPUSeconds == nil || *after.CPUSeconds != 12.5 {
		t.Errorf("workers %d, peak %v, cpu %v", after.NumWorkers, after.PeakRSSBytes, after.CPUSeconds)
	}
	// And what the node said about it, which is what a school reads.
	if after.Quality == nil || *after.Quality != quality {
		t.Errorf("quality = %v, want %v", after.Quality, quality)
	}
	if after.Verdict != "TIER_PRISTINE" {
		t.Errorf("verdict = %q", after.Verdict)
	}
	if after.StartedAt == nil || after.FinishedAt == nil {
		t.Error("a finished job has no timestamps")
	}

	active, err = jobs.CountActive(ctx, node.ID)
	if err != nil || active != 0 {
		t.Fatalf("CountActive = %d, %v, want 0 once the job is terminal", active, err)
	}
	mine, err := jobs.ForPlan(ctx, plan.ID)
	if err != nil || len(mine) != 1 {
		t.Fatalf("ForPlan = %d jobs, %v", len(mine), err)
	}
}

// Finish that closes nothing is an error: a row it did not update is a job
// this server still counts against its node.
func TestFinishOfAJobThatIsNotThereIsAnError(t *testing.T) {
	_, jobs, _, _ := newRegistry(t)
	err := jobs.Finish(t.Context(), 424242, arrango.Outcome{Status: arrango.StatusDone})
	if !errors.Is(err, arrango.ErrNoJob) {
		t.Errorf("finish of an unknown job = %v, want ErrNoJob", err)
	}
}

// A process that dies never runs Finish, and the rows it leaves open count
// against their node's concurrency for ever. The next boot closes them.
func TestCloseOrphansFreesCapacityLeftByACrash(t *testing.T) {
	registry, jobs, repository, store := newRegistry(t)
	ctx := t.Context()
	node, err := registry.Record(ctx, "node", paired(t, "127.0.0.1:19443"))
	if err != nil {
		t.Fatalf("record: %v", err)
	}
	plan := newPlan(t, repository, store)

	open := arrango.NewJob{PlanID: plan.ID, Version: 1, NodeID: node.ID, Workers: 4, StartedBy: owner}
	pending, err := jobs.Create(ctx, open)
	if err != nil {
		t.Fatalf("create pending: %v", err)
	}
	running, err := jobs.Create(ctx, open)
	if err != nil {
		t.Fatalf("create running: %v", err)
	}
	if err := jobs.Start(ctx, running.ID); err != nil {
		t.Fatalf("start: %v", err)
	}
	finished, err := jobs.Create(ctx, open)
	if err != nil {
		t.Fatalf("create finished: %v", err)
	}
	resultID := int64(4242)
	if err := jobs.Finish(ctx, finished.ID, arrango.Outcome{Status: arrango.StatusDone, Detail: "solved", ResultID: &resultID}); err != nil {
		t.Fatalf("finish: %v", err)
	}

	closed, err := jobs.CloseOrphans(ctx)
	if err != nil {
		t.Fatalf("close orphans: %v", err)
	}
	if closed != 2 {
		t.Errorf("closed %d jobs, want the pending one and the running one", closed)
	}
	for _, id := range []int64{pending.ID, running.ID} {
		job, err := jobs.Get(ctx, id)
		if err != nil {
			t.Fatalf("get: %v", err)
		}
		if job.Status != arrango.StatusFailed || job.FinishedAt == nil ||
			job.Reason != messages.KeySolveServerRestarted {
			t.Errorf("job %d = %+v, want failed and closed, saying the server restarted", id, job)
		}
	}
	// The one that was already terminal is untouched, schedule and all.
	done, err := jobs.Get(ctx, finished.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if done.Status != arrango.StatusDone || done.ResultID == nil || done.Detail != "solved" {
		t.Errorf("a terminal job was rewritten: %+v", done)
	}

	active, err := jobs.CountActive(ctx, node.ID)
	if err != nil || active != 0 {
		t.Errorf("CountActive = %d, %v, want 0: the node is free again", active, err)
	}
	// A second boot has nothing left to do.
	again, err := jobs.CloseOrphans(ctx)
	if err != nil || again != 0 {
		t.Errorf("CloseOrphans again = %d, %v, want 0", again, err)
	}
}

func newDispatcher(t *testing.T, registry *arrango.Registry, jobs *arrango.Jobs) *arrango.Dispatcher {
	t.Helper()
	return newDispatcherWith(t, registry, jobs, nil)
}

// newTestDialer is this server's dialer under a fresh identity.
func newTestDialer(t *testing.T) *solveclient.Dialer {
	t.Helper()
	self, err := solveclient.LoadIdentity(t.TempDir())
	if err != nil {
		t.Fatalf("identity: %v", err)
	}
	dialer, err := solveclient.NewDialer(self, "0.1.0")
	if err != nil {
		t.Fatalf("dialer: %v", err)
	}
	return dialer
}

// newDispatcherWith keeps timetables through persist.
func newDispatcherWith(t *testing.T, registry *arrango.Registry, jobs *arrango.Jobs,
	persist arrango.Persist) *arrango.Dispatcher {
	t.Helper()
	dispatcher, err := arrango.NewDispatcher(newTestDialer(t), registry, jobs, persist, nil)
	if err != nil {
		t.Fatalf("dispatcher: %v", err)
	}
	return dispatcher
}

// A revoked node is refused by admission, which runs before anything is
// dialled. The address points at a port nothing listens on, so a dial attempt
// would show up as a different error.
func TestAdmissionRefusesARevokedNodeBeforeDialling(t *testing.T) {
	registry, jobs, _, _ := newRegistry(t)
	ctx := t.Context()
	node, err := registry.Record(ctx, "node", paired(t, "127.0.0.1:1"))
	if err != nil {
		t.Fatalf("record: %v", err)
	}
	if err := registry.Revoke(ctx, node.ID); err != nil {
		t.Fatalf("revoke: %v", err)
	}

	dispatcher := newDispatcher(t, registry, jobs)
	_, err = dispatcher.Admit(ctx, node.ID, lessons(1, 1), &arrangorpcv1.SolverConfig{})
	if !errors.Is(err, arrango.ErrRevoked) {
		t.Errorf("err = %v, want ErrRevoked", err)
	}

	if _, err := dispatcher.Admit(ctx, 999, lessons(1, 1), &arrangorpcv1.SolverConfig{}); !errors.Is(err, arrango.ErrNoNode) {
		t.Errorf("admitting an unknown node = %v, want ErrNoNode", err)
	}
}

func TestAdmissionRefusesAtTheConcurrencyCeiling(t *testing.T) {
	registry, jobs, repository, store := newRegistry(t)
	ctx := t.Context()
	node, err := registry.Record(ctx, "node", paired(t, "127.0.0.1:1"))
	if err != nil {
		t.Fatalf("record: %v", err)
	}
	plan := newPlan(t, repository, store)
	for range arrango.DefaultConcurrentSolves {
		if _, err := jobs.Create(ctx, arrango.NewJob{PlanID: plan.ID, Version: 1, NodeID: node.ID, Workers: 4, StartedBy: owner}); err != nil {
			t.Fatalf("create job: %v", err)
		}
	}

	dispatcher := newDispatcher(t, registry, jobs)
	_, err = dispatcher.Admit(ctx, node.ID, lessons(1, 1), &arrangorpcv1.SolverConfig{})
	if !errors.Is(err, arrango.ErrNoCapacity) {
		t.Errorf("err = %v, want ErrNoCapacity", err)
	}
}

// Admission answers with the clamped worker count, so a caller-supplied zero
// cannot survive as far as the outgoing config.
func TestAdmissionClampsTheWorkerCount(t *testing.T) {
	fake := startFakeNode(t, &fakeNode{})
	registry, jobs, _, _ := newRegistry(t)
	ctx := t.Context()
	node, err := registry.Record(ctx, "node", fake.paired())
	if err != nil {
		t.Fatalf("record: %v", err)
	}

	dispatcher := newDispatcher(t, registry, jobs)
	admission, err := dispatcher.Admit(ctx, node.ID, lessons(1, 1),
		&arrangorpcv1.SolverConfig{NumWorkers: 0, IgnorePreviousPlacements: true})
	if err != nil {
		t.Fatalf("admit: %v", err)
	}
	if admission.Workers == 0 {
		t.Fatal("admission allowed zero workers, which means all cores")
	}
	if admission.Workers != solveclient.DefaultWorkers {
		t.Errorf("workers = %d, want the conservative default %d",
			admission.Workers, solveclient.DefaultWorkers)
	}
}

// A node that cannot answer is refused at admission, in seconds, and the
// refusal says so: accepting it only moved the same failure into the
// background, where the planner learned of it minutes later.
func TestAdmissionRefusesANodeThatCannotAnswer(t *testing.T) {
	registry, jobs, repository, store := newRegistry(t)
	dispatcher := newDispatcher(t, registry, jobs)
	ctx := t.Context()

	// Port 1 on loopback refuses at once, so the status call fails fast.
	node, err := registry.Record(ctx, "silent", paired(t, "127.0.0.1:1"))
	if err != nil {
		t.Fatalf("record: %v", err)
	}
	_ = newPlan(t, repository, store)

	_, err = dispatcher.Admit(ctx, node.ID, lessons(2, 2),
		&arrangorpcv1.SolverConfig{IgnorePreviousPlacements: true})
	if !errors.Is(err, solveclient.ErrUnreachable) {
		t.Fatalf("err = %v, want ErrUnreachable", err)
	}
}

// A cancelled solve is recorded as cancelled and keeps what it found.
//
// This needs a node that really streams. Cancelling during the dial produces a
// plain context.Canceled and proves nothing: the bug is that a cancelled gRPC
// STREAM returns a *status.Error with code Canceled, which does not wrap
// context.Canceled, so errors.Is answers false and every cancel was recorded
// as "failed" with a raw gRPC sentence for a detail.
//
// And the schedules already received were thrown away with it, which made
// "stop, this one is good enough" the action that loses the work.
func TestCancellingASolveKeepsWhatItFound(t *testing.T) {
	node := startFakeNode(t, &fakeNode{
		frames: []*arrangorpcv1.SolveUpdate{
			solutionFrame(1, 100), solutionFrame(1, 80), solutionFrame(1, 60),
		},
		hold: true,
	})

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
	// Cancel only once the schedules have actually arrived, or this proves
	// nothing about keeping them.
	waitFor(t, "the solve to receive its frames", func() bool {
		row, err := jobs.Get(ctx, job.ID)
		return err == nil && row.Status == arrango.StatusRunning
	})
	time.Sleep(200 * time.Millisecond)

	if !runner.Cancel(job.ID) {
		t.Fatal("Cancel reported nothing was running")
	}
	waitForTerminal(t, jobs, job.ID)

	closed, err := jobs.Get(ctx, job.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if closed.Status != arrango.StatusCancelled {
		t.Errorf("status = %q, want cancelled: a person pressed stop", closed.Status)
	}
	// The schedules it already found are kept. The node re-attaches the last
	// one to its own cancelled frame for the same reason.
	if closed.ResultID == nil {
		t.Error("cancelling threw away the schedule the solve had already found")
	}
	active, err := jobs.CountActive(ctx, recorded.ID)
	if err != nil || active != 0 {
		t.Errorf("CountActive = %d, %v, want 0", active, err)
	}
}

// Capacity negotiation asks a node that really answers, and clamps on what it
// says rather than on configuration.
func TestCapacityIsReadFromANodeThatAnswers(t *testing.T) {
	node := startFakeNode(t, &fakeNode{
		// 5 GB free: one worker needs 900 MB on top of a 1 GB base, so four fit.
		status: &arrangorpcv1.NodeStatusResponse{
			Cores: 24, AvailableBytes: 5 << 30, CommittedBytes: 0, RunningSolves: 1,
			Policy: &arrangorpcv1.PeerPolicy{MaxSolveSeconds: 86400},
		},
	})
	registry, jobs, repository, store := newRegistry(t)
	dispatcher := newDispatcher(t, registry, jobs)
	ctx := t.Context()

	recorded, err := registry.Record(ctx, "answering", node.paired())
	if err != nil {
		t.Fatalf("record: %v", err)
	}
	_ = newPlan(t, repository, store)

	admission, err := dispatcher.Admit(ctx, recorded.ID, lessons(2, 2),
		&arrangorpcv1.SolverConfig{NumWorkers: 24, IgnorePreviousPlacements: true})
	if err != nil {
		t.Fatalf("admit: %v", err)
	}
	if !admission.Capacity.Known {
		t.Fatal("the node answered and the capacity was not recorded")
	}
	if admission.Capacity.AffordableWorkers != 4 {
		t.Errorf("AffordableWorkers = %d, want 4 from 5 GB free",
			admission.Capacity.AffordableWorkers)
	}
	// 24 were asked for and the node affords 4.
	if admission.Workers != 4 {
		t.Errorf("Workers = %d, want 4: the node's answer beats the request",
			admission.Workers)
	}
	if admission.Capacity.RunningSolves != 1 || admission.Capacity.Cores != 24 {
		t.Errorf("capacity = %+v", admission.Capacity)
	}
}

// This server never asks a node for more concurrent solves than a node gives a
// newly paired peer.
//
// The node's own config.DefaultPolicy() is MaxConcurrentSolves: 1. Asking for a
// second does not fail fast: the node queues it, waits for a slot, then
// re-checks the caller's deadline after the wait, so the job sits silent for
// the length of the first solve and then fails on a deadline it cannot meet.
func TestTheConcurrencyDefaultMatchesWhatANodeGivesANewPeer(t *testing.T) {
	if arrango.DefaultConcurrentSolves != 1 {
		t.Errorf("DefaultConcurrentSolves = %d, want 1, which is the node's own default",
			arrango.DefaultConcurrentSolves)
	}
}

// A solve the node proved infeasible says which inputs cannot all hold.
//
// This is the difference between a school knowing what to change and a school
// retrying the same file. The node spends up to thirty extra seconds working
// it out and this server used to throw the answer away and report "no feasible
// solution within time limit".
func TestAnInfeasibleSolveExplainsItself(t *testing.T) {
	node := startFakeNode(t, &fakeNode{
		frames: []*arrangorpcv1.SolveUpdate{{
			Phase:  arrangorpcv1.SolvePhase_SOLVE_PHASE_DONE,
			Status: arrangorpcv1.SolveStatus_SOLVE_STATUS_INFEASIBLE,
			InfeasibleCore: &arrangorpcv1.InfeasibleCore{
				Minimal: true,
				Message: "teacher Kowalski's availability and 4 lessons cannot all hold",
				Items: []*arrangorpcv1.InfeasibleCoreItem{{
					Kind: "hard_rule", Rule: "teacher_availability",
					EntityId: 7, EntityName: "Kowalski",
					LessonIds: []uint32{11, 12, 13, 14},
					Message:   "teacher Kowalski's availability",
				}},
			},
		}},
	})

	runner, registry, jobs, repository, store := newRunner(t)
	ctx := t.Context()
	recorded, err := registry.Record(ctx, "explaining", node.paired())
	if err != nil {
		t.Fatalf("record: %v", err)
	}
	plan := newPlan(t, repository, store)

	job, err := runner.Start(ctx, order(plan.ID, recorded.ID, lessons(2, 2)))
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	waitForTerminal(t, jobs, job.ID)

	closed, err := jobs.Get(ctx, job.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if closed.Explanation == "" {
		t.Fatal("an infeasible solve recorded no explanation, so the school learns nothing")
	}
	if !strings.Contains(closed.Explanation, "Kowalski") {
		t.Errorf("explanation = %q, want it to name the input that clashes", closed.Explanation)
	}
	// No schedule, and nothing pretending there is one.
	if closed.ResultID != nil {
		t.Error("an infeasible solve kept a result")
	}
	if closed.Reason != messages.KeySolveInfeasible {
		t.Errorf("reason = %q, want %s", closed.Reason, messages.KeySolveInfeasible)
	}
}

// What the node says about a schedule travels with it: the rating a person
// reads and the tier that says whether it is clean.
func TestASolvedScheduleCarriesItsQualityAndVerdict(t *testing.T) {
	node := startFakeNode(t, &fakeNode{
		frames: []*arrangorpcv1.SolveUpdate{{
			Phase:     arrangorpcv1.SolvePhase_SOLVE_PHASE_DONE,
			Status:    arrangorpcv1.SolveStatus_SOLVE_STATUS_FEASIBLE,
			Objective: 4200,
			Snapshot: &arrangov1.ScheduleSnapshot{
				Lessons: []*arrangov1.ScheduledLesson{{
					LessonId:  1,
					Placement: &arrangov1.Placement{DayId: 1, StartPeriod: 1},
				}},
			},
			Score:   &arrangov1.ScoreReport{OverallQuality: 82.5, TotalPenalty: 4200},
			Verdict: &arrangorpcv1.ScheduleVerdict{Tier: arrangorpcv1.ScheduleVerdict_TIER_WARNINGS},
		}},
	})

	runner, registry, jobs, repository, store := newRunner(t)
	ctx := t.Context()
	recorded, err := registry.Record(ctx, "scoring", node.paired())
	if err != nil {
		t.Fatalf("record: %v", err)
	}
	plan := newPlan(t, repository, store)

	job, err := runner.Start(ctx, order(plan.ID, recorded.ID, lessons(2, 2)))
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	waitForTerminal(t, jobs, job.ID)

	closed, err := jobs.Get(ctx, job.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if closed.Quality == nil || *closed.Quality != 82.5 {
		t.Errorf("quality = %v, want 82.5 from the node's own report", closed.Quality)
	}
	if !strings.Contains(closed.Verdict, "WARNINGS") {
		t.Errorf("verdict = %q, want the node's tier", closed.Verdict)
	}
	if closed.ResultID == nil {
		t.Error("the schedule was not stored")
	}
}

// A baseline that does not hold is refused in a second, not after a full search
// budget spent improving a broken timetable.
//
// The node checks the schedule the solve would start from before anything is
// dispatched, and the refusal names the kinds of problem it found. A planner
// waiting ten minutes to be told "no feasible solution" learns nothing; this
// tells them at once, and what to look at.
func TestABaselineThatDoesNotHoldIsRefusedBeforeItIsDispatched(t *testing.T) {
	node := startFakeNode(t, &fakeNode{
		validation: &arrangov1.ValidationReport{
			Valid: false,
			Conflicts: []*arrangov1.Conflict{
				{Kind: arrangov1.Conflict_KIND_TEACHER_OVERLOADED, EntityId: 7},
				{Kind: arrangov1.Conflict_KIND_LOCKED_COLLISION, LessonIds: []uint32{3, 4}},
				// Twice, to prove the kinds are reported once each.
				{Kind: arrangov1.Conflict_KIND_TEACHER_OVERLOADED, EntityId: 9},
			},
		},
		// Frames it would have sent, so a dispatch that wrongly proceeds is
		// visible as a success rather than as a timeout.
		frames: []*arrangorpcv1.SolveUpdate{solutionFrame(1, 10)},
	})

	registry, jobs, repository, store := newRegistry(t)
	dispatcher := newDispatcher(t, registry, jobs)
	ctx := t.Context()

	recorded, err := registry.Record(ctx, "checking", node.paired())
	if err != nil {
		t.Fatalf("record: %v", err)
	}
	_ = newPlan(t, repository, store)

	// Improving an existing timetable, so there is a schedule to check.
	_, err = dispatcher.Admit(ctx, recorded.ID, lessons(2, 2),
		&arrangorpcv1.SolverConfig{IgnorePreviousPlacements: false})

	var invalid *solveclient.ErrModelInvalid
	if !errors.As(err, &invalid) {
		t.Fatalf("err = %v, want ErrModelInvalid", err)
	}
	if len(invalid.Conflicts) != 3 {
		t.Errorf("conflicts = %d, want all three", len(invalid.Conflicts))
	}
	// Kinds, deduplicated: what a client localizes from. The node's own
	// Conflict.message is documented as debug only.
	kinds := invalid.Kinds()
	if len(kinds) != 2 {
		t.Errorf("kinds = %v, want each kind once", kinds)
	}
	if !slices.Contains(kinds, "KIND_TEACHER_OVERLOADED") ||
		!slices.Contains(kinds, "KIND_LOCKED_COLLISION") {
		t.Errorf("kinds = %v, want both problems named", kinds)
	}
	// And it is a message a person reads, in their own language.
	if said := messages.Say(messages.Localizer("pl"), err); !strings.Contains(said, "nie da się ułożyć") {
		t.Errorf("pl = %q", said)
	}
	// The check ran against the baseline, not against nothing.
	calls := node.validateCalls()
	if len(calls) != 1 {
		t.Fatalf("Validate calls = %d, want exactly one", len(calls))
	}
	if got := len(calls[0].GetSnapshot().GetLessons()); got != 2 {
		t.Errorf("snapshot lessons = %d, want both placed lessons", got)
	}
}

// A solve from scratch is never checked against an empty timetable.
//
// Validate reports on a SCHEDULE. Asked with no snapshot it finds every lesson
// missing and every day breaking its load rule, which refused every solve of a
// plan that had never been placed: every imported plan. The inputs themselves
// are the node's own RunPreflight, inside Solve.
func TestAFromScratchSolveIsNotCheckedAgainstAnEmptyTimetable(t *testing.T) {
	node := startFakeNode(t, &fakeNode{
		validation: &arrangov1.ValidationReport{
			Valid: false,
			Conflicts: []*arrangov1.Conflict{
				{Kind: arrangov1.Conflict_KIND_MISSING_LESSON, LessonIds: []uint32{1}},
				{Kind: arrangov1.Conflict_KIND_MISSING_LESSON, LessonIds: []uint32{2}},
				{Kind: arrangov1.Conflict_KIND_DAILY_LOAD_VIOLATION, EntityId: 1},
			},
		},
	})

	registry, jobs, repository, store := newRegistry(t)
	dispatcher := newDispatcher(t, registry, jobs)
	ctx := t.Context()

	recorded, err := registry.Record(ctx, "scratch", node.paired())
	if err != nil {
		t.Fatalf("record: %v", err)
	}
	_ = newPlan(t, repository, store)

	// Nothing is placed, which is what an imported plan looks like.
	admission, err := dispatcher.Admit(ctx, recorded.ID, lessons(2, 0),
		&arrangorpcv1.SolverConfig{IgnorePreviousPlacements: true})
	if err != nil {
		t.Fatalf("a plan that has never been placed was refused: %v", err)
	}
	if admission.Workers == 0 {
		t.Error("admitted with no workers")
	}
	if calls := node.validateCalls(); len(calls) != 0 {
		t.Errorf("Validate calls = %d, want none when there is no schedule to check",
			len(calls))
	}
}

// BaselineSnapshot carries the previous placements, and only when the solve is
// actually improving on them.
// A node that answers its status but cannot run the check is not a refusal:
// a model that might be fine must not be blocked by a transient.
func TestAModelCheckThatCannotRunDoesNotBlockTheSolve(t *testing.T) {
	node := startFakeNode(t, &fakeNode{})
	registry, jobs, repository, store := newRegistry(t)
	dispatcher := newDispatcher(t, registry, jobs)
	ctx := t.Context()

	recorded, err := registry.Record(ctx, "checking", node.paired())
	if err != nil {
		t.Fatalf("record: %v", err)
	}
	_ = newPlan(t, repository, store)
	node.validateErr = errors.New("the node is out of memory for a check")

	// Improving the placed timetable, so there is a schedule to check.
	admission, err := dispatcher.Admit(ctx, recorded.ID, lessons(2, 2),
		&arrangorpcv1.SolverConfig{IgnorePreviousPlacements: false})
	if err != nil {
		t.Fatalf("admit: %v, want the solve to proceed", err)
	}
	if admission.Workers == 0 {
		t.Error("admission passed zero workers")
	}
	if calls := node.validateCalls(); len(calls) != 1 {
		t.Errorf("Validate calls = %d, want the one that failed", len(calls))
	}
}

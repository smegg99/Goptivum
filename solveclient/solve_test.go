// solveclient/solve_test.go

package solveclient_test

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	arrangorpcv1 "github.com/smegg99/arrango/backend/gen/arrangorpcv1"
	arrangov1 "github.com/smegg99/arrango/backend/gen/arrangov1"

	"github.com/smegg99/goptivum/solveclient"
)

// Zero means all cores to the solver, which is the documented path into the
// OOM killer. It must never reach the wire, whatever a caller sends.
func TestClampWorkersNeverReturnsZero(t *testing.T) {
	for _, one := range []struct {
		name               string
		requested, ceiling uint32
		want               uint32
	}{
		{"zero becomes the conservative default", 0, 0, solveclient.DefaultWorkers},
		{"zero with a ceiling still becomes the default", 0, 8, solveclient.DefaultWorkers},
		{"a sane request passes through", 6, 12, 6},
		{"a request above the ceiling is cut to it", 40, 12, 12},
		{"an absurd request is cut to the sanity ceiling", 100000, 0, solveclient.MaxWorkers},
		{"an absurd ceiling is not honoured", 100000, 99999, solveclient.MaxWorkers},
		{"one worker is a real answer", 1, 12, 1},
	} {
		t.Run(one.name, func(t *testing.T) {
			got := solveclient.ClampWorkers(one.requested, one.ceiling)
			if got != one.want {
				t.Errorf("ClampWorkers(%d, %d) = %d, want %d",
					one.requested, one.ceiling, got, one.want)
			}
			if got == 0 {
				t.Fatal("returned zero, which means all cores")
			}
		})
	}
}

// The node refuses a deadline that does not cover the budget it computes, and
// a deadline under the search kills a legitimate solve through its own
// transport, which from here is indistinguishable from a cancel.
func TestDeadlineCoversTheNodesWholeBudget(t *testing.T) {
	// The node's own formula, written out independently: search, explainer,
	// and 60s of validate, score and rescore after the search.
	nodeBudget := func(search, explain float64) time.Duration {
		if search <= 0 {
			search = 180
		}
		if explain <= 0 {
			explain = min(max(0.25*search, 5), 30)
		}
		return time.Duration((search + explain + 60) * float64(time.Second))
	}

	for _, one := range []struct{ search, explain float64 }{
		{0, 0}, {1, 0}, {20, 0}, {60, 0}, {180, 0}, {3600, 0}, {20, 45}, {600, 1},
	} {
		config := &arrangorpcv1.SolverConfig{
			TotalTimeSeconds: one.search, ExplainBudgetSeconds: one.explain,
		}
		deadline := solveclient.Deadline(config)
		want := nodeBudget(one.search, one.explain)

		if solveclient.NodeBudget(config) != want {
			t.Errorf("search %.0fs explain %.0fs: NodeBudget = %s, the node computes %s",
				one.search, one.explain, solveclient.NodeBudget(config), want)
		}
		if deadline <= want {
			t.Errorf("search %.0fs explain %.0fs: deadline %s does not cover the node's %s budget",
				one.search, one.explain, deadline, want)
		}
	}
}

func request() *arrangorpcv1.SolveRequest {
	return &arrangorpcv1.SolveRequest{
		Model:  &arrangov1.SchoolModel{Lessons: []*arrangov1.LessonInstance{{Id: 1, Duration: 1}}},
		Config: &arrangorpcv1.SolverConfig{TotalTimeSeconds: 5, NumWorkers: 2},
	}
}

// A whole solve over the wire: every frame reaches the observer, the result
// keeps the best timetable and the final frame's status and cost.
func TestASolveKeepsTheBestTimetableAndTheFinalFrame(t *testing.T) {
	node := startFakeNode(t, &fakeNode{frames: []*arrangorpcv1.SolveUpdate{
		solutionFrame(1, 100), solutionFrame(2, 60), solutionFrame(3, 80),
		doneFrame(arrangorpcv1.SolveStatus_SOLVE_STATUS_FEASIBLE),
	}})
	dialer, _ := newDialerAs(t, "0.1.0")
	conn := node.reach(t, dialer)

	solving, err := conn.Solve(t.Context(), request())
	if err != nil {
		t.Fatalf("solve: %v", err)
	}
	var seen atomic.Int32
	result, err := solving.Wait(func(*arrangorpcv1.SolveUpdate) { seen.Add(1) })
	if err != nil {
		t.Fatalf("wait: %v", err)
	}
	if seen.Load() != 4 {
		t.Errorf("the observer saw %d frames, want 4", seen.Load())
	}
	if !result.HasSchedule || result.Objective != 60 || result.Snapshot.GetLessons()[0].GetLessonId() != 2 {
		t.Errorf("kept objective %d lesson %v, want the best, 60 on lesson 2", result.Objective, result.Snapshot.GetLessons())
	}
	if result.Status != arrangorpcv1.SolveStatus_SOLVE_STATUS_FEASIBLE {
		t.Errorf("status = %v, want the final frame's", result.Status)
	}
	if result.Config.GetNumWorkers() != 3 || result.PeakRSSBytes != 1536<<20 || result.CPUSeconds != 42.5 {
		t.Errorf("cost = %d workers, %d bytes, %v s, want the final frame's", result.Config.GetNumWorkers(), result.PeakRSSBytes, result.CPUSeconds)
	}
	if sent := node.solveRequests(); len(sent) != 1 || sent[0].GetConfig().GetNumWorkers() != 2 {
		t.Errorf("the node was sent %v", sent)
	}
}

// A stream the node breaks returns what arrived before it broke, with the
// error: a restart must not throw away the timetables already received.
func TestABrokenStreamKeepsWhatArrived(t *testing.T) {
	node := startFakeNode(t, &fakeNode{
		frames:   []*arrangorpcv1.SolveUpdate{solutionFrame(1, 90), solutionFrame(1, 70)},
		solveErr: status.Error(codes.Unavailable, "node restarting"),
	})
	dialer, _ := newDialerAs(t, "0.1.0")
	conn := node.reach(t, dialer)

	solving, err := conn.Solve(t.Context(), request())
	if err != nil {
		t.Fatalf("solve: %v", err)
	}
	result, err := solving.Wait(nil)
	if err == nil {
		t.Fatal("a broken stream read as a clean end")
	}
	if solveclient.Cancelled(err) {
		t.Errorf("a node restart read as a cancel: %v", err)
	}
	if !result.HasSchedule || result.Objective != 70 {
		t.Errorf("kept %v at %d, want the best received, 70", result.HasSchedule, result.Objective)
	}
}

// Stopping a solve is a cancel, never a failure, and still returns what the
// stream carried.
func TestStoppingASolveIsACancelThatKeepsWhatArrived(t *testing.T) {
	node := startFakeNode(t, &fakeNode{
		frames: []*arrangorpcv1.SolveUpdate{solutionFrame(1, 50)},
		hold:   true,
	})
	dialer, _ := newDialerAs(t, "0.1.0")
	conn := node.reach(t, dialer)

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	solving, err := conn.Solve(ctx, request())
	if err != nil {
		t.Fatalf("solve: %v", err)
	}
	first := make(chan struct{}, 1)
	done := make(chan struct{})
	var result solveclient.Result
	go func() {
		defer close(done)
		result, err = solving.Wait(func(*arrangorpcv1.SolveUpdate) {
			select {
			case first <- struct{}{}:
			default:
			}
		})
	}()
	select {
	case <-first:
	case <-time.After(10 * time.Second):
		t.Fatal("no frame arrived")
	}
	cancel()
	<-done

	if !solveclient.Cancelled(err) {
		t.Fatalf("err = %v, want a cancel", err)
	}
	if !result.HasSchedule || result.Objective != 50 {
		t.Errorf("kept %v at %d, want the timetable that arrived", result.HasSchedule, result.Objective)
	}
}

// A clean end without a solution frame is no timetable, whatever the frames
// claimed: the outcome is in the final frame, in band.
func TestACleanEndWithoutATimetableIsNoSchedule(t *testing.T) {
	node := startFakeNode(t, &fakeNode{frames: []*arrangorpcv1.SolveUpdate{
		{Phase: arrangorpcv1.SolvePhase_SOLVE_PHASE_PROGRESS, Status: arrangorpcv1.SolveStatus_SOLVE_STATUS_FEASIBLE, SolutionsFound: 3},
		{
			Phase: arrangorpcv1.SolvePhase_SOLVE_PHASE_DONE, Status: arrangorpcv1.SolveStatus_SOLVE_STATUS_INFEASIBLE,
			InfeasibleCore: &arrangorpcv1.InfeasibleCore{Message: "Kowalska has 23 lessons in 20 periods"},
		},
	}})
	dialer, _ := newDialerAs(t, "0.1.0")
	conn := node.reach(t, dialer)

	solving, err := conn.Solve(t.Context(), request())
	if err != nil {
		t.Fatalf("solve: %v", err)
	}
	result, err := solving.Wait(nil)
	if err != nil {
		t.Fatalf("wait: %v", err)
	}
	if result.HasSchedule {
		t.Error("a stream with no solution frame reported a schedule")
	}
	if result.Status != arrangorpcv1.SolveStatus_SOLVE_STATUS_INFEASIBLE {
		t.Errorf("status = %v", result.Status)
	}
	if got := solveclient.Explain(result.Explanation); got != "Kowalska has 23 lessons in 20 periods" {
		t.Errorf("explanation = %q", got)
	}
}

// Close ends a solve nobody waits for, and a later Wait reads it as a cancel.
func TestClosingASolveEndsItAsACancel(t *testing.T) {
	node := startFakeNode(t, &fakeNode{hold: true})
	dialer, _ := newDialerAs(t, "0.1.0")
	conn := node.reach(t, dialer)

	started := time.Now()
	solving, err := conn.Solve(t.Context(), request())
	if err != nil {
		t.Fatalf("solve: %v", err)
	}
	solving.Close()
	_, err = solving.Wait(nil)
	if err == nil || !solveclient.Cancelled(err) {
		t.Fatalf("err = %v, want the closed solve to read as a cancel", err)
	}
	if took := time.Since(started); took > 5*time.Second {
		t.Errorf("closing took %s", took)
	}
}

// Explain prefers the node's sentence, then its items, then its hints.
func TestExplainUsesTheNodesOwnWords(t *testing.T) {
	for _, one := range []struct {
		name string
		core *arrangorpcv1.InfeasibleCore
		want string
	}{
		{"the message", &arrangorpcv1.InfeasibleCore{Message: " all of it ", Items: []*arrangorpcv1.InfeasibleCoreItem{{Message: "item"}}}, "all of it"},
		{"the items", &arrangorpcv1.InfeasibleCore{Items: []*arrangorpcv1.InfeasibleCoreItem{{Message: "one"}, {Message: " "}, {Message: "two"}}}, "one; two"},
		{"the hints", &arrangorpcv1.InfeasibleCore{Hints: []*arrangorpcv1.InfeasibleCoreItem{{Message: "hint"}}}, "hint"},
		{"nothing", nil, ""},
	} {
		t.Run(one.name, func(t *testing.T) {
			if got := solveclient.Explain(one.core); got != one.want {
				t.Errorf("Explain = %q, want %q", got, one.want)
			}
		})
	}
}

func TestCancelledTellsAStopFromAFailure(t *testing.T) {
	if !solveclient.Cancelled(context.Canceled) || !solveclient.Cancelled(status.Error(codes.Canceled, "stop")) {
		t.Error("a stop is not read as a cancel")
	}
	if solveclient.Cancelled(errors.New("node broke")) || solveclient.Cancelled(status.Error(codes.Unavailable, "gone")) {
		t.Error("a failure is read as a cancel")
	}
}

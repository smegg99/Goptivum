// arrango/outcome_test.go

package arrango_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	arrangorpcv1 "github.com/smegg99/arrango/backend/gen/arrangorpcv1"
	arrangov1 "github.com/smegg99/arrango/backend/gen/arrangov1"

	"github.com/smegg99/goptivum/backend/arrango"
	"github.com/smegg99/goptivum/backend/common/messages"
)

func placed(lessonID, day uint32) *arrangov1.ScheduleSnapshot {
	return &arrangov1.ScheduleSnapshot{Lessons: []*arrangov1.ScheduledLesson{{
		LessonId: lessonID, Placement: &arrangov1.Placement{DayId: day, StartPeriod: 1, RoomId: 5},
	}}}
}

// A timetable a solve finds is kept as a result of its plan, never as a new
// version of the main file, and the job records the result, who started it,
// and what the solve cost.
func TestASolvedTimetableIsKeptAsAResult(t *testing.T) {
	node := startFakeNode(t, &fakeNode{frames: []*arrangorpcv1.SolveUpdate{
		solutionFrame(1, 100),
		doneFrame(arrangorpcv1.SolveStatus_SOLVE_STATUS_FEASIBLE, placed(1, 2)),
	}})
	runner, registry, jobs, repository, store := newRunner(t)
	ctx := t.Context()
	recorded, err := registry.Record(ctx, "solving", node.paired())
	if err != nil {
		t.Fatalf("record: %v", err)
	}
	plan := newPlan(t, repository, store)

	job, err := runner.Start(ctx, order(plan.ID, recorded.ID, lessons(2, 2)))
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	if job.StartedBy != owner || job.NodeLabel != "solving" {
		t.Errorf("job = %+v", job)
	}
	waitForTerminal(t, jobs, job.ID)

	closed, err := jobs.Get(ctx, job.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if closed.Status != arrango.StatusDone || closed.Reason != "" || closed.ResultID == nil {
		t.Fatalf("job = %+v, want done with a result", closed)
	}
	if closed.StartedBy != owner || closed.NodeLabel != "solving" {
		t.Errorf("started by %+v on %q", closed.StartedBy, closed.NodeLabel)
	}
	// What the node ran with and what it cost, from its final frame.
	if closed.NumWorkers != 3 || closed.PeakRSSBytes == nil || *closed.PeakRSSBytes != 1536<<20 ||
		closed.CPUSeconds == nil || *closed.CPUSeconds != 42.5 {
		t.Errorf("workers %d, peak %v, cpu %v", closed.NumWorkers, closed.PeakRSSBytes, closed.CPUSeconds)
	}

	kept, err := repository.Result(ctx, plan.ID, *closed.ResultID)
	if err != nil {
		t.Fatalf("result: %v", err)
	}
	if kept.JobID != job.ID || kept.SourceVersion != 1 || kept.StartedBy != owner.ID ||
		kept.StartedByName != owner.Name || kept.Quality == nil || *kept.Quality != 91 ||
		kept.Verdict != "TIER_PRISTINE" {
		t.Errorf("result = %+v", kept)
	}
	doc, err := store.Load(ctx, kept.ObjectKey)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if got := doc.GetSchedule().GetLessons(); len(got) != 1 || got[0].GetPlacement().GetDayId() != 2 {
		t.Errorf("the result holds %v, want the final timetable", got)
	}
	if current, err := repository.Current(ctx, plan.ID); err != nil || current != 1 {
		t.Errorf("current = %d, %v: the solve wrote the main file", current, err)
	}
}

// A solve that keeps nothing says why with a key the client words, and keeps
// the node's own sentence as detail.
func TestAFailedSolveSaysWhy(t *testing.T) {
	cases := []struct {
		name   string
		node   *fakeNode
		reason string
	}{
		{"infeasible", &fakeNode{frames: []*arrangorpcv1.SolveUpdate{
			doneFrame(arrangorpcv1.SolveStatus_SOLVE_STATUS_INFEASIBLE, nil),
		}}, messages.KeySolveInfeasible},
		{"out of time", &fakeNode{frames: []*arrangorpcv1.SolveUpdate{
			doneFrame(arrangorpcv1.SolveStatus_SOLVE_STATUS_TIMEOUT, nil),
		}}, messages.KeySolveNoScheduleInTime},
		{"solver error", &fakeNode{frames: []*arrangorpcv1.SolveUpdate{
			doneFrame(arrangorpcv1.SolveStatus_SOLVE_STATUS_ERROR, nil),
		}}, messages.KeySolveNodeError},
		{"broken stream before any timetable", &fakeNode{
			frames:   []*arrangorpcv1.SolveUpdate{{Phase: arrangorpcv1.SolvePhase_SOLVE_PHASE_PROGRESS}},
			solveErr: status.Error(codes.Internal, "the solver process died"),
		}, messages.KeySolveNodeError},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			node := startFakeNode(t, tc.node)
			runner, registry, jobs, repository, store := newRunner(t)
			ctx := t.Context()
			recorded, err := registry.Record(ctx, "failing", node.paired())
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
			if closed.Status != arrango.StatusFailed || closed.Reason != tc.reason || closed.ResultID != nil {
				t.Errorf("job = %+v, want failed with %s", closed, tc.reason)
			}
			if listed, err := repository.Results(ctx, plan.ID); err != nil || len(listed) != 0 {
				t.Errorf("results = %+v, %v, want none", listed, err)
			}
		})
	}
}

// A solve whose node breaks the stream, restarts or runs past the deadline
// after sending a timetable still fails, and keeps the best one received.
func TestAFailedSolveKeepsTheBestTimetableReceived(t *testing.T) {
	best := solutionFrame(1, 80)
	best.Snapshot = placed(1, 3)
	best.Score = &arrangov1.ScoreReport{OverallQuality: 64}
	best.Verdict = &arrangorpcv1.ScheduleVerdict{Tier: arrangorpcv1.ScheduleVerdict_TIER_WARNINGS}
	frames := []*arrangorpcv1.SolveUpdate{solutionFrame(1, 100), best}

	cases := []struct {
		name string
		node *fakeNode
		// cut ends the stream from the node's side once both timetables arrived;
		// nil when the node ends it on its own.
		cut func(*fakeNode)
	}{
		{"broken stream", &fakeNode{frames: frames,
			solveErr: status.Error(codes.Internal, "the solver process died")}, nil},
		{"past the deadline", &fakeNode{frames: frames,
			solveErr: status.Error(codes.DeadlineExceeded, "context deadline exceeded")}, nil},
		{"node restart", &fakeNode{frames: frames, hold: true}, func(node *fakeNode) { node.stop() }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			node := startFakeNode(t, tc.node)
			runner, registry, jobs, repository, store := newRunner(t)
			ctx := t.Context()
			recorded, err := registry.Record(ctx, "failing", node.paired())
			if err != nil {
				t.Fatalf("record: %v", err)
			}
			plan := newPlan(t, repository, store)
			job, err := runner.Start(ctx, order(plan.ID, recorded.ID, lessons(2, 2)))
			if err != nil {
				t.Fatalf("start: %v", err)
			}
			if tc.cut != nil {
				waitFor(t, "both timetables", func() bool {
					preview, live := runner.Preview(job.ID)
					return live && preview.Received == 2
				})
				tc.cut(node)
			}
			waitForTerminal(t, jobs, job.ID)

			closed, err := jobs.Get(ctx, job.ID)
			if err != nil {
				t.Fatalf("get: %v", err)
			}
			if closed.Status != arrango.StatusFailed || closed.Reason != messages.KeySolveNodeError ||
				closed.ResultID == nil {
				t.Fatalf("job = %+v, want failed by the node with the best timetable kept", closed)
			}
			if closed.Quality == nil || *closed.Quality != 64 || closed.Verdict != "TIER_WARNINGS" {
				t.Errorf("job quality %v verdict %q, want the kept timetable's", closed.Quality, closed.Verdict)
			}
			kept, err := repository.Result(ctx, plan.ID, *closed.ResultID)
			if err != nil {
				t.Fatalf("result: %v", err)
			}
			if kept.JobID != job.ID || kept.SourceVersion != 1 || kept.Quality == nil || *kept.Quality != 64 {
				t.Errorf("result = %+v", kept)
			}
			doc, err := store.Load(ctx, kept.ObjectKey)
			if err != nil {
				t.Fatalf("load: %v", err)
			}
			if got := doc.GetSchedule().GetLessons(); len(got) != 1 || got[0].GetPlacement().GetDayId() != 3 {
				t.Errorf("the result holds %v, want the best timetable received", got)
			}
		})
	}
}

// A failed solve whose timetable cannot be stored says that, not only that the
// node failed.
func TestAFailedSolveWhoseTimetableCannotBeStoredSaysSo(t *testing.T) {
	node := startFakeNode(t, &fakeNode{
		frames:   []*arrangorpcv1.SolveUpdate{solutionFrame(1, 100)},
		solveErr: status.Error(codes.Unavailable, "the node went away"),
	})
	registry, jobs, repository, store := newRegistry(t)
	dispatcher := newDispatcherWith(t, registry, jobs, func(context.Context, arrango.Job, arrango.Result) (int64, error) {
		return 0, errors.New("storage: bucket unreachable")
	})
	ctx := t.Context()
	recorded, err := registry.Record(ctx, "failing", node.paired())
	if err != nil {
		t.Fatalf("record: %v", err)
	}
	plan := newPlan(t, repository, store)

	result, err := dispatcher.Solve(ctx, order(plan.ID, recorded.ID, lessons(2, 2)), nil)
	if err == nil {
		t.Fatal("a broken stream was reported as a finished solve")
	}
	if result.Job.Status != arrango.StatusFailed || result.Job.Reason != messages.KeySolveResultNotStored ||
		result.Job.ResultID != nil {
		t.Errorf("job = %+v", result.Job)
	}
}

// A timetable this server could not store is a failed solve that says so.
func TestATimetableThatCannotBeStoredFailsTheSolve(t *testing.T) {
	node := startFakeNode(t, &fakeNode{frames: []*arrangorpcv1.SolveUpdate{
		doneFrame(arrangorpcv1.SolveStatus_SOLVE_STATUS_FEASIBLE, placed(1, 1)),
	}})
	registry, jobs, repository, store := newRegistry(t)
	dispatcher := newDispatcherWith(t, registry, jobs, func(context.Context, arrango.Job, arrango.Result) (int64, error) {
		return 0, errors.New("storage: bucket unreachable")
	})
	ctx := t.Context()
	recorded, err := registry.Record(ctx, "solving", node.paired())
	if err != nil {
		t.Fatalf("record: %v", err)
	}
	plan := newPlan(t, repository, store)

	result, err := dispatcher.Solve(ctx, order(plan.ID, recorded.ID, lessons(2, 2)), nil)
	if err != nil {
		t.Fatalf("solve: %v", err)
	}
	if result.Job.Status != arrango.StatusFailed || result.Job.Reason != messages.KeySolveResultNotStored ||
		result.Job.ResultID != nil {
		t.Errorf("job = %+v", result.Job)
	}
}

// The runner keeps the best timetable a running solve sent, counts the ones
// it received, and drops both when the job ends.
func TestTheRunnerKeepsTheBestTimetableForThePreview(t *testing.T) {
	scored := solutionFrame(2, 80)
	scored.Snapshot = placed(2, 3)
	scored.Score = &arrangov1.ScoreReport{OverallQuality: 77}
	scored.Verdict = &arrangorpcv1.ScheduleVerdict{Tier: arrangorpcv1.ScheduleVerdict_TIER_WARNINGS}
	node := startFakeNode(t, &fakeNode{
		frames: []*arrangorpcv1.SolveUpdate{
			{Phase: arrangorpcv1.SolvePhase_SOLVE_PHASE_PROGRESS, SolutionsFound: 5},
			solutionFrame(1, 100),
			{Phase: arrangorpcv1.SolvePhase_SOLVE_PHASE_PROGRESS, SolutionsFound: 9},
			scored,
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

	waitFor(t, "both timetables", func() bool {
		preview, live := runner.Preview(job.ID)
		return live && preview.Received == 2
	})
	preview, _ := runner.Preview(job.ID)
	if preview.PlanVersion != 1 || preview.Snapshot.GetLessons()[0].GetLessonId() != 2 ||
		preview.Quality == nil || *preview.Quality != 77 || preview.Verdict != "TIER_WARNINGS" {
		t.Errorf("preview = %+v, want the best timetable", preview)
	}

	runner.Cancel(job.ID)
	waitForTerminal(t, jobs, job.ID)
	waitFor(t, "the preview to go", func() bool {
		_, live := runner.Preview(job.ID)
		return !live
	})
}

// Every update says how many timetables arrived so far, and the terminal one
// carries only what it knows.
func TestUpdatesCountWhatArrivedAndTheLastOneOmitsTheRest(t *testing.T) {
	node := startFakeNode(t, &fakeNode{
		frames: []*arrangorpcv1.SolveUpdate{solutionFrame(1, 100), solutionFrame(1, 90)},
		hold:   true,
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
	broker, live := runner.Broker(job.ID)
	if !live {
		t.Fatal("the solve is not running")
	}
	events, cancel := broker.Subscribe()
	defer cancel()
	// Subscribed after the frames may have gone out; the count is in the preview.
	waitFor(t, "both timetables", func() bool {
		preview, _ := runner.Preview(job.ID)
		return preview.Received == 2
	})
	runner.Cancel(job.ID)

	deadline := time.After(10 * time.Second)
	for {
		select {
		case event, open := <-events:
			if !open {
				t.Fatal("the stream closed with no terminal frame")
			}
			raw, err := json.Marshal(event.Data)
			if err != nil {
				t.Fatalf("encode: %v", err)
			}
			var fields map[string]any
			if err := json.Unmarshal(raw, &fields); err != nil {
				t.Fatalf("decode: %v", err)
			}
			if fields["terminal"] != true {
				continue
			}
			for _, absent := range []string{"phase", "status", "stage", "wall_seconds", "objective", "solutions_found"} {
				if _, present := fields[absent]; present {
					t.Errorf("the terminal frame carries %s: %s", absent, raw)
				}
			}
			if fields["received"] != float64(2) || fields["job_status"] != arrango.StatusCancelled {
				t.Errorf("terminal frame = %s", raw)
			}
			waitForTerminal(t, jobs, job.ID)
			return
		case <-deadline:
			t.Fatal("no terminal frame arrived")
		}
	}
}

// An improve streams its baseline first, and its search can then send a worse
// timetable before a better one. A solve that ends early keeps the best one
// received, and the preview shows it, while every arrival is counted.
func TestAWorseTimetableAfterABetterOneIsNotKept(t *testing.T) {
	baseline := solutionFrame(1, 50)
	baseline.Snapshot = placed(1, 2)
	worse := solutionFrame(1, 90)
	worse.Snapshot = placed(1, 4)
	node := startFakeNode(t, &fakeNode{
		frames: []*arrangorpcv1.SolveUpdate{baseline, worse},
		hold:   true,
	})
	runner, registry, jobs, repository, store := newRunner(t)
	ctx := t.Context()
	recorded, err := registry.Record(ctx, "improving", node.paired())
	if err != nil {
		t.Fatalf("record: %v", err)
	}
	plan := newPlan(t, repository, store)
	job, err := runner.Start(ctx, order(plan.ID, recorded.ID, lessons(2, 2)))
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	waitFor(t, "both timetables", func() bool {
		preview, live := runner.Preview(job.ID)
		return live && preview.Received == 2
	})
	preview, _ := runner.Preview(job.ID)
	if day := preview.Snapshot.GetLessons()[0].GetPlacement().GetDayId(); day != 2 {
		t.Errorf("the preview shows day %d, want the better timetable's day 2", day)
	}

	node.stop()
	waitForTerminal(t, jobs, job.ID)
	closed, err := jobs.Get(ctx, job.ID)
	if err != nil || closed.ResultID == nil {
		t.Fatalf("job = %+v, %v, want a result", closed, err)
	}
	kept, err := repository.Result(ctx, plan.ID, *closed.ResultID)
	if err != nil {
		t.Fatalf("result: %v", err)
	}
	doc, err := store.Load(ctx, kept.ObjectKey)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if day := doc.GetSchedule().GetLessons()[0].GetPlacement().GetDayId(); day != 2 {
		t.Errorf("the result holds day %d, want the better timetable's day 2", day)
	}
}

// A node that cannot finish attaches the newest timetable it forwarded to its
// final frame. When a better one arrived before it, the better one is kept.
func TestAFinalFrameWithAWorseTimetableDoesNotReplaceTheBest(t *testing.T) {
	baseline := solutionFrame(1, 50)
	baseline.Snapshot = placed(1, 2)
	worse := solutionFrame(1, 90)
	worse.Snapshot = placed(1, 4)
	final := doneFrame(arrangorpcv1.SolveStatus_SOLVE_STATUS_ERROR, placed(1, 4))
	final.Objective = 90
	node := startFakeNode(t, &fakeNode{frames: []*arrangorpcv1.SolveUpdate{baseline, worse, final}})
	runner, registry, jobs, repository, store := newRunner(t)
	ctx := t.Context()
	recorded, err := registry.Record(ctx, "crashing", node.paired())
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
	if err != nil || closed.ResultID == nil {
		t.Fatalf("job = %+v, %v, want a result", closed, err)
	}
	kept, err := repository.Result(ctx, plan.ID, *closed.ResultID)
	if err != nil {
		t.Fatalf("result: %v", err)
	}
	doc, err := store.Load(ctx, kept.ObjectKey)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if day := doc.GetSchedule().GetLessons()[0].GetPlacement().GetDayId(); day != 2 {
		t.Errorf("the result holds day %d, want the better timetable's day 2", day)
	}
}

// solveclient/absorb_internal_test.go

package solveclient

import (
	"testing"

	arrangorpcv1 "github.com/smegg99/arrango/backend/gen/arrangorpcv1"
	arrangov1 "github.com/smegg99/arrango/backend/gen/arrangov1"
)

// The trap this whole layer exists to survive: every callback in the solver
// sets FEASIBLE unconditionally, including the heartbeat, and solutions_found
// counts what the search found rather than what arrived here.
func TestOnlyASolutionFrameProvesAScheduleExists(t *testing.T) {
	snapshot := &arrangov1.ScheduleSnapshot{}

	for _, one := range []struct {
		name   string
		update *arrangorpcv1.SolveUpdate
		want   bool
	}{
		{
			name: "a progress heartbeat claiming feasible",
			update: &arrangorpcv1.SolveUpdate{
				Phase:          arrangorpcv1.SolvePhase_SOLVE_PHASE_PROGRESS,
				Status:         arrangorpcv1.SolveStatus_SOLVE_STATUS_FEASIBLE,
				SolutionsFound: 7,
			},
			want: false,
		},
		{
			name: "a started frame claiming feasible",
			update: &arrangorpcv1.SolveUpdate{
				Phase:  arrangorpcv1.SolvePhase_SOLVE_PHASE_STARTED,
				Status: arrangorpcv1.SolveStatus_SOLVE_STATUS_FEASIBLE,
			},
			want: false,
		},
		{
			name: "a solution frame with no snapshot",
			update: &arrangorpcv1.SolveUpdate{
				Phase:  arrangorpcv1.SolvePhase_SOLVE_PHASE_SOLUTION,
				Status: arrangorpcv1.SolveStatus_SOLVE_STATUS_FEASIBLE,
			},
			want: false,
		},
		{
			name: "a done frame that proved infeasible",
			update: &arrangorpcv1.SolveUpdate{
				Phase:  arrangorpcv1.SolvePhase_SOLVE_PHASE_DONE,
				Status: arrangorpcv1.SolveStatus_SOLVE_STATUS_INFEASIBLE,
			},
			want: false,
		},
		{
			name: "a solution frame carrying a snapshot",
			update: &arrangorpcv1.SolveUpdate{
				Phase:    arrangorpcv1.SolvePhase_SOLVE_PHASE_SOLUTION,
				Status:   arrangorpcv1.SolveStatus_SOLVE_STATUS_FEASIBLE,
				Snapshot: snapshot,
			},
			want: true,
		},
	} {
		t.Run(one.name, func(t *testing.T) {
			result := Result{}
			absorb(&result, one.update)
			if result.HasSchedule != one.want {
				t.Errorf("HasSchedule = %v, want %v", result.HasSchedule, one.want)
			}
		})
	}
}

// A run that ends without a solution is a failure even though the stream closed
// cleanly and every frame said FEASIBLE.
func TestAStreamOfFeasibleProgressIsStillNoSchedule(t *testing.T) {
	result := Result{}

	for range 20 {
		absorb(&result, &arrangorpcv1.SolveUpdate{
			Phase:          arrangorpcv1.SolvePhase_SOLVE_PHASE_PROGRESS,
			Status:         arrangorpcv1.SolveStatus_SOLVE_STATUS_FEASIBLE,
			SolutionsFound: 42,
		})
	}
	absorb(&result, &arrangorpcv1.SolveUpdate{
		Phase:   arrangorpcv1.SolvePhase_SOLVE_PHASE_DONE,
		Status:  arrangorpcv1.SolveStatus_SOLVE_STATUS_TIMEOUT,
		Message: "out of time",
	})

	if result.HasSchedule {
		t.Error("a stream that never carried a snapshot reported a schedule")
	}
	if result.Status != arrangorpcv1.SolveStatus_SOLVE_STATUS_TIMEOUT {
		t.Errorf("status = %v, want the terminal frame's", result.Status)
	}
	if result.Message != "out of time" {
		t.Errorf("message = %q, want the terminal frame's", result.Message)
	}
}

// A final frame whose timetable is worse than one already received does not
// replace it, and its verdict and score do not either.
func TestAWorseFinalFrameKeepsTheBestTimetable(t *testing.T) {
	result := Result{}
	absorb(&result, &arrangorpcv1.SolveUpdate{
		Phase: arrangorpcv1.SolvePhase_SOLVE_PHASE_SOLUTION, Objective: 40,
		Snapshot: &arrangov1.ScheduleSnapshot{Lessons: []*arrangov1.ScheduledLesson{{LessonId: 1}}},
		Score:    &arrangov1.ScoreReport{OverallQuality: 90},
	})
	absorb(&result, &arrangorpcv1.SolveUpdate{
		Phase: arrangorpcv1.SolvePhase_SOLVE_PHASE_DONE, Objective: 70,
		Snapshot: &arrangov1.ScheduleSnapshot{Lessons: []*arrangov1.ScheduledLesson{{LessonId: 2}}},
		Score:    &arrangov1.ScoreReport{OverallQuality: 60},
	})
	if result.Objective != 40 || result.Quality != 90 || result.Snapshot.GetLessons()[0].GetLessonId() != 1 {
		t.Errorf("kept objective %d quality %v, want the better 40 at 90", result.Objective, result.Quality)
	}
}

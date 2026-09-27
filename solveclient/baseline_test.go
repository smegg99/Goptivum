// solveclient/baseline_test.go

package solveclient_test

import (
	"errors"
	"testing"

	"google.golang.org/protobuf/proto"

	arrangorpcv1 "github.com/smegg99/arrango/backend/gen/arrangorpcv1"
	arrangov1 "github.com/smegg99/arrango/backend/gen/arrangov1"

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

func at(day, period, room uint32) *arrangov1.Placement {
	return &arrangov1.Placement{DayId: day, StartPeriod: period, RoomId: room}
}

// timetable places each lesson id where the map says.
func timetable(places map[uint32]*arrangov1.Placement) *arrangov1.ScheduleSnapshot {
	snapshot := &arrangov1.ScheduleSnapshot{}
	for id, placement := range places {
		snapshot.Lessons = append(snapshot.Lessons, &arrangov1.ScheduledLesson{LessonId: id, Placement: placement})
	}
	return snapshot
}

// An importer's placement is not the file's timetable: lesson 1 carries one
// that the timetable disagrees with, and lesson 3 one the timetable lacks.
func importedSchool() *arrangov1.SchoolModel {
	return &arrangov1.SchoolModel{Lessons: []*arrangov1.LessonInstance{
		{Id: 1, Duration: 1, HasPrevious: true, PreviousPlacement: at(5, 5, 9)},
		{Id: 2, Duration: 1},
		{Id: 3, Duration: 1, HasPrevious: true, PreviousPlacement: at(4, 1, 9)},
	}}
}

// The solver's ResolveBaseline abandons the whole baseline at the first lesson
// with no previous placement and says nothing, so the guarantee is silently
// gone and the user is told a worse schedule is the answer.
func TestBaselineCoverageIsRefusedBeforeDispatch(t *testing.T) {
	similar := &arrangorpcv1.SolverConfig{IgnorePreviousPlacements: false}
	scratch := &arrangorpcv1.SolverConfig{IgnorePreviousPlacements: true}

	if err := solveclient.CheckBaseline(lessons(10, 10), similar); err != nil {
		t.Errorf("a fully placed plan was refused: %v", err)
	}
	if err := solveclient.CheckBaseline(lessons(10, 3), scratch); err != nil {
		t.Errorf("a from-scratch solve needs no baseline, got %v", err)
	}

	err := solveclient.CheckBaseline(lessons(10, 3), similar)
	var incomplete *solveclient.ErrIncompleteBaseline
	if !errors.As(err, &incomplete) {
		t.Fatalf("err = %v, want a typed incomplete-baseline finding", err)
	}
	if incomplete.Unplaced != 7 || incomplete.Lessons != 10 {
		t.Errorf("finding = %+v, want 7 of 10 unplaced", incomplete)
	}
}

// An improve starts from the file's own timetable, whatever an importer left
// on the lessons.
func TestAnImproveStartsFromTheFilesOwnTimetable(t *testing.T) {
	model := importedSchool()
	solveclient.SetBaseline(model, timetable(map[uint32]*arrangov1.Placement{
		1: at(1, 1, 7), 2: at(1, 2, 0), 3: at(2, 3, 8),
		// A lesson the school no longer has places nothing.
		99: at(3, 3, 3),
	}))

	want := map[uint32]*arrangov1.Placement{1: at(1, 1, 7), 2: at(1, 2, 0), 3: at(2, 3, 8)}
	for _, lesson := range model.GetLessons() {
		if !lesson.GetHasPrevious() || !proto.Equal(lesson.GetPreviousPlacement(), want[lesson.GetId()]) {
			t.Errorf("lesson %d starts from %v (has %v), want %v", lesson.GetId(),
				lesson.GetPreviousPlacement(), lesson.GetHasPrevious(), want[lesson.GetId()])
		}
	}
	if len(model.GetLessons()) != 3 {
		t.Errorf("the school has %d lessons, want the 3 it had", len(model.GetLessons()))
	}
	if err := solveclient.CheckBaseline(model, &arrangorpcv1.SolverConfig{}); err != nil {
		t.Errorf("a fully placed file was refused: %v", err)
	}
}

// A lesson the timetable does not place starts from nothing, so the improve is
// refused with the count, never admitted from what an importer once set.
func TestALessonTheTimetableDoesNotPlaceIsCountedAsUnplaced(t *testing.T) {
	cases := []struct {
		name     string
		schedule *arrangov1.ScheduleSnapshot
		unplaced int
	}{
		{"one missing", timetable(map[uint32]*arrangov1.Placement{1: at(1, 1, 7), 2: at(1, 2, 0)}), 1},
		{"no timetable", nil, 3},
		{"an empty timetable", &arrangov1.ScheduleSnapshot{}, 3},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			model := importedSchool()
			solveclient.SetBaseline(model, tc.schedule)
			for _, lesson := range model.GetLessons() {
				placed := false
				for _, entry := range tc.schedule.GetLessons() {
					placed = placed || entry.GetLessonId() == lesson.GetId()
				}
				if !placed && (lesson.GetHasPrevious() || lesson.GetPreviousPlacement() != nil) {
					t.Errorf("lesson %d kept %v, which the timetable does not place",
						lesson.GetId(), lesson.GetPreviousPlacement())
				}
			}

			err := solveclient.CheckBaseline(model, &arrangorpcv1.SolverConfig{})
			var incomplete *solveclient.ErrIncompleteBaseline
			if !errors.As(err, &incomplete) {
				t.Fatalf("err = %v, want the incomplete baseline refusal", err)
			}
			if incomplete.Unplaced != tc.unplaced || incomplete.Lessons != 3 {
				t.Errorf("refusal = %+v, want %d of 3 unplaced", incomplete, tc.unplaced)
			}
			if snapshot := solveclient.BaselineSnapshot(model, &arrangorpcv1.SolverConfig{}); snapshot != nil {
				t.Errorf("a partial timetable was offered for checking: %v", snapshot)
			}
		})
	}
}

func TestBaselineSnapshotOnlyDescribesAnImprovingSolve(t *testing.T) {
	similar := &arrangorpcv1.SolverConfig{IgnorePreviousPlacements: false}
	scratch := &arrangorpcv1.SolverConfig{IgnorePreviousPlacements: true}

	if snapshot := solveclient.BaselineSnapshot(lessons(3, 3), scratch); snapshot != nil {
		t.Error("a from-scratch solve has no baseline to check")
	}
	if snapshot := solveclient.BaselineSnapshot(lessons(3, 1), similar); snapshot != nil {
		t.Error("an incomplete baseline is not a schedule; CheckBaseline refuses it")
	}
	if snapshot := solveclient.BaselineSnapshot(nil, similar); snapshot != nil {
		t.Error("no model, no baseline")
	}
	snapshot := solveclient.BaselineSnapshot(lessons(3, 3), similar)
	if snapshot == nil || len(snapshot.GetLessons()) != 3 {
		t.Fatalf("snapshot = %v, want all three placements", snapshot)
	}
	if snapshot.GetLessons()[0].GetLessonId() != 1 {
		t.Errorf("lesson id = %d, want the model's own id", snapshot.GetLessons()[0].GetLessonId())
	}
}

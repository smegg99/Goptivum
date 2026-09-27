// solveclient/baseline.go

package solveclient

import (
	"fmt"

	"google.golang.org/protobuf/proto"

	arrangorpcv1 "github.com/smegg99/arrango/backend/gen/arrangorpcv1"
	arrangov1 "github.com/smegg99/arrango/backend/gen/arrangov1"
)

// SetBaseline sets each lesson's previous placement from a file's timetable and clears it where the timetable places none.
func SetBaseline(model *arrangov1.SchoolModel, schedule *arrangov1.ScheduleSnapshot) {
	placed := make(map[uint32]*arrangov1.Placement, len(schedule.GetLessons()))
	for _, entry := range schedule.GetLessons() {
		if entry.GetPlacement() != nil {
			placed[entry.GetLessonId()] = entry.GetPlacement()
		}
	}
	for _, lesson := range model.GetLessons() {
		placement, ok := placed[lesson.GetId()]
		lesson.HasPrevious = ok
		lesson.PreviousPlacement = nil
		if ok {
			lesson.PreviousPlacement = proto.Clone(placement).(*arrangov1.Placement)
		}
	}
}

// CheckBaseline refuses an improve solve over a model whose lessons are not all
// placed, after SetBaseline.
//
// The solver's ResolveBaseline abandons the whole baseline at the first lesson
// with no previous placement, and says nothing: the solve runs, the "never end
// up worse than the previous timetable" guarantee is silently gone, and the
// user is told a worse schedule is the answer. Locked placements do not count,
// because that function reads previous_placement and nothing else.
func CheckBaseline(model *arrangov1.SchoolModel, config *arrangorpcv1.SolverConfig) error {
	if model == nil {
		return fmt.Errorf("solveclient: no model")
	}
	if config.GetIgnorePreviousPlacements() {
		return nil
	}
	unplaced := 0
	for _, lesson := range model.GetLessons() {
		if !lesson.GetHasPrevious() {
			unplaced++
		}
	}
	if unplaced == 0 {
		return nil
	}
	return &ErrIncompleteBaseline{Lessons: len(model.GetLessons()), Unplaced: unplaced}
}

// BaselineSnapshot is the schedule a solve starts from, or nil when it starts
// from nothing.
//
// Only an improving solve has one. CheckBaseline has already refused an
// improving solve whose baseline is incomplete, so every lesson here carries a
// previous placement and the snapshot describes the whole timetable.
func BaselineSnapshot(model *arrangov1.SchoolModel,
	config *arrangorpcv1.SolverConfig) *arrangov1.ScheduleSnapshot {
	if model == nil || config.GetIgnorePreviousPlacements() {
		return nil
	}
	lessons := make([]*arrangov1.ScheduledLesson, 0, len(model.GetLessons()))
	for _, lesson := range model.GetLessons() {
		if !lesson.GetHasPrevious() {
			return nil
		}
		lessons = append(lessons, &arrangov1.ScheduledLesson{
			LessonId:  lesson.GetId(),
			Placement: lesson.GetPreviousPlacement(),
		})
	}
	if len(lessons) == 0 {
		return nil
	}
	return &arrangov1.ScheduleSnapshot{Lessons: lessons}
}

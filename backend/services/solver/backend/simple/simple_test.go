// simple/simple_test.go

package simple

import (
	"strings"
	"testing"

	arrangov1 "github.com/smegg99/arrango/backend/gen/arrangov1"
)

func validSchool() School {
	return School{
		Name:          "Test",
		DayCount:      2,
		PeriodsPerDay: 6,
		Divisions: []Division{
			{Name: "1A", Year: 1, Groups: []string{"1/2", "2/2"}},
			{Name: "3B", Year: 3},
		},
		Teachers: []string{"J.Kowalska", "A.Nowak"},
		Rooms:    []string{"42", "sg1"},
		Lessons: []Lesson{
			{Division: "1A", Subject: "matematyka", Teacher: "J.Kowalska",
				Room: "42", Count: 3},
			{Division: "1A", Subject: "wf", Teacher: "A.Nowak", Room: "sg1",
				Group: "1/2", Count: 2},
			{Division: "3B", Subject: "projekt", Duration: 2}, // no teacher/room
		},
	}
}

func TestToModelHappyPath(t *testing.T) {
	model, err := ToModel(validSchool())
	if err != nil {
		t.Fatal(err)
	}
	if len(model.Days) != 2 || model.Days[0].Name != "Poniedziałek" {
		t.Errorf("days = %v", model.Days)
	}
	if len(model.Periods) != 6 {
		t.Errorf("periods = %d", len(model.Periods))
	}
	if len(model.Divisions) != 2 || len(model.Groups) != 2 {
		t.Errorf("divisions/groups = %d/%d",
			len(model.Divisions), len(model.Groups))
	}
	if len(model.Lessons) != 6 { // 3 + 2 + 1
		t.Fatalf("lessons = %d, want 6", len(model.Lessons))
	}
	var project *arrangov1.LessonInstance
	for _, l := range model.Lessons {
		if l.Duration == 2 {
			project = l
		}
	}
	if project == nil || project.RequiresTeacher || project.RequiresRoom {
		t.Errorf("duration-2 teacherless lesson wrong: %+v", project)
	}
	// Year priorities: year 1 -> 300, year 3 -> 100.
	priorities := map[uint32]uint32{}
	for _, y := range model.Years {
		priorities[uint32(y.Level)] = y.Priority
	}
	if priorities[1] != 300 || priorities[3] != 100 {
		t.Errorf("priorities = %v", priorities)
	}
}

func TestToModelReportsAllProblems(t *testing.T) {
	s := validSchool()
	s.Lessons = append(s.Lessons,
		Lesson{Division: "9Z", Subject: "x"},
		Lesson{Division: "1A", Subject: "y", Teacher: "Nieznany"},
		Lesson{Division: "1A", Subject: "z", Group: "5/9"},
		Lesson{Division: "1A", Subject: "w", Duration: 99},
	)
	_, err := ToModel(s)
	if err == nil {
		t.Fatal("expected error")
	}
	for _, want := range []string{"unknown division", "unknown teacher",
		"unknown group", "exceeds periodsPerDay"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q misses %q", err.Error(), want)
		}
	}
}

func TestRenderResultResolvesNames(t *testing.T) {
	model, err := ToModel(validSchool())
	if err != nil {
		t.Fatal(err)
	}
	lesson := model.Lessons[0]
	update := &arrangov1.SolveUpdate{
		Phase:  arrangov1.SolvePhase_SOLVE_PHASE_DONE,
		Status: arrangov1.SolveStatus_SOLVE_STATUS_OPTIMAL,
		Snapshot: &arrangov1.ScheduleSnapshot{
			Lessons: []*arrangov1.ScheduledLesson{{
				LessonId: lesson.Id,
				Placement: &arrangov1.Placement{
					DayId:       model.Days[1].Id,
					StartPeriod: 3,
					RoomId:      model.Rooms[0].Id,
				},
			}},
		},
		Score: &arrangov1.ScoreReport{
			OverallQuality: 91.5,
			SoftIssues: []*arrangov1.SoftIssue{{
				Category: "student_gap", Entity: "1A/1/2",
				DayId: model.Days[0].Id, Period: 2, Count: 1, Penalty: 30000,
			}},
		},
	}
	r := RenderResult(model, update)
	if r.Phase != "DONE" || r.Status != "OPTIMAL" || r.Quality != 91.5 {
		t.Errorf("header wrong: %+v", r)
	}
	if len(r.Schedule) != 1 {
		t.Fatalf("schedule = %v", r.Schedule)
	}
	p := r.Schedule[0]
	if p.Division != "1A" || p.Subject != "matematyka" ||
		p.Teacher != "J.Kowalska" || p.Room != "42" ||
		p.Day != "Wtorek" || p.Period != 3 {
		t.Errorf("placed = %+v", p)
	}
	if len(r.SoftIssues) != 1 || r.SoftIssues[0].Day != "Poniedziałek" {
		t.Errorf("issues = %+v", r.SoftIssues)
	}
}

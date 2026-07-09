// optivum/writer_test.go

package optivum

import (
	"testing"

	arrangov1 "github.com/smegg99/arrango/backend/gen/arrangov1"
)

func exportFixture() (*arrangov1.SchoolModel, *arrangov1.ScheduleSnapshot) {
	model := &arrangov1.SchoolModel{
		Name: "Test szkoła",
		Days: []*arrangov1.Day{
			{Id: 1, Name: "Poniedziałek", PeriodCount: 4},
			{Id: 2, Name: "Wtorek", PeriodCount: 4},
		},
		Periods: []*arrangov1.Period{
			{Id: 11, Name: "7:00- 7:45"}, {Id: 12, Name: "7:50- 8:35"},
			{Id: 13, Name: "8:40- 9:25"}, {Id: 14, Name: "9:30-10:15"},
		},
		Years:     []*arrangov1.Year{{Id: 20, Name: "Rok 1", Level: 1, Priority: 300}},
		Divisions: []*arrangov1.Division{{Id: 30, Name: "1aT", YearId: 20}},
		Splits: []*arrangov1.Split{{Id: 45, Name: "2", DivisionId: 30}},
		Groups: []*arrangov1.Group{
			{Id: 40, Name: "1/2", DivisionId: 30, SplitId: 45},
		},
		Teachers: []*arrangov1.Teacher{{Id: 50, Name: "J.Kowalska (JK)"}},
		Subjects: []*arrangov1.Subject{
			{Id: 60, Name: "matematyka"}, {Id: 61, Name: "j.angielski"},
		},
		Rooms: []*arrangov1.Room{{Id: 80, Name: "42", Designator: "42"}},
		Lessons: []*arrangov1.LessonInstance{
			{ // two-period whole-division block
				Id: 100, Duration: 2,
				Participants: []*arrangov1.Participant{{DivisionId: 30}},
				SubjectId:    60, TeacherId: 50,
				RequiresTeacher: true, RequiresRoom: true,
			},
			{ // split-group single period
				Id: 101, Duration: 1,
				Participants: []*arrangov1.Participant{
					{DivisionId: 30, GroupId: 40},
				},
				SubjectId: 61, TeacherId: 50,
				RequiresTeacher: true, RequiresRoom: true,
			},
		},
	}
	snapshot := &arrangov1.ScheduleSnapshot{
		Lessons: []*arrangov1.ScheduledLesson{
			{LessonId: 100, Placement: &arrangov1.Placement{
				DayId: 1, StartPeriod: 0, RoomId: 80}},
			{LessonId: 101, Placement: &arrangov1.Placement{
				DayId: 2, StartPeriod: 2, RoomId: 80}},
		},
	}
	return model, snapshot
}

// Our own export must be re-importable by our own parser with the same
// schedule content.
func TestExportRoundTripsThroughParser(t *testing.T) {
	model, snapshot := exportFixture()
	data, err := ExportZip(model, snapshot)
	if err != nil {
		t.Fatal(err)
	}
	back, err := ParseZip(data)
	if err != nil {
		t.Fatal(err)
	}
	if got := back.Summary.Divisions; got != 1 {
		t.Errorf("divisions = %d", got)
	}
	if got := back.Summary.Teachers; got != 1 {
		t.Errorf("teachers = %d", got)
	}
	if got := back.Summary.Rooms; got != 1 {
		t.Errorf("rooms = %d", got)
	}
	if got := back.Summary.Lessons; got != 2 {
		t.Fatalf("lessons = %d, want 2 (block re-merged)", got)
	}

	subjects := map[uint32]string{}
	for _, s := range back.Model.Subjects {
		subjects[s.Id] = s.Name
	}
	groups := map[uint32]string{}
	for _, g := range back.Model.Groups {
		groups[g.Id] = g.Name
	}
	var mat, ang *arrangov1.LessonInstance
	for _, l := range back.Model.Lessons {
		switch subjects[l.SubjectId] {
		case "matematyka":
			mat = l
		case "j.angielski":
			ang = l
		}
	}
	if mat == nil || ang == nil {
		t.Fatalf("subjects missing after round trip: %v", subjects)
	}
	if mat.Duration != 2 || mat.PreviousPlacement.StartPeriod != 0 {
		t.Errorf("matematyka block wrong: %+v", mat)
	}
	_, angGroupID := firstParticipant(ang)
	if angGroupID == 0 || groups[angGroupID] != "1/2" {
		t.Errorf("group suffix lost: %+v (groups %v)", ang, groups)
	}
	if ang.PreviousPlacement.StartPeriod != 2 {
		t.Errorf("angielski placement wrong: %+v", ang.PreviousPlacement)
	}
	// Day names survive.
	if back.Model.Days[0].Name != "Poniedziałek" ||
		back.Model.Days[1].Name != "Wtorek" {
		t.Errorf("day names lost: %v", back.Model.Days)
	}
	// Teacher full name survives via the n-page title.
	if back.Model.Teachers[0].Name != "J.Kowalska (JK)" {
		t.Errorf("teacher name = %q", back.Model.Teachers[0].Name)
	}
}

func TestExportRealImportRoundTrips(t *testing.T) {
	original := parseSample(t)
	data, err := ExportZip(original.Model, original.Snapshot)
	if err != nil {
		t.Fatal(err)
	}
	back, err := ParseZip(data)
	if err != nil {
		t.Fatal(err)
	}
	if back.Summary.Divisions != original.Summary.Divisions {
		t.Errorf("divisions %d != %d", back.Summary.Divisions,
			original.Summary.Divisions)
	}
	if back.Summary.Lessons != original.Summary.Lessons {
		t.Errorf("lessons %d != %d", back.Summary.Lessons,
			original.Summary.Lessons)
	}
	if back.Summary.Rooms != original.Summary.Rooms {
		t.Errorf("rooms %d != %d", back.Summary.Rooms, original.Summary.Rooms)
	}
}

func TestExportRejectsNil(t *testing.T) {
	if _, err := ExportZip(nil, nil); err == nil {
		t.Fatal("expected error")
	}
}

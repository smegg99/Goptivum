// formats/goptivum/fixture_test.go

package goptivum_test

import (
	"errors"
	"flag"
	"os"
	"testing"

	arrangov1 "github.com/smegg99/arrango/backend/gen/arrangov1"

	"github.com/smegg99/goptivum/backend/formats"
	"github.com/smegg99/goptivum/backend/formats/goptivum"
	"github.com/smegg99/goptivum/backend/internal/testsupport"
	"github.com/smegg99/goptivum/document"
	goptivumv1 "github.com/smegg99/goptivum/document/gen/goptivumv1"
	"github.com/smegg99/goptivum/document/gpf"
)

// The committed password-locked .gpf that Karol imports by hand (slice 18,
// test 4). age salts every file, so writing it again changes every byte:
// it is written only on request,
//
//	go test ./backend/formats/goptivum -run TestThePasswordFixture -update-fixture
var updateFixture = flag.Bool("update-fixture", false, "write "+passwordFixture+" again")

const (
	passwordFixture    = "gpf/password-synthetic.gpf"
	fixturePassphrase  = "synthetic-fixture"
	fixtureSchool      = "Szkoła Syntetyczna"
	fixtureLessons     = 6
	fixtureWorkFactor  = 16
	fixtureWrongPhrase = "not-the-fixture"
)

// syntheticSchool is invented data: two classes, two teachers, two rooms, a
// two-day week and a timetable, with Polish letters to show they survive.
func syntheticSchool() *goptivumv1.Document {
	model := &arrangov1.SchoolModel{
		Name: fixtureSchool,
		Days: []*arrangov1.Day{
			{Id: 1, Name: "Poniedziałek", PeriodCount: 3},
			{Id: 2, Name: "Wtorek", PeriodCount: 3},
		},
		Periods: []*arrangov1.Period{
			{Id: 3, Name: "08:00-08:45"}, {Id: 4, Name: "08:55-09:40"}, {Id: 5, Name: "09:50-10:35"},
		},
		Years: []*arrangov1.Year{{Id: 6, Name: "Rok 1", Level: 1, Priority: 100}},
		Divisions: []*arrangov1.Division{
			{Id: 7, Name: "1a", YearId: 6}, {Id: 8, Name: "1b", YearId: 6},
		},
		Teachers: []*arrangov1.Teacher{
			{Id: 9, Name: "Zofia Źrebięcka"}, {Id: 10, Name: "Łukasz Żółkiewski"},
		},
		Subjects: []*arrangov1.Subject{{Id: 11, Name: "Język polski"}, {Id: 12, Name: "Matematyka"}},
		Rooms: []*arrangov1.Room{
			{Id: 13, Name: "Sala 101", Designator: "101"}, {Id: 14, Name: "Sala 102", Designator: "102"},
		},
	}
	schedule := &arrangov1.ScheduleSnapshot{}
	placements := []struct{ division, subject, teacher, room, day, period uint32 }{
		{7, 11, 9, 13, 1, 0}, {7, 12, 10, 14, 1, 1}, {7, 11, 9, 13, 2, 0},
		{8, 12, 10, 14, 1, 0}, {8, 11, 9, 13, 1, 1}, {8, 12, 10, 14, 2, 2},
	}
	for i, p := range placements {
		id := uint32(20 + i)
		model.Lessons = append(model.Lessons, &arrangov1.LessonInstance{
			Id:              id,
			Participants:    []*arrangov1.Participant{{DivisionId: p.division}},
			SubjectId:       p.subject,
			TeacherId:       p.teacher,
			Duration:        1,
			RequiresTeacher: true,
			RequiresRoom:    true,
		})
		schedule.Lessons = append(schedule.Lessons, &arrangov1.ScheduledLesson{
			LessonId:  id,
			Placement: &arrangov1.Placement{DayId: p.day, StartPeriod: p.period, RoomId: p.room},
		})
	}
	built := document.New(model)
	built.Schedule = schedule
	return built
}

// The fixture opens with its passphrase and with nothing else, and the
// registry picks it up as Goptivum's own format.
func TestThePasswordFixture(t *testing.T) {
	if *updateFixture {
		file, err := gpf.Encode(syntheticSchool(), gpf.WriteOptions{
			Header:     gpf.NewHeader().Set(gpf.FieldSchool, fixtureSchool),
			Passphrase: fixturePassphrase, WorkFactor: fixtureWorkFactor,
		})
		if err != nil {
			t.Fatalf("encode: %v", err)
		}
		if err := os.WriteFile(testsupport.WritableFixture(t, passwordFixture), file, 0o644); err != nil {
			t.Fatalf("write: %v", err)
		}
	}

	data, err := os.ReadFile(testsupport.Fixture(t, passwordFixture))
	if err != nil {
		t.Fatalf("read: %v", err)
	}

	opened, err := gpf.Decode(data, gpf.ReadOptions{Passphrase: fixturePassphrase})
	if err != nil {
		t.Fatalf("open with %q: %v", fixturePassphrase, err)
	}
	if opened.Mode != gpf.ModePassword {
		t.Errorf("mode = %s, want %s", opened.Mode, gpf.ModePassword)
	}
	model := opened.Document.GetModel()
	if model.GetName() != fixtureSchool || model.GetTeachers()[1].GetName() != "Łukasz Żółkiewski" {
		t.Errorf("school = %q, teacher = %q", model.GetName(), model.GetTeachers()[1].GetName())
	}
	if len(model.GetLessons()) != fixtureLessons || len(opened.Document.GetSchedule().GetLessons()) != fixtureLessons {
		t.Errorf("lessons = %d, placed = %d, want %d of each",
			len(model.GetLessons()), len(opened.Document.GetSchedule().GetLessons()), fixtureLessons)
	}

	if _, err := gpf.Decode(data, gpf.ReadOptions{}); !errors.Is(err, gpf.ErrPasswordRequired) {
		t.Errorf("no passphrase: err = %v, want ErrPasswordRequired", err)
	}
	if _, err := gpf.Decode(data, gpf.ReadOptions{Passphrase: fixtureWrongPhrase}); !errors.Is(err, gpf.ErrWrongPassword) {
		t.Errorf("a wrong passphrase: err = %v, want ErrWrongPassword", err)
	}

	best, err := registry(t).Best(formats.Open("password-synthetic.gpf", data))
	if err != nil || best.ID() != goptivum.ID {
		t.Errorf("best = %v, %v, want %s", best, err, goptivum.ID)
	}
}

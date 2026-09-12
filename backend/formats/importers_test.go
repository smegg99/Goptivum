// formats/importers_test.go

package formats_test

import (
	"context"
	"testing"

	arrangov1 "github.com/smegg99/arrango/backend/gen/arrangov1"
	"golang.org/x/text/encoding/charmap"

	"github.com/smegg99/goptivum/backend/formats"
	optivumhtml "github.com/smegg99/goptivum/backend/formats/optivum/html"
	"github.com/smegg99/goptivum/backend/formats/optivum/pla"
	planxml "github.com/smegg99/goptivum/backend/formats/optivum/xml"
)

// The two Optivum formats a school can hand over have to be told apart by
// their content alone, since both arrive as an upload with any name.
func TestDetectionTellsTheOptivumFormatsApart(t *testing.T) {
	registry := formats.NewRegistry(planxml.New(), optivumhtml.NewImporter())
	if got := len(registry.Importers()); got != 2 {
		t.Fatalf("importers = %d, want 2", got)
	}

	cases := []struct {
		name string
		data []byte
		want string
	}{
		{"a plan container", planContainer(t), planxml.ID},
		{"an html export", htmlExport(t), optivumhtml.ID},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			source := formats.Open("upload.bin", test.data)

			matches, err := registry.Detect(source)
			if err != nil {
				t.Fatalf("detect: %v", err)
			}
			if len(matches) != 1 {
				t.Fatalf("matches = %d, want exactly one format to claim the file", len(matches))
			}
			if matches[0].ID != test.want || matches[0].Confidence != formats.Certain {
				t.Errorf("match = %s at %v, want %s at certain",
					matches[0].ID, matches[0].Confidence, test.want)
			}

			importer, err := registry.Best(source)
			if err != nil {
				t.Fatalf("best: %v", err)
			}
			result, err := importer.Import(context.Background(), source)
			if err != nil {
				t.Fatalf("import: %v", err)
			}
			if len(result.Model.GetLessons()) == 0 {
				t.Error("the import carried no lessons")
			}
			if result.HasErrors() {
				t.Error("a clean file produced an error finding")
			}
		})
	}

	if _, err := registry.Best(formats.Open("notes.txt", []byte("hello"))); err == nil {
		t.Error("an unrelated file was claimed by an importer")
	}
}

// planContainer is the smallest plan worth importing, encoded the way a real
// one is: iso-8859-2 inside the container.
func planContainer(t *testing.T) []byte {
	t.Helper()

	const plan = `<?xml version="1.0" encoding="iso-8859-2"?>
<plan wersja="12.00.0004">
 <dni><dzien kod="Pn" nazwa="Poniedziałek"/></dni>
 <lekcje><lekcja kod="1" od=" 8:00" do=" 8:45"/></lekcje>
 <nauczyciele><nauczyciel kod="AB" imie="Jan" nazwisko="Nowak"/></nauczyciele>
 <przedmioty><przedmiot kod="mat" nazwa="matematyka"/></przedmioty>
 <sale><sala kod="101" nazwa="Sala 101" poj="30"/></sale>
 <oddzialy><oddzial kod="1A" poziom="4" ch="20" dz="0">
  <grupy/><info><p d="0" g="0" i="*"/></info>
 </oddzial></oddzialy>
 <przydzialy><p kl="1A" nau="AB" prz="mat" sa="101" dz="0" godz="0" bk="1" pref="101"/></przydzialy>
</plan>`

	latin2, err := charmap.ISO8859_2.NewEncoder().Bytes([]byte(plan))
	if err != nil {
		t.Fatalf("encode as iso-8859-2: %v", err)
	}
	data, err := pla.Encode(pla.File{Magic: pla.Magic, XML: latin2, Compressed: true})
	if err != nil {
		t.Fatalf("wrap in a container: %v", err)
	}
	return data
}

// htmlExport renders one placed lesson through the exporter, which writes the
// same markers a VULCAN export carries.
func htmlExport(t *testing.T) []byte {
	t.Helper()

	model := &arrangov1.SchoolModel{
		Days:      []*arrangov1.Day{{Id: 1, Name: "Poniedziałek", PeriodCount: 1}},
		Periods:   []*arrangov1.Period{{Id: 2, Name: "8:00-8:45"}},
		Years:     []*arrangov1.Year{{Id: 3, Name: "Rok 1", Level: 1, Priority: 100}},
		Divisions: []*arrangov1.Division{{Id: 4, Name: "1A", YearId: 3}},
		Teachers:  []*arrangov1.Teacher{{Id: 5, Name: "Jan Nowak"}},
		Subjects:  []*arrangov1.Subject{{Id: 6, Name: "matematyka"}},
		Rooms:     []*arrangov1.Room{{Id: 7, Name: "101", Designator: "101"}},
		Lessons: []*arrangov1.LessonInstance{{
			Id: 8, SubjectId: 6, TeacherId: 5, Duration: 1,
			RequiresTeacher: true, RequiresRoom: true,
			Participants: []*arrangov1.Participant{{DivisionId: 4}},
		}},
	}
	snapshot := &arrangov1.ScheduleSnapshot{Lessons: []*arrangov1.ScheduledLesson{
		{LessonId: 8, Placement: &arrangov1.Placement{DayId: 1, StartPeriod: 0, RoomId: 7}},
	}}

	data, err := optivumhtml.NewExporter().Export(context.Background(),
		&formats.Result{Model: model, Schedule: snapshot})
	if err != nil {
		t.Fatalf("export: %v", err)
	}
	return data
}

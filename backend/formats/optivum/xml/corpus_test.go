// formats/optivum/xml/corpus_test.go

package xml_test

import (
	"bytes"
	"context"
	"encoding/xml"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/text/encoding/charmap"
	"google.golang.org/protobuf/proto"

	"github.com/smegg99/goptivum/backend/formats"
	"github.com/smegg99/goptivum/backend/formats/optivum/pla"
	planxml "github.com/smegg99/goptivum/backend/formats/optivum/xml"
	"github.com/smegg99/goptivum/backend/internal/testsupport"
)

// shape is an element census of one plan document, counted here rather than
// by the reader so that the two have to agree.
type shape struct {
	days      int
	periods   int
	teachers  int
	subjects  int
	buildings int
	rooms     int
	divisions int
	splits    int
	groups    int
	rows      int // przydzialy/p, one per period of one lesson
	lessons   int // rows that start a block
	placed    int // block starts the school has already put in the week
	roomSets  int
}

// censusOf walks the raw payload. It dispatches on the parent element, never
// on the tag name: <p> means a lesson under przydzialy, a marked slot under
// info and a duty roster under dyzuryplan.
func censusOf(t *testing.T, raw []byte) shape {
	t.Helper()

	decoder := xml.NewDecoder(bytes.NewReader(raw))
	decoder.CharsetReader = func(label string, input io.Reader) (io.Reader, error) {
		if strings.HasPrefix(strings.ToLower(label), "iso-8859-2") {
			return charmap.ISO8859_2.NewDecoder().Reader(input), nil
		}
		return input, nil
	}

	var found shape
	var stack []string
	for {
		token, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("scan payload: %v", err)
		}
		start, ok := token.(xml.StartElement)
		if !ok {
			if _, ok := token.(xml.EndElement); ok && len(stack) > 0 {
				stack = stack[:len(stack)-1]
			}
			continue
		}
		parent := ""
		if len(stack) > 0 {
			parent = stack[len(stack)-1]
		}
		attr := func(name string) string {
			for _, candidate := range start.Attr {
				if candidate.Name.Local == name {
					return candidate.Value
				}
			}
			return ""
		}
		switch start.Name.Local {
		case "dzien":
			found.days++
		case "lekcja":
			found.periods++
		case "nauczyciel":
			found.teachers++
		case "przedmiot":
			found.subjects++
		case "budynek":
			found.buildings++
		case "sala":
			if parent == "sale" {
				found.rooms++
			}
		case "oddzial":
			found.divisions++
		case "podzial":
			found.splits++
		case "grupa":
			found.groups++
		case "zbior":
			found.roomSets++
		case "p":
			if parent != "przydzialy" {
				break
			}
			found.rows++
			if attr("bk") == "" {
				break // a continuation of the block above it
			}
			found.lessons++
			if attr("dz") != "" && attr("godz") != "" {
				found.placed++
			}
		}
		stack = append(stack, start.Name.Local)
	}
	return found
}

// Every real plan file must import: the counts have to match the document's
// own census, and nothing may be lost badly enough to be an error.
func TestCorpusRealPlanFilesImport(t *testing.T) {
	for _, path := range testsupport.Corpus(t, "*.pla") {
		// The file is named, never its contents: these are real schools.
		t.Run(filepath.Base(path), func(t *testing.T) {
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("read: %v", err)
			}
			container, err := pla.Decode(data)
			if err != nil {
				t.Fatalf("decode container: %v", err)
			}
			found := censusOf(t, container.XML)

			source := formats.Open(filepath.Base(path), data)
			importer := planxml.New()

			confidence, err := importer.Detect(source)
			if err != nil {
				t.Fatalf("detect: %v", err)
			}
			if confidence != formats.Certain {
				t.Errorf("detect = %v, want certain for a real container", confidence)
			}

			result, err := importer.Import(context.Background(), source)
			if err != nil {
				t.Fatalf("import: %v", err)
			}
			checkModel(t, result)

			model := result.Model
			for _, check := range []struct {
				what      string
				got, want int
			}{
				{"days", len(model.GetDays()), found.days},
				{"periods", len(model.GetPeriods()), found.periods},
				{"teachers", len(model.GetTeachers()), found.teachers},
				{"subjects", len(model.GetSubjects()), found.subjects},
				{"buildings", len(model.GetBuildings()), found.buildings},
				{"rooms", len(model.GetRooms()), found.rooms},
				{"divisions", len(model.GetDivisions()), found.divisions},
				{"splits", len(model.GetSplits()), found.splits},
				{"groups", len(model.GetGroups()), found.groups},
				{"lessons", len(model.GetLessons()), found.lessons},
				{"placed lessons", len(result.Schedule.GetLessons()), found.placed},
			} {
				if check.got != check.want {
					t.Errorf("%s = %d, the document has %d", check.what, check.got, check.want)
				}
			}

			// Every row of the file is one period of exactly one lesson, so
			// the durations have to add up to the rows. A period counted
			// twice is a double-booked class the school never asked for.
			var periods int
			for _, lesson := range model.GetLessons() {
				periods += int(lesson.GetDuration())
			}
			if periods != found.rows {
				t.Errorf("lesson periods = %d, the document has %d assignment rows", periods, found.rows)
			}

			for _, finding := range result.Findings {
				if finding.Severity == formats.Error {
					// Kind and refs only: Detail names codes from the file.
					t.Errorf("error finding %s over %d entities", finding.Kind, len(finding.Entities))
				}
			}
			if t.Failed() {
				severities := map[formats.Severity]int{}
				for _, finding := range result.Findings {
					severities[finding.Severity]++
				}
				t.Logf("findings: %d info, %d warning, %d error",
					severities[formats.Info], severities[formats.Warning], severities[formats.Error])
			}

			again, err := importer.Import(context.Background(), source)
			if err != nil {
				t.Fatalf("second import: %v", err)
			}
			if !proto.Equal(model, again.Model) || !proto.Equal(result.Schedule, again.Schedule) {
				t.Error("two imports of one file produced different models")
			}
		})
	}
}

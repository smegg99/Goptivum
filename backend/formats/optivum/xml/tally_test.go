// formats/optivum/xml/tally_test.go

package xml_test

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/smegg99/goptivum/backend/formats"
	"github.com/smegg99/goptivum/backend/formats/optivum/pla"
	"github.com/smegg99/goptivum/backend/internal/testsupport"
)

// A finding the reader tallied says how many things it stands for, so the
// number a person reads is not buried in Detail (G10, G16).
func TestTalliedFindingsCarryTheirCount(t *testing.T) {
	// The fixture leaves one lesson unplaced; unplacing a second one makes the
	// count something a finding cannot get right by standing for one.
	body, err := os.ReadFile(testsupport.Fixture(t, filepath.Join("optivum", "plan-small.xml")))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	placed := `prz="mat" sa="102" dz="0" godz="2"`
	if !bytes.Contains(body, []byte(placed)) {
		t.Fatal("the fixture no longer places 2B's mathematics")
	}
	body = bytes.Replace(body, []byte(placed), []byte(`prz="mat" sa="102"`), 1)
	data, err := pla.Encode(pla.File{Magic: pla.Magic, XML: body, Compressed: true})
	if err != nil {
		t.Fatalf("wrap: %v", err)
	}
	result := importSource(t, formats.Open("plan-small.pla", data))

	onGrid := map[uint32]bool{}
	for _, scheduled := range result.Schedule.GetLessons() {
		if scheduled.GetPlacement() != nil {
			onGrid[scheduled.GetLessonId()] = true
		}
	}
	unplaced := 0
	for _, lesson := range result.Model.GetLessons() {
		if !onGrid[lesson.GetId()] {
			unplaced++
		}
	}
	if unplaced < 2 {
		t.Fatalf("%d unplaced lessons; this test needs several", unplaced)
	}

	found := false
	for _, finding := range result.Findings {
		switch finding.Kind {
		case formats.KindLessonUnplaced:
			found = true
			if finding.Count != unplaced {
				t.Errorf("lesson_unplaced count = %d, want the %d lessons with no placement", finding.Count, unplaced)
			}
		case formats.KindUnmappedElement, formats.KindUnmappedAttribute, formats.KindSplitKindInferred:
			if finding.Count < 1 {
				t.Errorf("%s count = %d, want what it tallied (%s)", finding.Kind, finding.Count, finding.Detail)
			}
		}
	}
	if !found {
		t.Error("no lesson_unplaced finding")
	}
}

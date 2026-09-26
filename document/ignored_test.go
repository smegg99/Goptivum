// document/ignored_test.go

package document_test

import (
	"testing"

	arrangov1 "github.com/smegg99/arrango/backend/gen/arrangov1"

	"github.com/smegg99/goptivum/document"
	goptivumv1 "github.com/smegg99/goptivum/document/gen/goptivumv1"
)

func entity(kind string, id uint32) *goptivumv1.FindingEntity {
	return &goptivumv1.FindingEntity{Kind: kind, Id: id}
}

func TestIgnoringMatchesKindEntitiesAndLessonsInAnyOrder(t *testing.T) {
	doc := document.New(&arrangov1.SchoolModel{Name: "Plan 2027"})
	doc.IgnoredFindings = []*goptivumv1.IgnoredFinding{{
		Kind:      "room_pool_overloaded",
		Entities:  []*goptivumv1.FindingEntity{entity("room", 3), entity("room", 1)},
		LessonIds: []uint32{9, 7},
	}, {
		Kind:     "teacher_overloaded",
		Entities: []*goptivumv1.FindingEntity{entity("teacher", 12)},
	}}
	ignoring := document.NewIgnoring(doc)

	cases := []struct {
		name    string
		finding *goptivumv1.IgnoredFinding
		want    bool
	}{
		{"same, reordered", &goptivumv1.IgnoredFinding{
			Kind:      "room_pool_overloaded",
			Entities:  []*goptivumv1.FindingEntity{entity("room", 1), entity("room", 3)},
			LessonIds: []uint32{7, 9},
		}, true},
		{"one lesson more", &goptivumv1.IgnoredFinding{
			Kind:      "room_pool_overloaded",
			Entities:  []*goptivumv1.FindingEntity{entity("room", 1), entity("room", 3)},
			LessonIds: []uint32{7, 9, 11},
		}, false},
		{"teacher", &goptivumv1.IgnoredFinding{
			Kind: "teacher_overloaded", Entities: []*goptivumv1.FindingEntity{entity("teacher", 12)},
		}, true},
		{"another teacher", &goptivumv1.IgnoredFinding{
			Kind: "teacher_overloaded", Entities: []*goptivumv1.FindingEntity{entity("teacher", 13)},
		}, false},
		{"same id, another kind of entity", &goptivumv1.IgnoredFinding{
			Kind: "teacher_overloaded", Entities: []*goptivumv1.FindingEntity{entity("room", 12)},
		}, false},
		{"another finding kind", &goptivumv1.IgnoredFinding{
			Kind: "availability_hole", Entities: []*goptivumv1.FindingEntity{entity("teacher", 12)},
		}, false},
	}
	for _, c := range cases {
		if got := ignoring.Ignores(c.finding); got != c.want {
			t.Errorf("%s: Ignores = %v, want %v", c.name, got, c.want)
		}
	}
	if got := ignoring.Len(); got != 2 {
		t.Errorf("Len = %d, want 2", got)
	}
}

// Two ids that print alike when joined must not read as one list.
func TestIgnoringTellsListsApartThatWouldJoinAlike(t *testing.T) {
	doc := document.New(&arrangov1.SchoolModel{Name: "Plan 2027"})
	doc.IgnoredFindings = []*goptivumv1.IgnoredFinding{{Kind: "locked_collision", LessonIds: []uint32{12}}}
	ignoring := document.NewIgnoring(doc)
	if ignoring.Ignores(&goptivumv1.IgnoredFinding{Kind: "locked_collision", LessonIds: []uint32{1, 2}}) {
		t.Error("lessons 1 and 2 match an ignore of lesson 12")
	}
}

func TestADocumentWithoutIgnoresIgnoresNothing(t *testing.T) {
	ignoring := document.NewIgnoring(nil)
	if ignoring.Ignores(&goptivumv1.IgnoredFinding{Kind: "invalid_model"}) || ignoring.Len() != 0 {
		t.Error("a nil document ignores something")
	}
}

func TestIgnoredFindingsSurviveTheCodec(t *testing.T) {
	doc := document.New(&arrangov1.SchoolModel{Name: "Plan 2027"})
	doc.IgnoredFindings = []*goptivumv1.IgnoredFinding{{
		Kind: "teacher_overloaded", Entities: []*goptivumv1.FindingEntity{entity("teacher", 12)},
	}}
	encoded, err := document.Encode(doc)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	decoded, err := document.Decode(encoded.Bytes)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !document.NewIgnoring(decoded).Ignores(doc.GetIgnoredFindings()[0]) {
		t.Error("the ignored finding did not survive the codec")
	}
}

func TestIgnoringMatchesExactTimeSpansInAnyOrder(t *testing.T) {
	monday := &arrangov1.TimeSpan{DayId: 1, StartPeriod: 3, PeriodSpan: 2}
	wednesday := &arrangov1.TimeSpan{DayId: 3, StartPeriod: 3, PeriodSpan: 2}
	finding := &goptivumv1.IgnoredFinding{Kind: "late_start", Entities: []*goptivumv1.FindingEntity{entity("division", 7)}, Spans: []*arrangov1.TimeSpan{monday}}
	doc := document.New(&arrangov1.SchoolModel{})
	doc.IgnoredFindings = []*goptivumv1.IgnoredFinding{finding}
	for _, spans := range [][]*arrangov1.TimeSpan{nil, {wednesday}, {{DayId: 1, StartPeriod: 4, PeriodSpan: 2}}, {{DayId: 1, StartPeriod: 3, PeriodSpan: 1}}} {
		other := &goptivumv1.IgnoredFinding{Kind: finding.Kind, Entities: finding.Entities, Spans: spans}
		if document.NewIgnoring(doc).Ignores(other) {
			t.Errorf("other occurrence ignored: %v", spans)
		}
	}
	finding.Spans = append(finding.Spans, wednesday)
	ignoring := document.NewIgnoring(doc)
	reordered := &goptivumv1.IgnoredFinding{Kind: finding.Kind, Entities: finding.Entities, Spans: []*arrangov1.TimeSpan{wednesday, monday}}
	if !ignoring.Ignores(reordered) {
		t.Fatal("span order changed identity")
	}
	encoded, err := document.Encode(doc)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := document.Decode(encoded.Bytes)
	if err != nil || !document.NewIgnoring(decoded).Ignores(reordered) {
		t.Fatalf("spans lost by codec: %v", err)
	}
}

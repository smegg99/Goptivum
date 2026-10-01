// formats/findings_test.go

package formats_test

import (
	"testing"

	arrangov1 "github.com/smegg99/arrango/backend/gen/arrangov1"

	"github.com/smegg99/goptivum/backend/formats"
)

// The same fact reported N times is one finding standing for N, so a person
// reads it once (G10, G16).
func TestMergeFoldsIdenticalFindingsIntoOneWithACount(t *testing.T) {
	teacher := formats.Ref(arrangov1.EntityKind_ENTITY_KIND_TEACHER, 7)
	merged := formats.Merge([]formats.Finding{
		{Kind: formats.KindSourceWarning, Severity: formats.Warning, Detail: "o1: day 1"},
		{Kind: formats.KindUnknownReference, Severity: formats.Warning, Entities: []*arrangov1.EntityRef{teacher}},
		{Kind: formats.KindSourceWarning, Severity: formats.Warning, Detail: "o2: day 3"},
		{Kind: formats.KindSourceWarning, Severity: formats.Warning, Detail: "o9: day 5"},
	})

	if len(merged) != 2 {
		t.Fatalf("merged = %d findings, want 2: %+v", len(merged), merged)
	}
	if merged[0].Kind != formats.KindSourceWarning || merged[0].Count != 3 {
		t.Errorf("first = %s x%d, want source_warning x3 in first-seen order", merged[0].Kind, merged[0].Count)
	}
	if merged[1].Kind != formats.KindUnknownReference || merged[1].Count != 1 {
		t.Errorf("second = %s x%d, want unknown_reference x1", merged[1].Kind, merged[1].Count)
	}
}

// Findings that differ in what a person reads stay apart: another entity or
// another severity is another fact.
func TestMergeKeepsDifferentFindingsApart(t *testing.T) {
	one := formats.Ref(arrangov1.EntityKind_ENTITY_KIND_TEACHER, 1)
	two := formats.Ref(arrangov1.EntityKind_ENTITY_KIND_TEACHER, 2)
	room := formats.Ref(arrangov1.EntityKind_ENTITY_KIND_ROOM, 1)
	merged := formats.Merge([]formats.Finding{
		{Kind: formats.KindUnknownReference, Severity: formats.Warning, Entities: []*arrangov1.EntityRef{one}},
		{Kind: formats.KindUnknownReference, Severity: formats.Warning, Entities: []*arrangov1.EntityRef{two}},
		{Kind: formats.KindUnknownReference, Severity: formats.Warning, Entities: []*arrangov1.EntityRef{room}},
		{Kind: formats.KindUnknownReference, Severity: formats.Info, Entities: []*arrangov1.EntityRef{one}},
		{Kind: formats.KindUnknownReference, Severity: formats.Warning},
		{Kind: formats.KindUnknownReference, Severity: formats.Warning, Entities: []*arrangov1.EntityRef{one, two}},
	})
	if len(merged) != 6 {
		t.Fatalf("merged = %d findings, want all 6 apart: %+v", len(merged), merged)
	}
	for _, finding := range merged {
		if finding.Count != 1 {
			t.Errorf("%s %s x%d, want 1", finding.Kind, finding.Severity, finding.Count)
		}
	}
}

// A reader that tallied a bulk loss says how many it stands for; merging adds
// the tallies, and an untallied finding counts as one.
func TestMergeAddsTallies(t *testing.T) {
	merged := formats.Merge([]formats.Finding{
		{Kind: formats.KindLessonUnplaced, Severity: formats.Info, Count: 80},
		{Kind: formats.KindLessonUnplaced, Severity: formats.Info},
		{Kind: formats.KindLessonUnplaced, Severity: formats.Info, Count: 4},
	})
	if len(merged) != 1 || merged[0].Count != 85 {
		t.Fatalf("merged = %+v, want one lesson_unplaced x85", merged)
	}
}

func TestMergeOfNothingIsEmpty(t *testing.T) {
	if merged := formats.Merge(nil); len(merged) != 0 {
		t.Errorf("merged = %+v, want none", merged)
	}
}

// document/ignored.go

package document

import (
	"slices"
	"strconv"
	"strings"

	goptivumv1 "github.com/smegg99/goptivum/document/gen/goptivumv1"
)

// Ignoring is the set of check findings one document ignores, built once to match many findings.
type Ignoring struct {
	keys map[string]bool
}

// NewIgnoring reads a document's ignored findings; a nil document ignores nothing.
func NewIgnoring(doc *goptivumv1.Document) Ignoring {
	keys := make(map[string]bool, len(doc.GetIgnoredFindings()))
	for _, ignored := range doc.GetIgnoredFindings() {
		keys[findingKey(ignored)] = true
	}
	return Ignoring{keys: keys}
}

// Ignores reports whether one ignored finding has this kind, these entities, lessons and exact time spans, in any order.
func (i Ignoring) Ignores(finding *goptivumv1.IgnoredFinding) bool {
	return i.keys[findingKey(finding)]
}

// Len is how many distinct findings the document ignores.
func (i Ignoring) Len() int { return len(i.keys) }

// findingKey closes each sorted list so reordered identities match without joining different lists.
func findingKey(finding *goptivumv1.IgnoredFinding) string {
	entities := make([]string, 0, len(finding.GetEntities()))
	for _, entity := range finding.GetEntities() {
		entities = append(entities, entity.GetKind()+":"+strconv.FormatUint(uint64(entity.GetId()), 10))
	}
	slices.Sort(entities)
	lessons := slices.Clone(finding.GetLessonIds())
	slices.Sort(lessons)

	var key strings.Builder
	key.WriteString(finding.GetKind())
	key.WriteString("|")
	key.WriteString(strings.Join(entities, ","))
	key.WriteString("|")
	for n, id := range lessons {
		if n > 0 {
			key.WriteString(",")
		}
		key.WriteString(strconv.FormatUint(uint64(id), 10))
	}
	spans := make([]string, 0, len(finding.GetSpans()))
	for _, span := range finding.GetSpans() {
		spans = append(spans, strconv.FormatUint(uint64(span.GetDayId()), 10)+":"+strconv.FormatUint(uint64(span.GetStartPeriod()), 10)+":"+strconv.FormatUint(uint64(span.GetPeriodSpan()), 10))
	}
	slices.Sort(spans)
	key.WriteString("|")
	key.WriteString(strings.Join(spans, ","))
	return key.String()
}

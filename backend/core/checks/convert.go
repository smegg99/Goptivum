// core/checks/convert.go

package checks

import (
	"regexp"
	"strings"

	arrangorpcv1 "github.com/smegg99/arrango/backend/gen/arrangorpcv1"
	arrangov1 "github.com/smegg99/arrango/backend/gen/arrangov1"

	"github.com/smegg99/goptivum/document"
	goptivumv1 "github.com/smegg99/goptivum/document/gen/goptivumv1"
)

// Severity is how much a finding matters to the person reading the check.
type Severity string

const (
	SeverityError   Severity = "error"
	SeverityWarning Severity = "warning"
	SeverityNote    Severity = "note"
)

// Part identifies the source of a finding.
type Part string

const (
	PartInput     Part = "input"
	PartTimetable Part = "timetable"
	PartRule      Part = "rule"
)

// Report is one version's check: every finding, the counts of those not ignored, and the rating.
type Report struct {
	Findings []Finding
	Errors   int
	Warnings int
	Notes    int
	Ignored  int
	// Score is nil unless the version's timetable places every lesson.
	Score *Score
}

// Finding is one problem, located, without the node's English message.
type Finding struct {
	Part     Part
	Kind     string
	Severity Severity
	Ignored  bool
	Entities []Entity
	Lessons  []Lesson
	Spans    []Span
	// Needed and Available are set together, for teacher_overloaded and room_pool_overloaded only.
	Needed    *uint32
	Available *uint32
	// Count is a soft issue's size; nil on hard findings.
	Count *uint32
}

// Entity is a typed id in the document, in the contract's words.
type Entity struct {
	Kind string
	ID   uint32
}

// Lesson is a lesson a finding names; the placement fields mean something only when Placed.
type Lesson struct {
	ID          uint32
	Placed      bool
	DayID       uint32
	StartPeriod uint32
	Duration    uint32
	RoomID      uint32
}

// Span is a run of periods on one day.
type Span struct {
	DayID       uint32
	StartPeriod uint32
	PeriodSpan  uint32
}

// Score is the node's rating of a complete timetable.
type Score struct {
	Rating          float64
	Quality         float64
	StudentsQuality float64
	TeachersQuality float64
	Metrics         []Metric
	Classes         []EntityScore
	Teachers        []EntityScore
}

// Metric is one school-level rating metric.
type Metric struct {
	Key        string
	Teachers   bool
	Applicable bool
	Rate       float64
	Subscore   float64
	Count      int64
}

// EntityScore is one class's or teacher's rating and what lowered it.
type EntityScore struct {
	ID      uint32
	Rating  float64
	Quality float64
	Items   []ScoreItem
}

// ScoreItem is how often one soft issue category hit an entity.
type ScoreItem struct {
	Category string
	Count    uint32
}

// Verdict sorting: stability is uncounted; two hygiene categories are notes.
var (
	preferenceCategories = map[string]bool{"stability": true}
	infoCategories       = map[string]bool{"start_variance": true, "subject_always_last": true}
)

// kindPattern is what the contract allows a finding kind to be.
var kindPattern = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)

// Convert maps the node's answer onto findings in the contract's words, input findings first, and marks what the document ignores.
func Convert(answer *arrangorpcv1.CheckReport, doc *goptivumv1.Document) Report {
	ignoring := document.NewIgnoring(doc)
	var findings []Finding
	for _, conflict := range answer.GetHard() {
		findings = append(findings, conflictFinding(conflict, SeverityError, PartInput))
	}
	for _, conflict := range answer.GetAdvisory() {
		findings = append(findings, conflictFinding(conflict, SeverityWarning, PartInput))
	}
	rated := answer.GetValidation() != nil
	if rated {
		for _, conflict := range answer.GetValidation().GetConflicts() {
			findings = append(findings, conflictFinding(conflict, SeverityError, PartTimetable))
		}
		for _, issue := range answer.GetScore().GetSoftIssues() {
			if severity, counted := softSeverity(issue); counted {
				findings = append(findings, issueFinding(issue, severity, PartRule))
			}
		}
		for _, issue := range answer.GetScore().GetInfoIssues() {
			if infoCategories[issue.GetCategory()] {
				findings = append(findings, issueFinding(issue, SeverityNote, PartTimetable))
			}
		}
	}

	report := Report{Findings: findings}
	for n := range report.Findings {
		finding := &report.Findings[n]
		finding.Ignored = ignoring.Ignores(identity(*finding))
		switch {
		case finding.Ignored:
			report.Ignored++
		case finding.Severity == SeverityError:
			report.Errors++
		case finding.Severity == SeverityWarning:
			report.Warnings++
		default:
			report.Notes++
		}
	}
	if rated && answer.GetScore() != nil {
		report.Score = scoreOf(answer.GetScore())
		capRatings(report.Score, report.Findings, doc.GetModel(), report.Errors)
	}
	return report
}

// softSeverity sorts a soft issue the way verdict.cc does; false means the verdict does not count it.
func softSeverity(issue *arrangov1.SoftIssue) (Severity, bool) {
	switch {
	case issue.GetCount() == 0:
		return "", false
	case issue.GetConfigHard():
		return SeverityError, true
	case preferenceCategories[issue.GetCategory()]:
		return "", false
	case infoCategories[issue.GetCategory()]:
		return SeverityNote, true
	default:
		return SeverityWarning, true
	}
}

// Drift is this server's timetable counts beside the node's verdict when they differ.
type Drift struct {
	Errors, Warnings, Notes                      int
	VerdictErrors, VerdictWarnings, VerdictInfos int
}

// verdictDrift compares the timetable's counts, before ignores, with the node's verdict; false when there is no verdict or they agree.
func verdictDrift(answer *arrangorpcv1.CheckReport) (Drift, bool) {
	verdict := answer.GetVerdict()
	if answer.GetValidation() == nil || verdict == nil {
		return Drift{}, false
	}
	drift := Drift{
		Errors:          len(answer.GetValidation().GetConflicts()),
		VerdictErrors:   int(verdict.GetErrors()),
		VerdictWarnings: int(verdict.GetWarnings()),
		VerdictInfos:    int(verdict.GetInfos()),
	}
	for _, issue := range answer.GetScore().GetSoftIssues() {
		severity, counted := softSeverity(issue)
		switch {
		case !counted:
		case severity == SeverityError:
			drift.Errors++
		case severity == SeverityWarning:
			drift.Warnings++
		default:
			drift.Notes++
		}
	}
	for _, issue := range answer.GetScore().GetInfoIssues() {
		if infoCategories[issue.GetCategory()] {
			drift.Notes++
		}
	}
	agree := drift.Errors == drift.VerdictErrors && drift.Warnings == drift.VerdictWarnings &&
		drift.Notes == drift.VerdictInfos
	return drift, !agree
}

func conflictFinding(conflict *arrangov1.Conflict, severity Severity, part Part) Finding {
	kind := conflict.GetKind()
	finding := Finding{
		Part:     part,
		Kind:     kindName(strings.TrimPrefix(kind.String(), "KIND_")),
		Severity: severity,
		Entities: entitiesOf(conflict.GetEntities(), unspecifiedAs(kind)),
		Lessons:  lessonsOf(conflict.GetLessons()),
		Spans:    spansOf(conflict.GetSpans()),
	}
	if kind == arrangov1.Conflict_KIND_TEACHER_OVERLOADED || kind == arrangov1.Conflict_KIND_ROOM_POOL_OVERLOADED {
		needed, available := conflict.GetNeeded(), conflict.GetAvailable()
		finding.Needed, finding.Available = &needed, &available
	}
	return finding
}

func issueFinding(issue *arrangov1.SoftIssue, severity Severity, part Part) Finding {
	count := issue.GetCount()
	return Finding{
		Part:     part,
		Kind:     kindName(issue.GetCategory()),
		Severity: severity,
		Entities: entitiesOf(issue.GetEntities(), ""),
		Lessons:  lessonsOf(issue.GetLessons()),
		Spans:    spansOf(issue.GetSpans()),
		Count:    &count,
	}
}

// kindName lowers an enum name; a value this build does not know prints as a number and becomes unknown.
func kindName(name string) string {
	name = strings.ToLower(name)
	if !kindPattern.MatchString(name) {
		return "unknown"
	}
	return name
}

// unspecifiedAs names the id the node sends with no entity kind: a split for a group size mismatch, a lesson link for link findings.
func unspecifiedAs(kind arrangov1.Conflict_Kind) string {
	switch kind {
	case arrangov1.Conflict_KIND_GROUP_SIZE_MISMATCH:
		return "split"
	case arrangov1.Conflict_KIND_LINK_SAME_DAY, arrangov1.Conflict_KIND_LINK_DIFFERENT_DAY,
		arrangov1.Conflict_KIND_LINK_CONSECUTIVE, arrangov1.Conflict_KIND_INVALID_RULE_CONFIG:
		return "link"
	}
	return ""
}

var entityKinds = map[arrangov1.EntityKind]string{
	arrangov1.EntityKind_ENTITY_KIND_DIVISION:       "division",
	arrangov1.EntityKind_ENTITY_KIND_GROUP:          "group",
	arrangov1.EntityKind_ENTITY_KIND_TEACHER:        "teacher",
	arrangov1.EntityKind_ENTITY_KIND_ROOM:           "room",
	arrangov1.EntityKind_ENTITY_KIND_SUBJECT:        "subject",
	arrangov1.EntityKind_ENTITY_KIND_YEAR:           "year",
	arrangov1.EntityKind_ENTITY_KIND_EXTERNAL_BLOCK: "external_block",
}

// entitiesOf drops a ref it cannot name rather than send an id with no kind.
func entitiesOf(refs []*arrangov1.EntityRef, unspecified string) []Entity {
	var out []Entity
	for _, ref := range refs {
		kind, known := entityKinds[ref.GetKind()]
		if ref.GetKind() == arrangov1.EntityKind_ENTITY_KIND_UNSPECIFIED {
			kind, known = unspecified, unspecified != ""
		}
		if known {
			out = append(out, Entity{Kind: kind, ID: ref.GetId()})
		}
	}
	return out
}

// lessonsOf keeps each structured lesson reference once.
func lessonsOf(refs []*arrangov1.LessonRef) []Lesson {
	var out []Lesson
	seen := map[uint32]bool{}
	for _, ref := range refs {
		if seen[ref.GetLessonId()] {
			continue
		}
		seen[ref.GetLessonId()] = true
		out = append(out, Lesson{
			ID: ref.GetLessonId(), Placed: ref.GetDayId() != 0,
			DayID: ref.GetDayId(), StartPeriod: ref.GetStartPeriod(),
			Duration: ref.GetDuration(), RoomID: ref.GetRoomId(),
		})
	}
	return out
}

func spansOf(spans []*arrangov1.TimeSpan) []Span {
	var out []Span
	for _, span := range spans {
		out = append(out, Span{DayID: span.GetDayId(), StartPeriod: span.GetStartPeriod(), PeriodSpan: span.GetPeriodSpan()})
	}
	return out
}

// identity is what an ignored finding in the document is matched against.
func identity(finding Finding) *goptivumv1.IgnoredFinding {
	out := &goptivumv1.IgnoredFinding{Kind: finding.Kind}
	for _, entity := range finding.Entities {
		out.Entities = append(out.Entities, &goptivumv1.FindingEntity{Kind: entity.Kind, Id: entity.ID})
	}
	for _, lesson := range finding.Lessons {
		out.LessonIds = append(out.LessonIds, lesson.ID)
	}
	for _, span := range finding.Spans {
		out.Spans = append(out.Spans, &arrangov1.TimeSpan{DayId: span.DayID, StartPeriod: span.StartPeriod, PeriodSpan: span.PeriodSpan})
	}
	return out
}

func scoreOf(score *arrangov1.ScoreReport) *Score {
	out := &Score{
		Quality:         score.GetOverallQuality(),
		StudentsQuality: score.GetAllStudentsQuality(),
		TeachersQuality: score.GetAllTeachersQuality(),
		Metrics:         []Metric{},
		Classes:         entityScores(score.GetDivisionScores()),
		Teachers:        entityScores(score.GetTeacherScores()),
	}
	for _, metric := range score.GetMetricScores() {
		out.Metrics = append(out.Metrics, Metric{
			Key: metric.GetKey(), Teachers: metric.GetTeachers(), Applicable: metric.GetApplicable(),
			Rate: metric.GetRate(), Subscore: metric.GetSubscore(), Count: metric.GetCount(),
		})
	}
	return out
}

func entityScores(scores []*arrangov1.EntityScore) []EntityScore {
	out := []EntityScore{}
	for _, score := range scores {
		items := []ScoreItem{}
		for _, item := range score.GetItems() {
			items = append(items, ScoreItem{Category: item.GetCategory(), Count: item.GetCount()})
		}
		out = append(out, EntityScore{ID: score.GetEntityId(), Quality: score.GetQuality(), Items: items})
	}
	return out
}

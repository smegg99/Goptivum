// core/checks/convert_test.go

package checks

import (
	"reflect"
	"testing"

	arrangorpcv1 "github.com/smegg99/arrango/backend/gen/arrangorpcv1"
	arrangov1 "github.com/smegg99/arrango/backend/gen/arrangov1"

	"github.com/smegg99/goptivum/document"
	goptivumv1 "github.com/smegg99/goptivum/document/gen/goptivumv1"
)

func ref(kind arrangov1.EntityKind, id uint32) *arrangov1.EntityRef {
	return &arrangov1.EntityRef{Kind: kind, Id: id}
}

func uint32p(value uint32) *uint32 { return &value }

func TestFindingPartsDistinguishInputTimetableAndRules(t *testing.T) {
	report := Convert(&arrangorpcv1.CheckReport{
		Hard:       []*arrangov1.Conflict{{Kind: arrangov1.Conflict_KIND_INVALID_MODEL}},
		Advisory:   []*arrangov1.Conflict{{Kind: arrangov1.Conflict_KIND_AVAILABILITY_HOLE}},
		Validation: &arrangov1.ValidationReport{Conflicts: []*arrangov1.Conflict{{Kind: arrangov1.Conflict_KIND_TEACHER_CONFLICT}}},
		Score: &arrangov1.ScoreReport{
			SoftIssues: []*arrangov1.SoftIssue{{Category: "student_gap", Count: 1}},
			InfoIssues: []*arrangov1.SoftIssue{{Category: "start_variance", Count: 1}},
		},
	}, nil)
	want := []Part{PartInput, PartInput, PartTimetable, PartRule, PartTimetable}
	if len(report.Findings) != len(want) {
		t.Fatalf("findings: %+v", report.Findings)
	}
	for n, finding := range report.Findings {
		if finding.Part != want[n] {
			t.Errorf("finding %d: part %q, want %q", n, finding.Part, want[n])
		}
	}
}

// The test school's Kowalska with 1b's Matematyka at 23: needed 31, has 30.
func TestAnOverloadedTeacherIsOneErrorWithItsNumbers(t *testing.T) {
	report := Convert(&arrangorpcv1.CheckReport{Hard: []*arrangov1.Conflict{{
		Kind:     arrangov1.Conflict_KIND_TEACHER_OVERLOADED,
		Message:  "teacher Kowalska needs 31 periods but 30 are available",
		EntityId: 12,
		Entities: []*arrangov1.EntityRef{ref(arrangov1.EntityKind_ENTITY_KIND_TEACHER, 12)},
		Needed:   31, Available: 30,
	}}}, nil)

	want := Report{
		Findings: []Finding{{
			Part: PartInput, Kind: "teacher_overloaded", Severity: SeverityError,
			Entities: []Entity{{Kind: "teacher", ID: 12}},
			Needed:   uint32p(31), Available: uint32p(30),
		}},
		Errors: 1,
	}
	if !reflect.DeepEqual(report, want) {
		t.Fatalf("report = %+v\nwant     %+v", report, want)
	}
}

// Zero periods free is a finding's real number, not an absent one.
func TestARoomPoolWithNoRoomCarriesZeroAvailable(t *testing.T) {
	report := Convert(&arrangorpcv1.CheckReport{Hard: []*arrangov1.Conflict{{
		Kind:      arrangov1.Conflict_KIND_ROOM_POOL_OVERLOADED,
		LessonIds: []uint32{7, 8},
		Lessons:   []*arrangov1.LessonRef{{LessonId: 7, Duration: 1}, {LessonId: 8, Duration: 2}},
		Needed:    3,
	}}}, nil)
	finding := report.Findings[0]
	if finding.Available == nil || *finding.Available != 0 || finding.Needed == nil || *finding.Needed != 3 {
		t.Fatalf("needed/available = %v/%v, want 3/0", finding.Needed, finding.Available)
	}
	wantLessons := []Lesson{{ID: 7, Duration: 1}, {ID: 8, Duration: 2}}
	if !reflect.DeepEqual(finding.Lessons, wantLessons) {
		t.Errorf("lessons = %+v, want %+v", finding.Lessons, wantLessons)
	}
}

// A school the node could not take names nothing and is never rated.
func TestAnInvalidModelIsOneUnlocatedError(t *testing.T) {
	report := Convert(&arrangorpcv1.CheckReport{Hard: []*arrangov1.Conflict{{
		Kind: arrangov1.Conflict_KIND_INVALID_MODEL, Message: "model calendar is empty",
	}}}, nil)
	want := Report{Findings: []Finding{{Part: PartInput, Kind: "invalid_model", Severity: SeverityError}}, Errors: 1}
	if !reflect.DeepEqual(report, want) {
		t.Fatalf("report = %+v, want %+v", report, want)
	}
}

// Lessons named only by id are listed too, unplaced; placed ones keep where they sit.
func TestLessonsAreTheUnionOfIdsAndRefs(t *testing.T) {
	report := Convert(&arrangorpcv1.CheckReport{Hard: []*arrangov1.Conflict{{
		Kind:      arrangov1.Conflict_KIND_LOCKED_COLLISION,
		LessonIds: []uint32{4, 5},
		Lessons:   []*arrangov1.LessonRef{{LessonId: 5, DayId: 2, StartPeriod: 0, Duration: 1, RoomId: 9}},
	}}}, nil)
	want := []Lesson{{ID: 5, Placed: true, DayID: 2, StartPeriod: 0, Duration: 1, RoomID: 9}, {ID: 4}}
	if got := report.Findings[0].Lessons; !reflect.DeepEqual(got, want) {
		t.Fatalf("lessons = %+v, want %+v", got, want)
	}
}

// The node has no entity kind for splits and links and sends them unspecified;
// the finding's kind says which one it is.
func TestUnspecifiedRefsAreNamedFromTheFindingKind(t *testing.T) {
	unspecified := ref(arrangov1.EntityKind_ENTITY_KIND_UNSPECIFIED, 40)
	report := Convert(&arrangorpcv1.CheckReport{
		Hard: []*arrangov1.Conflict{
			{Kind: arrangov1.Conflict_KIND_GROUP_SIZE_MISMATCH, Entities: []*arrangov1.EntityRef{unspecified}},
			{Kind: arrangov1.Conflict_KIND_LINK_SAME_DAY, Entities: []*arrangov1.EntityRef{unspecified}},
			{Kind: arrangov1.Conflict_KIND_INVALID_RULE_CONFIG, Entities: []*arrangov1.EntityRef{unspecified}},
			{Kind: arrangov1.Conflict_KIND_INVALID_RULE_CONFIG, Entities: []*arrangov1.EntityRef{ref(arrangov1.EntityKind_ENTITY_KIND_TEACHER, 12)}},
		},
		Advisory: []*arrangov1.Conflict{
			{Kind: arrangov1.Conflict_KIND_AVAILABILITY_HOLE, Entities: []*arrangov1.EntityRef{
				ref(arrangov1.EntityKind_ENTITY_KIND_EXTERNAL_BLOCK, 50), unspecified,
			}},
		},
	}, nil)

	want := [][]Entity{
		{{Kind: "split", ID: 40}},
		{{Kind: "link", ID: 40}},
		{{Kind: "link", ID: 40}},
		{{Kind: "teacher", ID: 12}},
		{{Kind: "external_block", ID: 50}},
	}
	for n, finding := range report.Findings {
		if !reflect.DeepEqual(finding.Entities, want[n]) {
			t.Errorf("finding %d (%s) entities = %+v, want %+v", n, finding.Kind, finding.Entities, want[n])
		}
	}
	if report.Errors != 4 || report.Warnings != 1 {
		t.Errorf("errors/warnings = %d/%d, want 4/1", report.Errors, report.Warnings)
	}
}

// verdict.cc's sorting, mirrored: the counts over a timetable match the node's
// own verdict, and preference categories and empty issues count nowhere.
func TestATimetableIsSortedTheWayTheNodesVerdictIs(t *testing.T) {
	division := []*arrangov1.EntityRef{ref(arrangov1.EntityKind_ENTITY_KIND_DIVISION, 3)}
	answer := &arrangorpcv1.CheckReport{
		Validation: &arrangov1.ValidationReport{Conflicts: []*arrangov1.Conflict{{
			Kind: arrangov1.Conflict_KIND_TEACHER_CONFLICT, LessonIds: []uint32{1, 2},
		}}},
		Score: &arrangov1.ScoreReport{
			SoftIssues: []*arrangov1.SoftIssue{
				{Category: "student_gap", Count: 2, Entities: division,
					Spans: []*arrangov1.TimeSpan{{DayId: 1, StartPeriod: 2, PeriodSpan: 2}}},
				{Category: "late_student", Count: 1, ConfigHard: true, Entities: division},
				{Category: "stability", Count: 4},
				{Category: "teacher_gap", Count: 0},
				{Category: "late_start", Count: 1},
			},
			InfoIssues: []*arrangov1.SoftIssue{{Category: "subject_always_last", Count: 1}},
		},
		Verdict: &arrangorpcv1.ScheduleVerdict{
			Tier: arrangorpcv1.ScheduleVerdict_TIER_ERRORS, Errors: 2, Warnings: 2, Infos: 1, ConfigHardErrors: 1,
		},
	}
	report := Convert(answer, nil)

	kinds := []string{}
	for _, finding := range report.Findings {
		kinds = append(kinds, finding.Kind+":"+string(finding.Severity))
	}
	wantKinds := []string{
		"teacher_conflict:error", "student_gap:warning", "late_student:error",
		"late_start:warning", "subject_always_last:note",
	}
	if !reflect.DeepEqual(kinds, wantKinds) {
		t.Fatalf("findings = %v, want %v", kinds, wantKinds)
	}
	if report.Errors != 2 || report.Warnings != 2 || report.Notes != 1 {
		t.Errorf("counts = %d/%d/%d, want the verdict's 2/1/2", report.Errors, report.Warnings, report.Notes)
	}
	if drift, drifted := verdictDrift(answer); drifted {
		t.Errorf("drift = %+v, want none", drift)
	}
	gap := report.Findings[1]
	if gap.Count == nil || *gap.Count != 2 || !reflect.DeepEqual(gap.Spans, []Span{{DayID: 1, StartPeriod: 2, PeriodSpan: 2}}) {
		t.Errorf("gap = %+v, want its count and span", gap)
	}
}

func TestADriftingVerdictIsNoticed(t *testing.T) {
	answer := &arrangorpcv1.CheckReport{
		Validation: &arrangov1.ValidationReport{Valid: true},
		Score: &arrangov1.ScoreReport{SoftIssues: []*arrangov1.SoftIssue{
			{Category: "a_new_hygiene_category", Count: 1},
		}},
		Verdict: &arrangorpcv1.ScheduleVerdict{Infos: 1},
	}
	drift, drifted := verdictDrift(answer)
	if !drifted || drift.Warnings != 1 || drift.Notes != 0 || drift.VerdictWarnings != 0 || drift.VerdictInfos != 1 {
		t.Errorf("drift = %+v (drifted %v), want our warning against the node's info", drift, drifted)
	}
	if _, drifted := verdictDrift(&arrangorpcv1.CheckReport{}); drifted {
		t.Error("a report with no verdict drifted")
	}
}

// Ignored findings stay listed, marked, and leave the counts.
func TestIgnoredFindingsAreMarkedAndNotCounted(t *testing.T) {
	doc := document.New(&arrangov1.SchoolModel{Name: "Plan 2027"})
	doc.IgnoredFindings = []*goptivumv1.IgnoredFinding{{
		Kind:     "teacher_overloaded",
		Entities: []*goptivumv1.FindingEntity{{Kind: "teacher", Id: 12}},
	}}
	report := Convert(&arrangorpcv1.CheckReport{
		Hard: []*arrangov1.Conflict{
			{Kind: arrangov1.Conflict_KIND_TEACHER_OVERLOADED, Needed: 31, Available: 30,
				Entities: []*arrangov1.EntityRef{ref(arrangov1.EntityKind_ENTITY_KIND_TEACHER, 12)}},
			{Kind: arrangov1.Conflict_KIND_TEACHER_OVERLOADED, Needed: 40, Available: 30,
				Entities: []*arrangov1.EntityRef{ref(arrangov1.EntityKind_ENTITY_KIND_TEACHER, 13)}},
		},
	}, doc)
	if !report.Findings[0].Ignored || report.Findings[1].Ignored {
		t.Fatalf("ignored = %v/%v, want the first only", report.Findings[0].Ignored, report.Findings[1].Ignored)
	}
	if report.Errors != 1 || report.Ignored != 1 {
		t.Errorf("errors/ignored = %d/%d, want 1/1", report.Errors, report.Ignored)
	}
}

func TestARatedTimetableCarriesItsScores(t *testing.T) {
	report := Convert(&arrangorpcv1.CheckReport{
		Validation: &arrangov1.ValidationReport{Valid: true},
		Score: &arrangov1.ScoreReport{
			OverallQuality: 91.5, AllStudentsQuality: 90, AllTeachersQuality: 94,
			TotalPenalty: 1234,
			DivisionScores: []*arrangov1.EntityScore{{EntityId: 3, Name: "1a", Quality: 88, Penalty: 40,
				Items: []*arrangov1.PenaltyItem{{Category: "student_gap", Penalty: 40, Count: 2}}}},
			TeacherScores: []*arrangov1.EntityScore{{EntityId: 12, Quality: 99}},
			MetricScores: []*arrangov1.MetricScore{
				{Key: "gaps", Applicable: true, Rate: 0.1, Subscore: 93, Count: 2},
				{Key: "t_late", Teachers: true, Applicable: false},
			},
		},
		Verdict: &arrangorpcv1.ScheduleVerdict{Tier: arrangorpcv1.ScheduleVerdict_TIER_PRISTINE},
	}, nil)
	want := &Score{
		Rating: 91.5, Quality: 91.5, StudentsQuality: 90, TeachersQuality: 94,
		Metrics: []Metric{
			{Key: "gaps", Applicable: true, Rate: 0.1, Subscore: 93, Count: 2},
			{Key: "t_late", Teachers: true},
		},
		Classes:  []EntityScore{{ID: 3, Rating: 88, Quality: 88, Items: []ScoreItem{{Category: "student_gap", Count: 2}}}},
		Teachers: []EntityScore{{ID: 12, Rating: 99, Quality: 99, Items: []ScoreItem{}}},
	}
	if !reflect.DeepEqual(report.Score, want) {
		t.Fatalf("score = %+v\nwant    %+v", report.Score, want)
	}

	// A timetable the node did not rate has no score, whatever else came back.
	unrated := Convert(&arrangorpcv1.CheckReport{Score: &arrangov1.ScoreReport{OverallQuality: 50}}, nil)
	if unrated.Score != nil {
		t.Errorf("an unvalidated report has a score: %+v", unrated.Score)
	}
}

func TestAKindThisBuildDoesNotKnowIsUnknown(t *testing.T) {
	report := Convert(&arrangorpcv1.CheckReport{
		Advisory: []*arrangov1.Conflict{{Kind: arrangov1.Conflict_Kind(999)}},
	}, nil)
	if got := report.Findings[0].Kind; got != "unknown" {
		t.Errorf("kind = %q, want unknown", got)
	}
}

func TestConvertMatchesIgnoredTimesWithoutHidingOtherDays(t *testing.T) {
	monday := &arrangov1.TimeSpan{DayId: 1, StartPeriod: 0, PeriodSpan: 3}
	wednesday := &arrangov1.TimeSpan{DayId: 3, StartPeriod: 0, PeriodSpan: 3}
	doc := document.New(&arrangov1.SchoolModel{})
	doc.IgnoredFindings = []*goptivumv1.IgnoredFinding{{Kind: "late_start", Entities: []*goptivumv1.FindingEntity{{Kind: "division", Id: 7}}, Spans: []*arrangov1.TimeSpan{monday}}}
	report := Convert(&arrangorpcv1.CheckReport{Validation: &arrangov1.ValidationReport{}, Score: &arrangov1.ScoreReport{SoftIssues: []*arrangov1.SoftIssue{
		{Category: "late_start", Count: 1, Entities: []*arrangov1.EntityRef{ref(arrangov1.EntityKind_ENTITY_KIND_DIVISION, 7)}, Spans: []*arrangov1.TimeSpan{monday}},
		{Category: "late_start", Count: 1, Entities: []*arrangov1.EntityRef{ref(arrangov1.EntityKind_ENTITY_KIND_DIVISION, 7)}, Spans: []*arrangov1.TimeSpan{wednesday}},
	}}}, doc)
	if report.Warnings != 1 || report.Ignored != 1 || !report.Findings[0].Ignored || report.Findings[1].Ignored {
		t.Fatalf("report: %+v", report)
	}
}

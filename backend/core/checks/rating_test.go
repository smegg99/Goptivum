// backend/core/checks/rating_test.go
package checks

import (
	arrangorpcv1 "github.com/smegg99/arrango/backend/gen/arrangorpcv1"
	arrangov1 "github.com/smegg99/arrango/backend/gen/arrangov1"
	"github.com/smegg99/goptivum/document"
	"testing"
)

func TestRatingAttributesErrorsOncePerEntityAndLiftsOnlyIgnoredCaps(t *testing.T) {
	doc := document.New(&arrangov1.SchoolModel{
		Lessons: []*arrangov1.LessonInstance{{Id: 1, TeacherId: 10, SupportTeacherId: 11,
			Participants: []*arrangov1.Participant{{DivisionId: 2}, {DivisionId: 3}}}},
	})
	answer := &arrangorpcv1.CheckReport{
		Hard: []*arrangov1.Conflict{{Kind: arrangov1.Conflict_KIND_TEACHER_OVERLOADED,
			Entities: []*arrangov1.EntityRef{ref(arrangov1.EntityKind_ENTITY_KIND_TEACHER, 10)}}},
		Validation: &arrangov1.ValidationReport{Conflicts: []*arrangov1.Conflict{
			{Kind: arrangov1.Conflict_KIND_LOCKED_COLLISION, LessonIds: []uint32{1, 1},
				Entities: []*arrangov1.EntityRef{ref(arrangov1.EntityKind_ENTITY_KIND_DIVISION, 2)}},
		}},
		Score: &arrangov1.ScoreReport{
			OverallQuality: 96,
			DivisionScores: []*arrangov1.EntityScore{{EntityId: 2, Quality: 90}, {EntityId: 3, Quality: 95}, {EntityId: 4, Quality: 97}},
			TeacherScores:  []*arrangov1.EntityScore{{EntityId: 10, Quality: 96}, {EntityId: 11, Quality: 97}, {EntityId: 12, Quality: 98}},
		},
	}
	report := Convert(answer, doc)
	if report.Score.Rating != 40 || report.Score.Quality != 96 {
		t.Fatalf("school score: %+v", report.Score)
	}
	wantClasses, wantTeachers := []float64{45, 45, 97}, []float64{40, 45, 98}
	for n, score := range report.Score.Classes {
		if score.Rating != wantClasses[n] {
			t.Errorf("class score: %+v", score)
		}
	}
	for n, score := range report.Score.Teachers {
		if score.Rating != wantTeachers[n] {
			t.Errorf("teacher score: %+v", score)
		}
	}
	doc.IgnoredFindings = append(doc.IgnoredFindings, identity(report.Findings[1]))
	report = Convert(answer, doc)
	if report.Errors != 1 || report.Score.Rating != 45 || report.Score.Classes[0].Rating != 90 || report.Score.Teachers[0].Rating != 45 || report.Score.Teachers[1].Rating != 97 {
		t.Fatalf("ignored collision: %+v", report)
	}
	doc.IgnoredFindings = append(doc.IgnoredFindings, identity(report.Findings[0]))
	report = Convert(answer, doc)
	if report.Errors != 0 || report.Score.Rating != 96 || report.Score.Teachers[0].Rating != 96 {
		t.Fatalf("all ignored: %+v", report.Score)
	}
}

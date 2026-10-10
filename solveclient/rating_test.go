// solveclient/rating_test.go
package solveclient

import (
	arrangov1 "github.com/smegg99/arrango/backend/gen/arrangov1"
	"reflect"
	"testing"
)

func TestRating(t *testing.T) {
	for _, item := range []struct {
		quality float64
		errors  int
		want    float64
	}{
		{96, 0, 96}, {96, 1, 45}, {96, 2, 40}, {37, 2, 37}, {100, 10, 0}, {96, 100, 0},
	} {
		if got := Rating(item.quality, item.errors); got != item.want {
			t.Errorf("Rating(%v, %d) = %v, want %v", item.quality, item.errors, got, item.want)
		}
	}
}

func TestFindingEntitiesIncludesParallelClassesAndSupportOnce(t *testing.T) {
	model := &arrangov1.SchoolModel{
		Groups: []*arrangov1.Group{{Id: 30, DivisionId: 3}},
		Splits: []*arrangov1.Split{{Id: 50, DivisionId: 5}},
		Lessons: []*arrangov1.LessonInstance{
			{Id: 1, TeacherId: 10, SupportTeacherId: 11, Participants: []*arrangov1.Participant{{DivisionId: 2}, {GroupId: 30}}},
			{Id: 2, TeacherId: 12, Participants: []*arrangov1.Participant{{DivisionId: 6}}},
		},
	}
	classes, teachers := NewFindingIndex(model).Entities(map[string][]uint32{
		"division": {2}, "teacher": {10}, "split": {50},
	}, []uint32{1, 1, 99})
	if !reflect.DeepEqual(classes, map[uint32]bool{2: true, 3: true, 5: true}) || !reflect.DeepEqual(teachers, map[uint32]bool{10: true, 11: true}) {
		t.Fatalf("classes %v, teachers %v", classes, teachers)
	}
	classes, teachers = NewFindingIndex(nil).Entities(nil, nil)
	if len(classes)+len(teachers) != 0 {
		t.Fatalf("empty model: %v %v", classes, teachers)
	}
}

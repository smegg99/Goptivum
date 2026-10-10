// document/ids_test.go

package document_test

import (
	"errors"
	"google.golang.org/protobuf/proto"
	"math"
	"testing"

	arrangov1 "github.com/smegg99/arrango/backend/gen/arrangov1"

	"github.com/smegg99/goptivum/document"
)

func TestTheIDCounterStartsAboveEveryIDInUse(t *testing.T) {
	doc := document.New(school())
	// school() uses ids 1 to 9.
	if got := doc.GetNextArrangoId(); got != 10 {
		t.Errorf("next_arrango_id = %d, want 10", got)
	}
	if read := decode(t, encode(t, doc).Bytes); read.GetNextArrangoId() != 10 {
		t.Errorf("next_arrango_id = %d after the codec, want 10", read.GetNextArrangoId())
	}
}

// A parallel block id is allocated from the same counter, so reusing one would merge two blocks.
func TestTheIDCounterCountsParallelBlocks(t *testing.T) {
	model := school()
	model.Lessons = append(model.Lessons,
		&arrangov1.LessonInstance{Id: 10, SubjectId: 7, TeacherId: 6, Duration: 1, ParallelBlockId: 500},
		&arrangov1.LessonInstance{Id: 11, SubjectId: 7, TeacherId: 6, Duration: 1, ParallelBlockId: 500},
	)
	if got := document.NextArrangoID(model); got != 501 {
		t.Errorf("NextArrangoID = %d, want 501, above parallel block 500", got)
	}
	if got := document.New(model).GetNextArrangoId(); got != 501 {
		t.Errorf("New gave next_arrango_id %d, want 501", got)
	}
}

func TestTheIDCounterOfAnEmptySchoolIsOne(t *testing.T) {
	if got := document.NextArrangoID(&arrangov1.SchoolModel{}); got != 1 {
		t.Errorf("NextArrangoID = %d, want 1", got)
	}
}

func TestCounterCannotWrapOrTrailModelIDs(t *testing.T) {
	for _, highest := range []uint32{40, math.MaxUint32} {
		model := &arrangov1.SchoolModel{Days: []*arrangov1.Day{{Id: highest}}}
		doc := document.New(model)
		doc.NextArrangoId = 3
		if _, err := document.Encode(doc); !errors.Is(err, document.ErrInvalidIDs) {
			t.Fatalf("encode highest %d: %v", highest, err)
		}
		raw, err := proto.Marshal(doc)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := document.Decode(compress(t, raw)); !errors.Is(err, document.ErrInvalidIDs) {
			t.Fatalf("decode highest %d: %v", highest, err)
		}
	}
	if next := document.NextArrangoID(&arrangov1.SchoolModel{Days: []*arrangov1.Day{{Id: math.MaxUint32}}}); next != 0 {
		t.Fatalf("exhausted counter wrapped to %d", next)
	}
}

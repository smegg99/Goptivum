// document/codec_test.go

package document_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"testing"

	"github.com/klauspost/compress/zstd"
	arrangov1 "github.com/smegg99/arrango/backend/gen/arrangov1"
	"google.golang.org/protobuf/encoding/protowire"
	"google.golang.org/protobuf/proto"

	"github.com/smegg99/goptivum/document"
	goptivumv1 "github.com/smegg99/goptivum/document/gen/goptivumv1"
)

// school is a hand-written synthetic school, never a real one.
func school() *arrangov1.SchoolModel {
	return &arrangov1.SchoolModel{
		Name:      "Szkoła Testowa",
		Days:      []*arrangov1.Day{{Id: 1, Name: "Poniedziałek", PeriodCount: 2}},
		Periods:   []*arrangov1.Period{{Id: 2, Name: "08:00-08:45"}, {Id: 3, Name: "08:55-09:40"}},
		Years:     []*arrangov1.Year{{Id: 4, Name: "Rok 1", Level: 1, Priority: 100}},
		Divisions: []*arrangov1.Division{{Id: 5, Name: "1A", YearId: 4}},
		Teachers:  []*arrangov1.Teacher{{Id: 6, Name: "Łukasz Żółć"}},
		Subjects:  []*arrangov1.Subject{{Id: 7, Name: "matematyka"}},
		Rooms:     []*arrangov1.Room{{Id: 8, Name: "Sala 101", Designator: "101"}},
		Lessons: []*arrangov1.LessonInstance{{
			Id: 9, SubjectId: 7, TeacherId: 6, Duration: 2,
			RequiresTeacher: true, RequiresRoom: true,
			Participants: []*arrangov1.Participant{{DivisionId: 5}},
		}},
	}
}

func encode(t *testing.T, doc *goptivumv1.Document) document.Encoded {
	t.Helper()
	encoded, err := document.Encode(doc)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	return encoded
}

func decode(t *testing.T, data []byte) *goptivumv1.Document {
	t.Helper()
	doc, err := document.Decode(data)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	return doc
}

func compress(t *testing.T, raw []byte) []byte {
	t.Helper()
	encoder, err := zstd.NewWriter(nil)
	if err != nil {
		t.Fatalf("zstd: %v", err)
	}
	return encoder.EncodeAll(raw, nil)
}

// Content addressing depends on byte stability, so the round trip compares bytes.
func TestRoundTripIsByteStable(t *testing.T) {
	original := document.New(school())
	first := encode(t, original)

	sum := sha256.Sum256(first.Bytes)
	if first.SHA256 != hex.EncodeToString(sum[:]) {
		t.Errorf("SHA256 = %s, want the hash of the bytes", first.SHA256)
	}
	if first.Size != int64(len(first.Bytes)) {
		t.Errorf("Size = %d, want %d", first.Size, len(first.Bytes))
	}

	read := decode(t, first.Bytes)
	if !proto.Equal(original, read) {
		t.Error("the document changed on its way through the codec")
	}
	if got := read.GetModel().GetTeachers()[0].GetName(); got != "Łukasz Żółć" {
		t.Errorf("teacher name = %q", got)
	}
	second := encode(t, read)
	if !bytes.Equal(first.Bytes, second.Bytes) || first.SHA256 != second.SHA256 {
		t.Error("encoding the decoded document gave different bytes")
	}
}

func TestSameSchoolHashesTheSame(t *testing.T) {
	first := encode(t, document.New(school()))
	second := encode(t, document.New(school()))
	if first.SHA256 != second.SHA256 {
		t.Errorf("hashes differ: %s and %s", first.SHA256, second.SHA256)
	}

	changed := school()
	changed.Name = "Inna Szkoła"
	if encode(t, document.New(changed)).SHA256 == first.SHA256 {
		t.Error("two different schools share one hash")
	}
}

// A newer writer's fields must survive an older reader that decodes and saves.
func TestAnUnknownFieldSurvivesDecodeAndEncode(t *testing.T) {
	original := document.New(school())
	outer := protowire.AppendString(protowire.AppendTag(nil, 99, protowire.BytesType), "nowe pole")
	inner := protowire.AppendVarint(protowire.AppendTag(nil, 9999, protowire.VarintType), 42)
	original.ProtoReflect().SetUnknown(outer)
	original.GetModel().ProtoReflect().SetUnknown(inner)
	first := encode(t, original)

	read := decode(t, first.Bytes)
	if got := read.ProtoReflect().GetUnknown(); !bytes.Equal(got, outer) {
		t.Errorf("document unknown fields = %x, want %x", got, outer)
	}
	if got := read.GetModel().ProtoReflect().GetUnknown(); !bytes.Equal(got, inner) {
		t.Errorf("model unknown fields = %x, want %x", got, inner)
	}

	second := encode(t, read)
	if !bytes.Equal(first.Bytes, second.Bytes) {
		t.Error("re-encoding dropped or moved the unknown fields")
	}
}

// A real school expands about 46 times, a decompression bomb ten thousand.
func TestDecodeRefusesABomb(t *testing.T) {
	bomb := compress(t, make([]byte, document.MaxDecoded+1))
	if _, err := document.Decode(bomb); !errors.Is(err, document.ErrTooLarge) {
		t.Errorf("error = %v, want ErrTooLarge", err)
	}
}

func TestDecodeRefusesRubbish(t *testing.T) {
	for _, test := range []struct {
		name string
		data []byte
	}{
		{"nothing", nil},
		{"not compressed", []byte("this is not a zstd frame")},
		{"compressed nothing", compress(t, nil)},
		{"compressed but not a school", compress(t, []byte("hello"))},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, err := document.Decode(test.data); err == nil {
				t.Error("decode accepted it")
			}
			if _, _, err := document.FromBytes(test.data); err == nil {
				t.Error("FromBytes accepted it")
			}
		})
	}
}

// Field 1 of a SchoolModel is a string and of a Document a varint; an unnamed school gets past that as format 0.
func TestABareSchoolModelIsNotADocument(t *testing.T) {
	for _, one := range []struct {
		name  string
		model *arrangov1.SchoolModel
	}{
		{"named", school()},
		{"unnamed", &arrangov1.SchoolModel{Teachers: school().GetTeachers()}},
	} {
		t.Run(one.name, func(t *testing.T) {
			raw, err := proto.MarshalOptions{Deterministic: true}.Marshal(one.model)
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			if _, err := document.Decode(compress(t, raw)); err == nil {
				t.Fatal("a bare SchoolModel was accepted as a document")
			}
		})
	}
}

func TestEncodeRefusesWhatDecodeWouldRefuse(t *testing.T) {
	for _, test := range []struct {
		name string
		doc  *goptivumv1.Document
	}{
		{"nothing", nil},
		{"no model", &goptivumv1.Document{Format: document.Format}},
		{"no format marker", &goptivumv1.Document{Model: school()}},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, err := document.Encode(test.doc); err == nil {
				t.Error("encode accepted it")
			}
		})
	}
}

func TestFromBytesHashesWhatItWasGiven(t *testing.T) {
	encoded := encode(t, document.New(school()))
	again, _, err := document.FromBytes(encoded.Bytes)
	if err != nil {
		t.Fatalf("from bytes: %v", err)
	}
	if again.SHA256 != encoded.SHA256 || again.Size != encoded.Size || !bytes.Equal(again.Bytes, encoded.Bytes) {
		t.Errorf("FromBytes = %s/%d, Encode = %s/%d", again.SHA256, again.Size, encoded.SHA256, encoded.Size)
	}
}

func TestAScheduleTravelsWithTheSchool(t *testing.T) {
	doc := document.New(school())
	doc.Schedule = &arrangov1.ScheduleSnapshot{
		Lessons: []*arrangov1.ScheduledLesson{{LessonId: 9}},
	}
	read := decode(t, encode(t, doc).Bytes)
	if lessons := read.GetSchedule().GetLessons(); len(lessons) != 1 || lessons[0].GetLessonId() != 9 {
		t.Errorf("schedule = %v", read.GetSchedule())
	}
}

func TestAnUnsolvedDocumentHasNoSchedule(t *testing.T) {
	if read := decode(t, encode(t, document.New(school())).Bytes); read.GetSchedule() != nil {
		t.Error("an unsolved document carries a schedule")
	}
}

// The redundancy is across records, so compressing sections separately loses it.
func TestOneStreamBeatsCompressingSections(t *testing.T) {
	model := school()
	for i := range 2000 {
		model.Lessons = append(model.Lessons, &arrangov1.LessonInstance{
			Id: uint32(100 + i), SubjectId: 7, TeacherId: 6, Duration: 1,
			RequiresTeacher: true, RequiresRoom: true,
			Participants:           []*arrangov1.Participant{{DivisionId: 5}},
			AllowedRoomDesignators: []string{"101"},
		})
	}
	raw, err := proto.MarshalOptions{Deterministic: true}.Marshal(model)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	whole := encode(t, document.New(model))

	const section = 4 << 10
	sectioned := 0
	for start := 0; start < len(raw); start += section {
		sectioned += len(compress(t, raw[start:min(start+section, len(raw))]))
	}
	t.Logf("%d bytes of protobuf: %d in one stream, %d in %d-byte sections",
		len(raw), whole.Size, sectioned, section)
	if int64(sectioned) <= whole.Size {
		t.Errorf("sections compressed to %d and one stream to %d", sectioned, whole.Size)
	}
}

func TestEncodeRejectsOversizedDocument(t *testing.T) {
	doc := document.New(&arrangov1.SchoolModel{Name: strings.Repeat("x", document.MaxDecoded+1)})
	if _, err := document.Encode(doc); !errors.Is(err, document.ErrTooLarge) {
		t.Fatalf("oversized document: %v", err)
	}
}

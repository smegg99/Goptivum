// document/notes_test.go

package document_test

import (
	"errors"
	"testing"

	arrangov1 "github.com/smegg99/arrango/backend/gen/arrangov1"
	"google.golang.org/protobuf/proto"

	"github.com/smegg99/goptivum/document"
	goptivumv1 "github.com/smegg99/goptivum/document/gen/goptivumv1"
)

// noted is the synthetic school with a second subject and teacher, a split
// of 1A, a short name each, a pool, hour targets and Rok 1's template.
func noted() *goptivumv1.Document {
	model := school()
	model.Subjects = append(model.Subjects, &arrangov1.Subject{Id: 10, Name: "fizyka"})
	model.Teachers = append(model.Teachers, &arrangov1.Teacher{Id: 11, Name: "Ewa Nowak"})
	model.Splits = []*arrangov1.Split{{Id: 12, Name: "Języki", DivisionId: 5}}
	doc := document.New(model)
	doc.SubjectShortNames = []*goptivumv1.SubjectShortName{{SubjectId: 7, ShortName: "Mat"}, {SubjectId: 10, ShortName: "Fiz"}}
	doc.TeacherPool = &goptivumv1.TeacherPool{Subjects: []*goptivumv1.SubjectPool{
		{SubjectId: 7, TeacherIds: []uint32{6, 11}},
		{SubjectId: 10, TeacherIds: []uint32{11}},
	}}
	doc.SchoolHourTarget = &goptivumv1.HourTarget{HalfPeriods: 36}
	doc.TeacherHourTargets = []*goptivumv1.TeacherHourTarget{{TeacherId: 6, HalfPeriods: 37}, {TeacherId: 11, HalfPeriods: 0}}
	doc.YearTemplates = []*goptivumv1.YearTemplate{{YearId: 4, Rows: []*goptivumv1.TemplateRow{
		{SubjectId: 7, HalfPeriods: 8},
		{SubjectId: 7, SplitName: "języki", HalfPeriods: 1},
		{SubjectId: 10, SplitName: "Języki", HalfPeriods: 80},
	}}}
	return doc
}

func TestNotesSurviveTheCodec(t *testing.T) {
	doc := noted()
	read := decode(t, encode(t, doc).Bytes)
	if !proto.Equal(doc, read) {
		t.Fatal("the editor notes changed on their way through the codec")
	}
}

// An empty pool is a pool with nobody in it, not a file without one.
func TestAnEmptyPoolStaysPresent(t *testing.T) {
	doc := document.New(school())
	doc.TeacherPool = &goptivumv1.TeacherPool{}
	if read := decode(t, encode(t, doc).Bytes); read.GetTeacherPool() == nil {
		t.Error("an empty pool read back as no pool")
	}
	if read := decode(t, encode(t, document.New(school())).Bytes); read.GetTeacherPool() != nil {
		t.Error("a file without a pool read back with one")
	}
}

func TestBrokenNotesAreRefused(t *testing.T) {
	cases := []struct {
		name  string
		spoil func(*goptivumv1.Document)
	}{
		{"short name of no subject", func(d *goptivumv1.Document) { d.SubjectShortNames[0].SubjectId = 99 }},
		{"two short names for one subject", func(d *goptivumv1.Document) { d.SubjectShortNames[1].SubjectId = 7 }},
		{"blank short name", func(d *goptivumv1.Document) { d.SubjectShortNames[0].ShortName = "" }},
		{"padded short name", func(d *goptivumv1.Document) { d.SubjectShortNames[0].ShortName = " Mat" }},
		{"short name over eight letters", func(d *goptivumv1.Document) { d.SubjectShortNames[0].ShortName = "Matematyk" }},
		{"short name taken in another case", func(d *goptivumv1.Document) { d.SubjectShortNames[1].ShortName = "MAT" }},
		{"pool of no subject", func(d *goptivumv1.Document) { d.TeacherPool.Subjects[1].SubjectId = 99 }},
		{"one subject in the pool twice", func(d *goptivumv1.Document) { d.TeacherPool.Subjects[1].SubjectId = 7 }},
		{"pool naming no teacher", func(d *goptivumv1.Document) { d.TeacherPool.Subjects[0].TeacherIds[1] = 99 }},
		{"pool naming a teacher twice", func(d *goptivumv1.Document) { d.TeacherPool.Subjects[0].TeacherIds[1] = 6 }},
		{"school target over 60 periods", func(d *goptivumv1.Document) { d.SchoolHourTarget.HalfPeriods = 121 }},
		{"target of no teacher", func(d *goptivumv1.Document) { d.TeacherHourTargets[1].TeacherId = 99 }},
		{"two targets for one teacher", func(d *goptivumv1.Document) { d.TeacherHourTargets[1].TeacherId = 6 }},
		{"teacher target over 60 periods", func(d *goptivumv1.Document) { d.TeacherHourTargets[0].HalfPeriods = 121 }},
		{"template of no year", func(d *goptivumv1.Document) { d.YearTemplates[0].YearId = 99 }},
		{"two templates for one year", func(d *goptivumv1.Document) {
			d.YearTemplates = append(d.YearTemplates, &goptivumv1.YearTemplate{YearId: 4})
		}},
		{"template row of no subject", func(d *goptivumv1.Document) { d.YearTemplates[0].Rows[0].SubjectId = 99 }},
		{"template row of a split no class has", func(d *goptivumv1.Document) { d.YearTemplates[0].Rows[2].SplitName = "WF" }},
		{"template row of a padded split", func(d *goptivumv1.Document) { d.YearTemplates[0].Rows[2].SplitName = "Języki " }},
		{"template row repeated in another case", func(d *goptivumv1.Document) {
			d.YearTemplates[0].Rows[1].SplitName = "JĘZYKI"
			d.YearTemplates[0].Rows[2].SubjectId = 7
		}},
		{"template row of no periods", func(d *goptivumv1.Document) { d.YearTemplates[0].Rows[0].HalfPeriods = 0 }},
		{"template row over 40 periods", func(d *goptivumv1.Document) { d.YearTemplates[0].Rows[0].HalfPeriods = 81 }},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			doc := noted()
			c.spoil(doc)
			if err := document.CheckNotes(doc); !errors.Is(err, document.ErrInvalidNotes) {
				t.Errorf("CheckNotes = %v, want ErrInvalidNotes", err)
			}
			if _, err := document.Encode(doc); !errors.Is(err, document.ErrInvalidNotes) {
				t.Errorf("Encode = %v, want ErrInvalidNotes", err)
			}
			// Bytes another writer made reach Decode without Encode's check.
			raw, err := proto.MarshalOptions{Deterministic: true}.Marshal(doc)
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			if _, err := document.Decode(compress(t, raw)); !errors.Is(err, document.ErrInvalidNotes) {
				t.Errorf("Decode = %v, want ErrInvalidNotes", err)
			}
			if _, _, err := document.FromBytes(compress(t, raw)); !errors.Is(err, document.ErrInvalidNotes) {
				t.Errorf("FromBytes = %v, want ErrInvalidNotes", err)
			}
		})
	}
}

// A split in a class of no year names no year's split.
func TestATemplateSplitMustBeInTheYear(t *testing.T) {
	doc := noted()
	doc.Model.Divisions = append(doc.Model.Divisions, &arrangov1.Division{Id: 13, Name: "Bez roku"})
	doc.Model.Splits = append(doc.Model.Splits, &arrangov1.Split{Id: 14, Name: "WF", DivisionId: 13})
	doc.YearTemplates[0].Rows[2].SplitName = "WF"
	if err := document.CheckNotes(doc); !errors.Is(err, document.ErrInvalidNotes) {
		t.Errorf("CheckNotes = %v, want ErrInvalidNotes", err)
	}
}

func TestShortNameFits(t *testing.T) {
	for text, want := range map[string]bool{"Ł": true, "Matematy": true, "Żółćżółć": true, "Matematyk": false, "": false, "Mat ": false, "M at": true} {
		if got := document.ShortNameFits(text); got != want {
			t.Errorf("ShortNameFits(%q) = %v, want %v", text, got, want)
		}
	}
}

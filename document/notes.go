// document/notes.go

package document

import (
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	arrangov1 "github.com/smegg99/arrango/backend/gen/arrangov1"

	goptivumv1 "github.com/smegg99/goptivum/document/gen/goptivumv1"
)

const (
	// MaxShortName is the most letters a subject's short name may have.
	MaxShortName = 8
	// MaxTargetHalves is the highest weekly hour target, in half periods (60 periods).
	MaxTargetHalves = 120
	// MaxRowHalves is the most a template row may take, in half periods (40 periods).
	MaxRowHalves = 80
)

// ErrInvalidNotes means a document's editor notes break their rules.
var ErrInvalidNotes = errors.New("document: invalid editor notes")

// CheckNotes refuses editor notes that break their rules: short names and
// pool entries that name no subject or teacher of the model or name one
// twice, a short name blank, too long, padded or another subject's in any
// case; hour targets over MaxTargetHalves or for a teacher the school does
// not have or twice; templates for a year the school does not have or twice,
// rows naming a subject it does not have, a split no class of the year has,
// a subject and split twice, or periods out of range.
func CheckNotes(doc *goptivumv1.Document) error {
	if err := checkShortsAndPool(doc); err != nil {
		return err
	}
	if err := checkTargets(doc); err != nil {
		return err
	}
	return checkTemplates(doc)
}

func checkShortsAndPool(doc *goptivumv1.Document) error {
	subjects := map[uint32]bool{}
	for _, subject := range doc.GetModel().GetSubjects() {
		subjects[subject.GetId()] = true
	}
	teachers := map[uint32]bool{}
	for _, teacher := range doc.GetModel().GetTeachers() {
		teachers[teacher.GetId()] = true
	}

	named := map[uint32]bool{}
	shorts := map[string]bool{}
	for _, short := range doc.GetSubjectShortNames() {
		id, text := short.GetSubjectId(), short.GetShortName()
		switch {
		case !subjects[id]:
			return fmt.Errorf("%w: a short name for subject %d, which the school does not have", ErrInvalidNotes, id)
		case named[id]:
			return fmt.Errorf("%w: subject %d has two short names", ErrInvalidNotes, id)
		case !ShortNameFits(text):
			return fmt.Errorf("%w: subject %d's short name is blank, padded or over %d letters", ErrInvalidNotes, id, MaxShortName)
		case shorts[strings.ToLower(text)]:
			return fmt.Errorf("%w: subject %d's short name is another subject's", ErrInvalidNotes, id)
		}
		named[id] = true
		shorts[strings.ToLower(text)] = true
	}

	pooled := map[uint32]bool{}
	for _, entry := range doc.GetTeacherPool().GetSubjects() {
		id := entry.GetSubjectId()
		switch {
		case !subjects[id]:
			return fmt.Errorf("%w: a pool for subject %d, which the school does not have", ErrInvalidNotes, id)
		case pooled[id]:
			return fmt.Errorf("%w: subject %d is in the pool twice", ErrInvalidNotes, id)
		}
		pooled[id] = true
		in := map[uint32]bool{}
		for _, teacher := range entry.GetTeacherIds() {
			switch {
			case !teachers[teacher]:
				return fmt.Errorf("%w: subject %d's pool names teacher %d, whom the school does not have", ErrInvalidNotes, id, teacher)
			case in[teacher]:
				return fmt.Errorf("%w: subject %d's pool names teacher %d twice", ErrInvalidNotes, id, teacher)
			}
			in[teacher] = true
		}
	}
	return nil
}

func checkTargets(doc *goptivumv1.Document) error {
	if school := doc.GetSchoolHourTarget(); school != nil && school.GetHalfPeriods() > MaxTargetHalves {
		return fmt.Errorf("%w: the school's hour target is over %d half periods", ErrInvalidNotes, MaxTargetHalves)
	}
	teachers := map[uint32]bool{}
	for _, teacher := range doc.GetModel().GetTeachers() {
		teachers[teacher.GetId()] = true
	}
	set := map[uint32]bool{}
	for _, target := range doc.GetTeacherHourTargets() {
		id := target.GetTeacherId()
		switch {
		case !teachers[id]:
			return fmt.Errorf("%w: an hour target for teacher %d, whom the school does not have", ErrInvalidNotes, id)
		case set[id]:
			return fmt.Errorf("%w: teacher %d has two hour targets", ErrInvalidNotes, id)
		case target.GetHalfPeriods() > MaxTargetHalves:
			return fmt.Errorf("%w: teacher %d's hour target is over %d half periods", ErrInvalidNotes, id, MaxTargetHalves)
		}
		set[id] = true
	}
	return nil
}

func checkTemplates(doc *goptivumv1.Document) error {
	model := doc.GetModel()
	subjects := map[uint32]bool{}
	for _, subject := range model.GetSubjects() {
		subjects[subject.GetId()] = true
	}
	years := map[uint32]bool{}
	for _, year := range model.GetYears() {
		years[year.GetId()] = true
	}
	splits := SplitNamesByYear(model)
	seen := map[uint32]bool{}
	for _, template := range doc.GetYearTemplates() {
		year := template.GetYearId()
		switch {
		case !years[year]:
			return fmt.Errorf("%w: a template for year %d, which the school does not have", ErrInvalidNotes, year)
		case seen[year]:
			return fmt.Errorf("%w: year %d has two templates", ErrInvalidNotes, year)
		}
		seen[year] = true
		rows := map[string]bool{}
		for _, row := range template.GetRows() {
			subject, split := row.GetSubjectId(), row.GetSplitName()
			key := fmt.Sprintf("%d/%s", subject, strings.ToLower(split))
			switch {
			case !subjects[subject]:
				return fmt.Errorf("%w: year %d's template names subject %d, which the school does not have", ErrInvalidNotes, year, subject)
			case split != strings.TrimSpace(split):
				return fmt.Errorf("%w: year %d's template names a padded split", ErrInvalidNotes, year)
			case split != "" && !splits[year][strings.ToLower(split)]:
				return fmt.Errorf("%w: year %d's template names a split no class of the year has", ErrInvalidNotes, year)
			case rows[key]:
				return fmt.Errorf("%w: year %d's template names subject %d for the same students twice", ErrInvalidNotes, year, subject)
			case row.GetHalfPeriods() < 1 || row.GetHalfPeriods() > MaxRowHalves:
				return fmt.Errorf("%w: year %d's template gives subject %d periods out of range", ErrInvalidNotes, year, subject)
			}
			rows[key] = true
		}
	}
	return nil
}

// SplitNamesByYear is the split names the classes of each year have, in lower case, by year id.
func SplitNamesByYear(model *arrangov1.SchoolModel) map[uint32]map[string]bool {
	yearOf := map[uint32]uint32{}
	for _, class := range model.GetDivisions() {
		yearOf[class.GetId()] = class.GetYearId()
	}
	out := map[uint32]map[string]bool{}
	for _, split := range model.GetSplits() {
		year, ok := yearOf[split.GetDivisionId()]
		if !ok || year == 0 {
			continue
		}
		if out[year] == nil {
			out[year] = map[string]bool{}
		}
		out[year][strings.ToLower(strings.TrimSpace(split.GetName()))] = true
	}
	return out
}

// ShortNameFits reports a short name of 1 to MaxShortName letters with no space at either end.
func ShortNameFits(text string) bool {
	return text != "" && text == strings.TrimSpace(text) && utf8.RuneCountInString(text) <= MaxShortName
}

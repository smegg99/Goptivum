// solveclient/rating.go
package solveclient

import arrangov1 "github.com/smegg99/arrango/backend/gen/arrangov1"

// Rating caps comfort by the number of unignored errors.
func Rating(quality float64, errors int) float64 {
	if errors <= 0 {
		return quality
	}
	return min(quality, max(0, 50-5*float64(errors)))
}

// FindingIndex owns one report's model lookups.
type FindingIndex struct {
	groups, splits map[uint32]uint32
	years          map[uint32][]uint32
	lessons        map[uint32]*arrangov1.LessonInstance
}

// NewFindingIndex scans the model once, then resolves each finding by ID.
func NewFindingIndex(model *arrangov1.SchoolModel) *FindingIndex {
	index := &FindingIndex{groups: map[uint32]uint32{}, splits: map[uint32]uint32{}, years: map[uint32][]uint32{}, lessons: map[uint32]*arrangov1.LessonInstance{}}
	for _, group := range model.GetGroups() {
		index.groups[group.GetId()] = group.GetDivisionId()
	}
	for _, split := range model.GetSplits() {
		index.splits[split.GetId()] = split.GetDivisionId()
	}
	for _, division := range model.GetDivisions() {
		index.years[division.GetYearId()] = append(index.years[division.GetYearId()], division.GetId())
	}
	for _, lesson := range model.GetLessons() {
		index.lessons[lesson.GetId()] = lesson
	}
	return index
}

// Entities returns unique classes and teachers named directly or through lessons.
func (i *FindingIndex) Entities(refs map[string][]uint32, lessonIDs []uint32) (classes, teachers map[uint32]bool) {
	classes, teachers = map[uint32]bool{}, map[uint32]bool{}
	for kind, ids := range refs {
		for _, id := range ids {
			switch kind {
			case "division":
				classes[id] = true
			case "teacher":
				teachers[id] = true
			case "group":
				classes[i.groups[id]] = true
			case "split":
				classes[i.splits[id]] = true
			case "year":
				for _, division := range i.years[id] {
					classes[division] = true
				}
			}
		}
	}
	for _, id := range lessonIDs {
		lesson := i.lessons[id]
		teachers[lesson.GetTeacherId()] = true
		teachers[lesson.GetSupportTeacherId()] = true
		for _, part := range lesson.GetParticipants() {
			classes[part.GetDivisionId()] = true
			classes[i.groups[part.GetGroupId()]] = true
		}
	}
	delete(classes, 0)
	delete(teachers, 0)
	return classes, teachers
}

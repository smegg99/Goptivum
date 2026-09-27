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

// FindingEntities returns unique classes and teachers named directly or through lessons.
func FindingEntities(model *arrangov1.SchoolModel, refs map[string][]uint32, lessonIDs []uint32) (classes, teachers map[uint32]bool) {
	classes, teachers = map[uint32]bool{}, map[uint32]bool{}
	groups, splits := map[uint32]uint32{}, map[uint32]uint32{}
	for _, group := range model.GetGroups() {
		groups[group.GetId()] = group.GetDivisionId()
	}
	for _, split := range model.GetSplits() {
		splits[split.GetId()] = split.GetDivisionId()
	}
	for kind, ids := range refs {
		for _, id := range ids {
			switch kind {
			case "division":
				classes[id] = true
			case "teacher":
				teachers[id] = true
			case "group":
				classes[groups[id]] = true
			case "split":
				classes[splits[id]] = true
			case "year":
				for _, division := range model.GetDivisions() {
					if division.GetYearId() == id {
						classes[division.GetId()] = true
					}
				}
			}
		}
	}

	lessons := map[uint32]bool{}
	for _, id := range lessonIDs {
		lessons[id] = true
	}
	for _, lesson := range model.GetLessons() {
		if !lessons[lesson.GetId()] {
			continue
		}
		teachers[lesson.GetTeacherId()] = true
		teachers[lesson.GetSupportTeacherId()] = true
		for _, part := range lesson.GetParticipants() {
			classes[part.GetDivisionId()] = true
			classes[groups[part.GetGroupId()]] = true
		}
	}
	delete(classes, 0)
	delete(teachers, 0)
	return classes, teachers
}

// document/ids.go

package document

import (
	arrangov1 "github.com/smegg99/arrango/backend/gen/arrangov1"

	goptivumv1 "github.com/smegg99/goptivum/document/gen/goptivumv1"
)

// New wraps a model as a document whose id counter starts above every id the model uses.
func New(model *arrangov1.SchoolModel) *goptivumv1.Document {
	return &goptivumv1.Document{Format: Format, Model: model, NextArrangoId: NextArrangoID(model)}
}

// NextArrangoID is the smallest id no entity in the model uses; one counter serves every kind.
func NextArrangoID(model *arrangov1.SchoolModel) uint32 {
	var highest uint32
	note := func(ids ...uint32) {
		for _, id := range ids {
			highest = max(highest, id)
		}
	}
	for _, day := range model.GetDays() {
		note(day.GetId())
	}
	for _, period := range model.GetPeriods() {
		note(period.GetId())
	}
	for _, year := range model.GetYears() {
		note(year.GetId())
	}
	for _, division := range model.GetDivisions() {
		note(division.GetId())
	}
	for _, group := range model.GetGroups() {
		note(group.GetId())
	}
	for _, teacher := range model.GetTeachers() {
		note(teacher.GetId())
	}
	for _, subject := range model.GetSubjects() {
		note(subject.GetId())
	}
	for _, building := range model.GetBuildings() {
		note(building.GetId())
	}
	for _, room := range model.GetRooms() {
		note(room.GetId())
	}
	for _, lesson := range model.GetLessons() {
		note(lesson.GetId(), lesson.GetParallelBlockId())
	}
	for _, block := range model.GetExternalBlocks() {
		note(block.GetId())
	}
	for _, split := range model.GetSplits() {
		note(split.GetId())
	}
	for _, link := range model.GetLessonLinks() {
		note(link.GetId())
	}
	return highest + 1
}

// formats/optivum/xml/invariants_test.go

package xml_test

import (
	"testing"

	arrangov1 "github.com/smegg99/arrango/backend/gen/arrangov1"

	"github.com/smegg99/goptivum/backend/formats"
)

// maxStreams is the solver's default bound on how many student streams one
// division may produce before a model is refused.
const maxStreams = 64

// checkModel asserts everything the solver's own model gate asserts, so an
// import that passes here cannot be refused as malformed. It never prints
// the school: only ids, counts and field names.
func checkModel(t *testing.T, result *formats.Result) {
	t.Helper()

	model := result.Model
	if model == nil {
		t.Fatal("no model")
	}
	if len(model.GetDays()) == 0 || len(model.GetPeriods()) == 0 {
		t.Fatalf("days = %d, periods = %d, both must be non-empty",
			len(model.GetDays()), len(model.GetPeriods()))
	}
	periods := uint32(len(model.GetPeriods()))

	dayCount := map[uint32]uint32{}
	unique := func(kind string, ids []uint32) {
		t.Helper()
		seen := map[uint32]bool{}
		for _, id := range ids {
			if id == 0 {
				t.Errorf("%s: id 0 is reserved for none/invalid", kind)
			}
			if seen[id] {
				t.Errorf("%s: id %d appears twice", kind, id)
			}
			seen[id] = true
		}
	}

	var ids []uint32
	for _, day := range model.GetDays() {
		ids = append(ids, day.GetId())
		if day.GetPeriodCount() == 0 || day.GetPeriodCount() > periods {
			t.Errorf("day %d: period_count = %d, must fit 1..%d",
				day.GetId(), day.GetPeriodCount(), periods)
		}
		dayCount[day.GetId()] = day.GetPeriodCount()
	}
	unique("days", ids)

	collect := func(n int, id func(int) uint32) []uint32 {
		out := make([]uint32, 0, n)
		for i := 0; i < n; i++ {
			out = append(out, id(i))
		}
		return out
	}
	unique("periods", collect(len(model.GetPeriods()), func(i int) uint32 { return model.GetPeriods()[i].GetId() }))
	unique("years", collect(len(model.GetYears()), func(i int) uint32 { return model.GetYears()[i].GetId() }))
	unique("divisions", collect(len(model.GetDivisions()), func(i int) uint32 { return model.GetDivisions()[i].GetId() }))
	unique("groups", collect(len(model.GetGroups()), func(i int) uint32 { return model.GetGroups()[i].GetId() }))
	unique("splits", collect(len(model.GetSplits()), func(i int) uint32 { return model.GetSplits()[i].GetId() }))
	unique("teachers", collect(len(model.GetTeachers()), func(i int) uint32 { return model.GetTeachers()[i].GetId() }))
	unique("subjects", collect(len(model.GetSubjects()), func(i int) uint32 { return model.GetSubjects()[i].GetId() }))
	unique("rooms", collect(len(model.GetRooms()), func(i int) uint32 { return model.GetRooms()[i].GetId() }))
	unique("buildings", collect(len(model.GetBuildings()), func(i int) uint32 { return model.GetBuildings()[i].GetId() }))
	unique("lessons", collect(len(model.GetLessons()), func(i int) uint32 { return model.GetLessons()[i].GetId() }))
	unique("external blocks", collect(len(model.GetExternalBlocks()), func(i int) uint32 { return model.GetExternalBlocks()[i].GetId() }))

	years := map[uint32]bool{}
	for _, year := range model.GetYears() {
		years[year.GetId()] = true
		if year.GetPriority() == 0 {
			t.Errorf("year %d: priority 0 means zero weight, not neutral", year.GetId())
		}
	}
	divisions := map[uint32]bool{}
	for _, division := range model.GetDivisions() {
		divisions[division.GetId()] = true
		if division.GetYearId() != 0 && !years[division.GetYearId()] {
			t.Errorf("division %d: year %d does not resolve", division.GetId(), division.GetYearId())
		}
		if division.GetCountSource() == arrangov1.CountSource_COUNT_SOURCE_UNKNOWN &&
			division.GetStudentCount() != 0 {
			t.Errorf("division %d: student_count %d with an unknown source",
				division.GetId(), division.GetStudentCount())
		}
	}

	splits := map[uint32]*arrangov1.Split{}
	for _, split := range model.GetSplits() {
		splits[split.GetId()] = split
		if !divisions[split.GetDivisionId()] {
			t.Errorf("split %d: division %d does not resolve", split.GetId(), split.GetDivisionId())
		}
	}

	groups := map[uint32]*arrangov1.Group{}
	fixedGroups := map[uint32]map[uint32]int{} // division -> split -> groups
	openGroups := map[uint32]int{}
	for _, group := range model.GetGroups() {
		groups[group.GetId()] = group
		if !divisions[group.GetDivisionId()] {
			t.Errorf("group %d: division %d does not resolve", group.GetId(), group.GetDivisionId())
		}
		split := splits[group.GetSplitId()]
		if group.GetSplitId() != 0 && split == nil {
			t.Errorf("group %d: split %d does not resolve", group.GetId(), group.GetSplitId())
			continue
		}
		switch {
		case split != nil && split.GetKind() == arrangov1.SplitKind_SPLIT_KIND_FIXED:
			if split.GetDivisionId() != group.GetDivisionId() {
				t.Errorf("group %d: split %d belongs to another division", group.GetId(), split.GetId())
			}
			if fixedGroups[group.GetDivisionId()] == nil {
				fixedGroups[group.GetDivisionId()] = map[uint32]int{}
			}
			fixedGroups[group.GetDivisionId()][split.GetId()]++
		default:
			openGroups[group.GetDivisionId()]++
		}
	}
	// request_validation.cc:141-156: the fixed splits' group counts multiply,
	// and the open groups multiply that product (open > limit / count).
	for division := range divisions {
		product := 1
		over := false
		for _, count := range fixedGroups[division] {
			if count == 0 {
				continue
			}
			if count > maxStreams/product {
				over = true
				break
			}
			product *= count
		}
		if open := openGroups[division]; !over && open > maxStreams/product {
			over = true
		}
		if over {
			t.Errorf("division %d: %d fixed-split streams times %d open groups, over the solver's %d",
				division, product, openGroups[division], maxStreams)
		}
	}

	teachers := map[uint32]bool{}
	for _, teacher := range model.GetTeachers() {
		teachers[teacher.GetId()] = true
	}
	subjects := map[uint32]bool{}
	for _, subject := range model.GetSubjects() {
		subjects[subject.GetId()] = true
	}
	rooms := map[uint32]bool{}
	designators := map[string]bool{}
	buildings := map[uint32]bool{}
	for _, building := range model.GetBuildings() {
		buildings[building.GetId()] = true
	}
	for _, room := range model.GetRooms() {
		rooms[room.GetId()] = true
		designators[room.GetDesignator()] = true
		if room.GetBuildingId() != 0 && !buildings[room.GetBuildingId()] {
			t.Errorf("room %d: building %d does not resolve", room.GetId(), room.GetBuildingId())
		}
	}

	lessons := map[uint32]*arrangov1.LessonInstance{}
	for _, lesson := range model.GetLessons() {
		lessons[lesson.GetId()] = lesson
		if lesson.GetDuration() == 0 || lesson.GetDuration() > periods {
			t.Errorf("lesson %d: duration = %d, must fit 1..%d", lesson.GetId(), lesson.GetDuration(), periods)
		}
		if len(lesson.GetParticipants()) == 0 {
			t.Errorf("lesson %d: no participants", lesson.GetId())
		}
		for _, participant := range lesson.GetParticipants() {
			if !divisions[participant.GetDivisionId()] {
				t.Errorf("lesson %d: division %d does not resolve", lesson.GetId(), participant.GetDivisionId())
			}
			if participant.GetGroupId() == 0 {
				continue
			}
			group := groups[participant.GetGroupId()]
			if group == nil {
				t.Errorf("lesson %d: group %d does not resolve", lesson.GetId(), participant.GetGroupId())
				continue
			}
			if group.GetDivisionId() != participant.GetDivisionId() {
				t.Errorf("lesson %d: group %d belongs to division %d, not %d",
					lesson.GetId(), group.GetId(), group.GetDivisionId(), participant.GetDivisionId())
			}
		}
		if lesson.GetRequiresTeacher() && lesson.GetTeacherId() == 0 {
			t.Errorf("lesson %d: requires a teacher and names none", lesson.GetId())
		}
		if lesson.GetTeacherId() != 0 && !teachers[lesson.GetTeacherId()] {
			t.Errorf("lesson %d: teacher %d does not resolve", lesson.GetId(), lesson.GetTeacherId())
		}
		if lesson.GetSubjectId() != 0 && !subjects[lesson.GetSubjectId()] {
			t.Errorf("lesson %d: subject %d does not resolve", lesson.GetId(), lesson.GetSubjectId())
		}
		if lesson.GetLocked() && lesson.GetLockedPlacement() == nil {
			t.Errorf("lesson %d: locked with no placement", lesson.GetId())
		}
		if lesson.GetHasPrevious() != (lesson.GetPreviousPlacement() != nil) {
			t.Errorf("lesson %d: has_previous = %v with placement %v",
				lesson.GetId(), lesson.GetHasPrevious(), lesson.GetPreviousPlacement() != nil)
		}
		if placement := lesson.GetPreviousPlacement(); placement != nil {
			checkPlacement(t, lesson, placement, dayCount, rooms)
		}
		for _, tier := range [][]string{
			lesson.GetAllowedRoomDesignators(),
			lesson.GetAllowedRoomDesignatorsTier2(),
			lesson.GetAllowedRoomDesignatorsTier3(),
		} {
			for _, designator := range tier {
				if !designators[designator] {
					t.Errorf("lesson %d: room designator %q matches no room", lesson.GetId(), designator)
				}
			}
		}
	}

	for _, block := range model.GetExternalBlocks() {
		if block.GetTarget() == arrangov1.ExternalBlock_TARGET_UNSPECIFIED {
			t.Errorf("external block %d: unset target silently becomes a division block", block.GetId())
		}
		if block.GetDuration() == 0 {
			t.Errorf("external block %d: zero duration", block.GetId())
		}
		limit, ok := dayCount[block.GetDayId()]
		if !ok {
			t.Errorf("external block %d: day %d does not resolve", block.GetId(), block.GetDayId())
			continue
		}
		if block.GetStartPeriod()+block.GetDuration() > limit {
			t.Errorf("external block %d: periods %d..%d do not fit a day of %d",
				block.GetId(), block.GetStartPeriod(),
				block.GetStartPeriod()+block.GetDuration()-1, limit)
		}
		resolved := false
		switch block.GetTarget() {
		case arrangov1.ExternalBlock_TARGET_DIVISION:
			resolved = divisions[block.GetTargetId()]
		case arrangov1.ExternalBlock_TARGET_GROUP:
			resolved = groups[block.GetTargetId()] != nil
		case arrangov1.ExternalBlock_TARGET_TEACHER:
			resolved = teachers[block.GetTargetId()]
		case arrangov1.ExternalBlock_TARGET_ROOM:
			resolved = rooms[block.GetTargetId()]
		}
		if !resolved {
			t.Errorf("external block %d: target %d does not resolve for kind %v",
				block.GetId(), block.GetTargetId(), block.GetTarget())
		}
	}

	for _, rule := range model.GetDailyLoadRules() {
		if rule.GetDivisionId() != 0 && !divisions[rule.GetDivisionId()] {
			t.Errorf("daily load rule: division %d does not resolve", rule.GetDivisionId())
		}
		if rule.GetMaxPerDay() != 0 && rule.GetMinPerDay() > rule.GetMaxPerDay() {
			t.Errorf("daily load rule for division %d: minimum %d above maximum %d",
				rule.GetDivisionId(), rule.GetMinPerDay(), rule.GetMaxPerDay())
		}
	}

	for _, link := range model.GetLessonLinks() {
		if link.GetKind() == arrangov1.LessonLinkKind_LESSON_LINK_UNSPECIFIED {
			t.Errorf("lesson link %d: unspecified kind is rejected by the solver", link.GetId())
		}
		if len(link.GetLessonIds()) < 2 {
			t.Errorf("lesson link %d: %d members, needs at least 2", link.GetId(), len(link.GetLessonIds()))
		}
	}

	// A parallel block of one member pins nothing and means the importer
	// mapped a staple it should have dropped.
	blockMembers := map[uint32]int{}
	for _, lesson := range model.GetLessons() {
		if lesson.GetParallelBlockId() != 0 {
			blockMembers[lesson.GetParallelBlockId()]++
		}
	}
	for id, members := range blockMembers {
		if members < 2 {
			t.Errorf("parallel block %d: %d member", id, members)
		}
	}

	for _, scheduled := range result.Schedule.GetLessons() {
		lesson := lessons[scheduled.GetLessonId()]
		if lesson == nil {
			t.Errorf("snapshot names lesson %d, which the model does not define", scheduled.GetLessonId())
			continue
		}
		if scheduled.GetPlacement() == nil {
			t.Errorf("snapshot entry for lesson %d has no placement", scheduled.GetLessonId())
			continue
		}
		checkPlacement(t, lesson, scheduled.GetPlacement(), dayCount, rooms)
	}
}

func checkPlacement(t *testing.T, lesson *arrangov1.LessonInstance,
	placement *arrangov1.Placement, dayCount map[uint32]uint32, rooms map[uint32]bool) {
	t.Helper()

	limit, ok := dayCount[placement.GetDayId()]
	if !ok {
		t.Errorf("lesson %d: placement names day %d, which the model does not define",
			lesson.GetId(), placement.GetDayId())
		return
	}
	if placement.GetStartPeriod()+lesson.GetDuration() > limit {
		t.Errorf("lesson %d: placed at periods %d..%d of a day holding %d",
			lesson.GetId(), placement.GetStartPeriod(),
			placement.GetStartPeriod()+lesson.GetDuration()-1, limit)
	}
	if placement.GetRoomId() != 0 && !rooms[placement.GetRoomId()] {
		t.Errorf("lesson %d: placed in room %d, which the model does not define",
			lesson.GetId(), placement.GetRoomId())
	}
}

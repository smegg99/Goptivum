// simple/render.go

package simple

import (
	arrangov1 "github.com/smegg99/arrango/backend/gen/arrangov1"
)

// PlacedLesson is one scheduled unit, all names resolved.
type PlacedLesson struct {
	Division string `json:"division"`
	Group    string `json:"group,omitempty"`
	Subject  string `json:"subject"`
	Teacher  string `json:"teacher,omitempty"`
	Room     string `json:"room,omitempty"`
	Day      string `json:"day"`
	Period   int    `json:"period"` // 0-based start period
	Duration int    `json:"duration"`
}

// Issue is a located soft-preference violation.
type Issue struct {
	Category string `json:"category"`
	Entity   string `json:"entity"`
	Day      string `json:"day,omitempty"`
	Period   int    `json:"period"`
	Count    int    `json:"count"`
	Penalty  int64  `json:"penalty"`
}

// Result is the name-based view of the latest solver update.
type Result struct {
	Phase          string         `json:"phase"`
	Status         string         `json:"status"`
	Quality        float64        `json:"quality"`
	Objective      int64          `json:"objective"`
	Bound          int64          `json:"bound"`
	SolutionsFound uint32         `json:"solutionsFound"`
	WallSeconds    float64        `json:"wallSeconds"`
	Message        string         `json:"message,omitempty"`
	Schedule       []PlacedLesson `json:"schedule"`
	HardConflicts  []string       `json:"hardConflicts"`
	SoftIssues     []Issue        `json:"softIssues"`
}

// RenderResult resolves the latest update against the model into names.
func RenderResult(model *arrangov1.SchoolModel,
	update *arrangov1.SolveUpdate) Result {
	result := Result{
		Phase:          trimPrefix(update.GetPhase().String(), "SOLVE_PHASE_"),
		Status:         trimPrefix(update.GetStatus().String(), "SOLVE_STATUS_"),
		Objective:      update.GetObjective(),
		Bound:          update.GetBestBound(),
		SolutionsFound: update.GetSolutionsFound(),
		WallSeconds:    update.GetWallTimeSeconds(),
		Message:        update.GetMessage(),
		Schedule:       []PlacedLesson{},
		HardConflicts:  []string{},
		SoftIssues:     []Issue{},
	}
	if score := update.GetScore(); score != nil {
		result.Quality = score.GetOverallQuality()
	}

	divisions := map[uint32]string{}
	for _, d := range model.GetDivisions() {
		divisions[d.GetId()] = d.GetName()
	}
	groups := map[uint32]string{}
	for _, g := range model.GetGroups() {
		groups[g.GetId()] = g.GetName()
	}
	teachers := map[uint32]string{}
	for _, t := range model.GetTeachers() {
		teachers[t.GetId()] = t.GetName()
	}
	subjects := map[uint32]string{}
	for _, s := range model.GetSubjects() {
		subjects[s.GetId()] = s.GetName()
	}
	rooms := map[uint32]string{}
	for _, r := range model.GetRooms() {
		rooms[r.GetId()] = r.GetName()
	}
	days := map[uint32]string{}
	for _, d := range model.GetDays() {
		days[d.GetId()] = d.GetName()
	}
	lessons := map[uint32]*arrangov1.LessonInstance{}
	for _, l := range model.GetLessons() {
		lessons[l.GetId()] = l
	}

	for _, sl := range update.GetSnapshot().GetLessons() {
		lesson := lessons[sl.GetLessonId()]
		if lesson == nil || sl.GetPlacement() == nil {
			continue
		}
		// The simple API always builds single-participant lessons; render
		// the first participant's division/group.
		var divisionID, groupID uint32
		if parts := lesson.GetParticipants(); len(parts) > 0 {
			divisionID = parts[0].GetDivisionId()
			groupID = parts[0].GetGroupId()
		}
		result.Schedule = append(result.Schedule, PlacedLesson{
			Division: divisions[divisionID],
			Group:    groups[groupID],
			Subject:  subjects[lesson.GetSubjectId()],
			Teacher:  teachers[lesson.GetTeacherId()],
			Room:     rooms[sl.GetPlacement().GetRoomId()],
			Day:      days[sl.GetPlacement().GetDayId()],
			Period:   int(sl.GetPlacement().GetStartPeriod()),
			Duration: int(lesson.GetDuration()),
		})
	}
	for _, c := range update.GetValidation().GetConflicts() {
		result.HardConflicts = append(result.HardConflicts, c.GetMessage())
	}
	for _, issue := range update.GetScore().GetSoftIssues() {
		result.SoftIssues = append(result.SoftIssues, Issue{
			Category: issue.GetCategory(),
			Entity:   issue.GetEntity(),
			Day:      days[issue.GetDayId()],
			Period:   int(issue.GetPeriod()),
			Count:    int(issue.GetCount()),
			Penalty:  issue.GetPenalty(),
		})
	}
	return result
}

func trimPrefix(s, prefix string) string {
	if len(s) > len(prefix) && s[:len(prefix)] == prefix {
		return s[len(prefix):]
	}
	return s
}

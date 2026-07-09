// simple/simple.go

// Package simple maps a name-based, id-free JSON description of a school
// (the raw demand data) onto the proto SchoolModel, and renders solver
// results back into names. Pure plumbing — no solver semantics.
package simple

import (
	"fmt"
	"strings"

	arrangov1 "github.com/smegg99/arrango/backend/gen/arrangov1"
)

var defaultDayNames = []string{
	"Poniedziałek", "Wtorek", "Środa", "Czwartek", "Piątek",
	"Sobota", "Niedziela",
}

// Priority by year level, same scheme as the demo data and Optivum import.
var levelPriority = map[int]uint32{1: 300, 2: 150, 3: 100, 4: 150, 5: 300}

// partitionOf derives the explicit partition label from a group suffix:
// "1/3" -> "3" (digits/digits only); anything else -> "" (unknown structure,
// conservative overlap).
func partitionOf(suffix string) string {
	slash := strings.IndexByte(suffix, '/')
	if slash <= 0 || slash+1 >= len(suffix) {
		return ""
	}
	for _, r := range suffix[:slash] {
		if r < '0' || r > '9' {
			return ""
		}
	}
	for _, r := range suffix[slash+1:] {
		if r < '0' || r > '9' {
			return ""
		}
	}
	return suffix[slash+1:]
}

// School is the raw demand data: who needs how many lessons of what.
// Everything is referenced by name; ids do not exist in this format.
type School struct {
	Name          string     `json:"name"`
	Days          []string   `json:"days"`          // day names; optional
	DayCount      int        `json:"dayCount"`      // used when days is empty
	PeriodsPerDay int        `json:"periodsPerDay"` // required, >= 1
	Divisions     []Division `json:"divisions"`
	Teachers      []string   `json:"teachers"`
	Rooms         []string   `json:"rooms"`
	Lessons       []Lesson   `json:"lessons"`
}

type Division struct {
	Name   string   `json:"name"`
	Year   int      `json:"year"`   // 1..5 sets priority; 0 = neutral
	Groups []string `json:"groups"` // e.g. ["1/2", "2/2"]; optional
}

// Lesson describes a weekly demand: `count` schedulable units of
// `duration` consecutive periods each.
type Lesson struct {
	Division string `json:"division"` // required
	Subject  string `json:"subject"`  // required
	Teacher  string `json:"teacher"`  // optional: empty = no teacher needed
	Room     string `json:"room"`     // optional: empty = no room needed
	Group    string `json:"group"`    // optional: empty = whole division
	Count    int    `json:"count"`    // default 1
	Duration int    `json:"duration"` // default 1
}

// ToModel converts the demand data into a solvable proto model. All
// referenced names must be declared; every problem is reported at once.
func ToModel(s School) (*arrangov1.SchoolModel, error) {
	var problems []string
	if s.PeriodsPerDay < 1 {
		problems = append(problems, "periodsPerDay must be >= 1")
	}
	days := s.Days
	if len(days) == 0 {
		count := s.DayCount
		if count < 1 {
			count = 5
		}
		if count > len(defaultDayNames) {
			problems = append(problems,
				fmt.Sprintf("dayCount %d exceeds %d; name days explicitly",
					count, len(defaultDayNames)))
		} else {
			days = defaultDayNames[:count]
		}
	}
	if len(s.Divisions) == 0 {
		problems = append(problems, "at least one division is required")
	}
	if len(s.Lessons) == 0 {
		problems = append(problems, "at least one lesson is required")
	}

	model := &arrangov1.SchoolModel{Name: s.Name}
	nextID := uint32(1)
	id := func() uint32 { v := nextID; nextID++; return v }

	for _, name := range days {
		model.Days = append(model.Days, &arrangov1.Day{
			Id: id(), Name: name,
			PeriodCount: uint32(s.PeriodsPerDay),
		})
	}
	for p := 0; p < s.PeriodsPerDay; p++ {
		model.Periods = append(model.Periods,
			&arrangov1.Period{Id: id(), Name: fmt.Sprint(p + 1)})
	}
	yearByLevel := map[int]uint32{}
	year := func(level int) uint32 {
		if v, ok := yearByLevel[level]; ok {
			return v
		}
		priority, ok := levelPriority[level]
		if !ok {
			priority = 100
		}
		y := &arrangov1.Year{Id: id(), Name: fmt.Sprintf("Rok %d", level),
			Level: uint32(level), Priority: priority}
		model.Years = append(model.Years, y)
		yearByLevel[level] = y.Id
		return y.Id
	}

	divisionByName := map[string]*arrangov1.Division{}
	groupByKey := map[string]uint32{} // "division|group"
	splitByKey := map[string]uint32{} // "divisionId|label"
	// x/N groups join one shared OPEN split per (division, N); named groups
	// stay split-less (implicit private open split in the solver).
	split := func(divisionID uint32, label string) uint32 {
		key := fmt.Sprintf("%d|%s", divisionID, label)
		if sid, ok := splitByKey[key]; ok {
			return sid
		}
		s := &arrangov1.Split{Id: id(), Name: label, DivisionId: divisionID,
			Kind: arrangov1.SplitKind_SPLIT_KIND_OPEN}
		model.Splits = append(model.Splits, s)
		splitByKey[key] = s.Id
		return s.Id
	}
	for _, d := range s.Divisions {
		if d.Name == "" {
			problems = append(problems, "division with empty name")
			continue
		}
		if _, dup := divisionByName[d.Name]; dup {
			problems = append(problems, "duplicate division "+d.Name)
			continue
		}
		division := &arrangov1.Division{
			Id: id(), Name: d.Name, YearId: year(d.Year),
		}
		model.Divisions = append(model.Divisions, division)
		divisionByName[d.Name] = division
		for _, g := range d.Groups {
			key := d.Name + "|" + g
			if _, dup := groupByKey[key]; dup {
				problems = append(problems,
					fmt.Sprintf("duplicate group %q in division %s", g, d.Name))
				continue
			}
			group := &arrangov1.Group{Id: id(), Name: g, DivisionId: division.Id}
			if label := partitionOf(g); label != "" {
				group.SplitId = split(division.Id, label)
			}
			model.Groups = append(model.Groups, group)
			groupByKey[key] = group.Id
		}
	}

	teacherByName := map[string]uint32{}
	for _, name := range s.Teachers {
		if _, dup := teacherByName[name]; dup {
			problems = append(problems, "duplicate teacher "+name)
			continue
		}
		t := &arrangov1.Teacher{Id: id(), Name: name}
		model.Teachers = append(model.Teachers, t)
		teacherByName[name] = t.Id
	}
	roomByName := map[string]uint32{}
	for _, name := range s.Rooms {
		if _, dup := roomByName[name]; dup {
			problems = append(problems, "duplicate room "+name)
			continue
		}
		r := &arrangov1.Room{Id: id(), Name: name, Designator: name}
		model.Rooms = append(model.Rooms, r)
		roomByName[name] = r.Id
	}

	subjectByName := map[string]uint32{}
	subject := func(name string) uint32 {
		if v, ok := subjectByName[name]; ok {
			return v
		}
		sub := &arrangov1.Subject{Id: id(), Name: name}
		model.Subjects = append(model.Subjects, sub)
		subjectByName[name] = sub.Id
		return sub.Id
	}

	for i, l := range s.Lessons {
		where := fmt.Sprintf("lessons[%d] (%s/%s)", i, l.Division, l.Subject)
		division, ok := divisionByName[l.Division]
		if !ok {
			problems = append(problems, where+": unknown division")
			continue
		}
		if l.Subject == "" {
			problems = append(problems, where+": subject is required")
			continue
		}
		var teacherID uint32
		if l.Teacher != "" {
			if teacherID, ok = teacherByName[l.Teacher]; !ok {
				problems = append(problems,
					where+": unknown teacher "+l.Teacher)
				continue
			}
		}
		var roomID uint32
		if l.Room != "" {
			if roomID, ok = roomByName[l.Room]; !ok {
				problems = append(problems, where+": unknown room "+l.Room)
				continue
			}
		}
		var groupID uint32
		if l.Group != "" {
			if groupID, ok = groupByKey[l.Division+"|"+l.Group]; !ok {
				problems = append(problems, fmt.Sprintf(
					"%s: unknown group %q in division %s",
					where, l.Group, l.Division))
				continue
			}
		}
		count := l.Count
		if count < 1 {
			count = 1
		}
		duration := l.Duration
		if duration < 1 {
			duration = 1
		}
		if duration > s.PeriodsPerDay {
			problems = append(problems, fmt.Sprintf(
				"%s: duration %d exceeds periodsPerDay %d",
				where, duration, s.PeriodsPerDay))
			continue
		}
		for n := 0; n < count; n++ {
			lesson := &arrangov1.LessonInstance{
				Id: id(),
				Participants: []*arrangov1.Participant{
					{DivisionId: division.Id, GroupId: groupID},
				},
				SubjectId:       subject(l.Subject),
				TeacherId:       teacherID,
				Duration:        uint32(duration),
				RequiresTeacher: teacherID != 0,
				RequiresRoom:    roomID != 0,
			}
			if roomID != 0 {
				// A named room pins the lesson to that designator.
				lesson.AllowedRoomDesignators = []string{l.Room}
			}
			model.Lessons = append(model.Lessons, lesson)
		}
	}

	if len(problems) > 0 {
		return nil, fmt.Errorf("invalid school: %s",
			strings.Join(problems, "; "))
	}
	return model, nil
}

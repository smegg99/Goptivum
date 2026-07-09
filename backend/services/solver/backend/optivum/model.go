// optivum/model.go

package optivum

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	arrangov1 "github.com/smegg99/arrango/backend/gen/arrangov1"
)

// firstToken returns the first whitespace-delimited token of s — the
// canonical Optivum room designator within a room page title.
func firstToken(s string) string {
	fields := strings.Fields(s)
	if len(fields) == 0 {
		return s
	}
	return fields[0]
}

// partitionOf derives the explicit partition label from an Optivum group
// suffix: "1/3" -> "3" (digits/digits only); anything else -> "" (unknown
// structure, conservative overlap).
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

// Priority by inferred year level, mirroring the demo presets.
var levelPriority = map[int]uint32{1: 300, 2: 150, 3: 100, 4: 150, 5: 300}

type builder struct {
	model    *arrangov1.SchoolModel
	snapshot *arrangov1.ScheduleSnapshot
	warnings []string
	nextID   uint32

	yearByLevel    map[int]uint32
	divisionByName map[string]uint32
	groupByKey     map[string]uint32 // "division|suffix"
	splitByKey     map[string]uint32 // "division|partition label"
	teacherByStem  map[string]uint32
	roomByStem     map[string]uint32
	subjectByName  map[string]uint32
}

func (b *builder) id() uint32 {
	id := b.nextID
	b.nextID++
	return id
}

func (b *builder) year(level int) uint32 {
	if id, ok := b.yearByLevel[level]; ok {
		return id
	}
	priority, ok := levelPriority[level]
	if !ok {
		priority = 100
	}
	id := b.id()
	b.model.Years = append(b.model.Years, &arrangov1.Year{
		Id: id, Name: "Rok " + strconv.Itoa(level),
		Level: uint32(level), Priority: priority,
	})
	b.yearByLevel[level] = id
	return id
}

func (b *builder) division(name string) uint32 {
	if id, ok := b.divisionByName[name]; ok {
		return id
	}
	level := 0
	if len(name) > 0 && name[0] >= '0' && name[0] <= '9' {
		level = int(name[0] - '0')
	}
	id := b.id()
	b.model.Divisions = append(b.model.Divisions, &arrangov1.Division{
		Id: id, Name: name, YearId: b.year(level),
	})
	b.divisionByName[name] = id
	return id
}

// One OPEN split per (division, x/N label): the export's x/N groups form one
// physical halving/thirding per division, exactly the old partition-string
// semantics but as first-class structure. Named groups get no split (the
// solver treats them as private open splits).
func (b *builder) split(divisionID uint32, label string) uint32 {
	key := strconv.Itoa(int(divisionID)) + "|" + label
	if id, ok := b.splitByKey[key]; ok {
		return id
	}
	id := b.id()
	b.model.Splits = append(b.model.Splits, &arrangov1.Split{
		Id: id, Name: label, DivisionId: divisionID,
		Kind: arrangov1.SplitKind_SPLIT_KIND_OPEN,
	})
	b.splitByKey[key] = id
	return id
}

func (b *builder) group(divisionName, suffix string) uint32 {
	key := divisionName + "|" + suffix
	if id, ok := b.groupByKey[key]; ok {
		return id
	}
	id := b.id()
	divisionID := b.division(divisionName)
	group := &arrangov1.Group{Id: id, Name: suffix, DivisionId: divisionID}
	// x/N groups join one shared open split per (division, N); named groups
	// stay split-less (implicit private open split in the solver).
	if label := partitionOf(suffix); label != "" {
		group.SplitId = b.split(divisionID, label)
	}
	b.model.Groups = append(b.model.Groups, group)
	b.groupByKey[key] = id
	return id
}

func (b *builder) teacher(stem, title, code string) uint32 {
	if id, ok := b.teacherByStem[stem]; ok {
		return id
	}
	name := title
	if name == "" {
		name = code
	}
	if name == "" {
		name = stem
	}
	id := b.id()
	b.model.Teachers = append(b.model.Teachers,
		&arrangov1.Teacher{Id: id, Name: name, SourceRef: stem})
	b.teacherByStem[stem] = id
	return id
}

func (b *builder) room(stem, name string) uint32 {
	if id, ok := b.roomByStem[stem]; ok {
		return id
	}
	if name == "" {
		name = stem
	}
	id := b.id()
	b.model.Rooms = append(b.model.Rooms, &arrangov1.Room{
		Id: id, Name: name, Designator: firstToken(name), SourceRef: stem,
	})
	b.roomByStem[stem] = id
	return id
}

func (b *builder) subject(name string) uint32 {
	if id, ok := b.subjectByName[name]; ok {
		return id
	}
	id := b.id()
	b.model.Subjects = append(b.model.Subjects,
		&arrangov1.Subject{Id: id, Name: name})
	b.subjectByName[name] = id
	return id
}

func build(p pages) (*Result, error) {
	b := &builder{
		model:          &arrangov1.SchoolModel{Name: "Import Optivum"},
		snapshot:       &arrangov1.ScheduleSnapshot{},
		nextID:         1,
		yearByLevel:    map[int]uint32{},
		divisionByName: map[string]uint32{},
		groupByKey:     map[string]uint32{},
		splitByKey:     map[string]uint32{},
		teacherByStem:  map[string]uint32{},
		roomByStem:     map[string]uint32{},
		subjectByName:  map[string]uint32{},
	}

	// Parse all division pages (numeric order keeps ids deterministic).
	var parsed []*divisionPage
	for _, stem := range sortedStems(p.divisions) {
		page, err := parseDivisionPage(stem, p.divisions[stem])
		if err != nil {
			b.warnings = append(b.warnings, err.Error())
			continue
		}
		b.warnings = append(b.warnings, page.warnings...)
		parsed = append(parsed, page)
		// Register the division with its source stem ("o20") up front.
		id := b.division(page.name)
		for _, d := range b.model.Divisions {
			if d.Id == id && d.SourceRef == "" {
				d.SourceRef = stem
			}
		}
	}
	if len(parsed) == 0 {
		// Surface why every page failed instead of an opaque error — the
		// reasons (e.g. "no title", "no timetable table") tell whether the
		// export is a different Optivum layout/encoding than we handle.
		reasons := b.warnings
		if len(reasons) > 8 {
			reasons = reasons[:8]
		}
		detail := "no page-level reasons collected"
		if len(reasons) > 0 {
			detail = strings.Join(reasons, "; ")
		}
		return nil, fmt.Errorf(
			"no division page parsed successfully (%d o*.html pages found): %s",
			len(p.divisions), detail)
	}

	// Calendar: widest day header row wins; periods span every row seen.
	var dayNames []string
	periodTimes := map[int]string{}
	maxPeriod := -1
	for _, page := range parsed {
		if len(page.dayNames) > len(dayNames) {
			dayNames = page.dayNames
		}
		for q, t := range page.periodTimes {
			if _, ok := periodTimes[q]; !ok {
				periodTimes[q] = t
			}
			if q > maxPeriod {
				maxPeriod = q
			}
		}
		for _, occ := range page.occurrences {
			if occ.period > maxPeriod {
				maxPeriod = occ.period
			}
		}
	}
	if len(dayNames) == 0 || maxPeriod < 0 {
		return nil, fmt.Errorf("could not detect days/periods from division pages")
	}
	periodCount := uint32(maxPeriod + 1)
	for _, name := range dayNames {
		b.model.Days = append(b.model.Days, &arrangov1.Day{
			Id: b.id(), Name: name, PeriodCount: periodCount,
		})
	}
	for q := 0; q <= maxPeriod; q++ {
		name := periodTimes[q]
		if name == "" {
			name = strconv.Itoa(q + 1)
		}
		b.model.Periods = append(b.model.Periods,
			&arrangov1.Period{Id: b.id(), Name: name})
	}

	// All teacher/room pages register entities even when never referenced,
	// so counts match the export.
	for _, stem := range sortedStems(p.teachers) {
		title := ""
		if root, err := parseHTML(p.teachers[stem]); err == nil {
			title = pageTitle(root)
		} else {
			b.warnings = append(b.warnings, fmt.Sprintf("%s: %v", stem, err))
		}
		b.teacher(stem, title, "")
	}
	for _, stem := range sortedStems(p.rooms) {
		title := ""
		if root, err := parseHTML(p.rooms[stem]); err == nil {
			title = pageTitle(root)
		} else {
			b.warnings = append(b.warnings, fmt.Sprintf("%s: %v", stem, err))
		}
		b.room(stem, title)
	}

	// Merge occurrences: same (division, group, subject, teacher, room) on
	// one day in adjacent periods = one multi-period lesson instance.
	type slot struct{ day, period int }
	occsByKey := map[string][]slot{}
	occMeta := map[string]occurrence{}
	for _, page := range parsed {
		seen := map[string]bool{}
		for _, occ := range page.occurrences {
			key := occ.division + "|" + occ.group + "|" + occ.subject + "|" +
				occ.teacherFile + "|" + occ.roomFile
			slotKey := fmt.Sprintf("%s|%d|%d", key, occ.day, occ.period)
			if seen[slotKey] {
				b.warnings = append(b.warnings, fmt.Sprintf(
					"%s: duplicate entry %s day %d period %d",
					occ.division, occ.subject, occ.day, occ.period))
				continue
			}
			seen[slotKey] = true
			occsByKey[key] = append(occsByKey[key], slot{occ.day, occ.period})
			occMeta[key] = occ
		}
	}

	keys := make([]string, 0, len(occsByKey))
	for key := range occsByKey {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	for _, key := range keys {
		occ := occMeta[key]
		slots := occsByKey[key]
		sort.Slice(slots, func(i, j int) bool {
			if slots[i].day != slots[j].day {
				return slots[i].day < slots[j].day
			}
			return slots[i].period < slots[j].period
		})
		divisionID := b.division(occ.division)
		var groupID uint32
		if occ.group != "" {
			groupID = b.group(occ.division, occ.group)
		}
		subjectID := b.subject(occ.subject)
		var teacherID uint32
		if occ.teacherFile != "" {
			teacherID = b.teacher(occ.teacherFile, "", occ.teacherCode)
		}
		var roomID uint32
		if occ.roomFile != "" {
			roomID = b.room(occ.roomFile, occ.roomName)
		}

		for i := 0; i < len(slots); {
			start := slots[i]
			duration := uint32(1)
			for i+1 < len(slots) && slots[i+1].day == start.day &&
				slots[i+1].period == slots[i].period+1 {
				duration++
				i++
			}
			i++
			placement := &arrangov1.Placement{
				DayId:       b.model.Days[start.day].Id,
				StartPeriod: uint32(start.period),
				RoomId:      roomID,
			}
			// Room eligibility stays empty (unrestricted): observed-room
			// inference arrives in increment 2. The imported room lives only
			// in previous_placement, never as an implicit "only this room".
			lesson := &arrangov1.LessonInstance{
				Id: b.id(),
				Participants: []*arrangov1.Participant{
					{DivisionId: divisionID, GroupId: groupID},
				},
				SubjectId:         subjectID,
				TeacherId:         teacherID,
				Duration:          duration,
				RequiresTeacher:   occ.teacherFile != "",
				RequiresRoom:      occ.roomFile != "",
				HasPrevious:       true,
				PreviousPlacement: placement,
			}
			if start.day >= len(b.model.Days) {
				b.warnings = append(b.warnings, fmt.Sprintf(
					"%s: %s day index %d out of range, lesson skipped",
					occ.division, occ.subject, start.day))
				continue
			}
			b.model.Lessons = append(b.model.Lessons, lesson)
			b.snapshot.Lessons = append(b.snapshot.Lessons,
				&arrangov1.ScheduledLesson{
					LessonId: lesson.Id, Placement: placement,
				})
		}
	}

	return &Result{
		Model:    b.model,
		Snapshot: b.snapshot,
		Warnings: b.warnings,
		Summary: Summary{
			Divisions: len(b.model.Divisions),
			Groups:    len(b.model.Groups),
			Teachers:  len(b.model.Teachers),
			Rooms:     len(b.model.Rooms),
			Subjects:  len(b.model.Subjects),
			Lessons:   len(b.model.Lessons),
			Days:      len(b.model.Days),
			Periods:   len(b.model.Periods),
		},
	}, nil
}

// formats/optivum/xml/build.go

package xml

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"

	arrangov1 "github.com/smegg99/arrango/backend/gen/arrangov1"

	"github.com/smegg99/goptivum/backend/formats"
)

// Marks an info cell carries for one slot of a week.
const (
	markBlocked   = "-" // the owner is unavailable
	markAllowed   = "*" // the division may be taught here
	markPreferred = "+" // the owner would rather be taught here
)

// Room markers an assignment may carry instead of a room code.
const (
	// anyRoom opens every room: the listed rooms are then a wish, not a rule.
	anyRoom = "*"
	// sharedRoom is a room Optivum does not name here. The preference list
	// still says which rooms are acceptable, so nothing is lost by reading it
	// as "no particular room".
	sharedRoom = "@"
)

// blockLabel names an unavailability taken from an info cell.
const blockLabel = "Blokada"

type divisionRef struct {
	id   uint32
	code string
}

type groupRef struct {
	id      uint32
	ordinal string
}

type roomRef struct {
	id         uint32
	designator string
}

type setRef struct {
	designators []string
	wildcard    bool
}

// builder turns one parsed document into a SchoolModel. Ids are allocated
// from a single counter in document order, so two imports of the same file
// produce the same model.
type builder struct {
	model    *arrangov1.SchoolModel
	schedule *arrangov1.ScheduleSnapshot
	findings []formats.Finding
	nextID   uint32

	dayIDs    []uint32
	periodIDs []uint32
	yearIDs   map[string]uint32
	divisions map[string]*divisionRef
	groups    map[string]*groupRef
	buildings map[string]uint32
	rooms     map[string]*roomRef
	roomSets  map[string]*setRef
	teachers  map[string]uint32
	subjects  map[string]uint32

	// Per day: the last schedulable period the document implies, 1-based.
	dayEnd []uint32

	blockedSubjects map[string]bool // subject code seen in a multi-period block

	fixedSplits          int
	uncertainSplits      int
	sharedRooms          int
	trimmedBlocks        int
	unplacedLessons      int
	openRoomPreferences  int
	unresolvedPrefTokens int
	preferredSlots       int
	unknownSlotMarks     int
	outOfRangeSlots      int
	degeneratePins       int
	unresolvedHomeRooms  int
	teacherLoads         int
	formTutors           int
	flaggedAssignments   int
}

func build(ctx context.Context, doc *document, shape *census) (*formats.Result, error) {
	b := &builder{
		model:           &arrangov1.SchoolModel{Name: strings.TrimSpace(doc.Facility.Name)},
		schedule:        &arrangov1.ScheduleSnapshot{},
		nextID:          1,
		yearIDs:         map[string]uint32{},
		divisions:       map[string]*divisionRef{},
		groups:          map[string]*groupRef{},
		buildings:       map[string]uint32{},
		rooms:           map[string]*roomRef{},
		roomSets:        map[string]*setRef{},
		teachers:        map[string]uint32{},
		subjects:        map[string]uint32{},
		blockedSubjects: map[string]bool{},
	}

	b.addCalendar(doc)
	b.addStructure(doc)
	b.addRooms(doc)
	b.addTeachers(doc)
	b.addSubjects(doc)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	b.addLessons(doc)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	// The days have to be closed out between reading the marked weeks and
	// emitting blocks: an external block outside its day's period count is
	// refused by the solver, not clamped.
	blocked := b.collectSlots(doc)
	b.finishDays()
	b.addUnavailability(blocked)
	b.addDailyLoad(doc)
	b.reportUnmapped(shape)

	return &formats.Result{Model: b.model, Schedule: b.schedule, Findings: b.findings}, nil
}

func (b *builder) id() uint32 {
	id := b.nextID
	b.nextID++
	return id
}

// sourceRef is Optivum's own code for an entity, namespaced by the importer
// so that a re-import updates a school instead of duplicating it.
func sourceRef(kind, code string) string {
	if code == "" {
		return ""
	}
	return ID + ":" + kind + ":" + code
}

func (b *builder) note(kind formats.FindingKind, severity formats.Severity, detail string, refs ...*arrangov1.EntityRef) {
	b.findings = append(b.findings, formats.Finding{
		Kind: kind, Severity: severity, Entities: refs, Detail: detail,
	})
}

// count parses a headcount or capacity. Optivum writes the attribute present
// but empty, and zero means "not stated" rather than "nobody".
func count(values ...string) (uint32, arrangov1.CountSource) {
	total := 0
	stated := false
	for _, value := range values {
		number, err := strconv.Atoi(strings.TrimSpace(value))
		if err != nil || number < 0 {
			continue
		}
		stated = true
		total += number
	}
	if !stated || total == 0 {
		return 0, arrangov1.CountSource_COUNT_SOURCE_UNKNOWN
	}
	return uint32(total), arrangov1.CountSource_COUNT_SOURCE_IMPORTED
}

// index reads a 0-based day or period index.
func index(value string) (int, bool) {
	number, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil || number < 0 {
		return 0, false
	}
	return number, true
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

// --- calendar ---------------------------------------------------------

func (b *builder) addCalendar(doc *document) {
	for i, d := range doc.Days {
		b.dayIDs = append(b.dayIDs, b.id())
		b.model.Days = append(b.model.Days, &arrangov1.Day{
			Id:   b.dayIDs[i],
			Name: firstNonEmpty(d.Name, d.Code, "Dzien "+strconv.Itoa(i+1)),
		})
	}
	b.dayEnd = make([]uint32, len(doc.Days))

	for i, p := range doc.Periods {
		b.periodIDs = append(b.periodIDs, b.id())
		b.model.Periods = append(b.model.Periods, &arrangov1.Period{
			Id:   b.periodIDs[i],
			Name: periodName(p, i),
		})
		// Placements index periods by document order; a code that disagrees
		// with that order means the two would drift apart.
		if code, ok := index(p.Code); ok && code != i+1 {
			b.note(formats.KindUnknownReference, formats.Warning,
				fmt.Sprintf("period %d carries code %d, document order wins", i+1, code))
		}
	}
}

// periodName prefers the school's own bell times, which Optivum pads.
func periodName(p period, i int) string {
	from, to := strings.TrimSpace(p.From), strings.TrimSpace(p.To)
	switch {
	case from != "" && to != "":
		return from + "-" + to
	case from != "":
		return from
	default:
		return firstNonEmpty(p.Code, strconv.Itoa(i+1))
	}
}

// --- years, divisions, splits, groups ---------------------------------

func (b *builder) addStructure(doc *document) {
	for _, d := range doc.Divisions {
		code := strings.TrimSpace(d.Code)
		if code == "" {
			b.note(formats.KindUnknownReference, formats.Warning, "division without a code, skipped")
			continue
		}
		if _, exists := b.divisions[code]; exists {
			b.note(formats.KindUnknownReference, formats.Warning,
				"division code "+code+" appears twice, the first wins")
			continue
		}

		yearID := b.year(d)
		students, source := count(d.Boys, d.Girls)
		id := b.id()
		b.model.Divisions = append(b.model.Divisions, &arrangov1.Division{
			Id:           id,
			Name:         code,
			YearId:       yearID,
			StudentCount: students,
			CountSource:  source,
			SourceRef:    sourceRef("oddzial", code),
		})
		ref := &divisionRef{id: id, code: code}
		b.divisions[code] = ref

		b.addSplits(ref, d)
		if strings.TrimSpace(d.Tutor) != "" {
			b.formTutors++
		}
	}
}

// year groups divisions by the level the document states. Optivum's poziom is
// offset from the year printed in a division code, so the level follows the
// code when it starts with a digit.
func (b *builder) year(d division) uint32 {
	key := strings.TrimSpace(d.Level)
	if id, ok := b.yearIDs[key]; ok {
		return id
	}

	level := 0
	if code := strings.TrimSpace(d.Code); code != "" && code[0] >= '0' && code[0] <= '9' {
		level = int(code[0] - '0')
	} else if stated, ok := index(key); ok {
		level = stated
	}

	id := b.id()
	b.model.Years = append(b.model.Years, &arrangov1.Year{
		Id:    id,
		Name:  "Rok " + strconv.Itoa(level),
		Level: uint32(level),
		// Percent multiplier; the document says nothing, so stay neutral.
		Priority: 100,
	})
	b.yearIDs[key] = id
	return id
}

func (b *builder) addSplits(div *divisionRef, d division) {
	for i, s := range d.Splits {
		name := firstNonEmpty(s.Code, s.Subject, "Podzial "+strconv.Itoa(i+1))
		kind, certain := splitKind(s)
		id := b.id()
		b.model.Splits = append(b.model.Splits, &arrangov1.Split{
			Id: id, Name: name, DivisionId: div.id, Kind: kind,
		})
		switch {
		case kind == arrangov1.SplitKind_SPLIT_KIND_FIXED:
			b.fixedSplits++
		case !certain:
			b.uncertainSplits++
		}

		for _, g := range s.Groups {
			code := strings.TrimSpace(g.Code)
			if code == "" {
				b.note(formats.KindUnknownReference, formats.Warning,
					"group without a code in division "+div.code,
					formats.Ref(arrangov1.EntityKind_ENTITY_KIND_DIVISION, div.id))
				continue
			}
			students, source := count(g.Students)
			groupID := b.id()
			b.model.Groups = append(b.model.Groups, &arrangov1.Group{
				Id:           groupID,
				Name:         code,
				DivisionId:   div.id,
				StudentCount: students,
				CountSource:  source,
				SourceRef:    sourceRef("grupa", div.code+"/"+name+"/"+code),
				SplitId:      id,
			})
			key := div.code + "|" + code
			if _, exists := b.groups[key]; exists {
				b.note(formats.KindUnknownReference, formats.Warning,
					"group code "+code+" appears twice in division "+div.code+", the first wins",
					formats.Ref(arrangov1.EntityKind_ENTITY_KIND_GROUP, groupID))
				continue
			}
			b.groups[key] = &groupRef{id: groupID, ordinal: strings.TrimSpace(g.Ordinal)}
		}
	}
}

// splitKind decides whether a split's groups overlap everything outside the
// split. Optivum never states it, so the split's own name decides: a split by
// gender or by a chosen subject is a fact about a student, a numbered split is
// a plan choice the school makes. Getting this wrong yields a timetable that
// looks right and collides in practice, so anything unrecognised stays OPEN
// and is reported.
func splitKind(s split) (arrangov1.SplitKind, bool) {
	// The subject counts towards a fact about a student: a generic split name
	// over religia or a language is still not a plan choice.
	name := strings.ToLower(strings.TrimSpace(s.Code) + " " + strings.TrimSpace(s.Subject))
	// Both spellings: a school that types without Polish letters still means
	// the same thing.
	fixed := []string{
		"płci", "plci", "chłop", "chlop", "dziewcz",
		"religi", "etyk", "język", "jezyk", "wyznan", "zawod",
	}
	for _, marker := range fixed {
		if strings.Contains(name, marker) {
			return arrangov1.SplitKind_SPLIT_KIND_FIXED, true
		}
	}
	// A split into a stated number of groups is a plan choice the school
	// makes, which is what OPEN means; its groups never constrain each other.
	// Any other name says nothing, and a wrong guess here yields a timetable
	// that looks right and collides in practice.
	code := strings.ToLower(strings.TrimSpace(s.Code))
	for _, marker := range []string{
		"na dwie", "na trzy", "na 2", "na 3", "na 4", "/2", "/3", "/4",
	} {
		if strings.Contains(code, marker) {
			return arrangov1.SplitKind_SPLIT_KIND_OPEN, true
		}
	}
	return arrangov1.SplitKind_SPLIT_KIND_OPEN, false
}

// --- rooms ------------------------------------------------------------

func (b *builder) addRooms(doc *document) {
	for _, bu := range doc.Buildings {
		code := strings.TrimSpace(bu.Code)
		if code == "" || b.buildings[code] != 0 {
			continue
		}
		id := b.id()
		b.model.Buildings = append(b.model.Buildings, &arrangov1.Building{Id: id, Name: code})
		b.buildings[code] = id
	}

	for _, r := range doc.Rooms {
		code := strings.TrimSpace(r.Code)
		if code == "" {
			b.note(formats.KindUnknownReference, formats.Warning, "room without a code, skipped")
			continue
		}
		if _, exists := b.rooms[code]; exists {
			b.note(formats.KindUnknownReference, formats.Warning,
				"room code "+code+" appears twice, the first wins")
			continue
		}
		capacity, source := count(r.Capacity)
		id := b.id()
		var buildingID uint32
		if wanted := strings.TrimSpace(r.Building); wanted != "" {
			buildingID = b.buildings[wanted]
			if buildingID == 0 {
				b.note(formats.KindUnknownReference, formats.Warning,
					"room "+code+" names building "+wanted+", which the document does not define",
					formats.Ref(arrangov1.EntityKind_ENTITY_KIND_ROOM, id))
			}
		}
		b.model.Rooms = append(b.model.Rooms, &arrangov1.Room{
			Id:             id,
			Name:           firstNonEmpty(r.Name, code),
			Designator:     code,
			Capacity:       capacity,
			CapacitySource: source,
			SourceRef:      sourceRef("sala", code),
			BuildingId:     buildingID,
		})
		b.rooms[code] = &roomRef{id: id, designator: code}
	}

	// Room sets are not entities in the model: a preference naming one
	// expands to the designators of its members.
	for _, s := range doc.RoomSets {
		code := strings.TrimSpace(s.Code)
		if code == "" {
			continue
		}
		if _, exists := b.roomSets[code]; exists {
			b.note(formats.KindUnknownReference, formats.Warning,
				"room set "+code+" appears twice, the first wins")
			continue
		}
		set := &setRef{}
		for _, token := range prefTokens(s.Elements) {
			switch {
			case token == anyRoom:
				set.wildcard = true
			case token == sharedRoom:
				b.sharedRooms++
			case b.rooms[token] != nil:
				set.designators = append(set.designators, b.rooms[token].designator)
			default:
				b.unresolvedPrefTokens++
			}
		}
		b.roomSets[code] = set
	}
}

// prefTokens splits a room preference or a room set into its members. Only a
// comma separates them: a room or set code may hold a space, and splitting one
// in half makes both halves unresolvable, which drops the restriction the
// school stated.
func prefTokens(value string) []string {
	var out []string
	for _, field := range strings.FieldsFunc(value, func(r rune) bool {
		return r == ',' || r == ';'
	}) {
		if trimmed := strings.TrimSpace(field); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}

// roomPreference is one assignment's room wish, in the order Optivum states
// it: the first token is the best choice and a wildcard opens every room.
type roomPreference struct {
	wildcard bool
	tiers    [][]string
}

func (b *builder) roomPreference(value string) roomPreference {
	var pref roomPreference
	for _, token := range prefTokens(value) {
		switch {
		case token == anyRoom:
			pref.wildcard = true
		case token == sharedRoom:
			b.sharedRooms++
		case b.rooms[token] != nil:
			pref.tiers = append(pref.tiers, []string{b.rooms[token].designator})
		case b.roomSets[token] != nil:
			set := b.roomSets[token]
			if set.wildcard {
				pref.wildcard = true
			}
			if len(set.designators) > 0 {
				pref.tiers = append(pref.tiers, set.designators)
			}
		default:
			b.unresolvedPrefTokens++
		}
	}
	return pref
}

// flat returns every designator the preference names, best first, without
// repeats.
func (p roomPreference) flat() []string {
	seen := map[string]bool{}
	var out []string
	for _, tier := range p.tiers {
		for _, designator := range tier {
			if seen[designator] {
				continue
			}
			seen[designator] = true
			out = append(out, designator)
		}
	}
	return out
}

// --- teachers and subjects --------------------------------------------

func (b *builder) addTeachers(doc *document) {
	for _, t := range doc.Teachers {
		code := strings.TrimSpace(t.Code)
		if code == "" {
			b.note(formats.KindUnknownReference, formats.Warning, "teacher without a code, skipped")
			continue
		}
		if _, exists := b.teachers[code]; exists {
			b.note(formats.KindUnknownReference, formats.Warning,
				"teacher code "+code+" appears twice, the first wins")
			continue
		}
		id := b.id()
		b.model.Teachers = append(b.model.Teachers, &arrangov1.Teacher{
			Id:                       id,
			Name:                     teacherName(t, code),
			SourceRef:                sourceRef("nauczyciel", code),
			PreferredRoomDesignators: b.teacherRooms(t),
		})
		b.teachers[code] = id
		if firstNonEmpty(t.Load, t.LoadReduction) != "" {
			b.teacherLoads++
		}
	}
}

// teacherName is a name and a surname, which is all a timetable needs about a
// person.
func teacherName(t teacher, code string) string {
	name := strings.TrimSpace(strings.TrimSpace(t.First) + " " + strings.TrimSpace(t.Last))
	return firstNonEmpty(name, code)
}

// teacherRooms reads the rooms a teacher would rather teach in. The home room
// is free text that starts with a room code; the preference list uses the same
// grammar as an assignment's.
func (b *builder) teacherRooms(t teacher) []string {
	pref := b.roomPreference(t.Prefers)
	designators := pref.flat()

	if home := b.homeRoom(t.Rooms); home != "" {
		for _, existing := range designators {
			if existing == home {
				return designators
			}
		}
		designators = append([]string{home}, designators...)
	} else if strings.TrimSpace(t.Rooms) != "" {
		b.unresolvedHomeRooms++
	}
	return designators
}

// homeRoom reads the room code a teacher's free-text home room starts with.
// The longest prefix wins, because a room code may itself hold a space.
func (b *builder) homeRoom(value string) string {
	fields := strings.Fields(value)
	for take := len(fields); take > 0; take-- {
		if room := b.rooms[strings.Join(fields[:take], " ")]; room != nil {
			return room.designator
		}
	}
	return ""
}

func (b *builder) addSubjects(doc *document) {
	// prefers_blocks is not in the file: a subject the school teaches in
	// multi-period blocks anywhere is a subject that prefers them.
	for _, a := range doc.Assignments {
		if length, ok := index(a.Length); ok && length > 1 {
			b.blockedSubjects[strings.TrimSpace(a.Subject)] = true
		}
	}

	for _, s := range doc.Subjects {
		code := strings.TrimSpace(s.Code)
		if code == "" {
			b.note(formats.KindUnknownReference, formats.Warning, "subject without a code, skipped")
			continue
		}
		if _, exists := b.subjects[code]; exists {
			b.note(formats.KindUnknownReference, formats.Warning,
				"subject code "+code+" appears twice, the first wins")
			continue
		}
		id := b.id()
		b.model.Subjects = append(b.model.Subjects, &arrangov1.Subject{
			Id:            id,
			Name:          firstNonEmpty(s.Name, code),
			PrefersBlocks: b.blockedSubjects[code],
			SourceRef:     sourceRef("przedmiot", code),
		})
		b.subjects[code] = id
	}
}

// --- lessons ----------------------------------------------------------

// addLessons turns assignment rows into lesson instances.
//
// One lesson block is stored as a chain of rows, one row per period: the
// first row carries the block length and every row points at the next by its
// 0-based position in the document. A row without a length is therefore a
// continuation of the row that named it, and never a lesson of its own.
func (b *builder) addLessons(doc *document) {
	rows := doc.Assignments
	continuation := make([]bool, len(rows))
	chains := make([][]int, len(rows))

	for i := range rows {
		length, ok := index(rows[i].Length)
		if !ok {
			continue
		}
		if length < 1 {
			length = 1
		}
		chain := []int{i}
		for len(chain) < length {
			next, ok := index(rows[chain[len(chain)-1]].Next)
			// One period of the week must never belong to two lessons, so a
			// row already claimed by another block ends this chain, as does a
			// row that names its own length and so starts its own block.
			if !ok || next < 0 || next >= len(rows) || next == i || continuation[next] {
				break
			}
			if _, isHead := index(rows[next].Length); isHead {
				break
			}
			continuation[next] = true
			chain = append(chain, next)
		}
		if len(chain) < length {
			b.note(formats.KindBrokenBlockChain, formats.Warning,
				fmt.Sprintf("block of %d periods has %d rows, shortened", length, len(chain)))
		}
		chains[i] = chain
	}

	lessonByRow := map[int]*arrangov1.LessonInstance{}
	var pinOrder []string
	pins := map[string][]int{}

	for i, row := range rows {
		if continuation[i] {
			continue
		}
		chain := chains[i]
		if chain == nil {
			// Neither a block start nor reachable from one: keep the demand
			// as a single period rather than dropping it.
			b.note(formats.KindBrokenBlockChain, formats.Warning,
				fmt.Sprintf("assignment row %d belongs to no block, read as one period", i+1))
			chain = []int{i}
		}
		lesson := b.lesson(rows, row, chain)
		if lesson == nil {
			continue
		}
		b.model.Lessons = append(b.model.Lessons, lesson)
		lessonByRow[i] = lesson

		if pin := strings.TrimSpace(row.Pin); pin != "" {
			if _, seen := pins[pin]; !seen {
				pinOrder = append(pinOrder, pin)
			}
			pins[pin] = append(pins[pin], i)
		}
		if strings.TrimSpace(row.Flags) != "" {
			b.flaggedAssignments++
		}
	}

	// A staple ("spinacz") pins rows that must start at the same time. Every
	// member of one staple shares a slot in both sample files, which is what
	// a parallel block means to the solver; a link would allow them to drift
	// to different periods of one day.
	for _, pin := range pinOrder {
		members := pins[pin]
		if len(members) < 2 {
			b.degeneratePins++
			continue
		}
		blockID := b.id()
		for _, row := range members {
			lessonByRow[row].ParallelBlockId = blockID
		}
	}
}

// lesson builds one lesson from a block's rows, or nil when a reference the
// lesson cannot do without is missing.
func (b *builder) lesson(rows []assignment, head assignment, chain []int) *arrangov1.LessonInstance {
	division := b.divisions[strings.TrimSpace(head.Division)]
	if division == nil {
		b.note(formats.KindLessonDropped, formats.Error,
			"assignment names division "+strings.TrimSpace(head.Division)+", which the document does not define")
		return nil
	}
	subjectID := b.subjects[strings.TrimSpace(head.Subject)]
	if subjectID == 0 {
		b.note(formats.KindLessonDropped, formats.Error,
			"assignment names subject "+strings.TrimSpace(head.Subject)+", which the document does not define",
			formats.Ref(arrangov1.EntityKind_ENTITY_KIND_DIVISION, division.id))
		return nil
	}

	var teacherID uint32
	if code := strings.TrimSpace(head.Teacher); code != "" {
		teacherID = b.teachers[code]
		if teacherID == 0 {
			b.note(formats.KindLessonDropped, formats.Error,
				"assignment names teacher "+code+", which the document does not define",
				formats.Ref(arrangov1.EntityKind_ENTITY_KIND_DIVISION, division.id),
				formats.Ref(arrangov1.EntityKind_ENTITY_KIND_SUBJECT, subjectID))
			return nil
		}
	}

	var groupID uint32
	if code := strings.TrimSpace(head.Group); code != "" {
		group := b.groups[division.code+"|"+code]
		switch {
		case group == nil:
			// Widening to the whole division keeps the lesson and cannot
			// create a collision; it only stops the solver parallelising it.
			b.note(formats.KindUnknownReference, formats.Warning,
				"assignment names group "+code+" in division "+division.code+
					", which the document does not define, read as the whole division",
				formats.Ref(arrangov1.EntityKind_ENTITY_KIND_DIVISION, division.id),
				formats.Ref(arrangov1.EntityKind_ENTITY_KIND_SUBJECT, subjectID))
		default:
			groupID = group.id
			if ordinal := strings.TrimSpace(head.Ordinal); ordinal != "" &&
				group.ordinal != "" && ordinal != group.ordinal {
				b.note(formats.KindGroupOrdinalMismatch, formats.Warning,
					"assignment says group ordinal "+ordinal+", group "+code+
						" of division "+division.code+" is number "+group.ordinal,
					formats.Ref(arrangov1.EntityKind_ENTITY_KIND_GROUP, group.id))
			}
		}
	}

	duration := uint32(len(chain))
	if periods := uint32(len(b.model.Periods)); periods > 0 && duration > periods {
		b.note(formats.KindBrokenBlockChain, formats.Warning,
			fmt.Sprintf("block of %d periods is longer than the school day, shortened to %d", duration, periods),
			formats.Ref(arrangov1.EntityKind_ENTITY_KIND_DIVISION, division.id))
		duration = periods
	}

	roomID := b.roomOf(head, division, subjectID)
	pref := b.roomPreference(head.Prefers)
	lesson := &arrangov1.LessonInstance{
		Id:              b.id(),
		SubjectId:       subjectID,
		TeacherId:       teacherID,
		Duration:        duration,
		RequiresTeacher: teacherID != 0,
		RequiresRoom:    roomID != 0 || pref.wildcard || len(pref.tiers) > 0,
		Participants: []*arrangov1.Participant{
			{DivisionId: division.id, GroupId: groupID},
		},
	}
	b.setRoomEligibility(lesson, pref)

	// The imported room is a stability hint, never an implicit "only this
	// room is allowed", so it lives in the placement and not in eligibility.
	if placement, ok := b.placement(head, roomID, duration, division); ok {
		lesson.HasPrevious = true
		lesson.PreviousPlacement = placement
		b.schedule.Lessons = append(b.schedule.Lessons, &arrangov1.ScheduledLesson{
			LessonId: lesson.Id,
			Placement: &arrangov1.Placement{
				DayId: placement.DayId, StartPeriod: placement.StartPeriod, RoomId: placement.RoomId,
			},
		})
	}

	for _, row := range chain[1:] {
		if strings.TrimSpace(rows[row].Room) != strings.TrimSpace(head.Room) {
			b.note(formats.KindBlockRoomChange, formats.Warning,
				"a period of this block sits in another room; the block keeps the first one",
				formats.Ref(arrangov1.EntityKind_ENTITY_KIND_DIVISION, division.id),
				formats.Ref(arrangov1.EntityKind_ENTITY_KIND_SUBJECT, subjectID))
			break
		}
	}
	return lesson
}

// roomOf resolves the room an assignment sits in, if any.
func (b *builder) roomOf(head assignment, division *divisionRef, subjectID uint32) uint32 {
	code := strings.TrimSpace(head.Room)
	if code == "" {
		return 0
	}
	if code == sharedRoom {
		b.sharedRooms++
		return 0
	}
	if room := b.rooms[code]; room != nil {
		return room.id
	}
	b.note(formats.KindUnknownReference, formats.Warning,
		"assignment sits in room "+code+", which the document does not define",
		formats.Ref(arrangov1.EntityKind_ENTITY_KIND_DIVISION, division.id),
		formats.Ref(arrangov1.EntityKind_ENTITY_KIND_SUBJECT, subjectID))
	return 0
}

// setRoomEligibility fills the three room tiers. A wildcard means the school
// accepts any room, and eligibility is the union of the tiers, so a wildcard
// has to leave all three empty or it would restrict what it opened.
func (b *builder) setRoomEligibility(lesson *arrangov1.LessonInstance, pref roomPreference) {
	if pref.wildcard {
		if len(pref.tiers) > 0 {
			b.openRoomPreferences++
		}
		return
	}
	seen := map[string]bool{}
	take := func(tier []string) []string {
		var out []string
		for _, designator := range tier {
			if seen[designator] {
				continue
			}
			seen[designator] = true
			out = append(out, designator)
		}
		return out
	}
	for i, tier := range pref.tiers {
		switch {
		case i == 0:
			lesson.AllowedRoomDesignators = take(tier)
		case i == 1:
			lesson.AllowedRoomDesignatorsTier2 = take(tier)
		default:
			lesson.AllowedRoomDesignatorsTier3 = append(lesson.AllowedRoomDesignatorsTier3, take(tier)...)
		}
	}
}

// placement reads where the school has already put this lesson. Day and
// period are 0-based indices into document order.
func (b *builder) placement(head assignment, roomID, duration uint32, division *divisionRef) (*arrangov1.Placement, bool) {
	day, hasDay := index(head.Day)
	period, hasPeriod := index(head.Period)
	// Both attributes absent is a lesson the school has not placed yet, which
	// is ordinary. A value that is there but unreadable is not.
	if strings.TrimSpace(head.Day) == "" && strings.TrimSpace(head.Period) == "" {
		b.unplacedLessons++
		return nil, false
	}
	if !hasDay || !hasPeriod || day >= len(b.dayIDs) || period >= len(b.periodIDs) {
		b.note(formats.KindUnknownReference, formats.Warning,
			fmt.Sprintf("assignment sits at day %q period %q, outside the school week", head.Day, head.Period),
			formats.Ref(arrangov1.EntityKind_ENTITY_KIND_DIVISION, division.id))
		b.unplacedLessons++
		return nil, false
	}

	if end := uint32(period) + duration; end > b.dayEnd[day] {
		b.dayEnd[day] = end
	}
	return &arrangov1.Placement{
		DayId:       b.dayIDs[day],
		StartPeriod: uint32(period),
		RoomId:      roomID,
	}, true
}

// --- unavailability, daily load, calendar close-out -------------------

type slot struct{ day, period int }

// ownerSlots is one entity's blocked week, kept in document order so that
// external block ids are stable across imports.
type ownerSlots struct {
	target  arrangov1.ExternalBlock_Target
	id      uint32
	blocked []slot
}

// collectSlots reads the marked week of every teacher, room and division. A
// division's allowed marks are the school's own statement of how long its
// teaching day is, which is what bounds each day of the model.
func (b *builder) collectSlots(doc *document) []ownerSlots {
	var out []ownerSlots

	add := func(target arrangov1.ExternalBlock_Target, id uint32, cells []infoCell) {
		if id == 0 || len(cells) == 0 {
			return
		}
		owner := ownerSlots{target: target, id: id}
		for _, cell := range cells {
			day, hasDay := index(cell.Day)
			period, hasPeriod := index(cell.Period)
			if !hasDay || !hasPeriod || day >= len(b.dayIDs) || period >= len(b.periodIDs) {
				b.outOfRangeSlots++
				continue
			}
			switch strings.TrimSpace(cell.Mark) {
			case markBlocked:
				owner.blocked = append(owner.blocked, slot{day: day, period: period})
			case markAllowed:
				if end := uint32(period + 1); end > b.dayEnd[day] {
					b.dayEnd[day] = end
				}
			case markPreferred:
				b.preferredSlots++
			default:
				b.unknownSlotMarks++
			}
		}
		if len(owner.blocked) > 0 {
			out = append(out, owner)
		}
	}

	for _, t := range doc.Teachers {
		add(arrangov1.ExternalBlock_TARGET_TEACHER, b.teachers[strings.TrimSpace(t.Code)], t.Cells)
	}
	for _, r := range doc.Rooms {
		if room := b.rooms[strings.TrimSpace(r.Code)]; room != nil {
			add(arrangov1.ExternalBlock_TARGET_ROOM, room.id, r.Cells)
		}
	}
	for _, d := range doc.Divisions {
		if division := b.divisions[strings.TrimSpace(d.Code)]; division != nil {
			add(arrangov1.ExternalBlock_TARGET_DIVISION, division.id, d.Cells)
		}
	}
	return out
}

// addUnavailability merges each owner's blocked slots into one external block
// per run of consecutive periods. Target is always set explicitly: an unset
// target silently becomes a division block.
func (b *builder) addUnavailability(owners []ownerSlots) {
	for _, owner := range owners {
		for _, run := range runs(owner.blocked) {
			limit := b.model.Days[run.day].GetPeriodCount()
			if uint32(run.start) >= limit {
				b.trimmedBlocks++
				continue
			}
			duration := run.length
			if uint32(run.start)+duration > limit {
				duration = limit - uint32(run.start)
				b.trimmedBlocks++
			}
			b.model.ExternalBlocks = append(b.model.ExternalBlocks, &arrangov1.ExternalBlock{
				Id:          b.id(),
				Name:        blockLabel,
				Target:      owner.target,
				TargetId:    owner.id,
				DayId:       b.dayIDs[run.day],
				StartPeriod: uint32(run.start),
				Duration:    duration,
			})
		}
	}
}

type run struct {
	day    int
	start  int
	length uint32
}

// runs merges slots into runs of consecutive periods on one day.
func runs(slots []slot) []run {
	if len(slots) == 0 {
		return nil
	}
	sorted := make([]slot, len(slots))
	copy(sorted, slots)
	sort.Slice(sorted, func(i, j int) bool {
		if sorted[i].day != sorted[j].day {
			return sorted[i].day < sorted[j].day
		}
		return sorted[i].period < sorted[j].period
	})

	var out []run
	current := run{day: sorted[0].day, start: sorted[0].period, length: 1}
	for _, next := range sorted[1:] {
		if next.day == current.day && next.period == current.start+int(current.length) {
			current.length++
			continue
		}
		if next.day == current.day && next.period < current.start+int(current.length) {
			continue // the same slot marked twice
		}
		out = append(out, current)
		current = run{day: next.day, start: next.period, length: 1}
	}
	return append(out, current)
}

// addDailyLoad carries the school's own bounds on a division's teaching day.
func (b *builder) addDailyLoad(doc *document) {
	for _, d := range doc.Divisions {
		division := b.divisions[strings.TrimSpace(d.Code)]
		if division == nil {
			continue
		}
		minimum, hasMinimum := index(d.MinPerDay)
		maximum, hasMaximum := index(d.MaxPerDay)
		if (!hasMinimum || minimum == 0) && (!hasMaximum || maximum == 0) {
			continue
		}
		if hasMaximum && maximum > 0 && minimum > maximum {
			b.note(formats.KindUnknownReference, formats.Warning,
				fmt.Sprintf("division %s wants at least %d and at most %d lessons a day; the minimum is dropped",
					division.code, minimum, maximum),
				formats.Ref(arrangov1.EntityKind_ENTITY_KIND_DIVISION, division.id))
			minimum = 0
		}
		b.model.DailyLoadRules = append(b.model.DailyLoadRules, &arrangov1.DailyLoadRule{
			DivisionId: division.id,
			MinPerDay:  uint32(minimum),
			MaxPerDay:  uint32(maximum),
		})
	}
}

// finishDays sets how many periods each day can hold. A day nobody marked and
// nobody teaches on keeps the full period list.
func (b *builder) finishDays() {
	periods := uint32(len(b.model.Periods))
	if len(b.model.Days) == 0 || periods == 0 {
		b.note(formats.KindEmptyCalendar, formats.Error,
			fmt.Sprintf("the document defines %d days and %d periods", len(b.model.Days), periods))
		return
	}
	for i, day := range b.model.Days {
		end := b.dayEnd[i]
		if end == 0 {
			end = periods
		}
		if end > periods {
			end = periods
		}
		day.PeriodCount = end
	}
}

// --- findings ---------------------------------------------------------

// reportUnmapped says what the reader did not read. Bulk is aggregated: one
// finding per kind with a count, never one per row.
func (b *builder) reportUnmapped(shape *census) {
	for _, path := range shape.unmapped() {
		b.note(formats.KindUnmappedElement, formats.Info,
			fmt.Sprintf("%s: %d not read", path, shape.counts[path]))
	}

	if b.teacherLoads > 0 {
		b.note(formats.KindUnmappedAttribute, formats.Info,
			fmt.Sprintf("teaching load and its reduction: %d teachers, no field in the model", b.teacherLoads))
	}
	if b.formTutors > 0 {
		b.note(formats.KindUnmappedAttribute, formats.Info,
			fmt.Sprintf("form tutor: %d divisions, no field in the model", b.formTutors))
	}
	if b.flaggedAssignments > 0 {
		b.note(formats.KindUnmappedAttribute, formats.Info,
			fmt.Sprintf("assignment flags: %d rows, not interpreted", b.flaggedAssignments))
	}

	if b.fixedSplits > 0 || b.uncertainSplits > 0 {
		b.note(formats.KindSplitKindInferred, formats.Info,
			fmt.Sprintf("the file does not state whether a split is a fact about a student: %d read as fixed by name, %d unrecognised names kept open",
				b.fixedSplits, b.uncertainSplits))
	}
	if b.sharedRooms > 0 {
		b.note(formats.KindUnmappedAttribute, formats.Info,
			fmt.Sprintf("%d room references are the unnamed-room marker %q, read as no particular room", b.sharedRooms, sharedRoom))
	}
	if b.unplacedLessons > 0 {
		b.note(formats.KindLessonUnplaced, formats.Info,
			fmt.Sprintf("%d lessons carry no placement", b.unplacedLessons))
	}
	if b.openRoomPreferences > 0 {
		b.note(formats.KindRoomPreferenceOpen, formats.Info,
			fmt.Sprintf("%d lessons accept any room, so their preferred rooms are not a restriction", b.openRoomPreferences))
	}
	if b.unresolvedPrefTokens > 0 {
		b.note(formats.KindUnknownReference, formats.Warning,
			fmt.Sprintf("%d room preferences name neither a room nor a room set", b.unresolvedPrefTokens))
	}
	if b.unresolvedHomeRooms > 0 {
		b.note(formats.KindUnknownReference, formats.Info,
			fmt.Sprintf("%d teacher home rooms do not start with a known room", b.unresolvedHomeRooms))
	}
	if b.preferredSlots > 0 {
		b.note(formats.KindUnmappedSlotMark, formats.Info,
			fmt.Sprintf("%d slots are marked as preferred, which the model cannot express", b.preferredSlots))
	}
	if b.unknownSlotMarks > 0 {
		b.note(formats.KindUnmappedSlotMark, formats.Warning,
			fmt.Sprintf("%d slots carry a mark this reader does not know", b.unknownSlotMarks))
	}
	if b.outOfRangeSlots > 0 {
		b.note(formats.KindUnmappedSlotMark, formats.Warning,
			fmt.Sprintf("%d marked slots name a day or period the file does not define", b.outOfRangeSlots))
	}
	if b.trimmedBlocks > 0 {
		b.note(formats.KindUnmappedSlotMark, formats.Info,
			fmt.Sprintf("%d blocked slots sit after the last teaching period of their day", b.trimmedBlocks))
	}
	if b.degeneratePins > 0 {
		b.note(formats.KindUnknownReference, formats.Info,
			fmt.Sprintf("%d staples pin a single lesson, so nothing is pinned", b.degeneratePins))
	}
}

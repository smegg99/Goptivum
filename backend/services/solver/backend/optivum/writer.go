// optivum/writer.go

package optivum

import (
	"archive/zip"
	"bytes"
	"fmt"
	"html"
	"sort"
	"strings"

	arrangov1 "github.com/smegg99/arrango/backend/gen/arrangov1"
)

// ExportZip renders the schedule as a simplified Optivum-style HTML export:
// one o*.html per division, n*.html per teacher, s*.html per room, plus an
// index.html. It uses the same structural markers as the VULCAN export
// (tytulnapis, table.tabela, td.nr/g/l, span.p, a.n/a.s/a.o), so ParseZip
// re-imports it losslessly.
func ExportZip(model *arrangov1.SchoolModel,
	snapshot *arrangov1.ScheduleSnapshot) ([]byte, error) {
	if model == nil || snapshot == nil {
		return nil, fmt.Errorf("model and snapshot are required")
	}
	e := newExporter(model, snapshot)

	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	add := func(name, content string) error {
		f, err := w.Create(name)
		if err != nil {
			return err
		}
		_, err = f.Write([]byte(content))
		return err
	}

	if err := add("index.html", e.indexPage()); err != nil {
		return nil, err
	}
	for i, d := range model.Divisions {
		if err := add(fmt.Sprintf("o%d.html", i+1), e.divisionPage(d)); err != nil {
			return nil, err
		}
	}
	for i, t := range model.Teachers {
		if err := add(fmt.Sprintf("n%d.html", i+1), e.teacherPage(t)); err != nil {
			return nil, err
		}
	}
	for i, r := range model.Rooms {
		if err := add(fmt.Sprintf("s%d.html", i+1), e.roomPage(r)); err != nil {
			return nil, err
		}
	}
	if err := w.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// One rendered lesson occurrence inside a page cell.
type exportEntry struct {
	divisionID uint32
	subject    string // incl. -g/N suffix
	teacherID  uint32
	roomID     uint32
}

type exporter struct {
	model    *arrangov1.SchoolModel
	snapshot *arrangov1.ScheduleSnapshot

	divisionByID map[uint32]*arrangov1.Division
	groupByID    map[uint32]*arrangov1.Group
	teacherByID  map[uint32]*arrangov1.Teacher
	subjectByID  map[uint32]*arrangov1.Subject
	roomByID     map[uint32]*arrangov1.Room
	lessonByID   map[uint32]*arrangov1.LessonInstance
	dayIndex     map[uint32]int
	fileOf       map[string]string // "o:<id>" / "n:<id>" / "s:<id>" -> file

	// cells[kind:<id>][day][period] -> entries
	cells map[string][][][]exportEntry
}

func newExporter(model *arrangov1.SchoolModel,
	snapshot *arrangov1.ScheduleSnapshot) *exporter {
	e := &exporter{
		model:        model,
		snapshot:     snapshot,
		divisionByID: map[uint32]*arrangov1.Division{},
		groupByID:    map[uint32]*arrangov1.Group{},
		teacherByID:  map[uint32]*arrangov1.Teacher{},
		subjectByID:  map[uint32]*arrangov1.Subject{},
		roomByID:     map[uint32]*arrangov1.Room{},
		lessonByID:   map[uint32]*arrangov1.LessonInstance{},
		dayIndex:     map[uint32]int{},
		fileOf:       map[string]string{},
		cells:        map[string][][][]exportEntry{},
	}
	for _, d := range model.Divisions {
		e.divisionByID[d.Id] = d
	}
	for _, g := range model.Groups {
		e.groupByID[g.Id] = g
	}
	for _, t := range model.Teachers {
		e.teacherByID[t.Id] = t
	}
	for _, s := range model.Subjects {
		e.subjectByID[s.Id] = s
	}
	for _, r := range model.Rooms {
		e.roomByID[r.Id] = r
	}
	for _, l := range model.Lessons {
		e.lessonByID[l.Id] = l
	}
	for i, d := range model.Days {
		e.dayIndex[d.Id] = i
	}
	for i, d := range model.Divisions {
		e.fileOf["o:"+fmt.Sprint(d.Id)] = fmt.Sprintf("o%d.html", i+1)
	}
	for i, t := range model.Teachers {
		e.fileOf["n:"+fmt.Sprint(t.Id)] = fmt.Sprintf("n%d.html", i+1)
	}
	for i, r := range model.Rooms {
		e.fileOf["s:"+fmt.Sprint(r.Id)] = fmt.Sprintf("s%d.html", i+1)
	}
	e.fillCells()
	return e
}

func (e *exporter) grid(key string) [][][]exportEntry {
	if g, ok := e.cells[key]; ok {
		return g
	}
	g := make([][][]exportEntry, len(e.model.Days))
	for d := range g {
		g[d] = make([][]exportEntry, e.periodCount())
	}
	e.cells[key] = g
	return g
}

func (e *exporter) periodCount() int { return len(e.model.Periods) }

func (e *exporter) fillCells() {
	for _, sl := range e.snapshot.Lessons {
		lesson := e.lessonByID[sl.LessonId]
		if lesson == nil || sl.Placement == nil {
			continue
		}
		day, ok := e.dayIndex[sl.Placement.DayId]
		if !ok {
			continue
		}
		baseSubject := ""
		if s := e.subjectByID[lesson.SubjectId]; s != nil {
			baseSubject = s.Name
		}
		teacherID := lesson.TeacherId
		if !lesson.RequiresTeacher {
			teacherID = 0
		}
		roomID := sl.Placement.RoomId
		duration := lesson.Duration
		if duration == 0 {
			duration = 1
		}
		// One lesson may serve several participants (merged/cross-division
		// groups). Each participant renders in its own division grid; the
		// teacher and room grids receive the lesson exactly once.
		participants := lesson.GetParticipants()
		for q := sl.Placement.StartPeriod; q < sl.Placement.StartPeriod+duration; q++ {
			if int(q) >= e.periodCount() {
				break
			}
			for pi, part := range participants {
				subject := baseSubject
				if g := e.groupByID[part.GetGroupId()]; g != nil {
					subject += "-" + g.Name
				}
				entry := exportEntry{
					divisionID: part.GetDivisionId(),
					subject:    subject,
					teacherID:  teacherID,
					roomID:     roomID,
				}
				e.grid("o:"+fmt.Sprint(part.GetDivisionId()))[day][q] =
					append(e.grid("o:"+fmt.Sprint(part.GetDivisionId()))[day][q], entry)
				if pi != 0 {
					continue // teacher/room grids get the lesson only once
				}
				if teacherID != 0 {
					e.grid("n:"+fmt.Sprint(teacherID))[day][q] =
						append(e.grid("n:"+fmt.Sprint(teacherID))[day][q], entry)
				}
				if roomID != 0 {
					e.grid("s:"+fmt.Sprint(roomID))[day][q] =
						append(e.grid("s:"+fmt.Sprint(roomID))[day][q], entry)
				}
			}
		}
	}
	// Deterministic order inside every cell.
	for _, g := range e.cells {
		for d := range g {
			for q := range g[d] {
				sort.Slice(g[d][q], func(i, j int) bool {
					a, b := g[d][q][i], g[d][q][j]
					if a.subject != b.subject {
						return a.subject < b.subject
					}
					return a.divisionID < b.divisionID
				})
			}
		}
	}
}

func (e *exporter) linkDivision(id uint32) string {
	d := e.divisionByID[id]
	if d == nil {
		return ""
	}
	return fmt.Sprintf(`<a href="%s" class="o">%s</a>`,
		e.fileOf["o:"+fmt.Sprint(id)], html.EscapeString(d.Name))
}

func (e *exporter) linkTeacher(id uint32) string {
	t := e.teacherByID[id]
	if t == nil {
		return ""
	}
	return fmt.Sprintf(`<a href="%s" class="n">%s</a>`,
		e.fileOf["n:"+fmt.Sprint(id)], html.EscapeString(t.Name))
}

func (e *exporter) linkRoom(id uint32) string {
	r := e.roomByID[id]
	if r == nil {
		return ""
	}
	return fmt.Sprintf(`<a href="%s" class="s">%s</a>`,
		e.fileOf["s:"+fmt.Sprint(id)], html.EscapeString(r.Name))
}

// kind: which link belongs on the page ('o' pages link teacher+room, ...).
func (e *exporter) entryHTML(entry exportEntry, kind byte) string {
	parts := []string{}
	if kind != 'o' {
		if l := e.linkDivision(entry.divisionID); l != "" {
			parts = append(parts, l)
		}
	}
	parts = append(parts,
		`<span class="p">`+html.EscapeString(entry.subject)+`</span>`)
	if kind != 'n' {
		if l := e.linkTeacher(entry.teacherID); l != "" {
			parts = append(parts, l)
		}
	}
	if kind != 's' {
		if l := e.linkRoom(entry.roomID); l != "" {
			parts = append(parts, l)
		}
	}
	return strings.Join(parts, " ")
}

const pageStyle = `<style>
body { font-family: sans-serif; margin: 16px; }
.tytulnapis { font-size: 1.4em; font-weight: bold; }
table.tabela { border-collapse: collapse; margin-top: 12px; }
table.tabela th, table.tabela td { border: 1px solid #999; padding: 4px 8px; }
td.nr { text-align: right; color: #666; }
td.g { white-space: nowrap; color: #666; }
span.p { font-weight: bold; }
a { color: #3f51b5; text-decoration: none; }
</style>`

func (e *exporter) page(title, key string, kind byte) string {
	var b strings.Builder
	b.WriteString("<html><head>\n")
	b.WriteString(`<meta http-equiv="Content-Type" content="text/html; charset=utf-8">` + "\n")
	b.WriteString("<title>" + html.EscapeString(title) + "</title>\n")
	b.WriteString(pageStyle + "\n</head>\n<body>\n")
	b.WriteString(`<span class="tytulnapis">` + html.EscapeString(title) +
		"</span>\n")
	b.WriteString(`<table class="tabela">` + "\n<tr>\n<th>Nr</th>\n<th>Godz</th>\n")
	for _, d := range e.model.Days {
		b.WriteString("<th>" + html.EscapeString(d.Name) + "</th>\n")
	}
	b.WriteString("</tr>\n")
	grid := e.grid(key)
	for q := 0; q < e.periodCount(); q++ {
		b.WriteString("<tr>\n")
		b.WriteString(fmt.Sprintf(`<td class="nr">%d</td>`, q+1) + "\n")
		b.WriteString(`<td class="g">` +
			html.EscapeString(e.model.Periods[q].Name) + "</td>\n")
		for d := range e.model.Days {
			entries := grid[d][q]
			if len(entries) == 0 {
				b.WriteString(`<td class="l">&nbsp;</td>` + "\n")
				continue
			}
			rendered := make([]string, len(entries))
			for i, entry := range entries {
				rendered[i] = e.entryHTML(entry, kind)
			}
			b.WriteString(`<td class="l">` + strings.Join(rendered, "<br>") +
				"</td>\n")
		}
		b.WriteString("</tr>\n")
	}
	b.WriteString("</table>\n")
	b.WriteString(`<p><a href="index.html">Indeks</a></p>` + "\n")
	b.WriteString("</body></html>\n")
	return b.String()
}

func (e *exporter) divisionPage(d *arrangov1.Division) string {
	return e.page(d.Name, "o:"+fmt.Sprint(d.Id), 'o')
}

func (e *exporter) teacherPage(t *arrangov1.Teacher) string {
	return e.page(t.Name, "n:"+fmt.Sprint(t.Id), 'n')
}

func (e *exporter) roomPage(r *arrangov1.Room) string {
	return e.page(r.Name, "s:"+fmt.Sprint(r.Id), 's')
}

func (e *exporter) indexPage() string {
	var b strings.Builder
	b.WriteString("<html><head>\n")
	b.WriteString(`<meta http-equiv="Content-Type" content="text/html; charset=utf-8">` + "\n")
	title := e.model.Name
	if title == "" {
		title = "Plan lekcji"
	}
	b.WriteString("<title>" + html.EscapeString(title) + "</title>\n")
	b.WriteString(pageStyle + "\n</head>\n<body>\n")
	b.WriteString(`<span class="tytulnapis">` + html.EscapeString(title) +
		"</span>\n")
	section := func(name string, items []string) {
		b.WriteString("<h3>" + name + "</h3>\n<p>" +
			strings.Join(items, " · ") + "</p>\n")
	}
	var divisions, teachers, rooms []string
	for _, d := range e.model.Divisions {
		divisions = append(divisions, e.linkDivision(d.Id))
	}
	for _, t := range e.model.Teachers {
		teachers = append(teachers, e.linkTeacher(t.Id))
	}
	for _, r := range e.model.Rooms {
		rooms = append(rooms, e.linkRoom(r.Id))
	}
	section("Oddziały", divisions)
	section("Nauczyciele", teachers)
	section("Sale", rooms)
	b.WriteString("</body></html>\n")
	return b.String()
}

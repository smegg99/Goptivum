// optivum/parser.go

package optivum

import (
	"archive/zip"
	"bytes"
	"fmt"
	"io/fs"
	"path"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"golang.org/x/net/html"

	arrangov1 "github.com/smegg99/arrango/backend/gen/arrangov1"
)

// Result of parsing one VULCAN Optivum HTML export: the raw demand data as
// a solvable SchoolModel plus the imported timetable as a snapshot.
type Result struct {
	Model    *arrangov1.SchoolModel
	Snapshot *arrangov1.ScheduleSnapshot
	Warnings []string
	Summary  Summary
}

type Summary struct {
	Divisions int `json:"divisions"`
	Groups    int `json:"groups"`
	Teachers  int `json:"teachers"`
	Rooms     int `json:"rooms"`
	Subjects  int `json:"subjects"`
	Lessons   int `json:"lessons"`
	Days      int `json:"days"`
	Periods   int `json:"periods"`
}

var (
	fileRe   = regexp.MustCompile(`^([ons])(\d+)\.html$`)
	suffixRe = regexp.MustCompile(`^(.*)-(\d+)/(\d+)$`)
)

// One lesson occurrence in one cell of a division page.
type occurrence struct {
	division    string
	day         int
	period      int
	subject     string // suffix stripped
	group       string // "" or e.g. "1/3"
	teacherFile string // "n37" or ""
	teacherCode string
	roomFile    string // "s21" or ""
	roomName    string
}

type pages struct {
	divisions map[string][]byte // "o1" -> bytes, etc.
	teachers  map[string][]byte
	rooms     map[string][]byte
}

// ParseFS parses an export laid out as files anywhere inside fsys.
// Only returns an error when nothing usable was found.
func ParseFS(fsys fs.FS) (*Result, error) {
	p := pages{
		divisions: map[string][]byte{},
		teachers:  map[string][]byte{},
		rooms:     map[string][]byte{},
	}
	err := fs.WalkDir(fsys, ".", func(fpath string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		m := fileRe.FindStringSubmatch(path.Base(fpath))
		if m == nil {
			return nil
		}
		data, err := fs.ReadFile(fsys, fpath)
		if err != nil {
			return err
		}
		stem := m[1] + m[2]
		switch m[1] {
		case "o":
			p.divisions[stem] = data
		case "n":
			p.teachers[stem] = data
		case "s":
			p.rooms[stem] = data
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("walk archive: %w", err)
	}
	if len(p.divisions) == 0 {
		return nil, fmt.Errorf("no division pages (o*.html) found in archive")
	}
	return build(p)
}

// ParseZip parses an uploaded zip archive of the export.
func ParseZip(data []byte) (*Result, error) {
	reader, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, fmt.Errorf("open zip: %w", err)
	}
	return ParseFS(reader)
}

// --- HTML helpers -----------------------------------------------------

func hasClass(n *html.Node, class string) bool {
	for _, token := range strings.Fields(attr(n, "class")) {
		if token == class {
			return true
		}
	}
	return false
}

func attr(n *html.Node, key string) string {
	for _, a := range n.Attr {
		if a.Key == key {
			return a.Val
		}
	}
	return ""
}

func text(n *html.Node) string {
	var b strings.Builder
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.TextNode {
			b.WriteString(n.Data)
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(n)
	return strings.TrimSpace(b.String())
}

func findAll(root *html.Node, match func(*html.Node) bool) []*html.Node {
	var out []*html.Node
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode && match(n) {
			out = append(out, n)
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(root)
	return out
}

func pageTitle(root *html.Node) string {
	nodes := findAll(root, func(n *html.Node) bool {
		return hasClass(n, "tytulnapis")
	})
	if len(nodes) == 0 {
		return ""
	}
	return text(nodes[0])
}

func parseHTML(data []byte) (*html.Node, error) {
	return html.Parse(bytes.NewReader(data))
}

// --- division page parsing --------------------------------------------

type divisionPage struct {
	name        string // first token of the title, e.g. "5fgT"
	fullTitle   string
	dayNames    []string
	periodTimes map[int]string // 0-based period -> "7:00- 7:45"
	occurrences []occurrence
	warnings    []string
}

func hrefStem(href string) string {
	base := path.Base(href)
	if m := fileRe.FindStringSubmatch(base); m != nil {
		return m[1] + m[2]
	}
	return ""
}

// Parses one o*.html page. Cell entries start at each span.p; the teacher
// (a.n) and room (a.s) links that follow belong to the entry.
func parseDivisionPage(stem string, data []byte) (*divisionPage, error) {
	root, err := parseHTML(data)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", stem, err)
	}
	page := &divisionPage{periodTimes: map[int]string{}}
	page.fullTitle = pageTitle(root)
	if page.fullTitle == "" {
		return nil, fmt.Errorf("%s: no title", stem)
	}
	page.name = strings.Fields(page.fullTitle)[0]

	tables := findAll(root, func(n *html.Node) bool {
		return n.Data == "table" && hasClass(n, "tabela")
	})
	if len(tables) == 0 {
		return nil, fmt.Errorf("%s: no timetable table", stem)
	}
	rows := findAll(tables[0], func(n *html.Node) bool { return n.Data == "tr" })
	for _, row := range rows {
		headers := findAll(row, func(n *html.Node) bool { return n.Data == "th" })
		if len(headers) > 2 {
			for _, h := range headers[2:] {
				page.dayNames = append(page.dayNames, text(h))
			}
			continue
		}
		cells := findAll(row, func(n *html.Node) bool { return n.Data == "td" })
		if len(cells) < 3 {
			continue
		}
		period := -1
		day := 0
		for _, cell := range cells {
			switch {
			case hasClass(cell, "nr"):
				nr, err := strconv.Atoi(text(cell))
				if err != nil {
					page.warnings = append(page.warnings,
						fmt.Sprintf("%s: bad period number %q", stem, text(cell)))
					continue
				}
				period = nr - 1
			case hasClass(cell, "g"):
				if period >= 0 {
					page.periodTimes[period] = text(cell)
				}
			case hasClass(cell, "l"):
				if period >= 0 {
					page.parseCell(stem, cell, day, period)
				}
				day++
			}
		}
	}
	return page, nil
}

func (page *divisionPage) parseCell(stem string, cell *html.Node, day, period int) {
	var current *occurrence
	flush := func() {
		if current != nil && current.subject != "" {
			page.occurrences = append(page.occurrences, *current)
		}
		current = nil
	}
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode {
			switch {
			case n.Data == "span" && hasClass(n, "p"):
				flush()
				current = &occurrence{
					division: page.name,
					day:      day,
					period:   period,
					subject:  text(n),
				}
				if m := suffixRe.FindStringSubmatch(current.subject); m != nil {
					current.subject = m[1]
					current.group = m[2] + "/" + m[3]
				}
				return // don't descend into the subject span
			case n.Data == "a" && hasClass(n, "n"):
				if current != nil {
					current.teacherFile = hrefStem(attr(n, "href"))
					current.teacherCode = text(n)
					if current.teacherFile == "" {
						page.warnings = append(page.warnings, fmt.Sprintf(
							"%s: day %d period %d: teacher link %q not understood",
							stem, day, period, attr(n, "href")))
					}
				}
				return
			case n.Data == "a" && hasClass(n, "s"):
				if current != nil {
					current.roomFile = hrefStem(attr(n, "href"))
					current.roomName = text(n)
					if current.roomFile == "" {
						page.warnings = append(page.warnings, fmt.Sprintf(
							"%s: day %d period %d: room link %q not understood",
							stem, day, period, attr(n, "href")))
					}
				}
				return
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(cell)
	flush()
}

// sortedStems returns map keys ordered by their numeric part (o1, o2, o10).
func sortedStems[V any](m map[string]V) []string {
	stems := make([]string, 0, len(m))
	for stem := range m {
		stems = append(stems, stem)
	}
	sort.Slice(stems, func(i, j int) bool {
		a, _ := strconv.Atoi(stems[i][1:])
		b, _ := strconv.Atoi(stems[j][1:])
		return a < b
	})
	return stems
}

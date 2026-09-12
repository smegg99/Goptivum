// formats/optivum/xml/document.go

package xml

import "encoding/xml"

// These structs are the allowlist: encoding/xml ignores every attribute and
// element not named here, so a field VULCAN adds in its next version cannot
// arrive by accident.
//
// Four attributes must never be added. A real nauczyciel carries pesel (the
// Polish national identity number), dataur (a date of birth) and email, and a
// real placowka carries regon (the institution's registry number). A timetable
// needs a name and a surname and nothing else about a person, and what is
// imported lands in the database, in backups and in every file a school sends
// on. They are empty in the files this reader was built against; another
// school's will not be.
//
// Every attribute is a string. Optivum writes attributes present but empty
// (lekcja@czas, oddzial@sala), and an int field makes encoding/xml reject the
// whole document over one empty value.

// document is the plan XML a .pla container decodes to.
type document struct {
	XMLName     xml.Name     `xml:"plan"`
	Facility    facility     `xml:"placowka"`
	Days        []day        `xml:"dni>dzien"`
	Periods     []period     `xml:"lekcje>lekcja"`
	Teachers    []teacher    `xml:"nauczyciele>nauczyciel"`
	Subjects    []subject    `xml:"przedmioty>przedmiot"`
	Buildings   []building   `xml:"budynki>budynek"`
	Rooms       []room       `xml:"sale>sala"`
	Divisions   []division   `xml:"oddzialy>oddzial"`
	RoomSets    []roomSet    `xml:"zbiory>zbior"`
	Assignments []assignment `xml:"przydzialy>p"`
}

type facility struct {
	Name string `xml:"nazwa,attr"`
}

type day struct {
	Code string `xml:"kod,attr"`
	Name string `xml:"nazwa,attr"`
}

// period carries the school's real bell times in od and do.
type period struct {
	Code string `xml:"kod,attr"`
	From string `xml:"od,attr"`
	To   string `xml:"do,attr"`
}

type teacher struct {
	Code    string `xml:"kod,attr"`
	First   string `xml:"imie,attr"`
	Last    string `xml:"nazwisko,attr"`
	Rooms   string `xml:"sala,attr"`
	Prefers string `xml:"pref,attr"`
	// Teaching load and its reduction: read only to report that the
	// SchoolModel has nowhere to put them.
	Load          string     `xml:"pensum,attr"`
	LoadReduction string     `xml:"znizka,attr"`
	Cells         []infoCell `xml:"info>p"`
}

type subject struct {
	Code string `xml:"kod,attr"`
	Name string `xml:"nazwa,attr"`
}

type building struct {
	Code string `xml:"kod,attr"`
}

type room struct {
	Code     string     `xml:"kod,attr"`
	Name     string     `xml:"nazwa,attr"`
	Building string     `xml:"bud,attr"`
	Capacity string     `xml:"poj,attr"`
	Cells    []infoCell `xml:"info>p"`
}

type division struct {
	Code      string `xml:"kod,attr"`
	Level     string `xml:"poziom,attr"`
	Boys      string `xml:"ch,attr"`
	Girls     string `xml:"dz,attr"`
	MinPerDay string `xml:"pmin,attr"`
	MaxPerDay string `xml:"pmax,attr"`
	// Form tutor: read only to report that the SchoolModel has nowhere for it.
	Tutor  string     `xml:"wych,attr"`
	Splits []split    `xml:"grupy>podzial"`
	Cells  []infoCell `xml:"info>p"`
}

// split is one cutting of a division into groups, stated outright by Optivum
// instead of inferred from a finished timetable.
type split struct {
	Code    string  `xml:"kod,attr"`
	Subject string  `xml:"przedmiot,attr"`
	Groups  []group `xml:"grupa"`
}

type group struct {
	Code     string `xml:"kod,attr"`
	Students string `xml:"lu,attr"`
	Ordinal  string `xml:"nr,attr"`
}

// roomSet is a named set of rooms that a room preference may name instead of
// listing every room.
type roomSet struct {
	Code     string `xml:"kod,attr"`
	Elements string `xml:"el,attr"`
}

// infoCell is one marked slot of a teacher's, room's or division's week.
// The free-text note beside it is never read: it may carry a person's name.
type infoCell struct {
	Day    string `xml:"d,attr"`
	Period string `xml:"g,attr"`
	Mark   string `xml:"i,attr"`
}

// assignment is one lesson block: who teaches what to whom, how long it runs
// and, when the plan is laid out, where it sits.
//
// Day and Period are 0-based indices into the document order of dni/dzien and
// lekcje/lekcja. Length is the block's period count and is present on the
// block's first row only; Next is the 0-based row index of the next period of
// the same block, so a row without Length is a continuation and not a lesson
// of its own.
type assignment struct {
	Division string `xml:"kl,attr"`
	Group    string `xml:"gr,attr"`
	Ordinal  string `xml:"aog,attr"`
	Teacher  string `xml:"nau,attr"`
	Subject  string `xml:"prz,attr"`
	Room     string `xml:"sa,attr"`
	Day      string `xml:"dz,attr"`
	Period   string `xml:"godz,attr"`
	Length   string `xml:"bk,attr"`
	Next     string `xml:"nb,attr"`
	Pin      string `xml:"sp,attr"`
	Prefers  string `xml:"pref,attr"`
	// Flags: read only to report that they are not interpreted.
	Flags string `xml:"f,attr"`
}

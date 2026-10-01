// optivum/charset_test.go

package optivum

import (
	"context"
	"strings"
	"testing"
	"testing/fstest"

	"golang.org/x/text/encoding"
	"golang.org/x/text/encoding/charmap"

	"github.com/smegg99/goptivum/backend/formats"
)

// divisionPage is a synthetic o1.html in the shape a VULCAN export writes,
// with meta as its declaration (empty declares nothing).
func divisionPageHTML(meta string) string {
	return `<html><head>` + meta + `<title>Plan lekcji oddziału - 1a</title></head><body>
<span class="tytulnapis">1a</span>
<table class="tabela">
<tr><th>Nr</th><th>Godz</th><th>Poniedziałek</th><th>Wtorek</th></tr>
<tr><td class="nr">1</td><td class="g">8:00- 8:45</td>
<td class="l"><span class="p">język polski</span> <a href="n1.html" class="n">ŁŻ</a> <a href="s1.html" class="s">źródło</a></td>
<td class="l">&nbsp;</td></tr>
</table></body></html>`
}

func teacherPageHTML(meta string) string {
	return `<html><head>` + meta + `<title>Plan lekcji nauczyciela - Łukasz Żółć</title></head><body>
<span class="tytulnapis">Łukasz Żółć (ŁŻ)</span>
<table class="tabela"><tr><th>Nr</th><th>Godz</th><th>Poniedziałek</th></tr></table></body></html>`
}

// encoded writes text in enc, the way a school's export sits on disk.
func encoded(t *testing.T, enc encoding.Encoding, text string) []byte {
	t.Helper()
	data, err := enc.NewEncoder().Bytes([]byte(text))
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	return data
}

// An export declares its charset, and the importer reads by it: Polish
// letters in an iso-8859-2 or windows-1250 export come out as written (G27).
func TestPagesAreDecodedByTheirDeclaredCharset(t *testing.T) {
	cases := []struct {
		name string
		enc  encoding.Encoding
		meta string
	}{
		{"iso-8859-2 in http-equiv", charmap.ISO8859_2,
			`<meta http-equiv="Content-Type" content="text/html; charset=iso-8859-2">`},
		{"iso-8859-2 in meta charset", charmap.ISO8859_2, `<meta charset="ISO-8859-2">`},
		{"iso-8859-2 in a lowercase http-equiv", charmap.ISO8859_2,
			`<meta http-equiv="content-type" content="text/html; charset=ISO-8859-2">`},
		{"windows-1250 in http-equiv", charmap.Windows1250,
			`<meta http-equiv="content-type" content="text/html; charset=windows-1250">`},
		{"utf-8 declared", encoding.Nop,
			`<meta http-equiv="Content-Type" content="text/html; charset=utf-8">`},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			result, err := ParseFS(fstest.MapFS{
				"plany/o1.html": {Data: encoded(t, test.enc, divisionPageHTML(test.meta))},
				"plany/n1.html": {Data: encoded(t, test.enc, teacherPageHTML(test.meta))},
			})
			if err != nil {
				t.Fatalf("parse: %v", err)
			}
			checkPolish(t, result)
		})
	}
}

// A page that declares nothing is read as UTF-8 when it is valid UTF-8, and
// as windows-1250 otherwise: the export comes from Polish Windows software.
func TestUndeclaredPagesAreUTF8OrWindows1250(t *testing.T) {
	for name, enc := range map[string]encoding.Encoding{"utf-8": encoding.Nop, "windows-1250": charmap.Windows1250} {
		t.Run(name, func(t *testing.T) {
			result, err := ParseFS(fstest.MapFS{
				"o1.html": {Data: encoded(t, enc, divisionPageHTML(""))},
				"n1.html": {Data: encoded(t, enc, teacherPageHTML(""))},
			})
			if err != nil {
				t.Fatalf("parse: %v", err)
			}
			checkPolish(t, result)
		})
	}
}

// A declaration is read from the head only: a meta tag inside the body is
// page text, and the page is still read as the UTF-8 it is.
func TestADeclarationInTheBodyIsIgnored(t *testing.T) {
	page := strings.Replace(divisionPageHTML(""), `<span class="tytulnapis">`,
		`<meta charset="iso-8859-2"><span class="tytulnapis">`, 1)
	result, err := ParseFS(fstest.MapFS{
		"o1.html": {Data: []byte(page)},
		"n1.html": {Data: []byte(teacherPageHTML(""))},
	})
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	checkPolish(t, result)
}

func checkPolish(t *testing.T, result *Result) {
	t.Helper()
	if got := result.Model.GetDays()[0].GetName(); got != "Poniedziałek" {
		t.Errorf("day = %q, want Poniedziałek", got)
	}
	if got := result.Model.GetSubjects()[0].GetName(); got != "język polski" {
		t.Errorf("subject = %q, want język polski", got)
	}
	if got := result.Model.GetTeachers()[0].GetName(); got != "Łukasz Żółć (ŁŻ)" {
		t.Errorf("teacher = %q, want Łukasz Żółć (ŁŻ)", got)
	}
	if got := result.Model.GetRooms()[0].GetName(); got != "źródło" {
		t.Errorf("room = %q, want źródło", got)
	}
}

// indexHTML is the export's index.html: VULCAN titles it "<school>. Plan
// lekcji", with the school blank when nobody typed one.
func indexHTML(title string) string {
	return `<html><head><meta http-equiv="Content-Type" content="text/html; charset=iso-8859-2">` +
		`<title>` + title + `</title></head><frameset cols="160,*">` +
		`<frame name="list" src="lista.html"><frame name="plan" src="plany/o1.html"></frameset></html>`
}

// The project is named after the school the export names, and after nothing
// when it names none, so the file name is used instead.
func TestTheSchoolNameComesFromTheExportsIndex(t *testing.T) {
	cases := []struct {
		name  string
		index string // absent when empty
		want  string
	}{
		{"a VULCAN title", "Zespół Szkół nr 5. Plan lekcji", "Zespół Szkół nr 5"},
		{"a blank school", ". Plan lekcji", ""},
		{"a title with no suffix", "Liceum Ogólnokształcące im. Żwirki", "Liceum Ogólnokształcące im. Żwirki"},
		{"only the program's own words", "Plan lekcji", ""},
		{"no index at all", "", ""},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			files := fstest.MapFS{
				"plan/plany/o1.html": {Data: []byte(divisionPageHTML(""))},
			}
			if test.index != "" {
				files["plan/index.html"] = &fstest.MapFile{Data: encoded(t, charmap.ISO8859_2, indexHTML(test.index))}
			}
			result, err := NewImporter().Import(context.Background(), formats.Source{Name: "plan.zip", FS: files})
			if err != nil {
				t.Fatalf("import: %v", err)
			}
			if got := result.Model.GetName(); got != test.want {
				t.Errorf("school = %q, want %q", got, test.want)
			}
		})
	}
}

// Goptivum's own export writes the school's name as the index title, and
// reading it back gives the same name.
func TestOwnExportKeepsTheSchoolName(t *testing.T) {
	model, snapshot := exportFixture()
	model.Name = "Szkoła Podstawowa w Żółwinie"
	data, err := NewExporter().Export(context.Background(), &formats.Result{Model: model, Schedule: snapshot})
	if err != nil {
		t.Fatalf("export: %v", err)
	}
	result, err := NewImporter().Import(context.Background(), formats.Open("export.zip", data))
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if got := result.Model.GetName(); got != model.Name {
		t.Errorf("school = %q, want %q", got, model.Name)
	}
	if strings.Contains(result.Model.GetName(), "Import") {
		t.Errorf("school = %q, a placeholder rather than the school", result.Model.GetName())
	}
}

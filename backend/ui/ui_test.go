// ui/ui_test.go

package ui_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/smegg99/goptivum/backend/ui"
)

// A buffer is not a terminal, so the writer resolves to plain text. Every test
// here reads the composition, never an escape sequence.
func render(t *testing.T, draw func(*ui.UI)) string {
	t.Helper()

	var buffer bytes.Buffer
	draw(ui.New(&buffer))
	return buffer.String()
}

func TestPlainWriterCarriesNoEscapes(t *testing.T) {
	out := render(t, func(u *ui.UI) {
		u.Line(u.S.Accent, "goptivum-server")
		u.Fields(ui.Field{Label: "address", Value: "127.0.0.1:8833"})
		u.Table([]string{"element", "count"}, [][]string{{"oddzial", "34"}})
	})

	if strings.Contains(out, "\x1b[") {
		t.Errorf("a writer that is not a terminal emitted escapes:\n%q", out)
	}
	for _, want := range []string{"goptivum-server", "address", "127.0.0.1:8833", "element", "oddzial", "34"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
}

// State is glyph plus word plus color, so it keeps its meaning for a reader
// who cannot tell the colors apart and in a terminal that has none.
func TestStateReadsWithoutColor(t *testing.T) {
	cases := []struct {
		name string
		draw func(*ui.UI) string
		want string
	}{
		{"on", func(u *ui.UI) string { return u.State(true, "ready") }, "[*] ready"},
		{"off", func(u *ui.UI) string { return u.State(false, "waiting") }, "[ ] waiting"},
		{"problem", func(u *ui.UI) string { return u.Problem("unreachable") }, "[x] unreachable"},
		{"warning", func(u *ui.UI) string { return u.Warning("a dependency is down") }, "[!] a dependency is down"},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			out := render(t, func(u *ui.UI) { u.Raw(test.draw(u)) })
			if strings.TrimSpace(out) != test.want {
				t.Errorf("rendered %q, want %q", strings.TrimSpace(out), test.want)
			}
		})
	}
}

// The label column is a style's width, never spaces someone counted.
func TestFieldsAlignOnOneColumn(t *testing.T) {
	out := render(t, func(u *ui.UI) {
		u.Fields(
			ui.Field{Label: "address", Value: "one"},
			ui.Field{Label: "server version", Value: "two"},
		)
	})

	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("rendered %d lines:\n%s", len(lines), out)
	}
	first := strings.Index(lines[0], "one")
	second := strings.Index(lines[1], "two")
	if first != second {
		t.Errorf("values start at %d and %d; the label column does not align", first, second)
	}
}

// A table wider than the terminal wraps its cells. The width is a ceiling, so
// a table narrower than the terminal is left alone rather than stretched.
func TestTableFitsTheTerminal(t *testing.T) {
	wide := [][]string{{"p", "3775", "aog bk d dz f g godz gr i k kl m n nau nb pref prz sa sp tpl zm"}}

	var narrow bytes.Buffer
	ui.NewFor(&narrow, 40, true).Table([]string{"element", "count", "attributes"}, wide)
	for _, line := range strings.Split(strings.TrimRight(narrow.String(), "\n"), "\n") {
		if width := len([]rune(line)); width > 40 {
			t.Errorf("a line is %d columns wide in a 40 column terminal: %q", width, line)
		}
	}

	var roomy bytes.Buffer
	ui.NewFor(&roomy, 200, true).Table([]string{"element", "count"}, [][]string{{"oddzial", "34"}})
	for _, line := range strings.Split(strings.TrimRight(roomy.String(), "\n"), "\n") {
		if width := len([]rune(line)); width > 60 {
			t.Errorf("a small table was stretched to %d columns: %q", width, line)
		}
	}
}

func TestTableBordersItsRows(t *testing.T) {
	out := render(t, func(u *ui.UI) {
		u.Table([]string{"kind", "count"}, [][]string{{"lesson", "1333"}, {"room", "45"}})
	})

	for _, want := range []string{"kind", "count", "lesson", "1333", "room", "45"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
	if !strings.Contains(out, "╭") {
		t.Errorf("the table lost its border:\n%s", out)
	}
}

// Both palettes resolve, so a light terminal is not an afterthought.
func TestBothPalettesResolve(t *testing.T) {
	for _, dark := range []bool{true, false} {
		styles := ui.NewStyles(dark)
		if styles.Accent.Render("x") == "" || styles.Ink.Render("x") == "" {
			t.Errorf("dark=%v: a style rendered nothing", dark)
		}
	}
}

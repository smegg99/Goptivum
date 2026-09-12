// ui/ui.go

package ui

import (
	"fmt"
	"io"
	"os"
	"strings"

	"charm.land/lipgloss/v2"
	"charm.land/lipgloss/v2/table"
	"github.com/charmbracelet/colorprofile"
	"github.com/charmbracelet/x/term"
)

// UI writes through a color profile resolved from the target and the
// environment, so a pipe, a dumb terminal and NO_COLOR all come out plain.
type UI struct {
	out io.Writer
	S   Styles
	// width is the terminal's, or 0 when the target is not one. A table is
	// held to it so a narrow terminal wraps cells instead of the lines.
	width int
}

// New wraps a writer. The background is only queried when the answer could
// change a color: the query is an escape sequence with its own timeout.
func New(out io.Writer) *UI {
	dark := true
	width := 0
	if file, ok := out.(*os.File); ok && term.IsTerminal(file.Fd()) {
		if columns, _, err := term.GetSize(file.Fd()); err == nil {
			width = columns
		}
		if colorprofile.Detect(out, os.Environ()) > colorprofile.ASCII {
			dark = lipgloss.HasDarkBackground(os.Stdin, file)
		}
	}
	return NewFor(out, width, dark)
}

// NewFor wraps a writer with a width and a palette the caller already knows,
// which is what a resize message carries and what a test needs.
func NewFor(out io.Writer, width int, dark bool) *UI {
	return &UI{out: colorprofile.NewWriter(out, os.Environ()), S: NewStyles(dark), width: width}
}

// Raw writes one line with no styling, for a value someone has to copy.
func (u *UI) Raw(line string) { fmt.Fprintln(u.out, line) }

// Line writes one styled line.
func (u *UI) Line(style lipgloss.Style, format string, args ...any) {
	fmt.Fprintln(u.out, style.Render(fmt.Sprintf(format, args...)))
}

// Blank separates blocks. Padding belongs to a style; a blank line is layout.
func (u *UI) Blank() { fmt.Fprintln(u.out) }

// Field is one label and its value.
type Field struct {
	Label string
	Value string
	Style lipgloss.Style
}

// Fields prints an aligned label block, which is how every summary here reads.
func (u *UI) Fields(fields ...Field) {
	for _, f := range fields {
		value := f.Style
		if value.Value() == "" {
			value = u.S.Ink
		}
		fmt.Fprintln(u.out, lipgloss.JoinHorizontal(lipgloss.Top,
			u.S.Key.Render(f.Label), value.Render(f.Value)))
	}
}

// Table prints a bordered table. Column widths and wrapping are the table's
// business, never manual padding.
func (u *UI) Table(headers []string, rows [][]string) {
	rendered := table.New().
		Headers(headers...).
		Rows(rows...).
		Border(lipgloss.RoundedBorder()).
		BorderStyle(u.S.Border).
		StyleFunc(func(row, _ int) lipgloss.Style {
			if row == table.HeaderRow {
				return u.S.Header
			}
			return u.S.Cell
		})

	// A width is a ceiling, not a target: given one, the table stretches its
	// columns to fill it, so it is only set when the table would overflow.
	out := rendered.Render()
	if u.width > 0 && lipgloss.Width(out) > u.width {
		out = rendered.Width(u.width).Render()
	}
	fmt.Fprintln(u.out, out)
}

// Panel prints one bordered surface, for something the reader must not miss.
func (u *UI) Panel(lines ...string) {
	fmt.Fprintln(u.out, u.S.Panel.Render(strings.Join(lines, "\n")))
}

// State reads as glyph plus word plus color, so it survives NO_COLOR and a
// reader who cannot tell the colors apart.
func (u *UI) State(ok bool, word string) string {
	if ok {
		return u.S.Good.Render("[*] " + word)
	}
	return u.S.Muted.Render("[ ] " + word)
}

// Problem is the same shape for something that went wrong.
func (u *UI) Problem(word string) string { return u.S.Bad.Render("[x] " + word) }

// Warning is the same shape for something that is not wrong yet.
func (u *UI) Warning(word string) string { return u.S.Warn.Render("[!] " + word) }

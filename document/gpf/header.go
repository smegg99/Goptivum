// document/gpf/header.go

package gpf

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

// Brand is the first line of the plaintext header, so `head file.gpf` says what
// the file is. The digit is part of the name and never increments: the
// authoritative version is the preheader's binary field.
const Brand = "GPF1"

// headerLead is the human line under the brand.
const headerLead = "# Goptivum Project File"

// headerEnd terminates the block.
const headerEnd = "---"

// Header field names. Everything here is a disclosure surface: readable by
// anyone holding the file, encrypted payload or not. Never put anything here
// that the payload is being encrypted to protect.
const (
	FieldSchool = "school"
	FieldPlan   = "plan"
	FieldSaved  = "saved"
	FieldBy     = "by"
	FieldApp    = "app"
	FieldFormat = "format"
	FieldMode   = "mode"
	FieldRealm  = "realm"
	FieldKeys   = "keys"
	// FieldPlanID and FieldPlanVersion travel so a reader can say "this looks
	// like a copy of your plan wrzesien". They drive nothing: an imported .gpf
	// always becomes a new plan.
	FieldPlanID      = "plan_id"
	FieldPlanVersion = "plan_version"
	// FieldValidFrom and FieldValidTo are when the timetable applies. They are
	// here rather than in the payload because nothing a person merely reads
	// should need a protobuf decoder to reach.
	FieldValidFrom = "valid_from"
	FieldValidTo   = "valid_to"
)

// Mode names, as the header spells them. These are disclosure, never a
// decision: what the reader does is decided by the binary flags, and a mode
// line that disagrees with them means the file is corrupt.
const (
	ModePlain    = "plain"
	ModePassword = "password"
	ModeIdentity = "identity"
)

// maxFieldValue bounds one value. A header that fits MaxHeader could still hold
// one enormous line, and realm is compared against local configuration.
const maxFieldValue = 4096

// Header is the branded plaintext block.
type Header struct {
	fields map[string]string
	order  []string
}

// NewHeader starts an empty block.
func NewHeader() *Header {
	return &Header{fields: map[string]string{}}
}

// Set adds or replaces a field. An empty value is dropped rather than written
// as a blank line, so a reader never has to tell "absent" from "empty".
func (h *Header) Set(name, value string) *Header {
	value = strings.TrimSpace(value)
	if value == "" {
		delete(h.fields, name)
		return h
	}
	if _, had := h.fields[name]; !had {
		h.order = append(h.order, name)
	}
	h.fields[name] = value
	return h
}

// Get is one field, empty when absent.
func (h *Header) Get(name string) string { return h.fields[name] }

// Fields is every name present, in the order they were written.
func (h *Header) Fields() []string { return append([]string(nil), h.order...) }

// Encode writes the block. It refuses a value that would break the format
// rather than writing a file that cannot be read back: a newline would forge a
// second field and a colon in a name would split it in the wrong place.
func (h *Header) Encode() ([]byte, error) {
	var out strings.Builder
	out.WriteString(Brand)
	out.WriteByte('\n')
	out.WriteString(headerLead)
	out.WriteByte('\n')

	width := 0
	for _, name := range h.order {
		if len(name) > width {
			width = len(name)
		}
	}
	for _, name := range h.order {
		value := h.fields[name]
		if err := checkField(name, value); err != nil {
			return nil, err
		}
		out.WriteString(name)
		out.WriteByte(':')
		// Padded so a person reading it sees a column, which is the whole
		// reason this is a plaintext block and not a length-prefixed one.
		out.WriteString(strings.Repeat(" ", width-len(name)+1))
		out.WriteString(value)
		out.WriteByte('\n')
	}
	out.WriteString(headerEnd)
	out.WriteByte('\n')
	return []byte(out.String()), nil
}

// checkField refuses anything that would not survive a round trip.
func checkField(name, value string) error {
	if name == "" || strings.ContainsAny(name, ":\r\n \t") {
		return fmt.Errorf("%w: %q is not a field name", ErrMalformed, name)
	}
	if len(value) > maxFieldValue {
		return fmt.Errorf("%w: %s is %d bytes, over the %d cap",
			ErrMalformed, name, len(value), maxFieldValue)
	}
	if strings.ContainsAny(value, "\r\n") {
		return fmt.Errorf("%w: %s carries a line break", ErrMalformed, name)
	}
	if strings.ContainsRune(value, 0) {
		return fmt.Errorf("%w: %s carries a NUL", ErrMalformed, name)
	}
	if !utf8.ValidString(value) {
		return fmt.Errorf("%w: %s is not valid UTF-8", ErrMalformed, name)
	}
	return nil
}

// parseHeader reads the block back.
//
// Every field is untrusted text and is treated that way even though this is not
// a parser with a grammar: a line that is not `name: value` is refused rather
// than skipped, because a reader that quietly ignores what it does not
// understand cannot tell a new field from a corrupted one.
func parseHeader(block []byte) (*Header, error) {
	if !utf8.Valid(block) {
		return nil, fmt.Errorf("%w: the header is not valid UTF-8", ErrMalformed)
	}
	lines := strings.Split(strings.TrimSuffix(string(block), "\n"), "\n")
	if len(lines) < 3 {
		return nil, fmt.Errorf("%w: the header is too short to be one", ErrMalformed)
	}
	if lines[0] != Brand {
		return nil, fmt.Errorf("%w: the header does not start with %s", ErrMalformed, Brand)
	}
	if lines[len(lines)-1] != headerEnd {
		return nil, fmt.Errorf("%w: the header does not end with %s", ErrMalformed, headerEnd)
	}

	header := NewHeader()
	for _, line := range lines[1 : len(lines)-1] {
		if strings.HasPrefix(line, "#") || strings.TrimSpace(line) == "" {
			continue
		}
		name, value, found := strings.Cut(line, ":")
		if !found {
			return nil, fmt.Errorf("%w: %q is not a header field", ErrMalformed, line)
		}
		name = strings.TrimSpace(name)
		value = strings.TrimSpace(value)
		if err := checkField(name, value); err != nil {
			return nil, err
		}
		if _, already := header.fields[name]; already {
			// Two values for one name is a file that means two things.
			return nil, fmt.Errorf("%w: %s appears twice", ErrMalformed, name)
		}
		header.Set(name, value)
	}
	return header, nil
}

// modeOf is what the binary flags say the payload is. The header's own mode
// line is never consulted for this.
func modeOf(flags uint16) string {
	switch {
	case flags&FlagEncrypted == 0:
		return ModePlain
	case flags&FlagPassphrase != 0:
		return ModePassword
	default:
		return ModeIdentity
	}
}

// checkModeAgrees refuses a file whose header text contradicts its flags.
//
// The flags decide what happens; this is the one place the text is checked at
// all, and a disagreement means the file is corrupt rather than that one of
// them wins. A header claiming `plain` over an encrypted payload is either a
// broken writer or an attempt to talk a reader out of decrypting.
func checkModeAgrees(header *Header, flags uint16) error {
	stated := header.Get(FieldMode)
	if stated == "" {
		return nil
	}
	if actual := modeOf(flags); stated != actual {
		return fmt.Errorf("%w: the header says mode %q and the flags say %q",
			ErrMalformed, stated, actual)
	}
	return nil
}

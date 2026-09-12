// formats/optivum/xml/importer.go

// Package xml reads the plan XML inside an Optivum ".pla" container and
// returns it as an arrango SchoolModel.
//
// The container codec lives in the sibling pla package and hands back the
// payload still encoded as iso-8859-2. This package parses those raw bytes
// through an xml.Decoder with a CharsetReader, because the prolog declares
// the encoding and transcoding first makes encoding/xml refuse the document.
//
// What the reader takes from the file is declared by the structs in
// document.go and by nothing else. A person's national identity number, date
// of birth and email address are in the file and are never read.
package xml

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/xml"
	"fmt"
	"io"
	"io/fs"
	"strings"

	"golang.org/x/text/encoding/charmap"

	"github.com/smegg99/goptivum/backend/formats"
	"github.com/smegg99/goptivum/backend/formats/optivum/pla"
)

// ID is this importer's stable identity. It namespaces every source_ref the
// importer emits, which is what lets the server tell a re-import of the same
// school from a different school.
const ID = "optivum.pla"

// containerPrefix is the magic every Optivum plan generation starts with. It
// is spelled out here rather than taken from pla, which stays untouched.
const containerPrefix = "PlanOptivum"

const (
	// maxContainer bounds what the reader pulls into memory for one file.
	maxContainer = 64 << 20
	// maxMagic bounds a plausible magic; the format's own is 14 bytes.
	maxMagic = 64
)

// Importer reads Optivum ".pla" plan files.
type Importer struct{}

// New returns the Optivum plan-file importer.
func New() *Importer { return &Importer{} }

func (*Importer) ID() string { return ID }

// Detect looks for the container header, never at the file name.
func (*Importer) Detect(src formats.Source) (formats.Confidence, error) {
	name, err := containerPath(src)
	if err != nil {
		return formats.No, err
	}
	if name == "" {
		return formats.No, nil
	}
	return formats.Certain, nil
}

// Import decodes the container, reads the plan XML and builds the school.
func (imp *Importer) Import(ctx context.Context, src formats.Source) (*formats.Result, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	name, err := containerPath(src)
	if err != nil {
		return nil, err
	}
	if name == "" {
		return nil, fmt.Errorf("%s: no Optivum plan container in %q", ID, src.Name)
	}
	raw, err := fs.ReadFile(src.FS, name)
	if err != nil {
		return nil, fmt.Errorf("%s: read %s: %w", ID, name, err)
	}
	if len(raw) > maxContainer {
		return nil, fmt.Errorf("%s: %s is %d bytes, over the %d byte limit", ID, name, len(raw), maxContainer)
	}

	container, err := pla.Decode(raw)
	if err != nil {
		return nil, fmt.Errorf("%s: decode container: %w", ID, err)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	census, err := elementCensus(container.XML)
	if err != nil {
		return nil, fmt.Errorf("%s: scan document: %w", ID, err)
	}
	doc, err := parse(container.XML)
	if err != nil {
		return nil, fmt.Errorf("%s: parse document: %w", ID, err)
	}
	return build(ctx, doc, census)
}

// parse unmarshals the raw iso-8859-2 payload. The CharsetReader is what
// makes this work: without it encoding/xml fails with
// `xml: encoding "iso-8859-2" declared but Decoder.CharsetReader is nil`.
func parse(raw []byte) (*document, error) {
	decoder := xml.NewDecoder(bytes.NewReader(raw))
	decoder.CharsetReader = charsetReader

	var doc document
	if err := decoder.Decode(&doc); err != nil {
		return nil, err
	}
	return &doc, nil
}

// charsetReader resolves the encodings an Optivum plan declares. A
// mis-decoded Polish name is a bug that reaches a school before anyone
// notices, so an unknown label is an error rather than a guess.
func charsetReader(label string, input io.Reader) (io.Reader, error) {
	switch strings.ToLower(label) {
	case "iso-8859-2", "iso8859-2", "iso_8859-2", "latin2", "l2":
		return charmap.ISO8859_2.NewDecoder().Reader(input), nil
	case "utf-8", "utf8", "":
		return input, nil
	default:
		return nil, fmt.Errorf("unsupported charset %q", label)
	}
}

// containerPath returns the first file in the Source whose own header says it
// is an Optivum plan container, or "" when there is none.
func containerPath(src formats.Source) (string, error) {
	files, err := formats.Files(src)
	if err != nil {
		return "", err
	}
	for _, name := range files {
		head, err := formats.Head(src.FS, name, 2+len(containerPrefix))
		if err != nil {
			return "", err
		}
		if isContainer(head) {
			return name, nil
		}
	}
	return "", nil
}

// isContainer applies the container's own header rule: a uint16 magic length
// followed by a magic naming the format.
func isContainer(head []byte) bool {
	if len(head) < 2+len(containerPrefix) {
		return false
	}
	length := int(binary.LittleEndian.Uint16(head[:2]))
	if length < len(containerPrefix) || length > maxMagic {
		return false
	}
	return strings.HasPrefix(string(head[2:]), containerPrefix)
}

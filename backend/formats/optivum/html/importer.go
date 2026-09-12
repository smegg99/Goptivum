// optivum/importer.go

package optivum

import (
	"context"
	"fmt"
	"path"
	"strings"

	"github.com/smegg99/goptivum/backend/formats"
)

// ID is this importer's stable identity, and the namespace of every
// source_ref it emits.
const ID = "optivum.html"

// markers are the structural class names a VULCAN export carries. They are
// what distinguishes an Optivum export from any other directory of HTML.
var markers = []string{"tytulnapis", "tabela"}

// Importer reads a VULCAN Optivum HTML export, zipped or unpacked.
type Importer struct{}

// NewImporter returns the Optivum HTML export importer.
func NewImporter() *Importer { return &Importer{} }

func (*Importer) ID() string { return ID }

// Detect looks for division pages carrying the export's own markers, never
// at the file name alone: any directory can hold a file called o1.html.
func (*Importer) Detect(src formats.Source) (formats.Confidence, error) {
	files, err := formats.Files(src)
	if err != nil {
		return formats.No, err
	}

	found := formats.No
	for _, name := range files {
		match := fileRe.FindStringSubmatch(path.Base(name))
		if match == nil || match[1] != "o" {
			continue
		}
		found = formats.Maybe
		head, err := formats.Head(src.FS, name, 8<<10)
		if err != nil {
			return formats.No, err
		}
		text := string(head)
		for _, marker := range markers {
			if strings.Contains(text, marker) {
				return formats.Certain, nil
			}
		}
	}
	return found, nil
}

// Import parses the export and carries its warnings across as findings. The
// parser reads a rendered timetable, so it never sees a personal field: an
// export page carries a name, a subject, a room and a class.
func (imp *Importer) Import(ctx context.Context, src formats.Source) (*formats.Result, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if src.FS == nil {
		return nil, fmt.Errorf("%s: no filesystem in source %q", ID, src.Name)
	}

	parsed, err := ParseFS(src.FS)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", ID, err)
	}

	// The parser reports in prose. Its warnings keep one kind between them,
	// because the reasons are page-level parse notes rather than typed data
	// losses the UI can localise.
	findings := make([]formats.Finding, 0, len(parsed.Warnings))
	for _, warning := range parsed.Warnings {
		findings = append(findings, formats.Finding{
			Kind:     formats.KindSourceWarning,
			Severity: formats.Warning,
			Detail:   warning,
		})
	}
	return &formats.Result{
		Model:    parsed.Model,
		Schedule: parsed.Snapshot,
		Findings: findings,
	}, nil
}

// Exporter writes a VULCAN-style Optivum HTML export.
type Exporter struct{}

// NewExporter returns the Optivum HTML export writer.
func NewExporter() *Exporter { return &Exporter{} }

func (*Exporter) ID() string { return ID }

// Export renders the school and its timetable as a zipped HTML export.
func (*Exporter) Export(ctx context.Context, result *formats.Result) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if result == nil {
		return nil, fmt.Errorf("%s: nothing to export", ID)
	}
	return ExportZip(result.Model, result.Schedule)
}

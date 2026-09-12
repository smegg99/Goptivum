// optivum/importer_test.go

package optivum

import (
	"context"
	"testing"
	"testing/fstest"

	"github.com/smegg99/goptivum/backend/formats"
)

// The adapter is exercised against our own export, which carries the same
// structural markers as a VULCAN one.
func exportedZip(t *testing.T) []byte {
	t.Helper()

	model, snapshot := exportFixture()
	data, err := NewExporter().Export(context.Background(),
		&formats.Result{Model: model, Schedule: snapshot})
	if err != nil {
		t.Fatalf("export: %v", err)
	}
	return data
}

func TestDetectsExportByItsMarkers(t *testing.T) {
	source := formats.Open("export.zip", exportedZip(t))
	confidence, err := NewImporter().Detect(source)
	if err != nil {
		t.Fatalf("detect: %v", err)
	}
	if confidence != formats.Certain {
		t.Errorf("detect = %v, want certain", confidence)
	}
}

func TestDetectionIsByContentNotByName(t *testing.T) {
	cases := []struct {
		name string
		fsys fstest.MapFS
		want formats.Confidence
	}{
		{
			name: "a division page without the markers is only a maybe",
			fsys: fstest.MapFS{"o1.html": {Data: []byte("<html><body>nothing here</body></html>")}},
			want: formats.Maybe,
		},
		{
			name: "an unrelated archive is not an export",
			fsys: fstest.MapFS{
				"index.html": {Data: []byte("<html>tytulnapis tabela</html>")},
				"notes.txt":  {Data: []byte("hello")},
			},
			want: formats.No,
		},
		{
			name: "an empty source is not an export",
			fsys: fstest.MapFS{},
			want: formats.No,
		},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			got, err := NewImporter().Detect(formats.Source{Name: "x", FS: test.fsys})
			if err != nil {
				t.Fatalf("detect: %v", err)
			}
			if got != test.want {
				t.Errorf("detect = %v, want %v", got, test.want)
			}
		})
	}
}

func TestImportsThroughTheInterface(t *testing.T) {
	source := formats.Open("export.zip", exportedZip(t))

	result, err := NewImporter().Import(context.Background(), source)
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if result.Model == nil || result.Schedule == nil {
		t.Fatal("import returned no model or no timetable")
	}
	if got := len(result.Model.GetDivisions()); got != 1 {
		t.Errorf("divisions = %d, want 1", got)
	}
	if got := len(result.Schedule.GetLessons()); got != 2 {
		t.Errorf("placed lessons = %d, want 2", got)
	}
	if result.HasErrors() {
		t.Error("a clean export produced an error finding")
	}
}

// A page the parser cannot read is a finding, not a failed import.
func TestBrokenPageBecomesAFinding(t *testing.T) {
	model, snapshot := exportFixture()
	data, err := ExportZip(model, snapshot)
	if err != nil {
		t.Fatalf("export: %v", err)
	}
	source := formats.Open("export.zip", data)
	files, err := formats.Files(source)
	if err != nil {
		t.Fatalf("list: %v", err)
	}

	// Rebuild the export as a directory, with one teacher page emptied.
	fsys := fstest.MapFS{}
	for _, name := range files {
		content, err := formats.Head(source.FS, name, 1<<20)
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		if name == "n50.html" {
			content = []byte("<html><body></body></html>")
		}
		fsys[name] = &fstest.MapFile{Data: content}
	}

	result, err := NewImporter().Import(context.Background(), formats.Source{Name: "d", FS: fsys})
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if got := len(result.Model.GetTeachers()); got == 0 {
		t.Error("a teacher page that says nothing still registers its teacher")
	}
	for _, finding := range result.Findings {
		if finding.Severity == formats.Error {
			t.Errorf("unexpected error finding: %s", finding.Kind)
		}
	}
}

func TestImportRejectsAnEmptySource(t *testing.T) {
	if _, err := NewImporter().Import(context.Background(), formats.Source{Name: "x"}); err == nil {
		t.Fatal("import accepted a source with no filesystem")
	}
	if _, err := NewImporter().Import(context.Background(),
		formats.Source{Name: "x", FS: fstest.MapFS{}}); err == nil {
		t.Fatal("import accepted a source with no export in it")
	}
}

func TestExportRejectsNothing(t *testing.T) {
	if _, err := NewExporter().Export(context.Background(), nil); err == nil {
		t.Fatal("export accepted a nil result")
	}
}

func TestImporterAndExporterShareTheirID(t *testing.T) {
	if NewImporter().ID() != NewExporter().ID() {
		t.Error("the importer and the exporter of one format must share an ID")
	}
	var _ formats.Importer = NewImporter()
	var _ formats.Exporter = NewExporter()
}

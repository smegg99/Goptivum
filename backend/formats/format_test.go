// formats/format_test.go

package formats_test

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"testing"
	"testing/fstest"

	"github.com/smegg99/goptivum/backend/formats"
)

// fake is an importer that claims whatever the test tells it to.
type fake struct {
	id         string
	confidence formats.Confidence
	err        error
}

func (f fake) ID() string { return f.id }

func (f fake) Detect(formats.Source) (formats.Confidence, error) {
	return f.confidence, f.err
}

func (f fake) Import(context.Context, formats.Source) (*formats.Result, error) {
	return &formats.Result{}, nil
}

func TestRegistryKeepsRegistrationOrder(t *testing.T) {
	registry := formats.NewRegistry(fake{id: "b"}, fake{id: "a"})

	var ids []string
	for _, importer := range registry.Importers() {
		ids = append(ids, importer.ID())
	}
	if len(ids) != 2 || ids[0] != "b" || ids[1] != "a" {
		t.Errorf("importers = %v, want registration order", ids)
	}
	if _, ok := registry.Importer("a"); !ok {
		t.Error("lookup by id failed")
	}
	if _, ok := registry.Importer("missing"); ok {
		t.Error("lookup invented an importer")
	}
}

// A duplicate id silently shadows a whole format, so it is a panic and not an
// error return: nothing sensible can continue past it.
func TestRegisterPanicsOnADuplicateID(t *testing.T) {
	cases := []struct {
		name     string
		register func(*formats.Registry)
	}{
		{"duplicate", func(r *formats.Registry) { r.Register(fake{id: "same"}); r.Register(fake{id: "same"}) }},
		{"empty id", func(r *formats.Registry) { r.Register(fake{id: ""}) }},
		{"nil importer", func(r *formats.Registry) { r.Register(nil) }},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			defer func() {
				if recover() == nil {
					t.Error("no panic")
				}
			}()
			test.register(formats.NewRegistry())
		})
	}
}

func TestDetectRanksTheBestClaimFirst(t *testing.T) {
	registry := formats.NewRegistry(
		fake{id: "maybe", confidence: formats.Maybe},
		fake{id: "no", confidence: formats.No},
		fake{id: "certain", confidence: formats.Certain},
	)

	matches, err := registry.Detect(formats.Source{Name: "x"})
	if err != nil {
		t.Fatalf("detect: %v", err)
	}
	if len(matches) != 2 {
		t.Fatalf("matches = %d, want the two that claimed the file", len(matches))
	}
	if matches[0].ID != "certain" || matches[1].ID != "maybe" {
		t.Errorf("matches = %v, want certain before maybe", matches)
	}

	best, err := registry.Best(formats.Source{Name: "x"})
	if err != nil || best.ID() != "certain" {
		t.Errorf("best = %v, %v", best, err)
	}
}

// Nothing claiming a file is an empty slice and a nil error: a file we do not
// know is not a broken file.
func TestDetectReturnsNothingRatherThanAnError(t *testing.T) {
	registry := formats.NewRegistry(fake{id: "none", confidence: formats.No})

	matches, err := registry.Detect(formats.Source{Name: "x"})
	if err != nil || len(matches) != 0 {
		t.Errorf("detect = %v, %v", matches, err)
	}
	if _, err := registry.Best(formats.Source{Name: "x"}); !errors.Is(err, formats.ErrUnrecognised) {
		t.Errorf("best error = %v, want ErrUnrecognised", err)
	}
}

func TestDetectReportsWhenEveryImporterFailedToLook(t *testing.T) {
	failure := errors.New("cannot read")
	registry := formats.NewRegistry(fake{id: "broken", err: failure})

	if _, err := registry.Detect(formats.Source{Name: "x"}); !errors.Is(err, failure) {
		t.Errorf("detect error = %v, want the importer's own", err)
	}

	// One importer failing while another claims the file is not an error.
	mixed := formats.NewRegistry(
		fake{id: "broken", err: failure},
		fake{id: "certain", confidence: formats.Certain},
	)
	matches, err := mixed.Detect(formats.Source{Name: "x"})
	if err != nil || len(matches) != 1 {
		t.Errorf("detect = %v, %v", matches, err)
	}
}

func TestOpenNormalisesAZipAndASingleFile(t *testing.T) {
	var buffer bytes.Buffer
	writer := zip.NewWriter(&buffer)
	entry, err := writer.Create("inner/plan.pla")
	if err != nil {
		t.Fatalf("create zip entry: %v", err)
	}
	if _, err := entry.Write([]byte("payload")); err != nil {
		t.Fatalf("write zip entry: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close zip: %v", err)
	}

	zipped := formats.Open("upload.zip", buffer.Bytes())
	files, err := formats.Files(zipped)
	if err != nil {
		t.Fatalf("list zip: %v", err)
	}
	if len(files) != 1 || files[0] != "inner/plan.pla" {
		t.Errorf("zip files = %v", files)
	}

	plain := formats.Open("/tmp/some/plan.pla", []byte("not a zip"))
	files, err = formats.Files(plain)
	if err != nil {
		t.Fatalf("list file: %v", err)
	}
	if len(files) != 1 || files[0] != "plan.pla" {
		t.Errorf("single file = %v, want the base name", files)
	}
	head, err := formats.Head(plain.FS, files[0], 3)
	if err != nil {
		t.Fatalf("head: %v", err)
	}
	if string(head) != "not" {
		t.Errorf("head = %q", head)
	}

	odd := formats.Open("../..", []byte("x"))
	files, err = formats.Files(odd)
	if err != nil {
		t.Fatalf("list odd name: %v", err)
	}
	if len(files) != 1 || files[0] != "upload" {
		t.Errorf("odd name = %v, want a safe fallback", files)
	}
}

func TestHeadReadsShortFilesWhole(t *testing.T) {
	fsys := fstest.MapFS{"a.txt": {Data: []byte("hi")}}

	head, err := formats.Head(fsys, "a.txt", 64)
	if err != nil {
		t.Fatalf("head: %v", err)
	}
	if string(head) != "hi" {
		t.Errorf("head = %q", head)
	}
	if _, err := formats.Head(fsys, "missing.txt", 64); err == nil {
		t.Error("head invented a file")
	}
}

func TestResultReportsItsWorstFinding(t *testing.T) {
	result := &formats.Result{Findings: []formats.Finding{
		{Kind: formats.KindUnmappedElement, Severity: formats.Info},
		{Kind: formats.KindUnknownReference, Severity: formats.Warning},
	}}
	if result.HasErrors() {
		t.Error("HasErrors is true without an error finding")
	}
	result.Findings = append(result.Findings,
		formats.Finding{Kind: formats.KindLessonDropped, Severity: formats.Error})
	if !result.HasErrors() {
		t.Error("HasErrors is false with an error finding")
	}
}

func TestSeverityAndConfidenceReadAsWords(t *testing.T) {
	if formats.Error.String() != "error" || formats.Info.String() != "info" ||
		formats.Warning.String() != "warning" {
		t.Error("severity names changed")
	}
	if formats.Certain.String() != "certain" || formats.Maybe.String() != "maybe" ||
		formats.No.String() != "no" {
		t.Error("confidence names changed")
	}
}

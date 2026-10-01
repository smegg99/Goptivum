// formats/goptivum/goptivum_test.go

package goptivum_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"filippo.io/age"

	arrangov1 "github.com/smegg99/arrango/backend/gen/arrangov1"

	"github.com/smegg99/goptivum/backend/formats"
	"github.com/smegg99/goptivum/backend/formats/goptivum"
	optivumhtml "github.com/smegg99/goptivum/backend/formats/optivum/html"
	planxml "github.com/smegg99/goptivum/backend/formats/optivum/xml"
	"github.com/smegg99/goptivum/document"
	goptivumv1 "github.com/smegg99/goptivum/document/gen/goptivumv1"
	"github.com/smegg99/goptivum/document/gpf"
)

// solvedDocument is a small solved plan, so an import carries a timetable.
func solvedDocument() *goptivumv1.Document {
	built := document.New(&arrangov1.SchoolModel{
		Name:     "Szkoła Testowa",
		Days:     []*arrangov1.Day{{Id: 1, Name: "Poniedziałek", PeriodCount: 2}},
		Periods:  []*arrangov1.Period{{Id: 2, Name: "8:00"}},
		Teachers: []*arrangov1.Teacher{{Id: 3, Name: "Łukasz Żółć"}},
		Rooms:    []*arrangov1.Room{{Id: 4, Name: "12"}},
		Lessons:  []*arrangov1.LessonInstance{{Id: 5, TeacherId: 3, Duration: 1}},
	})
	built.Schedule = &arrangov1.ScheduleSnapshot{
		Lessons: []*arrangov1.ScheduledLesson{
			{LessonId: 5, Placement: &arrangov1.Placement{DayId: 1, StartPeriod: 2, RoomId: 4}},
		},
	}
	return built
}

func header(t *testing.T) *gpf.Header {
	t.Helper()
	return gpf.NewHeader().Set(gpf.FieldSchool, "Szkoła Testowa")
}

// plainFile is a valid unencrypted file.
func plainFile(t *testing.T) []byte {
	t.Helper()
	file, err := gpf.Encode(solvedDocument(), gpf.WriteOptions{Header: header(t)})
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	return file
}

// registry is the real one the server builds, so detection is tested against
// the competitors a file actually meets.
func registry(t *testing.T) *formats.Registry {
	t.Helper()
	return formats.NewRegistry(planxml.New(), optivumhtml.NewImporter(), goptivum.NewImporter(""))
}

// Detection is eight bytes and a comparison, and it wins outright: this is the
// only format in the registry that can be Certain from its first bytes alone.
func TestDetectionIsCertainFromTheMagicAlone(t *testing.T) {
	source := formats.Open("wrzesien.gpf", plainFile(t))
	best, err := registry(t).Best(source)
	if err != nil {
		t.Fatalf("best: %v", err)
	}
	if best.ID() != goptivum.ID {
		t.Fatalf("best = %s, want %s", best.ID(), goptivum.ID)
	}

	// The extension is not what decided it: detection is by content, so the
	// same bytes under any name still win.
	renamed := formats.Open("something.zip", plainFile(t))
	best, err = registry(t).Best(renamed)
	if err != nil || best.ID() != goptivum.ID {
		t.Errorf("best for a renamed file = %v, %v", best, err)
	}
}

// A file that is not a .gpf is not ours, and saying so is not an error: an
// importer that errored here would be counted as a non-match anyway and would
// only hide a real failure.
func TestDetectionDeclinesWhatIsNotOurs(t *testing.T) {
	source := formats.Open("plan.html", []byte("<html><body>not a gpf</body></html>"))
	confidence, err := goptivum.NewImporter("").Detect(source)
	if err != nil {
		t.Errorf("detect returned an error for someone else's file: %v", err)
	}
	if confidence != formats.No {
		t.Errorf("confidence = %v, want No", confidence)
	}
}

// Importing through the registry gives back the whole envelope, not a model
// somebody reassembled. The id counter is the reason: recomputing it from the
// model would hand out an id the sender already spent.
func TestImportCarriesTheWholeEnvelope(t *testing.T) {
	original := solvedDocument()
	file, err := gpf.Encode(original, gpf.WriteOptions{Header: header(t)})
	if err != nil {
		t.Fatalf("encode: %v", err)
	}

	result, err := goptivum.NewImporter("").Import(t.Context(), formats.Open("wrzesien.gpf", file))
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if result.Document == nil {
		t.Fatal("the result carries no document, so the counter is lost")
	}
	if result.Document.GetNextArrangoId() != original.GetNextArrangoId() {
		t.Errorf("next_arrango_id = %d, want %d",
			result.Document.GetNextArrangoId(), original.GetNextArrangoId())
	}
	if len(result.Schedule.GetLessons()) != 1 {
		t.Error("the schedule did not come through")
	}
	// Goptivum's own format maps nothing and loses nothing, so there is nothing
	// to report.
	if len(result.Findings) != 0 {
		t.Errorf("findings = %v, want none for our own format", result.Findings)
	}
}

// Without a passphrase a password-locked file is refused with the reason
// rather than opened. The passphrase never rides on formats.Source, which
// every detector in the registry reads.
func TestTheRegistryImporterRefusesAnEncryptedFile(t *testing.T) {
	file, err := gpf.Encode(solvedDocument(), gpf.WriteOptions{
		Header: header(t), Passphrase: "correct horse", WorkFactor: 10,
	})
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	_, err = goptivum.NewImporter("").Import(t.Context(), formats.Open("wrzesien.gpf", file))
	if !errors.Is(err, gpf.ErrPasswordRequired) {
		t.Fatalf("err = %v, want ErrPasswordRequired", err)
	}
}

// The winning importer alone is handed the passphrase, and it opens the file
// with it; a wrong one is said as such, and a plain file ignores it (B17).
func TestAPassphraseOpensAPasswordLockedFile(t *testing.T) {
	file, err := gpf.Encode(solvedDocument(), gpf.WriteOptions{
		Header: header(t), Passphrase: "correct horse", WorkFactor: 10,
	})
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	var unlocker formats.Unlocker = goptivum.NewImporter("")

	result, err := unlocker.ImportUnlocked(t.Context(), formats.Open("wrzesien.gpf", file), "correct horse")
	if err != nil {
		t.Fatalf("import with the passphrase: %v", err)
	}
	if result.Document == nil || result.Model.GetName() != "Szkoła Testowa" {
		t.Errorf("imported %+v, want the locked school", result.Model)
	}

	_, err = unlocker.ImportUnlocked(t.Context(), formats.Open("wrzesien.gpf", file), "wrong horse")
	if !errors.Is(err, gpf.ErrWrongPassword) {
		t.Errorf("a wrong passphrase: err = %v, want ErrWrongPassword", err)
	}
	_, err = unlocker.ImportUnlocked(t.Context(), formats.Open("wrzesien.gpf", file), "")
	if !errors.Is(err, gpf.ErrPasswordRequired) {
		t.Errorf("no passphrase: err = %v, want ErrPasswordRequired", err)
	}

	plain, err := unlocker.ImportUnlocked(t.Context(), formats.Open("wrzesien.gpf", plainFile(t)), "correct horse")
	if err != nil || plain.Document == nil {
		t.Errorf("a plain file with a passphrase: %v, want it opened", err)
	}
}

// The realm the server was configured with reaches the importer, so a file from
// another school is refused at the registry boundary too.
func TestTheImporterCarriesTheServersRealm(t *testing.T) {
	file, err := gpf.Encode(solvedDocument(), gpf.WriteOptions{
		Header: header(t).Set(gpf.FieldRealm, "szkola.edu.pl"),
	})
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	var wrong *gpf.WrongRealmError
	_, err = goptivum.NewImporter("liceum.pl").Import(t.Context(), formats.Open("w.gpf", file))
	if !errors.As(err, &wrong) {
		t.Fatalf("err = %v, want WrongRealmError", err)
	}
}

// Export writes a file this server can read back, with its own realm on it.
func TestExportRoundTripsThroughTheRegistryShape(t *testing.T) {
	exporter := goptivum.NewExporter("szkola.edu.pl", "goptivum-server 1.2.3")
	written, err := exporter.Export(t.Context(), &formats.Result{Document: solvedDocument()})
	if err != nil {
		t.Fatalf("export: %v", err)
	}
	read, err := gpf.Decode(written, gpf.ReadOptions{Realm: "szkola.edu.pl"})
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if read.Header.Get(gpf.FieldRealm) != "szkola.edu.pl" {
		t.Error("the exported file does not say which school it belongs to")
	}
	if !strings.Contains(read.Header.Get(gpf.FieldApp), "goptivum-server") {
		t.Error("the exported file does not say what wrote it")
	}
	if read.Mode != gpf.ModePlain {
		t.Errorf("mode = %q, want plain from the registry exporter", read.Mode)
	}
}

// schoolKey is a school's own key as the escrow store hands it over.
type schoolKey struct{ identity *age.X25519Identity }

func (k schoolKey) Identity(context.Context) (age.Identity, error) { return k.identity, nil }

func (k schoolKey) Recipient(context.Context) (age.Recipient, error) {
	if k.identity == nil {
		return nil, nil
	}
	return k.identity.Recipient(), nil
}

// The variant names what a file is encrypted to, so a file kept from before
// the school had a key, or for another key, is known to be stale.
func TestTheVariantNamesTheKeyAFileIsEncryptedTo(t *testing.T) {
	identity, err := age.GenerateX25519Identity()
	if err != nil {
		t.Fatalf("key: %v", err)
	}
	for name, one := range map[string]struct {
		exporter *goptivum.Exporter
		variant  string
		mode     string
	}{
		"no key source": {goptivum.NewExporter("", "goptivum-server"), "", gpf.ModePlain},
		"no key held":   {goptivum.NewExporter("", "goptivum-server").WithKeys(schoolKey{}), "", gpf.ModePlain},
		"the school's":  {goptivum.NewExporter("", "goptivum-server").WithKeys(schoolKey{identity}), identity.Recipient().String(), gpf.ModeIdentity},
	} {
		variant, err := one.exporter.Variant(t.Context())
		if err != nil || variant != one.variant {
			t.Errorf("%s: variant = %q, %v; want %q", name, variant, err, one.variant)
		}
		written, err := one.exporter.Export(t.Context(), &formats.Result{Document: solvedDocument()})
		if err != nil {
			t.Fatalf("%s: export: %v", name, err)
		}
		read, err := gpf.Decode(written, gpf.ReadOptions{Identities: []age.Identity{identity}})
		if err != nil || read.Mode != one.mode {
			t.Errorf("%s: mode = %v, %v; want %s", name, read, err, one.mode)
		}
	}
}

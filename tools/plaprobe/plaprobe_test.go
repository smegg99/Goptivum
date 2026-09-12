// tools/plaprobe/plaprobe_test.go

package main

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/text/encoding/charmap"

	"github.com/smegg99/goptivum/backend/formats/optivum/pla"
)

// secret stands for what a real file carries and this tool must not print:
// census and tree report shape, never a value.
const secret = "Zolkiewski"

const plan = `<?xml version="1.0" encoding="iso-8859-2"?>
<plan wersja="12.00.0004">
 <dni><dzien kod="Pn" nazwa="Poniedzialek"/></dni>
 <lekcje><lekcja kod="1" od=" 8:00" do=" 8:45" czas=""/></lekcje>
 <nauczyciele><nauczyciel kod="AB" imie="Jan" nazwisko="` + secret + `"/></nauczyciele>
 <przydzialy><p kl="1A" nau="AB" prz="mat" dz="0" godz="0" bk="1"/></przydzialy>
</plan>`

func container(t *testing.T) string {
	t.Helper()

	latin2, err := charmap.ISO8859_2.NewEncoder().Bytes([]byte(plan))
	if err != nil {
		t.Fatalf("encode as iso-8859-2: %v", err)
	}
	data, err := pla.Encode(pla.File{Magic: pla.Magic, XML: latin2, Compressed: true})
	if err != nil {
		t.Fatalf("wrap: %v", err)
	}
	path := filepath.Join(t.TempDir(), "plan.pla")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	return path
}

func run(t *testing.T, args ...string) (string, error) {
	t.Helper()

	root := newRootCmd()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs(args)
	err := root.Execute()
	return out.String(), err
}

func TestCensusCountsElementsWithoutPrintingValues(t *testing.T) {
	out, err := run(t, "census", container(t))
	if err != nil {
		t.Fatalf("census: %v", err)
	}
	for _, want := range []string{"nauczyciel", "przydzialy", "imie", "nazwisko", "PlanOptivum400"} {
		if !strings.Contains(out, want) {
			t.Errorf("census does not report %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, secret) {
		t.Error("census printed a value out of the file")
	}
}

func TestTreeReportsValueShapesWithoutPrintingValues(t *testing.T) {
	out, err := run(t, "tree", container(t))
	if err != nil {
		t.Fatalf("tree: %v", err)
	}
	for _, want := range []string{"nauczyciel", "in nauczyciele", "@nazwisko", "text(len<=", "@kod", "empty", "time"} {
		if !strings.Contains(out, want) {
			t.Errorf("tree does not report %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, secret) {
		t.Error("tree printed a value out of the file")
	}
}

// xml is the one command that writes what is inside, and it writes nothing
// else, so a redirect produces a usable document.
func TestXMLWritesTheDocumentAndNothingElse(t *testing.T) {
	path := container(t)

	stdout := os.Stdout
	read, write, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	os.Stdout = write
	_, runErr := run(t, "xml", path)
	write.Close()
	os.Stdout = stdout

	var document bytes.Buffer
	if _, err := document.ReadFrom(read); err != nil {
		t.Fatalf("read: %v", err)
	}
	if runErr != nil {
		t.Fatalf("xml: %v", runErr)
	}
	if !strings.HasPrefix(document.String(), "<?xml") {
		t.Errorf("the document does not start the file:\n%q", document.String()[:40])
	}
	if !strings.Contains(document.String(), secret) {
		t.Error("the decoded document lost its content")
	}
	if !strings.Contains(document.String(), "Poniedzialek") {
		t.Error("the payload did not survive the transcode")
	}
}

// This tool has no configuration file, so the environment names the language
// it prints in.
func TestCensusSpeaksTheEnvironmentsLanguage(t *testing.T) {
	t.Setenv("GOPTIVUM_LANG", "pl")

	out, err := run(t, "census", container(t))
	if err != nil {
		t.Fatalf("census: %v", err)
	}
	for _, want := range []string{"plik", "sygnatura", "kontener", "liczba", "atrybuty"} {
		if !strings.Contains(out, want) {
			t.Errorf("census did not render %q in Polish:\n%s", want, out)
		}
	}
	if strings.Contains(out, "attributes") {
		t.Error("an English column survived into the Polish rendering")
	}
}

func TestMisuseIsAUsageError(t *testing.T) {
	path := container(t)
	cases := []struct {
		name string
		args []string
	}{
		{"unknown command", []string{"frobnicate"}},
		{"no file", []string{"census"}},
		{"two files", []string{"census", path, path}},
		{"unknown flag", []string{"census", "--wat", path}},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			if _, err := run(t, test.args...); !errors.Is(err, errUsage) {
				t.Errorf("error = %v, want a usage error so the process exits 2", err)
			}
		})
	}
}

func TestAFileThatIsNotAContainerIsAnError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "notes.txt")
	if err := os.WriteFile(path, []byte("this is not a plan"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	_, err := run(t, "census", path)
	if err == nil {
		t.Fatal("census accepted something that is not a container")
	}
	if errors.Is(err, errUsage) {
		t.Error("a file that cannot be read is not a misuse of the command")
	}
	if _, err := run(t, "census", filepath.Join(t.TempDir(), "missing.pla")); err == nil {
		t.Error("census accepted a file that is not there")
	}
}

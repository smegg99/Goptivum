// document/gpf/gpf_test.go

package gpf_test

import (
	"bytes"
	"encoding/binary"
	"errors"
	"strings"
	"testing"

	"filippo.io/age"
	"google.golang.org/protobuf/proto"

	arrangov1 "github.com/smegg99/arrango/backend/gen/arrangov1"

	"github.com/smegg99/goptivum/document"
	goptivumv1 "github.com/smegg99/goptivum/document/gen/goptivumv1"
	"github.com/smegg99/goptivum/document/gpf"
)

func school() *arrangov1.SchoolModel {
	return &arrangov1.SchoolModel{
		Name:     "Szkoła Testowa",
		Days:     []*arrangov1.Day{{Id: 1, Name: "Poniedziałek", PeriodCount: 2}},
		Periods:  []*arrangov1.Period{{Id: 2, Name: "8:00"}},
		Teachers: []*arrangov1.Teacher{{Id: 3, Name: "Łukasz Żółć"}},
		Rooms:    []*arrangov1.Room{{Id: 4, Name: "12"}},
		Lessons:  []*arrangov1.LessonInstance{{Id: 5, TeacherId: 3, Duration: 1}},
	}
}

// solvedDocument is a solved plan, so the round trip covers everything the
// envelope carries rather than just the school.
func solvedDocument() *goptivumv1.Document {
	built := document.New(school())
	built.Schedule = &arrangov1.ScheduleSnapshot{
		Lessons: []*arrangov1.ScheduledLesson{
			{LessonId: 5, Placement: &arrangov1.Placement{DayId: 1, StartPeriod: 2, RoomId: 4}},
		},
	}
	return built
}

func header(t *testing.T) *gpf.Header {
	t.Helper()
	return gpf.NewHeader().
		Set(gpf.FieldSchool, "Zespol Szkol Ekonomiczno-Mechanicznych").
		Set(gpf.FieldPlan, "wrzesien v3").
		Set(gpf.FieldSaved, "2026-09-12T14:22:31Z").
		Set(gpf.FieldBy, "karol").
		Set(gpf.FieldApp, "goptivum-desktop 0.4.2")
}

// The milestone's gate: every mode round trips, and what comes back is the same
// document that went in, compared on bytes rather than on structs.
func TestRoundTripsInEveryMode(t *testing.T) {
	identity, err := age.GenerateX25519Identity()
	if err != nil {
		t.Fatalf("identity: %v", err)
	}

	for _, one := range []struct {
		name  string
		write gpf.WriteOptions
		read  gpf.ReadOptions
		mode  string
	}{
		{
			name:  "plain",
			write: gpf.WriteOptions{Header: header(t)},
			mode:  gpf.ModePlain,
		},
		{
			name: "password",
			// A low work factor so the suite does not spend a second per case.
			write: gpf.WriteOptions{Header: header(t), Passphrase: "correct horse", WorkFactor: 10},
			read:  gpf.ReadOptions{Passphrase: "correct horse"},
			mode:  gpf.ModePassword,
		},
		{
			name:  "identity",
			write: gpf.WriteOptions{Header: header(t), Recipients: []age.Recipient{identity.Recipient()}},
			read:  gpf.ReadOptions{Identities: []age.Identity{identity}},
			mode:  gpf.ModeIdentity,
		},
	} {
		t.Run(one.name, func(t *testing.T) {
			original := solvedDocument()
			file, err := gpf.Encode(original, one.write)
			if err != nil {
				t.Fatalf("encode: %v", err)
			}
			if !gpf.Looks(file) {
				t.Fatal("the file does not start with the magic")
			}

			read, err := gpf.Decode(file, one.read)
			if err != nil {
				t.Fatalf("decode: %v", err)
			}
			if read.Mode != one.mode {
				t.Errorf("mode = %q, want %q", read.Mode, one.mode)
			}

			// Bytes, not struct equality: content addressing depends on byte
			// stability and a map-ordering difference would pass a struct
			// comparison while breaking deduplication.
			want, err := proto.MarshalOptions{Deterministic: true}.Marshal(original)
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			got, err := proto.MarshalOptions{Deterministic: true}.Marshal(read.Document)
			if err != nil {
				t.Fatalf("marshal read: %v", err)
			}
			if !bytes.Equal(want, got) {
				t.Error("the document changed on its way through the container")
			}
			// The envelope's own fields survive, which is why it exists.
			if read.Document.GetNextArrangoId() != original.GetNextArrangoId() {
				t.Error("the id counter did not survive")
			}
			if len(read.Document.GetSchedule().GetLessons()) != 1 {
				t.Error("the schedule did not survive")
			}
			// The header is disclosure and travels in the clear even when the
			// payload does not.
			if read.Header.Get(gpf.FieldSchool) == "" {
				t.Error("the header lost the school name")
			}
			if read.Header.Get(gpf.FieldMode) != one.mode {
				t.Errorf("header mode = %q, want %q", read.Header.Get(gpf.FieldMode), one.mode)
			}
		})
	}
}

// A person holding the file can see what it is without any tool, from either
// end. That is the whole reason the header and footer are plaintext.
func TestTheFileIsLegibleFromBothEnds(t *testing.T) {
	file, err := gpf.Encode(solvedDocument(), gpf.WriteOptions{
		Header: header(t), Passphrase: "correct horse", WorkFactor: 10,
	})
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	head := string(file[:200])
	if !strings.Contains(head, gpf.Brand) || !strings.Contains(head, "Goptivum Project File") {
		t.Errorf("head of the file is not legible: %q", head)
	}
	if !strings.Contains(head, "Zespol Szkol") {
		t.Error("the school name is not readable in the header")
	}
	tail := string(file[len(file)-200:])
	if !strings.Contains(tail, gpf.Brand) {
		t.Errorf("tail of the file is not legible: %q", tail)
	}
	// The payload is not.
	if bytes.Contains(file, []byte("Łukasz")) {
		t.Error("an encrypted file carries its teacher names in the clear")
	}
}

// Any one of several recipients opens the file, which is what "encrypted to the
// school" means when more than one key is current.
func TestAnyOneRecipientOpensAnIdentityFile(t *testing.T) {
	var identities []*age.X25519Identity
	var recipients []age.Recipient
	for range 3 {
		identity, err := age.GenerateX25519Identity()
		if err != nil {
			t.Fatalf("identity: %v", err)
		}
		identities = append(identities, identity)
		recipients = append(recipients, identity.Recipient())
	}
	file, err := gpf.Encode(solvedDocument(), gpf.WriteOptions{Header: header(t), Recipients: recipients})
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	if keys := headerOf(t, file).Get(gpf.FieldKeys); keys != "3 recipients" {
		t.Errorf("keys = %q, want 3 recipients", keys)
	}
	for i, identity := range identities {
		if _, err := gpf.Decode(file, gpf.ReadOptions{Identities: []age.Identity{identity}}); err != nil {
			t.Errorf("identity %d could not open the file: %v", i, err)
		}
	}
	// And a key that was not a recipient cannot.
	stranger, err := age.GenerateX25519Identity()
	if err != nil {
		t.Fatalf("identity: %v", err)
	}
	if _, err := gpf.Decode(file, gpf.ReadOptions{Identities: []age.Identity{stranger}}); !errors.Is(err, gpf.ErrIdentityRequired) {
		t.Errorf("a stranger's key = %v, want ErrIdentityRequired", err)
	}
}

// headerOf reads just the header, for a test that wants to look at it without
// opening the payload.
func headerOf(t *testing.T, file []byte) *gpf.Header {
	t.Helper()
	read, err := gpf.Decode(file, gpf.ReadOptions{Passphrase: "correct horse"})
	if err != nil {
		// An identity file cannot be opened here, so fall back to reading the
		// plaintext block directly.
		length := binary.LittleEndian.Uint32(file[12:16])
		block := file[gpf.PreheaderSize : gpf.PreheaderSize+int(length)]
		parsed := gpf.NewHeader()
		for _, line := range strings.Split(string(block), "\n") {
			name, value, found := strings.Cut(line, ":")
			if found && !strings.HasPrefix(line, "#") {
				parsed.Set(strings.TrimSpace(name), strings.TrimSpace(value))
			}
		}
		return parsed
	}
	return read.Header
}

func TestEncodingDoesNotReuseOwnedHeaderFields(t *testing.T) {
	key, err := age.GenerateX25519Identity()
	if err != nil {
		t.Fatal(err)
	}
	original := gpf.NewHeader().Set(gpf.FieldPlan, "Synthetic").Set(gpf.FieldKeys, "stale")
	before, err := original.Encode()
	if err != nil {
		t.Fatal(err)
	}
	for _, options := range []gpf.WriteOptions{
		{Header: original, Recipients: []age.Recipient{key.Recipient()}},
		{Header: original, Passphrase: "synthetic", WorkFactor: 10},
		{Header: original},
	} {
		data, err := gpf.Encode(solvedDocument(), options)
		if err != nil {
			t.Fatal(err)
		}
		file, err := gpf.Decode(data, gpf.ReadOptions{Passphrase: "synthetic", Identities: []age.Identity{key}})
		if err != nil {
			t.Fatal(err)
		}
		expected := ""
		if len(options.Recipients) != 0 {
			expected = "1 recipients"
		}
		if file.Header.Get(gpf.FieldKeys) != expected {
			t.Fatal("header retained keys from another mode")
		}
		after, err := original.Encode()
		if err != nil || !bytes.Equal(before, after) {
			t.Fatal("encoding changed the caller's header")
		}
	}
}

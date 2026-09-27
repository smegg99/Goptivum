// document/gpf/hostile_test.go

package gpf_test

import (
	"bytes"
	"encoding/binary"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"filippo.io/age"

	"github.com/klauspost/compress/zstd"

	"github.com/smegg99/goptivum/document"
	"github.com/smegg99/goptivum/document/gpf"
)

// plainFile is a valid file to take apart.
func plainFile(t *testing.T) []byte {
	t.Helper()
	file, err := gpf.Encode(solvedDocument(), gpf.WriteOptions{Header: header(t)})
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	return file
}

// Every length in this format is checked against the bytes actually present
// before it sizes an allocation. These are crafted files, not comments.
func TestEveryLengthIsCheckedBeforeItAllocates(t *testing.T) {
	for _, one := range []struct {
		name  string
		craft func(t *testing.T, file []byte) []byte
	}{
		{
			name: "header longer than the file",
			craft: func(t *testing.T, file []byte) []byte {
				binary.LittleEndian.PutUint32(file[12:16], 0xFFFFFF)
				return file
			},
		},
		{
			name: "header over the one megabyte cap",
			craft: func(t *testing.T, file []byte) []byte {
				binary.LittleEndian.PutUint32(file[12:16], 1<<21)
				return file
			},
		},
		{
			name: "chunk claiming more than remains",
			craft: func(t *testing.T, file []byte) []byte {
				at := chunkStart(t, file)
				binary.LittleEndian.PutUint32(file[at+1:at+5], 0xFFFFFF)
				return file
			},
		},
		{
			name: "chunk over the payload cap",
			craft: func(t *testing.T, file []byte) []byte {
				at := chunkStart(t, file)
				binary.LittleEndian.PutUint32(file[at+1:at+5], 1<<30)
				return file
			},
		},
		{
			name: "footer longer than the file",
			craft: func(t *testing.T, file []byte) []byte {
				binary.LittleEndian.PutUint32(file[len(file)-gpf.LocatorSize:], 0xFFFFFF)
				return file
			},
		},
		{
			name: "footer over its cap",
			craft: func(t *testing.T, file []byte) []byte {
				binary.LittleEndian.PutUint32(file[len(file)-gpf.LocatorSize:], 1<<20)
				return file
			},
		},
		{
			name:  "truncated to nothing but a preheader",
			craft: func(t *testing.T, file []byte) []byte { return file[:gpf.PreheaderSize] },
		},
		{
			name:  "truncated mid-preheader",
			craft: func(t *testing.T, file []byte) []byte { return file[:10] },
		},
		{
			name:  "no locator at the end",
			craft: func(t *testing.T, file []byte) []byte { return file[:len(file)-4] },
		},
	} {
		t.Run(one.name, func(t *testing.T) {
			crafted := one.craft(t, plainFile(t))
			if _, err := gpf.Decode(crafted, gpf.ReadOptions{}); err == nil {
				t.Fatal("a crafted file was accepted")
			}
		})
	}
}

// A decompression bomb fails on the decoder's limit rather than on memory. The
// cap works because the payload goes through the document module, which uses
// DecodeAll: a zstd reader built here would cap only the window and let this
// through.
func TestADecompressionBombIsRefused(t *testing.T) {
	encoder, err := zstd.NewWriter(nil, zstd.WithWindowSize(128<<10))
	if err != nil {
		t.Fatalf("zstd: %v", err)
	}
	bomb := encoder.EncodeAll(make([]byte, 200<<20), nil)
	t.Logf("bomb: %d bytes compressed from %d", len(bomb), 200<<20)

	file := plainFile(t)
	crafted := replacePayload(t, file, bomb)

	_, err = gpf.Decode(crafted, gpf.ReadOptions{})
	if !errors.Is(err, document.ErrTooLarge) {
		t.Fatalf("err = %v, want document.ErrTooLarge", err)
	}
}

// An unknown chunk type refuses the file when its critical bit is set, and is
// skipped when it is not. That one bit is what lets this format grow.
func TestAnUnknownChunkIsRefusedOrSkippedByItsCriticalBit(t *testing.T) {
	for _, one := range []struct {
		name    string
		kind    byte
		refused bool
	}{
		{"critical", 0x82, true},
		{"advisory", 0x02, false},
	} {
		t.Run(one.name, func(t *testing.T) {
			file := plainFile(t)
			at := chunkStart(t, file)
			// A chunk of the unknown type, in front of the payload.
			extra := []byte{one.kind, 0, 0, 0, 0}
			binary.LittleEndian.PutUint32(extra[1:5], 4)
			extra = append(extra, 'n', 'o', 'p', 'e')

			crafted := append([]byte{}, file[:at]...)
			crafted = append(crafted, extra...)
			crafted = append(crafted, file[at:]...)

			read, err := gpf.Decode(crafted, gpf.ReadOptions{})
			if one.refused {
				var unsupported *gpf.UnsupportedAlgorithmError
				if !errors.As(err, &unsupported) {
					t.Fatalf("err = %v, want UnsupportedAlgorithmError", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("an advisory chunk was not skipped: %v", err)
			}
			if read.Document.GetModel().GetName() == "" {
				t.Error("the rest of the file did not read")
			}
		})
	}
}

// A newer file is refused rather than half-read, and an unknown critical flag
// with it. Neither costs a decryption.
func TestAFileThisReaderDoesNotUnderstandIsRefused(t *testing.T) {
	t.Run("newer version", func(t *testing.T) {
		file := plainFile(t)
		binary.LittleEndian.PutUint16(file[8:10], gpf.Version+1)
		var unsupported *gpf.UnsupportedVersionError
		if _, err := gpf.Decode(file, gpf.ReadOptions{}); !errors.As(err, &unsupported) {
			t.Fatalf("err = %v, want UnsupportedVersionError", err)
		}
	})
	t.Run("unknown critical flag", func(t *testing.T) {
		file := plainFile(t)
		binary.LittleEndian.PutUint16(file[10:12], 1<<4)
		var unsupported *gpf.UnsupportedAlgorithmError
		if _, err := gpf.Decode(file, gpf.ReadOptions{}); !errors.As(err, &unsupported) {
			t.Fatalf("err = %v, want UnsupportedAlgorithmError", err)
		}
	})
	t.Run("unknown advisory flag is ignored", func(t *testing.T) {
		file := plainFile(t)
		binary.LittleEndian.PutUint16(file[10:12], 1<<12)
		if _, err := gpf.Decode(file, gpf.ReadOptions{}); err != nil {
			t.Fatalf("an advisory flag was not ignored: %v", err)
		}
	})
	t.Run("not a gpf at all", func(t *testing.T) {
		if _, err := gpf.Decode([]byte("PK\x03\x04 this is a zip"), gpf.ReadOptions{}); !errors.Is(err, gpf.ErrNotGPF) {
			t.Errorf("err = %v, want ErrNotGPF", err)
		}
	})
}

// The refusals a caller can act on are answered from plaintext, before any age
// call. The proof is that they fire on a file this reader has no key for: if
// anything tried to decrypt first, the error would be about decryption.
func TestTheRefusalsFireBeforeAnythingIsDecrypted(t *testing.T) {
	t.Run("password required", func(t *testing.T) {
		file, err := gpf.Encode(solvedDocument(), gpf.WriteOptions{
			Header: header(t), Passphrase: "correct horse", WorkFactor: 10,
		})
		if err != nil {
			t.Fatalf("encode: %v", err)
		}
		if _, err := gpf.Decode(file, gpf.ReadOptions{}); !errors.Is(err, gpf.ErrPasswordRequired) {
			t.Fatalf("err = %v, want ErrPasswordRequired", err)
		}
		// And a wrong one is a different answer from a missing one.
		if _, err := gpf.Decode(file, gpf.ReadOptions{Passphrase: "wrong"}); !errors.Is(err, gpf.ErrWrongPassword) {
			t.Fatalf("err = %v, want ErrWrongPassword", err)
		}
	})

	t.Run("wrong realm", func(t *testing.T) {
		file, err := gpf.Encode(solvedDocument(), gpf.WriteOptions{
			Header:     header(t).Set(gpf.FieldRealm, "szkola.edu.pl"),
			Passphrase: "correct horse", WorkFactor: 10,
		})
		if err != nil {
			t.Fatalf("encode: %v", err)
		}
		// The right password is supplied, so if this reached age it would
		// succeed. It must not reach age.
		var wrong *gpf.WrongRealmError
		_, err = gpf.Decode(file, gpf.ReadOptions{Passphrase: "correct horse", Realm: "liceum.pl"})
		if !errors.As(err, &wrong) {
			t.Fatalf("err = %v, want WrongRealmError", err)
		}
		if wrong.File != "szkola.edu.pl" || wrong.Local != "liceum.pl" {
			t.Errorf("the refusal does not name both schools: %v", wrong)
		}
		// The same file opens for the school it belongs to.
		if _, err := gpf.Decode(file, gpf.ReadOptions{Passphrase: "correct horse", Realm: "szkola.edu.pl"}); err != nil {
			t.Errorf("the owning school could not open it: %v", err)
		}
		// A reader with no realm configured does not refuse anything.
		if _, err := gpf.Decode(file, gpf.ReadOptions{Passphrase: "correct horse"}); err != nil {
			t.Errorf("a reader with no realm refused: %v", err)
		}
	})
}

// The header is disclosure and the flags are the decision. A file whose text
// says plain over an encrypted payload is corrupt, not an argument to settle.
func TestAHeaderThatContradictsTheFlagsIsRefused(t *testing.T) {
	file, err := gpf.Encode(solvedDocument(), gpf.WriteOptions{
		Header: header(t), Passphrase: "correct horse", WorkFactor: 10,
	})
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	// Rewrite the mode line in place, keeping the header the same length so
	// every other offset in the file stays valid.
	length := int(binary.LittleEndian.Uint32(file[12:16]))
	block := file[gpf.PreheaderSize : gpf.PreheaderSize+length]
	at := bytes.Index(block, []byte(gpf.ModePassword))
	if at < 0 {
		t.Fatalf("no mode line in the header:\n%s", block)
	}
	copy(block[at:], []byte(gpf.ModePlain+"   "))

	if _, err := gpf.Decode(file, gpf.ReadOptions{Passphrase: "correct horse"}); err == nil {
		t.Fatal("a file whose header contradicts its flags was accepted")
	}
}

// A header value that would forge a second field is refused on write, so a file
// that cannot be read back is never produced.
func TestAHeaderValueCannotForgeAField(t *testing.T) {
	for _, one := range []struct{ name, value string }{
		{"line break", "wrzesien\nrealm: attacker.example"},
		{"carriage return", "wrzesien\rrealm: attacker.example"},
		{"nul", "wrzesien\x00"},
		{"not utf-8", "wrzesien\xff\xfe"},
	} {
		t.Run(one.name, func(t *testing.T) {
			_, err := gpf.Encode(solvedDocument(), gpf.WriteOptions{
				Header: gpf.NewHeader().Set(gpf.FieldPlan, one.value),
			})
			if err == nil {
				t.Fatal("a header value that breaks the format was written")
			}
		})
	}
}

// chunkStart is where the payload chunk begins, read the way the format says
// rather than assumed.
func chunkStart(t *testing.T, file []byte) int {
	t.Helper()
	return gpf.PreheaderSize + int(binary.LittleEndian.Uint32(file[12:16]))
}

// replacePayload swaps the payload chunk's body, keeping the file valid
// everywhere else.
func replacePayload(t *testing.T, file, body []byte) []byte {
	t.Helper()
	at := chunkStart(t, file)
	footerLen := binary.LittleEndian.Uint32(file[len(file)-gpf.LocatorSize:])
	tail := file[len(file)-gpf.LocatorSize-int(footerLen):]

	out := append([]byte{}, file[:at]...)
	frame := make([]byte, 5)
	frame[0] = file[at]
	binary.LittleEndian.PutUint32(frame[1:5], uint32(len(body)))
	out = append(out, frame...)
	out = append(out, body...)
	return append(out, tail...)
}

// A file wrapped to keys says so without naming them, and a reader with no key
// learns that rather than a decryption failure.
func TestAnIdentityFileWithoutAKeyIsRefusedEarly(t *testing.T) {
	identity, err := age.GenerateX25519Identity()
	if err != nil {
		t.Fatalf("identity: %v", err)
	}
	file, err := gpf.Encode(solvedDocument(), gpf.WriteOptions{
		Header: header(t), Recipients: []age.Recipient{identity.Recipient()},
	})
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	if _, err := gpf.Decode(file, gpf.ReadOptions{}); !errors.Is(err, gpf.ErrIdentityRequired) {
		t.Fatalf("err = %v, want ErrIdentityRequired", err)
	}
	// The recipient list is a count, never the keys themselves.
	if bytes.Contains(file[:chunkStart(t, file)], []byte(identity.Recipient().String())) {
		t.Error("the header names a recipient key")
	}
	if !strings.Contains(string(file[:chunkStart(t, file)]), "1 recipients") {
		t.Error("the header does not say how many keys open this file")
	}
}

// A password and a key list together is refused with a message about what the
// caller did, not with age's own complaint about stanzas.
func TestAFileIsWrappedToAPasswordOrToKeysNeverBoth(t *testing.T) {
	identity, err := age.GenerateX25519Identity()
	if err != nil {
		t.Fatalf("identity: %v", err)
	}
	_, err = gpf.Encode(solvedDocument(), gpf.WriteOptions{
		Header:     header(t),
		Passphrase: "correct horse",
		Recipients: []age.Recipient{identity.Recipient()},
	})
	if err == nil {
		t.Fatal("a file was wrapped both ways")
	}
	if !strings.Contains(err.Error(), "never both") {
		t.Errorf("err = %v, want a message about the caller's choice", err)
	}
}

// A partial file is never visible at the target path. A .gpf is written whole
// and once, so temp-fsync-rename is the entire crash story.
func TestWriteIsAtomic(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "plan.gpf")

	file := plainFile(t)
	if err := gpf.WriteFile(path, file); err != nil {
		t.Fatalf("write: %v", err)
	}
	read, err := gpf.ReadFile(path, gpf.ReadOptions{})
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if read.Document.GetModel().GetName() == "" {
		t.Error("the file did not survive the round trip through disk")
	}

	// A failed write leaves the target untouched and no temp file behind.
	if err := gpf.WriteFile(filepath.Join(directory, "missing", "plan.gpf"), file); err == nil {
		t.Fatal("a write into a directory that does not exist succeeded")
	}
	entries, err := os.ReadDir(directory)
	if err != nil {
		t.Fatalf("read dir: %v", err)
	}
	if len(entries) != 1 || entries[0].Name() != "plan.gpf" {
		names := []string{}
		for _, entry := range entries {
			names = append(names, entry.Name())
		}
		t.Errorf("the directory holds %v, want only plan.gpf", names)
	}
}

// Every refusal carries a catalogue key and its parameters, never a sentence:
// each app that reads a .gpf words it in its reader's language.
func TestRefusalsCarryAKeyAndItsParameters(t *testing.T) {
	identity, err := age.GenerateX25519Identity()
	if err != nil {
		t.Fatalf("identity: %v", err)
	}
	encrypted, err := gpf.Encode(solvedDocument(), gpf.WriteOptions{
		Header:     header(t).Set(gpf.FieldRealm, "szkola.edu.pl"),
		Passphrase: "correct horse", WorkFactor: 10,
	})
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	keyed, err := gpf.Encode(solvedDocument(), gpf.WriteOptions{
		Header: header(t), Recipients: []age.Recipient{identity.Recipient()},
	})
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	newer := plainFile(t)
	binary.LittleEndian.PutUint16(newer[8:10], gpf.Version+1)
	truncated := plainFile(t)
	truncated = truncated[:len(truncated)-3]

	for _, one := range []struct {
		name    string
		file    []byte
		options gpf.ReadOptions
		key     string
		data    map[string]any
	}{
		{"password required", encrypted, gpf.ReadOptions{}, "gpf.passwordRequired", nil},
		{"wrong password", encrypted, gpf.ReadOptions{Passphrase: "nope"}, "gpf.wrongPassword", nil},
		{"wrong realm", encrypted, gpf.ReadOptions{Realm: "liceum.pl", Passphrase: "correct horse"},
			"gpf.wrongRealm", map[string]any{"File": "szkola.edu.pl", "Local": "liceum.pl"}},
		{"identity required", keyed, gpf.ReadOptions{}, "gpf.identityRequired", nil},
		{"newer version", newer, gpf.ReadOptions{}, "gpf.unsupportedVersion",
			map[string]any{"Found": gpf.Version + 1, "Known": gpf.Version}},
		{"not ours", []byte("PK\x03\x04"), gpf.ReadOptions{}, "gpf.notAFile", nil},
		{"damaged", truncated, gpf.ReadOptions{}, "gpf.malformed", nil},
	} {
		t.Run(one.name, func(t *testing.T) {
			_, err := gpf.Decode(one.file, one.options)
			var refusal gpf.Refusal
			if !errors.As(err, &refusal) {
				t.Fatalf("err = %v, want a keyed refusal", err)
			}
			if refusal.Key() != one.key {
				t.Errorf("key = %q, want %q", refusal.Key(), one.key)
			}
			if !reflect.DeepEqual(refusal.Data(), one.data) {
				t.Errorf("data = %#v, want %#v", refusal.Data(), one.data)
			}
			// The error's own text starts with the key, so a log has something
			// stable and nothing here decides what language a reader gets.
			if !strings.HasPrefix(err.Error(), one.key) {
				t.Errorf("err.Error() = %q, want it to start with %s", err.Error(), one.key)
			}
		})
	}
}

// RefusalKeys lists every key a refusal carries, so each app can check its catalogue
// words all of them.
func TestRefusalKeysListsEveryRefusal(t *testing.T) {
	want := []string{
		"gpf.identityRequired", "gpf.malformed", "gpf.notAFile", "gpf.passwordRequired",
		"gpf.unsupportedAlgorithm", "gpf.unsupportedVersion", "gpf.wrongPassword", "gpf.wrongRealm",
	}
	got := slices.Sorted(slices.Values(gpf.RefusalKeys()))
	if !slices.Equal(got, want) {
		t.Errorf("RefusalKeys() = %v, want %v", got, want)
	}
}

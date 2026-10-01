// formats/goptivum/refusals_test.go

package goptivum_test

import (
	"encoding/binary"
	"strings"
	"testing"

	"filippo.io/age"

	"github.com/smegg99/goptivum/backend/common/messages"
	"github.com/smegg99/goptivum/document/gpf"
)

// The server's catalogue words every key a .gpf refusal carries, in both
// languages: the document module names the keys and cannot check them itself.
func TestTheCatalogueWordsEveryRefusal(t *testing.T) {
	for _, key := range gpf.RefusalKeys() {
		for _, lang := range []string{"pl", "en"} {
			if said := messages.Localize(messages.Localizer(lang), key); said == key {
				t.Errorf("%s has no %s text", key, lang)
			}
		}
	}
}

// Every refusal a person reads comes from the catalogue, in their language:
// the error carries a key and the edge that knows the reader's language
// renders it.
func TestRefusalsAreLocalized(t *testing.T) {
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

	for _, one := range []struct {
		name    string
		file    []byte
		options gpf.ReadOptions
		polish  string
		english string
	}{
		{"password required", encrypted, gpf.ReadOptions{}, "wymaga hasła", "needs a password"},
		{"wrong password", encrypted, gpf.ReadOptions{Passphrase: "nope"}, "nie otwiera", "does not open"},
		{"wrong realm", encrypted, gpf.ReadOptions{Realm: "liceum.pl", Passphrase: "correct horse"},
			"należy do szkola.edu.pl", "belongs to szkola.edu.pl"},
		{"identity required", keyed, gpf.ReadOptions{}, "wymaga klucza", "needs a key"},
		{"newer version", newer, gpf.ReadOptions{}, "nowszą wersją", "newer version"},
		{"not ours", []byte("PK\x03\x04"), gpf.ReadOptions{}, "nie jest plik", "not a Goptivum"},
	} {
		t.Run(one.name, func(t *testing.T) {
			_, err := gpf.Decode(one.file, one.options)
			if err == nil {
				t.Fatal("the file was accepted")
			}
			polish := messages.Say(messages.Localizer("pl"), err)
			if !strings.Contains(polish, one.polish) {
				t.Errorf("pl = %q, want it to contain %q", polish, one.polish)
			}
			english := messages.Say(messages.Localizer("en"), err)
			if !strings.Contains(english, one.english) {
				t.Errorf("en = %q, want it to contain %q", english, one.english)
			}
		})
	}
}

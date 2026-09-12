// formats/optivum/pla/corpus_test.go

package pla_test

import (
	"testing"

	"golang.org/x/text/encoding/charmap"

	"github.com/smegg99/goptivum/backend/formats/optivum/pla"
	"github.com/smegg99/goptivum/backend/internal/testsupport"
)

// Every real container must decode, transcode and round-trip unchanged.
func TestRealContainersDecodeAndRoundTrip(t *testing.T) {
	for _, path := range testsupport.Corpus(t, "*.pla") {
		t.Run(path, func(t *testing.T) {
			raw := readFile(t, path)

			file, err := pla.Decode(raw)
			if err != nil {
				t.Fatalf("decode: %v", err)
			}
			if len(file.XML) == 0 {
				t.Fatal("decoded to an empty payload")
			}

			// Payload is iso-8859-2; a mis-decoded Polish name is a silent bug.
			utf8XML, err := charmap.ISO8859_2.NewDecoder().Bytes(file.XML)
			if err != nil {
				t.Fatalf("transcode iso-8859-2: %v", err)
			}
			if len(utf8XML) < len(file.XML) {
				t.Fatalf("transcode shrank the payload: %d -> %d", len(file.XML), len(utf8XML))
			}

			// A lossy round trip silently corrupts a school's file on export.
			encoded, err := pla.Encode(file)
			if err != nil {
				t.Fatalf("encode: %v", err)
			}
			again, err := pla.Decode(encoded)
			if err != nil {
				t.Fatalf("decode after encode: %v", err)
			}
			if string(again.XML) != string(file.XML) {
				t.Fatal("round trip changed the payload")
			}
			if again.Magic != file.Magic || again.Compressed != file.Compressed {
				t.Fatalf("round trip changed the envelope: magic %q->%q compressed %v->%v",
					file.Magic, again.Magic, file.Compressed, again.Compressed)
			}
		})
	}
}

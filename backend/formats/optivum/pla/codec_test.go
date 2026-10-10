// core/optivum/pla/codec_test.go

package pla

import (
	"bytes"
	"compress/zlib"
	"encoding/binary"
	"errors"
	"github.com/smegg99/goptivum/backend/internal/testsupport"
	"io"
	"os"
	"testing"
)

type zeros struct{}

func (zeros) Read(p []byte) (int, error) {
	clear(p)
	return len(p), nil
}

func TestPayloadLimitsApplyBeforeAndAfterInflation(t *testing.T) {
	t.Run("compressed", func(t *testing.T) {
		var compressed bytes.Buffer
		writer := zlib.NewWriter(&compressed)
		if _, err := io.CopyN(writer, zeros{}, MaxDecoded+1); err != nil {
			t.Fatal(err)
		}
		if err := writer.Close(); err != nil {
			t.Fatal(err)
		}
		container := make([]byte, 2+len(Magic)+compressed.Len())
		binary.LittleEndian.PutUint16(container, uint16(len(Magic)))
		copy(container[2:], Magic)
		copy(container[2+len(Magic):], compressed.Bytes())
		mask(container[2+len(Magic):])
		if _, err := Decode(container); !errors.Is(err, ErrTooLarge) {
			t.Fatalf("compressed payload: %v", err)
		}
	})
	t.Run("plain", func(t *testing.T) {
		container := make([]byte, 2+len(Magic)+MaxDecoded+1)
		binary.LittleEndian.PutUint16(container, uint16(len(Magic)))
		copy(container[2:], Magic)
		mask(container[2+len(Magic):])
		if _, err := Decode(container); !errors.Is(err, ErrTooLarge) {
			t.Fatalf("plain payload: %v", err)
		}
		if _, err := Encode(File{XML: container[2+len(Magic):]}); !errors.Is(err, ErrTooLarge) {
			t.Fatalf("oversized encode: %v", err)
		}
	})
}

func TestRoundTripPreservesPayload(t *testing.T) {
	want := File{
		Magic:      Magic,
		XML:        []byte(`<?xml version="1.0" encoding="iso-8859-2"?><plan/>`),
		Compressed: true,
	}
	encoded, err := Encode(want)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	got, err := Decode(encoded)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if got.Magic != want.Magic {
		t.Errorf("magic = %q, want %q", got.Magic, want.Magic)
	}
	if !bytes.Equal(got.XML, want.XML) {
		t.Errorf("payload round trip mismatch:\n got %q\nwant %q", got.XML, want.XML)
	}
	if !got.Compressed {
		t.Error("Compressed = false, want true")
	}
}

// An uncompressed payload stays readable: the header rule decides, so a
// container written without zlib is not mistaken for corrupt.
func TestUncompressedPayloadRoundTrips(t *testing.T) {
	want := File{Magic: Magic, XML: []byte("<plan/>")}
	encoded, err := Encode(want)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	got, err := Decode(encoded)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if got.Compressed {
		t.Error("Compressed = true, want false")
	}
	if !bytes.Equal(got.XML, want.XML) {
		t.Errorf("payload = %q, want %q", got.XML, want.XML)
	}
}

func TestEncodeDefaultsTheMagic(t *testing.T) {
	encoded, err := Encode(File{XML: []byte("<plan/>")})
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	got, err := Decode(encoded)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if got.Magic != Magic {
		t.Errorf("magic = %q, want %q", got.Magic, Magic)
	}
}

func TestDecodeRejectsBadInput(t *testing.T) {
	cases := []struct {
		name string
		data []byte
		want error
	}{
		{"empty", nil, ErrTruncated},
		{"header only", []byte{0x0e}, ErrTruncated},
		{"magic cut short", append([]byte{0x0e, 0x00}, []byte("PlanOpt")...), ErrTruncated},
		{"foreign file", append([]byte{0x04, 0x00}, []byte("%PDF")...), ErrUnknownFormat},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			if _, err := Decode(test.data); !errors.Is(err, test.want) {
				t.Fatalf("Decode error = %v, want %v", err, test.want)
			}
		})
	}
}

func TestEncodeRejectsForeignMagic(t *testing.T) {
	if _, err := Encode(File{Magic: "SomethingElse"}); !errors.Is(err, ErrUnknownFormat) {
		t.Fatalf("Encode error = %v, want %v", err, ErrUnknownFormat)
	}
}

// A corrupt compressed payload must surface as an error, not as silent
// garbage: flip a byte inside a real container and expect a failure.
func TestDecodeReportsBrokenPayload(t *testing.T) {
	encoded, err := Encode(File{XML: bytes.Repeat([]byte("<plan/>"), 64), Compressed: true})
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	encoded[len(encoded)-1] ^= 0xFF
	if _, err := Decode(encoded); err == nil {
		t.Fatal("Decode accepted a corrupted payload")
	}
}

// The real export decodes into the plan XML, and re-encoding it yields the
// same XML back (byte-identical container output is not expected: deflate
// implementations differ).
func TestCorpusDecodesTheRealExport(t *testing.T) {
	data, err := os.ReadFile(testsupport.Corpus(t, "*.pla")[0])
	if err != nil {
		t.Fatalf("read sample: %v", err)
	}
	file, err := Decode(data)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if file.Magic != Magic {
		t.Errorf("magic = %q, want %q", file.Magic, Magic)
	}
	if !file.Compressed {
		t.Error("Compressed = false, want true for a real export")
	}
	if !bytes.HasPrefix(file.XML, []byte("<?xml")) {
		t.Fatalf("payload does not start with an XML declaration: %.32q", file.XML)
	}
	for _, want := range []string{"<plan ", "<lekcje>", "iso-8859-2"} {
		if !bytes.Contains(file.XML, []byte(want)) {
			t.Errorf("payload missing %q", want)
		}
	}

	reencoded, err := Encode(file)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	again, err := Decode(reencoded)
	if err != nil {
		t.Fatalf("Decode re-encoded: %v", err)
	}
	if !bytes.Equal(again.XML, file.XML) {
		t.Error("re-encoded container does not carry the same XML")
	}
	// The header is reproduced verbatim, whatever the payload bytes do.
	if !bytes.Equal(reencoded[:2+len(Magic)], data[:2+len(Magic)]) {
		t.Error("re-encoded header differs from the original")
	}
	if got := binary.LittleEndian.Uint16(reencoded[:2]); int(got) != len(Magic) {
		t.Errorf("magic length = %d, want %d", got, len(Magic))
	}
}

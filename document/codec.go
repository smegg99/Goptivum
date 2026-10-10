// document/codec.go

// Package document is one Goptivum plan document: its type, its codec and its id counter.
package document

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"

	"github.com/klauspost/compress/zstd"
	"google.golang.org/protobuf/proto"

	goptivumv1 "github.com/smegg99/goptivum/document/gen/goptivumv1"
)

const (
	// Format is the only value Document.Format carries; it tells a Document from a bare SchoolModel.
	Format = 1
	// MaxDecoded caps what one document may expand to; a real school is about 1 MB.
	MaxDecoded = 64 << 20
)

// ErrTooLarge means a payload expands past MaxDecoded, which no real school does.
var ErrTooLarge = errors.New("document: payload expands past the limit")

// ErrNotADocument means the payload decoded but is not a Goptivum document.
var ErrNotADocument = errors.New("document: payload is not a Goptivum document")

// Encoded is one document as stored: one zstd stream over its deterministic protobuf.
type Encoded struct {
	Bytes  []byte
	SHA256 string // hex of the hash of Bytes
	Size   int64
}

// Built once: both are safe for concurrent use and hold the compression tables.
var (
	encoder *zstd.Encoder
	decoder *zstd.Decoder
)

func init() {
	var err error
	encoder, err = zstd.NewWriter(nil, zstd.WithEncoderLevel(zstd.SpeedBetterCompression))
	if err != nil {
		panic("document: build zstd encoder: " + err.Error())
	}
	decoder, err = zstd.NewReader(nil, zstd.WithDecoderMaxMemory(MaxDecoded))
	if err != nil {
		panic("document: build zstd decoder: " + err.Error())
	}
}

// Encode marshals deterministically, so one document always gives the same bytes; CheckNotes must pass.
func Encode(doc *goptivumv1.Document) (Encoded, error) {
	if doc == nil {
		return Encoded{}, fmt.Errorf("document: nothing to encode")
	}
	if doc.GetModel() == nil {
		return Encoded{}, fmt.Errorf("document: a document needs a model")
	}
	if doc.GetFormat() != Format {
		return Encoded{}, fmt.Errorf("%w: format %d, build it with New", ErrNotADocument, doc.GetFormat())
	}
	if proto.Size(doc) > MaxDecoded {
		return Encoded{}, fmt.Errorf("%w: over %d bytes", ErrTooLarge, MaxDecoded)
	}
	if err := CheckNotes(doc); err != nil {
		return Encoded{}, err
	}
	if err := checkIDs(doc); err != nil {
		return Encoded{}, err
	}
	raw, err := proto.MarshalOptions{Deterministic: true}.Marshal(doc)
	if err != nil {
		return Encoded{}, fmt.Errorf("document: marshal: %w", err)
	}
	return encoded(encoder.EncodeAll(raw, nil)), nil
}

// Decode refuses an empty payload, which would otherwise read as an empty school, and notes CheckNotes refuses.
func Decode(data []byte) (*goptivumv1.Document, error) {
	if len(data) == 0 {
		return nil, fmt.Errorf("document: empty payload")
	}
	// DecodeAll honours the memory cap; a streaming reader caps only the window.
	raw, err := decoder.DecodeAll(data, nil)
	if err != nil {
		if errors.Is(err, zstd.ErrDecoderSizeExceeded) {
			return nil, fmt.Errorf("%w: over %d bytes", ErrTooLarge, MaxDecoded)
		}
		return nil, fmt.Errorf("document: decompress: %w", err)
	}
	if len(raw) == 0 {
		return nil, fmt.Errorf("document: payload decompressed to nothing")
	}

	var doc goptivumv1.Document
	if err := proto.Unmarshal(raw, &doc); err != nil {
		return nil, fmt.Errorf("document: unmarshal: %w", err)
	}
	if doc.GetFormat() != Format {
		return nil, fmt.Errorf("%w: format %d", ErrNotADocument, doc.GetFormat())
	}
	if doc.GetModel() == nil {
		return nil, fmt.Errorf("%w: no model", ErrNotADocument)
	}
	if err := CheckNotes(&doc); err != nil {
		return nil, err
	}
	if err := checkIDs(&doc); err != nil {
		return nil, err
	}
	return &doc, nil
}

// FromBytes takes an already encoded document as it is, after checking that it decodes.
func FromBytes(data []byte) (Encoded, *goptivumv1.Document, error) {
	doc, err := Decode(data)
	if err != nil {
		return Encoded{}, nil, err
	}
	return encoded(data), doc, nil
}

func encoded(data []byte) Encoded {
	sum := sha256.Sum256(data)
	return Encoded{Bytes: data, SHA256: hex.EncodeToString(sum[:]), Size: int64(len(data))}
}

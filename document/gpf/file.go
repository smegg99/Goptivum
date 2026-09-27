// document/gpf/file.go

package gpf

import (
	"fmt"

	"filippo.io/age"

	"github.com/smegg99/goptivum/document"
	goptivumv1 "github.com/smegg99/goptivum/document/gen/goptivumv1"
)

// ReadOptions is what a reader brings to a file.
type ReadOptions struct {
	// Passphrase opens a password-wrapped file.
	Passphrase string
	// Identities open a key-wrapped file. Any one that matches is enough.
	Identities []age.Identity
	// Realm is the reader's own school, if it knows it. Empty means no realm
	// check, and it is a message aid rather than a security boundary.
	Realm string
}

// WriteOptions is how a file is wrapped. Neither a passphrase nor recipients
// means Plain: no age at all.
type WriteOptions struct {
	// Header is the branded block. The mode, format and key count lines are
	// written from what actually happened, so a caller cannot set them wrong.
	Header *Header
	// Passphrase wraps to a password. Exclusive with Recipients, because age
	// refuses a file that mixes an scrypt recipient with any other.
	Passphrase string
	// Recipients wrap to keys. Any one of them opens the file.
	Recipients []age.Recipient
	// WorkFactor overrides the scrypt cost, as a power of two. Zero takes age's
	// own default of 2^18, about a second, which is the right answer for a
	// person typing a password.
	WorkFactor int
	// Footer is the block at the end of the file. Nil writes the default.
	Footer *Header
}

// File is what reading a .gpf produced.
type File struct {
	// Header is the plaintext block, exactly as the file carried it. Every
	// value in it is untrusted text that somebody else wrote.
	Header *Header
	// Footer is the block at the end.
	Footer *Header
	// Document is the school, its schedule and its id counter.
	Document *goptivumv1.Document
	// Mode is what the binary flags said, never what the header claimed.
	Mode string
}

// Encode writes a whole .gpf.
//
// The payload is exactly what the document module produces, wrapped and framed.
// This does not re-decide what a document is.
func Encode(doc *goptivumv1.Document, options WriteOptions) ([]byte, error) {
	encoded, err := document.Encode(doc)
	if err != nil {
		return nil, err
	}
	sealed, flags, err := wrap(encoded.Bytes, options)
	if err != nil {
		return nil, err
	}

	header := options.Header
	if header == nil {
		header = NewHeader()
	}
	// Written from what happened, not from what the caller said: these three
	// are the ones a reader could be misled by.
	header.Set(FieldFormat, fmt.Sprintf("%d", Version))
	header.Set(FieldMode, modeOf(flags))
	if flags&FlagEncrypted != 0 && flags&FlagPassphrase == 0 {
		header.Set(FieldKeys, fmt.Sprintf("%d recipients", len(options.Recipients)))
	}
	headerBytes, err := header.Encode()
	if err != nil {
		return nil, err
	}
	if len(headerBytes) > MaxHeader {
		return nil, fmt.Errorf("%w: the header is %d bytes, over the %d cap",
			ErrMalformed, len(headerBytes), MaxHeader)
	}

	footer := options.Footer
	if footer == nil {
		footer = defaultFooter(header)
	}
	footerBytes, err := footer.Encode()
	if err != nil {
		return nil, err
	}
	if len(footerBytes) > MaxFooter {
		return nil, fmt.Errorf("%w: the footer is %d bytes, over the %d cap",
			ErrMalformed, len(footerBytes), MaxFooter)
	}

	out := writePreheader(preheader{
		Version: Version, Flags: flags, HeaderLen: uint32(len(headerBytes)),
	})
	out = append(out, headerBytes...)
	out = append(out, writeChunk(ChunkPayload, sealed)...)
	out = append(out, footerBytes...)
	out = append(out, writeLocator(len(footerBytes))...)
	return out, nil
}

// Decode reads a whole .gpf.
//
// The order is the point. The binary fields are validated first, then the
// refusals a caller can act on are answered from the header and the flags, and
// only then is anything decrypted or decompressed. A file this reader will
// refuse is refused before it costs anything.
func Decode(data []byte, options ReadOptions) (*File, error) {
	head, err := readPreheader(data)
	if err != nil {
		return nil, err
	}

	headerStart := PreheaderSize
	headerEndAt := headerStart + int(head.HeaderLen)
	header, err := parseHeader(data[headerStart:headerEndAt])
	if err != nil {
		return nil, err
	}
	if err := checkModeAgrees(header, head.Flags); err != nil {
		return nil, err
	}
	// Before any age call: a file from another school is refused whether or not
	// this reader could have opened it.
	if err := checkRealm(header, options.Realm); err != nil {
		return nil, err
	}

	footerBytes, region, err := readFooterRegion(data, headerEndAt)
	if err != nil {
		return nil, err
	}
	footer, err := parseHeader(footerBytes)
	if err != nil {
		return nil, err
	}

	chunks, err := readChunks(region)
	if err != nil {
		return nil, err
	}
	if len(chunks) != 1 {
		return nil, fmt.Errorf("%w: %d payload chunks, want exactly one",
			ErrMalformed, len(chunks))
	}

	plain, err := unwrap(chunks[0].Body, head.Flags, options)
	if err != nil {
		return nil, err
	}
	// Through the document module, which uses DecodeAll and therefore caps the
	// decompressed size. A zstd reader built here would cap only the window and
	// let a bomb through.
	doc, err := document.Decode(plain)
	if err != nil {
		return nil, err
	}
	return &File{
		Header: header, Footer: footer, Document: doc, Mode: modeOf(head.Flags),
	}, nil
}

// defaultFooter is branding and nothing else.
//
// No hash, deliberately: age authenticates the payload and detects truncation,
// a zstd frame carries its own checksum, and protobuf fails on corruption, so
// an integrity field here would catch nothing the layers below already catch.
// What the footer earns its place with is that the file is identifiable from
// its end as well as its start, and that `tail` is as legible as `head`.
func defaultFooter(header *Header) *Header {
	footer := NewHeader()
	footer.Set(FieldFormat, header.Get(FieldFormat))
	footer.Set(FieldMode, header.Get(FieldMode))
	return footer
}

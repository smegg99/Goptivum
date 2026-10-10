// document/gpf/gpf.go

// Package gpf reads and writes the Goptivum Project File, the container a
// school's plan travels in: emailed, copied to a stick, opened cold by someone
// else's reader.
//
// It is a container and not a second encoding. The payload is exactly the bytes
// the document module already produces, protobuf then zstd, wrapped in a branded
// plaintext header, an optional age layer and a footer. Nothing here re-decides
// what a document is.
//
// Two rules run through the whole package. Every length is checked against the
// bytes actually present before it sizes an allocation, because the first thing
// this code meets is a file a stranger sent. And decompression goes through
// the document module and never through a zstd reader built here: the decoder's
// memory limit caps the output on DecodeAll and only the window on the
// streaming path, so a bomb streams through the latter with a nil error.
package gpf

import (
	"encoding/binary"
	"fmt"
)

// Magic is the first eight bytes of every .gpf, in every version there will
// ever be.
//
// PNG's signature with its own tag. The high bit catches a transport that
// strips to seven bits, GPF is what `head` shows a person, the CR LF and LF
// catch a transfer that translated line endings either way, and the 0x1a is DOS
// end-of-file so `type file.gpf` stops rather than filling a terminal.
var Magic = [8]byte{0x89, 'G', 'P', 'F', '\r', '\n', 0x1a, '\n'}

// Version is what this reader writes and the highest it knows how to read.
const Version uint16 = 1

// Flags in the preheader. Bits 0 to 7 are critical and bits 8 to 15 are
// advisory: a reader that meets an unknown critical bit refuses the file, and
// ignores an unknown advisory one.
const (
	// FlagEncrypted clear means the payload is zstd(protobuf) with no age
	// involved at all, not age with a no-op recipient.
	FlagEncrypted uint16 = 1 << 0
	// FlagPassphrase is set only alongside FlagEncrypted, and says the single
	// recipient is scrypt. It exists so PasswordRequired can be answered
	// without handing the payload to age first.
	FlagPassphrase uint16 = 1 << 1

	// knownFlags is every bit this reader understands.
	knownFlags = FlagEncrypted | FlagPassphrase
	// criticalFlags is the half a reader may not ignore.
	criticalFlags uint16 = 0x00FF
)

// Chunk types. The high bit is the critical bit: an unknown type with it set
// means refuse the file, and with it clear means skip using the length. Numbers
// are never reused, even for a type that is removed.
const (
	chunkCritical byte = 0x80
	// ChunkPayload is the document itself.
	ChunkPayload byte = 0x81
)

// Sizes. Every one of these bounds an allocation.
const (
	// PreheaderSize is magic, version, flags and header_len.
	PreheaderSize = 8 + 2 + 2 + 4
	// LocatorSize is footer_len and the magic again.
	LocatorSize = 4 + 8
	// MaxHeader bounds the plaintext header. A real one is a few hundred bytes.
	MaxHeader = 1 << 20
	// MaxFooter bounds the plaintext footer, which is smaller still.
	MaxFooter = 1 << 16
	// MaxPayload bounds the encoded document before decompression. A real
	// school is about 22 KB compressed; this is room for an enormous one and
	// far below what a file could claim.
	MaxPayload = 64 << 20
	// MaxFile bounds a whole .gpf read off a filesystem, which is the one place
	// a size arrives from somewhere other than an already-capped upload.
	MaxFile = 128 << 20
)

// Every refusal a person can read is a Refusal: a message key from the gpf.*
// entries of the Goptivum catalogues and its parameters, never a sentence. This
// module has no catalogue; each app that reads a .gpf words the key in its
// reader's language. Error is the key, so a log has something stable to grep.

// Refusal is why a file was refused, as a catalogue key and its parameters.
type Refusal interface {
	error
	Key() string
	Data() map[string]any
}

// The keys a Refusal carries.
const (
	KeyNotAFile             = "gpf.notAFile"
	KeyMalformed            = "gpf.malformed"
	KeyUnsupportedVersion   = "gpf.unsupportedVersion"
	KeyUnsupportedAlgorithm = "gpf.unsupportedAlgorithm"
	KeyPasswordRequired     = "gpf.passwordRequired"
	KeyIdentityRequired     = "gpf.identityRequired"
	KeyWrongPassword        = "gpf.wrongPassword"
	KeyWrongRealm           = "gpf.wrongRealm"
)

// RefusalKeys is every key a Refusal carries, for a catalogue check.
func RefusalKeys() []string {
	return []string{KeyNotAFile, KeyMalformed, KeyUnsupportedVersion, KeyUnsupportedAlgorithm,
		KeyPasswordRequired, KeyIdentityRequired, KeyWrongPassword, KeyWrongRealm}
}

// refusal is a Refusal without parameters; the sentinels below are its values.
type refusal struct{ key string }

func (r *refusal) Error() string        { return r.key }
func (r *refusal) Key() string          { return r.key }
func (r *refusal) Data() map[string]any { return nil }

// ErrNotGPF means the bytes are not a .gpf at all. Detection returns this
// rather than a parse error: a file that is not ours is not a broken one.
var ErrNotGPF Refusal = &refusal{key: KeyNotAFile}

// ErrMalformed is a .gpf that cannot be read: a length past the end of the
// file, a missing locator, a chunk that claims more than is there.
var ErrMalformed Refusal = &refusal{key: KeyMalformed}

// UnsupportedVersionError carries a file version other than the supported one.
type UnsupportedVersionError struct {
	Found uint16
	Known uint16
}

func (e *UnsupportedVersionError) Error() string { return KeyUnsupportedVersion }
func (e *UnsupportedVersionError) Key() string   { return KeyUnsupportedVersion }
func (e *UnsupportedVersionError) Data() map[string]any {
	return map[string]any{"Found": e.Found, "Known": e.Known}
}

// UnsupportedAlgorithmError is a critical flag or chunk type this reader does
// not know. Refusing is the point: guessing would open a file whose payload
// this reader cannot actually interpret.
type UnsupportedAlgorithmError struct {
	// Flags is the unknown critical bits, and Chunk the unknown critical chunk
	// type. Exactly one is set.
	Flags uint16
	Chunk byte
}

// Error keeps the numbers out of the key's parameters. Which bit or chunk type
// a reader did not know is a developer's question, and it goes to the log
// after the key; the person is told their Goptivum is too old.
func (e *UnsupportedAlgorithmError) Error() string {
	if e.Chunk != 0 {
		return fmt.Sprintf("%s: unknown critical chunk type 0x%02x", KeyUnsupportedAlgorithm, e.Chunk)
	}
	return fmt.Sprintf("%s: unknown critical flags 0x%04x", KeyUnsupportedAlgorithm, e.Flags)
}

func (e *UnsupportedAlgorithmError) Key() string          { return KeyUnsupportedAlgorithm }
func (e *UnsupportedAlgorithmError) Data() map[string]any { return nil }

// preheader is the fixed head of every file.
type preheader struct {
	Version   uint16
	Flags     uint16
	HeaderLen uint32
}

// Looks reports whether the bytes begin with the magic, which is the whole of
// detection: no parsing, no allocation, one comparison.
func Looks(data []byte) bool {
	return len(data) >= len(Magic) && [8]byte(data[:8]) == Magic
}

// readPreheader parses the fixed head and refuses anything this reader cannot
// honour, before a single length is trusted.
func readPreheader(data []byte) (preheader, error) {
	if !Looks(data) {
		return preheader{}, ErrNotGPF
	}
	if len(data) < PreheaderSize {
		return preheader{}, fmt.Errorf("%w: %d bytes is shorter than the preheader", ErrMalformed, len(data))
	}
	head := preheader{
		Version:   binary.LittleEndian.Uint16(data[8:10]),
		Flags:     binary.LittleEndian.Uint16(data[10:12]),
		HeaderLen: binary.LittleEndian.Uint32(data[12:16]),
	}
	if head.Version != Version {
		return preheader{}, &UnsupportedVersionError{Found: head.Version, Known: Version}
	}
	// Critical bits first, before the header is read: fixed binary fields are
	// validated before free-form text, because the order is ours to choose.
	if unknown := head.Flags &^ knownFlags & criticalFlags; unknown != 0 {
		return preheader{}, &UnsupportedAlgorithmError{Flags: unknown}
	}
	if head.Flags&FlagPassphrase != 0 && head.Flags&FlagEncrypted == 0 {
		return preheader{}, fmt.Errorf("%w: a passphrase flag on an unencrypted payload", ErrMalformed)
	}
	if head.HeaderLen > MaxHeader {
		return preheader{}, fmt.Errorf("%w: header claims %d bytes, over the %d cap",
			ErrMalformed, head.HeaderLen, MaxHeader)
	}
	if uint64(PreheaderSize)+uint64(head.HeaderLen) > uint64(len(data)) {
		return preheader{}, fmt.Errorf("%w: header claims %d bytes and %d are left",
			ErrMalformed, head.HeaderLen, len(data)-PreheaderSize)
	}
	return head, nil
}

// writePreheader lays the fixed head down.
func writePreheader(head preheader) []byte {
	out := make([]byte, PreheaderSize)
	copy(out, Magic[:])
	binary.LittleEndian.PutUint16(out[8:10], head.Version)
	binary.LittleEndian.PutUint16(out[10:12], head.Flags)
	binary.LittleEndian.PutUint32(out[12:16], head.HeaderLen)
	return out
}

// chunk is one framed section: a type byte and a length-prefixed body.
// readPayload walks the framed region between the header and the footer.
//
// An unknown type with the critical bit set refuses the whole file. With it
// clear the chunk is skipped using its own length, which is what lets a later
// version add a section this reader has never heard of.
func readPayload(region []byte) ([]byte, error) {
	var payload []byte
	count := 0
	for offset := 0; offset < len(region); {
		if len(region)-offset < 1+4 {
			return nil, fmt.Errorf("%w: %d bytes left is too few for a chunk frame",
				ErrMalformed, len(region)-offset)
		}
		kind := region[offset]
		length := binary.LittleEndian.Uint32(region[offset+1 : offset+5])
		offset += 5

		// Checked against what is actually left, before it sizes anything.
		if uint64(length) > uint64(len(region)-offset) {
			return nil, fmt.Errorf("%w: chunk 0x%02x claims %d bytes and %d are left",
				ErrMalformed, kind, length, len(region)-offset)
		}
		if length > MaxPayload {
			return nil, fmt.Errorf("%w: chunk 0x%02x claims %d bytes, over the %d cap",
				ErrMalformed, kind, length, MaxPayload)
		}
		body := region[offset : offset+int(length)]
		offset += int(length)

		if kind != ChunkPayload {
			if kind&chunkCritical != 0 {
				return nil, &UnsupportedAlgorithmError{Chunk: kind}
			}
			continue
		}
		count++
		if count > 1 {
			return nil, fmt.Errorf("%w: more than one payload", ErrMalformed)
		}
		payload = body
	}
	if count == 0 {
		return nil, fmt.Errorf("%w: no payload", ErrMalformed)
	}
	return payload, nil
}

// writeChunk frames one section.
func writeChunk(kind byte, body []byte) []byte {
	out := make([]byte, 5, 5+len(body))
	out[0] = kind
	binary.LittleEndian.PutUint32(out[1:5], uint32(len(body)))
	return append(out, body...)
}

// writeLocator closes the file: how long the footer was, then the magic again,
// so a reader landing at the end can find its way back.
func writeLocator(footerLen int) []byte {
	out := make([]byte, LocatorSize)
	binary.LittleEndian.PutUint32(out[0:4], uint32(footerLen))
	copy(out[4:], Magic[:])
	return out
}

// readFooterRegion finds the footer from the end of the file and returns it
// with the bytes that precede it.
//
// The locator is the only way in from this end, and it is checked the same way
// as everything else: the magic has to be there and the length has to fit in
// what is actually present.
func readFooterRegion(data []byte, after int) (body []byte, rest []byte, err error) {
	if len(data) < after+LocatorSize {
		return nil, nil, fmt.Errorf("%w: no room for a locator", ErrMalformed)
	}
	locator := data[len(data)-LocatorSize:]
	if [8]byte(locator[4:]) != Magic {
		return nil, nil, fmt.Errorf("%w: the file does not end with the magic", ErrMalformed)
	}
	footerLen := binary.LittleEndian.Uint32(locator[0:4])
	if footerLen > MaxFooter {
		return nil, nil, fmt.Errorf("%w: footer claims %d bytes, over the %d cap",
			ErrMalformed, footerLen, MaxFooter)
	}
	end := len(data) - LocatorSize
	if uint64(footerLen) > uint64(end-after) {
		return nil, nil, fmt.Errorf("%w: footer claims %d bytes and %d are left",
			ErrMalformed, footerLen, end-after)
	}
	start := end - int(footerLen)
	return data[start:end], data[after:start], nil
}

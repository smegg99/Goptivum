// core/optivum/pla/pla.go

// Package pla reads and writes Optivum ".pla" timetable containers.
//
// The container is a thin envelope around the plan XML:
//
//	uint16le magic length | magic string | payload
//
// where the payload is the XML compressed with zlib and then XOR-ed byte by
// byte with a fixed 8-byte key. The XML itself is iso-8859-2 encoded; this
// package hands it back verbatim and leaves transcoding to the caller.
//
// The codec compiles out under the "no_optivum" build tag: Available reports
// false and every entry point returns ErrUnavailable, so dependent code keeps
// building with the feature stripped from the binary.
package pla

import "errors"

// Magic is the container generation this codec writes; Optivum 4.x files
// carry it verbatim.
const Magic = "PlanOptivum400"

// magicPrefix is shared by every generation seen so far and is what
// distinguishes a plan container from an unrelated file.
const magicPrefix = "PlanOptivum"

// key is the fixed XOR key of the format. It is obfuscation, not a secret:
// it keeps casual editors out of the file and nothing more.
var key = [8]byte{0x1A, 0xFA, 0x3B, 0xF8, 0x76, 0x07, 0x15, 0xD5}

var (
	// ErrUnavailable is returned by every entry point in a build made with
	// the "no_optivum" tag.
	ErrUnavailable = errors.New("pla: built without Optivum support")
	// ErrTruncated means the file ends inside its own header.
	ErrTruncated = errors.New("pla: file is shorter than its header claims")
	// ErrUnknownFormat means the magic is not an Optivum plan magic.
	ErrUnknownFormat = errors.New("pla: not an Optivum plan container")
	// ErrMagicTooLong means a caller supplied a magic that does not fit the
	// container's uint16 length field.
	ErrMagicTooLong = errors.New("pla: magic does not fit the header")
)

// File is a decoded container.
type File struct {
	// Magic is the generation string, e.g. "PlanOptivum400".
	Magic string
	// XML is the plan payload, still iso-8859-2 encoded.
	XML []byte
	// Compressed reports whether the payload was zlib-compressed inside the
	// container. Encode reproduces the same choice.
	Compressed bool
}

// mask XORs a payload in place with the format key. The transform is its own
// inverse, so encoding and decoding share it.
func mask(payload []byte) {
	for i := range payload {
		payload[i] ^= key[i%len(key)]
	}
}

// looksZlib applies the RFC 1950 header rule: deflate compression method and
// a header checksum divisible by 31. Optivum writes zlib, but a raw payload
// stays readable rather than erroring out.
func looksZlib(payload []byte) bool {
	if len(payload) < 2 {
		return false
	}
	if payload[0]&0x0F != 8 {
		return false
	}
	return (uint16(payload[0])<<8|uint16(payload[1]))%31 == 0
}

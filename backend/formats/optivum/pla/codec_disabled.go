//go:build no_optivum

// core/optivum/pla/codec_disabled.go

package pla

// Available reports whether this build carries the Optivum codec.
const Available = false

// Decode always fails in a build made without Optivum support.
func Decode(data []byte) (File, error) {
	return File{}, ErrUnavailable
}

// Encode always fails in a build made without Optivum support.
func Encode(file File) ([]byte, error) {
	return nil, ErrUnavailable
}

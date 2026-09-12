//go:build no_optivum

// core/optivum/pla/codec_disabled_test.go

package pla

import (
	"errors"
	"testing"
)

// Built with -tags no_optivum the package still compiles and every entry
// point refuses, so callers can report the missing feature instead of
// failing to build.
func TestDisabledBuildRefuses(t *testing.T) {
	if Available {
		t.Fatal("Available = true under the no_optivum tag")
	}
	if _, err := Decode([]byte{0x0e, 0x00}); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("Decode error = %v, want %v", err, ErrUnavailable)
	}
	if _, err := Encode(File{XML: []byte("<plan/>")}); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("Encode error = %v, want %v", err, ErrUnavailable)
	}
}

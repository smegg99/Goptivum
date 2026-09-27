// document/gpf/panic_test.go

package gpf_test

import (
	"math/rand"
	"testing"

	"github.com/smegg99/goptivum/document/gpf"
)

// No crafted file makes the reader panic.
//
// An error is correct behaviour and a panic is not: this parser is reached by
// an upload, so a panic is a denial of service against a school's server. Two
// hundred thousand mutations of a valid file: single bytes, multi-byte bursts,
// truncations and excisions.
func TestNoMutationPanics(t *testing.T) {
	valid := plainFile(t)
	source := rand.New(rand.NewSource(1))

	for i := range 200000 {
		mutant := append([]byte(nil), valid...)
		switch i % 4 {
		case 0:
			mutant[source.Intn(len(mutant))] = byte(source.Intn(256))
		case 1:
			mutant = mutant[:source.Intn(len(mutant)+1)]
		case 2:
			for range 8 {
				mutant[source.Intn(len(mutant))] = byte(source.Intn(256))
			}
		case 3:
			at := source.Intn(len(mutant))
			mutant = append(mutant[:at], mutant[min(at+source.Intn(64), len(mutant)):]...)
		}
		func() {
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("panic on mutant %d: %v", i, r)
				}
			}()
			_, _ = gpf.Decode(mutant, gpf.ReadOptions{})
		}()
	}
}

// Every truncation of a valid file, exhaustively.
func TestEveryTruncationIsHandled(t *testing.T) {
	valid := plainFile(t)
	for n := range len(valid) + 1 {
		func() {
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("panic at length %d: %v", n, r)
				}
			}()
			_, _ = gpf.Decode(valid[:n], gpf.ReadOptions{})
		}()
	}
}

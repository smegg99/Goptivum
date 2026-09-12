// formats/optivum/pla/corpus_helper_test.go

package pla_test

import (
	"os"
	"testing"
)

func readFile(t *testing.T, path string) []byte {
	t.Helper()

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return data
}

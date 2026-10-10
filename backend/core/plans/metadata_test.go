package plans_test

import (
	"errors"
	"github.com/smegg99/goptivum/backend/core/plans"
	"testing"
)

func TestMetadataCannotBreakGPFHeaders(t *testing.T) {
	for _, character := range []string{"\r", "\n", "\x00"} {
		for _, field := range []string{"name", "school_year"} {
			t.Run(field+"/"+character, func(t *testing.T) {
				name, year := "Plan", "2026/2027"
				if field == "name" {
					name += character
				} else {
					year += character
				}
				_, _, err := plans.Clean(name, year)
				var invalid *plans.Invalid
				if !errors.As(err, &invalid) || invalid.Field != field || invalid.Code != "invalid" {
					t.Fatalf("refusal: %v", err)
				}
			})
		}
	}
	name, year, err := plans.Clean("  Plan Żółty  ", " 2026/2027 ")
	if err != nil || name != "Plan Żółty" || year != "2026/2027" {
		t.Fatalf("valid metadata: %q %q %v", name, year, err)
	}
}

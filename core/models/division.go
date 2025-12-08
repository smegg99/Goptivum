// core/models/division.go
package models

import (
	"fmt"
	"regexp"
)

type Division struct {
	Schedule   Schedule `validate:"required"`
	Name       string   `validate:"required"`
	Designator string   `validate:"required"`
}

func (d *Division) Validate() error {
	if len(d.Name) < 1 {
		return fmt.Errorf("division name must not be empty")
	}

	nameRegex := regexp.MustCompile(`^[0-9]+[a-zA-Z/]+$`)
	if !nameRegex.MatchString(d.Name) {
		return fmt.Errorf("invalid division name format: expected digit(s) followed by letter(s) and optional '/', got %s", d.Name)
	}

	if len(d.Designator) < 1 {
		return fmt.Errorf("division designator must be at least 1 character")
	}

	divisionRegex := regexp.MustCompile(`^[0-9]+[a-zA-Z]+$`)
	if !divisionRegex.MatchString(d.Designator) {
		return fmt.Errorf("invalid division designator format: expected digit(s) followed by letter(s), got %s", d.Designator)
	}

	return nil
}

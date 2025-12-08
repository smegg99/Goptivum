// core/models/teacher.go
package models

import (
	"fmt"
	"regexp"
)

type Teacher struct {
	Schedule   Schedule `validate:"required"`
	Name       string   `validate:"required"`
	Designator string   `validate:"required"`
}

func (t *Teacher) Validate() error {
	if len(t.Name) < 1 {
		return fmt.Errorf("teacher name must be at least 1 character long")
	}

	nameRegex := regexp.MustCompile(`^[A-ZŻŹĆĄŚĘŁÓŃ]\.[A-ZŻŹĆĄŚĘŁÓŃ][a-zżźćńąśęłóń]+$`)
	if !nameRegex.MatchString(t.Name) {
		return fmt.Errorf("invalid teacher name format: expected pattern 'X.Surname', got %s", t.Name)
	}

	if len(t.Designator) < 2 {
		return fmt.Errorf("teacher designator must be at least 2 characters long")
	}

	designatorRegex := regexp.MustCompile(`^[A-ZŻŹĆĄŚĘŁÓŃ]{2,}$`)
	if !designatorRegex.MatchString(t.Designator) {
		return fmt.Errorf("invalid teacher designator format: expected uppercase letters, got %s", t.Designator)
	}

	return nil
}

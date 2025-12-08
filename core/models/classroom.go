// core/models/classroom.go
package models

import (
	"fmt"
	"regexp"
)

type Classroom struct {
	Schedule   Schedule `validate:"required"`
	Name       string   `validate:"required"`
	Designator string   `validate:"required"`
}

func (c *Classroom) Validate() error {
	if len(c.Name) < 1 {
		return fmt.Errorf("classroom name must be at least 1 character")
	}

	classroomRegex := regexp.MustCompile(`^[a-zA-ZżźćńąśęłóńŻŹĆŃĄŚĘŁÓŃ0-9]+$`)
	if !classroomRegex.MatchString(c.Name) {
		return fmt.Errorf("invalid classroom name format: must contain only alphanumeric characters, got %s", c.Name)
	}

	if len(c.Designator) < 1 {
		return fmt.Errorf("classroom designator must be at least 1 character")
	}

	designatorRegex := regexp.MustCompile(`^[a-zA-ZżźćńąśęłóńŻŹĆŃĄŚĘŁÓŃ0-9]+$`)
	if !designatorRegex.MatchString(c.Designator) {
		return fmt.Errorf("invalid classroom designator format: must contain only alphanumeric characters, got %s", c.Designator)
	}

	return nil
}

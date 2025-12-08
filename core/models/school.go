// core/models/school.go
package models

import (
	"fmt"
	"regexp"
)

type TimeRange struct {
	Start string `validate:"required"`
	End   string `validate:"required"`
}

func (tr *TimeRange) Validate() error {
	timeRegex := regexp.MustCompile(`^([0-1]?[0-9]|2[0-3]):[0-5][0-9]$`)

	if !timeRegex.MatchString(tr.Start) {
		return fmt.Errorf("invalid Start time format: expected hh:mm, got %s", tr.Start)
	}
	if !timeRegex.MatchString(tr.End) {
		return fmt.Errorf("invalid End time format: expected hh:mm, got %s", tr.End)
	}

	return nil
}

type School struct {
	Teachers     []Teacher
	Classrooms   []Classroom
	Divisions    []Division
	LessonBlocks []TimeRange
}

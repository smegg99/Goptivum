// core/models/lesson.go
package models

import "fmt"

type GroupType string

const (
	NoGroup GroupType = ""
	Group1  GroupType = "1/3"
	Group2  GroupType = "2/3"
	Group3  GroupType = "3/3"
)

type Lesson struct {
	Name                string `validate:"required"`
	GroupType           GroupType
	TeacherDesignator   string
	DivisionDesignator  string
	ClassroomDesignator string
}

func (l *Lesson) Validate() error {
	if len(l.Name) < 1 {
		return fmt.Errorf("lesson name must be at least 1 character long")
	}

	return nil
}

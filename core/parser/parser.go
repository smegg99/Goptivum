// core/parser/parser.go
package parser

import (
	"io"

	"smegg.me/goptivum/core/parser/classroom"
	"smegg.me/goptivum/core/parser/common"
	"smegg.me/goptivum/core/parser/division"
	"smegg.me/goptivum/core/parser/school"
	"smegg.me/goptivum/core/parser/teacher"
)

type Parser = common.Parser
type TeacherParser = teacher.Parser
type DivisionParser = division.Parser
type ClassroomParser = classroom.Parser
type SchoolParser = school.Parser

func NewTeacherParser() *TeacherParser {
	return teacher.New()
}

func NewDivisionParser() *DivisionParser {
	return division.New()
}

func NewClassroomParser() *ClassroomParser {
	return classroom.New()
}

func NewSchoolParser() *SchoolParser {
	return school.New()
}

var (
	_ Parser = (*TeacherParser)(nil)
	_ Parser = (*DivisionParser)(nil)
	_ Parser = (*ClassroomParser)(nil)
)

var _ interface {
	Parse(io.Reader) error
} = (*TeacherParser)(nil)

// core/parser/validation/uniqueness.go
package validation

import (
	"fmt"

	"smegg.me/goptivum/core/models"
)

type UniquenessResult struct {
	TeacherDesignators   map[string]string
	DivisionDesignators  map[string]string
	ClassroomDesignators map[string]string
	Errors               []string
	Warnings             []string
}

func (r *UniquenessResult) HasErrors() bool {
	return len(r.Errors) > 0
}

func (r *UniquenessResult) HasWarnings() bool {
	return len(r.Warnings) > 0
}

func ValidateTeacherUniqueness(teachers []models.Teacher) *UniquenessResult {
	result := &UniquenessResult{
		TeacherDesignators: make(map[string]string),
	}

	nameMap := make(map[string]string)

	for _, t := range teachers {
		if existing, ok := result.TeacherDesignators[t.Designator]; ok {
			result.Errors = append(result.Errors,
				fmt.Sprintf("Teacher designator %q collision: %q and %q", t.Designator, existing, t.Name))
		} else {
			result.TeacherDesignators[t.Designator] = t.Name
		}

		if existing, ok := nameMap[t.Name]; ok {
			result.Warnings = append(result.Warnings,
				fmt.Sprintf("Teacher name %q has multiple designators: %q and %q", t.Name, existing, t.Designator))
		} else {
			nameMap[t.Name] = t.Designator
		}
	}

	return result
}

func ValidateDivisionUniqueness(divisions []models.Division) *UniquenessResult {
	result := &UniquenessResult{
		DivisionDesignators: make(map[string]string),
	}

	nameMap := make(map[string]string)

	for _, d := range divisions {
		if existing, ok := result.DivisionDesignators[d.Designator]; ok {
			result.Errors = append(result.Errors,
				fmt.Sprintf("Division designator %q collision: %q and %q", d.Designator, existing, d.Name))
		} else {
			result.DivisionDesignators[d.Designator] = d.Name
		}

		if existing, ok := nameMap[d.Name]; ok {
			result.Warnings = append(result.Warnings,
				fmt.Sprintf("Division name %q has multiple designators: %q and %q", d.Name, existing, d.Designator))
		} else {
			nameMap[d.Name] = d.Designator
		}
	}

	return result
}

func ValidateClassroomUniqueness(classrooms []models.Classroom) *UniquenessResult {
	result := &UniquenessResult{
		ClassroomDesignators: make(map[string]string),
	}

	nameMap := make(map[string]string)

	for _, c := range classrooms {
		if existing, ok := result.ClassroomDesignators[c.Designator]; ok {
			result.Errors = append(result.Errors,
				fmt.Sprintf("Classroom designator %q collision: %q and %q", c.Designator, existing, c.Name))
		} else {
			result.ClassroomDesignators[c.Designator] = c.Name
		}

		if existing, ok := nameMap[c.Name]; ok {
			result.Warnings = append(result.Warnings,
				fmt.Sprintf("Classroom name %q has multiple designators: %q and %q", c.Name, existing, c.Designator))
		} else {
			nameMap[c.Name] = c.Designator
		}
	}

	return result
}

func ValidateSchoolUniqueness(school *models.School) *UniquenessResult {
	result := &UniquenessResult{
		TeacherDesignators:   make(map[string]string),
		DivisionDesignators:  make(map[string]string),
		ClassroomDesignators: make(map[string]string),
	}

	teacherResult := ValidateTeacherUniqueness(school.Teachers)
	result.TeacherDesignators = teacherResult.TeacherDesignators
	result.Errors = append(result.Errors, teacherResult.Errors...)
	result.Warnings = append(result.Warnings, teacherResult.Warnings...)

	divisionResult := ValidateDivisionUniqueness(school.Divisions)
	result.DivisionDesignators = divisionResult.DivisionDesignators
	result.Errors = append(result.Errors, divisionResult.Errors...)
	result.Warnings = append(result.Warnings, divisionResult.Warnings...)

	classroomResult := ValidateClassroomUniqueness(school.Classrooms)
	result.ClassroomDesignators = classroomResult.ClassroomDesignators
	result.Errors = append(result.Errors, classroomResult.Errors...)
	result.Warnings = append(result.Warnings, classroomResult.Warnings...)

	return result
}

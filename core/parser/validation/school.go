// core/parser/validation/school.go
package validation

import (
	"smegg.me/goptivum/core/models"
)

type SchoolValidationResult struct {
	EntityValidation *ValidationResult
	UniquenessResult *UniquenessResult
	TeacherCount     int
	DivisionCount    int
	ClassroomCount   int
}

func (r *SchoolValidationResult) HasErrors() bool {
	return r.EntityValidation.HasErrors() || r.UniquenessResult.HasErrors()
}

func (r *SchoolValidationResult) HasWarnings() bool {
	return r.EntityValidation.HasWarnings() || r.UniquenessResult.HasWarnings()
}

func ValidateSchool(school *models.School) *SchoolValidationResult {
	result := &SchoolValidationResult{
		EntityValidation: &ValidationResult{},
		TeacherCount:     len(school.Teachers),
		DivisionCount:    len(school.Divisions),
		ClassroomCount:   len(school.Classrooms),
	}

	for _, t := range school.Teachers {
		if vr := ValidateTeacher(t); vr.HasErrors() {
			result.EntityValidation.Merge(vr)
		}
	}

	for _, d := range school.Divisions {
		if vr := ValidateDivision(d); vr.HasErrors() {
			result.EntityValidation.Merge(vr)
		}
	}

	for _, c := range school.Classrooms {
		if vr := ValidateClassroom(c); vr.HasErrors() {
			result.EntityValidation.Merge(vr)
		}
	}

	result.UniquenessResult = ValidateSchoolUniqueness(school)

	return result
}

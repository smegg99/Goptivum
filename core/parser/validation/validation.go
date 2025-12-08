// core/parser/validation/validation.go
package validation

import (
	"fmt"

	"smegg.me/goptivum/core/models"
)

type ValidationError struct {
	EntityType string
	Field      string
	Message    string
}

func (e ValidationError) Error() string {
	return fmt.Sprintf("%s.%s: %s", e.EntityType, e.Field, e.Message)
}

type ValidationResult struct {
	Errors   []ValidationError
	Warnings []string
}

func (r *ValidationResult) HasErrors() bool {
	return len(r.Errors) > 0
}

func (r *ValidationResult) HasWarnings() bool {
	return len(r.Warnings) > 0
}

func (r *ValidationResult) AddError(entityType, field, message string) {
	r.Errors = append(r.Errors, ValidationError{
		EntityType: entityType,
		Field:      field,
		Message:    message,
	})
}

func (r *ValidationResult) AddWarning(message string) {
	r.Warnings = append(r.Warnings, message)
}

func (r *ValidationResult) Merge(other *ValidationResult) {
	r.Errors = append(r.Errors, other.Errors...)
	r.Warnings = append(r.Warnings, other.Warnings...)
}

func ValidateTeacher(t models.Teacher) *ValidationResult {
	result := &ValidationResult{}

	if t.Name == "" {
		result.AddError("Teacher", "Name", "name cannot be empty")
	}

	if t.Designator == "" {
		result.AddError("Teacher", "Designator", "designator cannot be empty")
	}

	return result
}

func ValidateDivision(d models.Division) *ValidationResult {
	result := &ValidationResult{}

	if d.Name == "" {
		result.AddError("Division", "Name", "name cannot be empty")
	}

	if d.Designator == "" {
		result.AddError("Division", "Designator", "designator cannot be empty")
	}

	return result
}

func ValidateClassroom(c models.Classroom) *ValidationResult {
	result := &ValidationResult{}

	if c.Name == "" {
		result.AddError("Classroom", "Name", "name cannot be empty")
	}

	if c.Designator == "" {
		result.AddError("Classroom", "Designator", "designator cannot be empty")
	}

	return result
}

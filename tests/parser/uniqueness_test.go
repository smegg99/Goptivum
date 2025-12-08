// tests/parser/uniqueness_test.go
package parser_test

import (
	"strings"
	"testing"

	"smegg.me/goptivum/common/logger"
	"smegg.me/goptivum/core/parser"
	"smegg.me/goptivum/core/parser/validation"
)

func TestTeacherUniqueness(t *testing.T) {
	p := parser.NewSchoolParser()
	if err := p.ParseDirectory(resourcesDir); err != nil {
		t.Fatalf("Failed to parse directory: %v", err)
	}

	logger.Infof("%s", strings.Repeat("=", 70))
	logger.Info("TEACHER UNIQUENESS CHECK")
	logger.Infof("%s", strings.Repeat("=", 70))

	result := validation.ValidateTeacherUniqueness(p.School.Teachers)

	for _, err := range result.Errors {
		logger.Errorf("DUPLICATE DESIGNATOR: %s", err)
		t.Errorf("DUPLICATE DESIGNATOR: %s", err)
	}
	for _, warn := range result.Warnings {
		logger.Warnf("Duplicate name (allowed): %s", warn)
	}

	if !result.HasErrors() {
		logger.Infof("All %d teachers have unique designators", len(p.School.Teachers))
	}
	logger.Infof("%s", strings.Repeat("=", 70))
}

func TestDivisionUniqueness(t *testing.T) {
	p := parser.NewSchoolParser()
	if err := p.ParseDirectory(resourcesDir); err != nil {
		t.Fatalf("Failed to parse directory: %v", err)
	}

	logger.Infof("%s", strings.Repeat("=", 70))
	logger.Info("DIVISION UNIQUENESS CHECK")
	logger.Infof("%s", strings.Repeat("=", 70))

	result := validation.ValidateDivisionUniqueness(p.School.Divisions)

	for _, err := range result.Errors {
		logger.Errorf("DUPLICATE DESIGNATOR: %s", err)
		t.Errorf("DUPLICATE DESIGNATOR: %s", err)
	}
	for _, warn := range result.Warnings {
		logger.Warnf("Duplicate name (allowed): %s", warn)
	}

	if !result.HasErrors() {
		logger.Infof("All %d divisions have unique designators", len(p.School.Divisions))
	}
	logger.Infof("%s", strings.Repeat("=", 70))
}

func TestClassroomUniqueness(t *testing.T) {
	p := parser.NewSchoolParser()
	if err := p.ParseDirectory(resourcesDir); err != nil {
		t.Fatalf("Failed to parse directory: %v", err)
	}

	logger.Infof("%s", strings.Repeat("=", 70))
	logger.Info("CLASSROOM UNIQUENESS CHECK")
	logger.Infof("%s", strings.Repeat("=", 70))

	result := validation.ValidateClassroomUniqueness(p.School.Classrooms)

	for _, err := range result.Errors {
		logger.Errorf("DUPLICATE DESIGNATOR: %s", err)
		t.Errorf("DUPLICATE DESIGNATOR: %s", err)
	}
	for _, warn := range result.Warnings {
		logger.Warnf("Duplicate name (allowed): %s", warn)
	}

	if !result.HasErrors() {
		logger.Infof("All %d classrooms have unique designators", len(p.School.Classrooms))
	}
	logger.Infof("%s", strings.Repeat("=", 70))
}

func TestAllUniqueness(t *testing.T) {
	p := parser.NewSchoolParser()
	if err := p.ParseDirectory(resourcesDir); err != nil {
		t.Fatalf("Failed to parse directory: %v", err)
	}

	logger.Infof("%s", strings.Repeat("=", 70))
	logger.Info("COMPREHENSIVE UNIQUENESS REPORT")
	logger.Infof("%s", strings.Repeat("=", 70))

	result := validation.ValidateSchoolUniqueness(p.School)

	// Report warnings first
	if result.HasWarnings() {
		logger.Warnf("Name duplicates found (%d total) - this is allowed:", len(result.Warnings))
		for _, w := range result.Warnings {
			logger.Warnf("  %s", w)
		}
	}

	// Report errors
	if result.HasErrors() {
		logger.Error("DESIGNATOR COLLISIONS FOUND - TEST FAILED")
		for _, e := range result.Errors {
			logger.Errorf("  %s", e)
			t.Errorf("  ERROR: %s", e)
		}
	} else {
		logger.Info("DESIGNATOR UNIQUENESS CHECK PASSED")
		logger.Infof("  Teachers:   %d unique designators", len(result.TeacherDesignators))
		logger.Infof("  Divisions:  %d unique designators", len(result.DivisionDesignators))
		logger.Infof("  Classrooms: %d unique designators", len(result.ClassroomDesignators))
	}

	logger.Infof("%s", strings.Repeat("=", 70))
}

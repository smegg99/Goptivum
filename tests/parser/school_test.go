// tests/parser/school_test.go
package parser_test

import (
	"strings"
	"testing"

	"smegg.me/goptivum/common/logger"
	"smegg.me/goptivum/core/parser"
)

func TestSchoolParser(t *testing.T) {
	p := parser.NewSchoolParser()
	if err := p.ParseDirectory(resourcesDir); err != nil {
		t.Fatalf("Failed to parse directory: %v", err)
	}

	logger.Infof("Parsed %d teachers", len(p.School.Teachers))
	logger.Infof("Parsed %d divisions", len(p.School.Divisions))
	logger.Infof("Parsed %d classrooms", len(p.School.Classrooms))

	if len(p.School.Teachers) == 0 {
		t.Error("No teachers parsed")
	}
	if len(p.School.Divisions) == 0 {
		t.Error("No divisions parsed")
	}
	if len(p.School.Classrooms) == 0 {
		t.Error("No classrooms parsed")
	}
}

func TestParseDirectorySummary(t *testing.T) {
	p := parser.NewSchoolParser()
	if err := p.ParseDirectory(resourcesDir); err != nil {
		t.Fatalf("Failed to parse directory: %v", err)
	}

	logger.Infof("%s", strings.Repeat("=", 70))
	logger.Info("SCHOOL DIRECTORY PARSE SUMMARY")
	logger.Infof("%s", strings.Repeat("=", 70))

	logger.Infof("TEACHERS: %d", len(p.School.Teachers))
	logger.Infof("%s", strings.Repeat("-", 50))
	for i, teacher := range p.School.Teachers {
		logger.Infof("  [%3d] %-30s (%s)", i+1, truncate(teacher.Name, 30), teacher.Designator)
	}

	logger.Infof("DIVISIONS: %d", len(p.School.Divisions))
	logger.Infof("%s", strings.Repeat("-", 50))
	for i, division := range p.School.Divisions {
		logger.Infof("  [%3d] %-35s (%s)", i+1, truncate(division.Name, 35), division.Designator)
	}

	logger.Infof("CLASSROOMS: %d", len(p.School.Classrooms))
	logger.Infof("%s", strings.Repeat("-", 50))
	for i, classroom := range p.School.Classrooms {
		logger.Infof("  [%3d] %-35s (%s)", i+1, truncate(classroom.Name, 35), classroom.Designator)
	}

	logger.Infof("%s", strings.Repeat("=", 70))
	logger.Infof("TOTAL: %d teachers, %d divisions, %d classrooms",
		len(p.School.Teachers), len(p.School.Divisions), len(p.School.Classrooms))
	logger.Infof("%s", strings.Repeat("=", 70))

	if len(p.School.Teachers) != 91 {
		t.Errorf("Expected 91 teachers, got %d", len(p.School.Teachers))
	}
	if len(p.School.Divisions) != 34 {
		t.Errorf("Expected 34 divisions, got %d", len(p.School.Divisions))
	}
	if len(p.School.Classrooms) != 42 {
		t.Errorf("Expected 42 classrooms, got %d", len(p.School.Classrooms))
	}
}

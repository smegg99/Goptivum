// tests/parser/classroom_test.go
package parser_test

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"smegg.me/goptivum/common/logger"
	"smegg.me/goptivum/core/parser"
)

var expectedClassrooms = []struct {
	File       string
	Name       string
	Designator string
}{
	{"s1.html", "4", "4"},
	{"s10.html", "38 Pracownia informatyczna", "38"},
	{"s11.html", "39 Pracownia informatyczna", "39"},
	{"s12.html", "107 Pracownia programistyczna", "107"},
	{"s13.html", "108 Pracownina systemów i sieci internat", "108"},
	{"s14.html", "109 Pracownia informatyczna internat", "109"},
	{"s15.html", "40", "40"},
	{"s16.html", "41", "41"},
	{"s17.html", "42", "42"},
	{"s18.html", "43", "43"},
	{"s19.html", "44", "44"},
	{"s2.html", "8 Prac. telekomunikacyjna Hades", "8"},
	{"s20.html", "45", "45"},
	{"s21.html", "46", "46"},
	{"s22.html", "47", "47"},
	{"s23.html", "48", "48"},
	{"s24.html", "51", "51"},
	{"s25.html", "52", "52"},
	{"s26.html", "16 Świetlica szk.", "16"},
	{"s27.html", "sj1 Sala językowa 1", "sj1"},
	{"s28.html", "sj2 Sala językowa 2", "sj2"},
	{"s29.html", "sj3 Sala językowa 3", "sj3"},
	{"s3.html", "6 Prac. UTK 1 Hades", "6"},
	{"s30.html", "sj4 Sala językowa 4", "sj4"},
	{"s31.html", "sj5 Sala językowa 5", "sj5"},
	{"s32.html", "sj6 Sala językowa 6", "sj6"},
	{"s33.html", "sj7 Sala językowa 7", "sj7"},
	{"s34.html", "sg1 Sala gimnastyczna", "sg1"},
	{"s35.html", "sg2 Sala gimnastyczna", "sg2"},
	{"s36.html", "sg3 Sala gimnastyczna", "sg3"},
	{"s37.html", "sg4 Sala gimnastyczna", "sg4"},
	{"s38.html", "106 Pracownia elektryczna int. 1p.", "106"},
	{"s39.html", "pe3 Pracownia elektryczna tył internatu", "pe3"},
	{"s4.html", "7", "7"},
	{"s40.html", "pe4 Pracownia elektryczna tył internatu", "pe4"},
	{"s41.html", "prautom Pracownia automatyki internat", "prautom"},
	{"s42.html", "SKat Salka Katechetyczna w kościele NSPJ", "SKat"},
	{"s5.html", "11", "11"},
	{"s6.html", "12", "12"},
	{"s7.html", "13", "13"},
	{"s8.html", "14", "14"},
	{"s9.html", "15 Pracownina systemów i sieci", "15"},
}

func TestClassroomParser(t *testing.T) {
	file, err := os.Open(filepath.Join(resourcesDir, "s1.html"))
	if err != nil {
		t.Skipf("Test file not found: %v", err)
	}
	defer file.Close()

	p := parser.NewClassroomParser()
	if err := p.Parse(file); err != nil {
		t.Fatalf("Failed to parse classroom: %v", err)
	}

	if p.Classroom.Name == "" {
		t.Error("Classroom name is empty")
	}
	if p.Classroom.Designator == "" {
		t.Error("Classroom designator is empty")
	}
	logger.Infof("Classroom: %s (%s)", p.Classroom.Name, p.Classroom.Designator)
}

func TestAllClassrooms(t *testing.T) {
	files, err := filepath.Glob(filepath.Join(resourcesDir, "s*.html"))
	if err != nil {
		t.Fatalf("Failed to glob classroom files: %v", err)
	}
	if len(files) == 0 {
		t.Skip("No classroom files found")
	}

	sort.Strings(files)
	logger.Infof("%s", strings.Repeat("=", 70))
	logger.Infof("CLASSROOMS (s*.html) - Total: %d files", len(files))
	logger.Infof("%s", strings.Repeat("=", 70))

	var failed bool
	for _, filePath := range files {
		fileName := filepath.Base(filePath)
		file, err := os.Open(filePath)
		if err != nil {
			logger.Errorf("%-12s | Failed to open: %v", fileName, err)
			failed = true
			continue
		}

		p := parser.NewClassroomParser()
		if err := p.Parse(file); err != nil {
			file.Close()
			logger.Errorf("%-12s | Failed to parse: %v", fileName, err)
			failed = true
			continue
		}
		file.Close()

		if p.Classroom.Name == "" {
			logger.Errorf("%-12s | Classroom name is empty", fileName)
			failed = true
			continue
		}
		if p.Classroom.Designator == "" {
			logger.Errorf("%-12s | Classroom designator is empty", fileName)
			failed = true
			continue
		}

		scheduleCount := countScheduleLessons(p.Classroom.Schedule)
		logger.Infof("%-12s | %-35s | %-8s | %3d lessons",
			fileName, truncate(p.Classroom.Name, 35),
			p.Classroom.Designator, scheduleCount)
	}

	if failed {
		t.Fail()
	}
}

func TestExpectedClassroomValues(t *testing.T) {
	logger.Infof("%s", strings.Repeat("=", 70))
	logger.Info("EXPECTED CLASSROOM VALUES VERIFICATION")
	logger.Infof("%s", strings.Repeat("=", 70))

	var failed bool
	for _, tc := range expectedClassrooms {
		file, err := os.Open(filepath.Join(resourcesDir, tc.File))
		if err != nil {
			logger.Warnf("[SKIP] %-12s | Test file not found: %v", tc.File, err)
			continue
		}

		p := parser.NewClassroomParser()
		if err := p.Parse(file); err != nil {
			file.Close()
			logger.Errorf("%-12s | Parse failed: %v", tc.File, err)
			failed = true
			continue
		}
		file.Close()

		if p.Classroom.Name != tc.Name {
			logger.Errorf("%-12s | Name: got %q, want %q", tc.File, p.Classroom.Name, tc.Name)
			failed = true
		}
		if p.Classroom.Designator != tc.Designator {
			logger.Errorf("%-12s | Designator: got %q, want %q", tc.File, p.Classroom.Designator, tc.Designator)
			failed = true
		}

		if p.Classroom.Name == tc.Name && p.Classroom.Designator == tc.Designator {
			logger.Infof("%-12s | Name: %-35s | Designator: %s", tc.File, tc.Name, tc.Designator)
		}
	}

	if failed {
		t.Fail()
	}
}

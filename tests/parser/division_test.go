// tests/parser/division_test.go
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

var expectedDivisions = []struct {
	File       string
	Name       string
	Designator string
}{
	{"o1.html", "5fgT 5elektryk/elektronik", "5fgT"},
	{"o10.html", "4iT 4informatyk", "4iT"},
	{"o11.html", "4mT 4mechatronik", "4mT"},
	{"o12.html", "4tT 4teleinformatyk", "4tT"},
	{"o13.html", "4eT 4mechatronik", "4eT"},
	{"o14.html", "4jT 4informatyk", "4jT"},
	{"o15.html", "4gT 4elektronik", "4gT"},
	{"o16.html", "3d 3programista", "3d"},
	{"o17.html", "3p 3programista", "3p"},
	{"o18.html", "3m 3mechatronik", "3m"},
	{"o19.html", "3i 3informatyk", "3i"},
	{"o2.html", "5mT 5mechatronik", "5mT"},
	{"o20.html", "3g 3elektronik", "3g"},
	{"o21.html", "3f 3elektryk", "3f"},
	{"o22.html", "31t 3teleinformatyk", "31t"},
	{"o23.html", "2p 2programista", "2p"},
	{"o24.html", "2d 2programista", "2d"},
	{"o25.html", "2i 2informatyk", "2i"},
	{"o26.html", "2m 2mechatronik", "2m"},
	{"o27.html", "2f 2elektryk", "2f"},
	{"o28.html", "1p 1programista", "1p"},
	{"o29.html", "1i 1informatyk", "1i"},
	{"o3.html", "5iT 5informatyk", "5iT"},
	{"o30.html", "1m 1mechatronik", "1m"},
	{"o31.html", "1f 1elektryk", "1f"},
	{"o32.html", "1g 1elektronik", "1g"},
	{"o33.html", "1t 1teleinformatyk", "1t"},
	{"o34.html", "1e 1mechatronik", "1e"},
	{"o4.html", "5pT 5programista", "5pT"},
	{"o5.html", "5tT 5teleinformatyk", "5tT"},
	{"o6.html", "5dT 5programista", "5dT"},
	{"o7.html", "4fT 4elektryk", "4fT"},
	{"o8.html", "4dT 4programista", "4dT"},
	{"o9.html", "4pT 4programista", "4pT"},
}

func TestDivisionParser(t *testing.T) {
	file, err := os.Open(filepath.Join(resourcesDir, "o1.html"))
	if err != nil {
		t.Skipf("Test file not found: %v", err)
	}
	defer file.Close()

	p := parser.NewDivisionParser()
	if err := p.Parse(file); err != nil {
		t.Fatalf("Failed to parse division: %v", err)
	}

	if p.Division.Name == "" {
		t.Error("Division name is empty")
	}
	if p.Division.Designator == "" {
		t.Error("Division designator is empty")
	}
	logger.Infof("Division: %s (%s)", p.Division.Name, p.Division.Designator)
}

func TestAllDivisions(t *testing.T) {
	files, err := filepath.Glob(filepath.Join(resourcesDir, "o*.html"))
	if err != nil {
		t.Fatalf("Failed to glob division files: %v", err)
	}
	if len(files) == 0 {
		t.Skip("No division files found")
	}

	sort.Strings(files)
	logger.Infof("%s", strings.Repeat("=", 70))
	logger.Infof("DIVISIONS (o*.html) - Total: %d files", len(files))
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

		p := parser.NewDivisionParser()
		if err := p.Parse(file); err != nil {
			file.Close()
			logger.Errorf("%-12s | Failed to parse: %v", fileName, err)
			failed = true
			continue
		}
		file.Close()

		if p.Division.Name == "" {
			logger.Errorf("%-12s | Division name is empty", fileName)
			failed = true
			continue
		}
		if p.Division.Designator == "" {
			logger.Errorf("%-12s | Division designator is empty", fileName)
			failed = true
			continue
		}

		scheduleCount := countScheduleLessons(p.Division.Schedule)
		logger.Infof("%-12s | %-30s | %-6s | %3d lessons",
			fileName, truncate(p.Division.Name, 30),
			p.Division.Designator, scheduleCount)
	}

	if failed {
		t.Fail()
	}
}

func TestExpectedDivisionValues(t *testing.T) {
	logger.Infof("%s", strings.Repeat("=", 70))
	logger.Info("EXPECTED DIVISION VALUES VERIFICATION")
	logger.Infof("%s", strings.Repeat("=", 70))

	var failed bool
	for _, tc := range expectedDivisions {
		file, err := os.Open(filepath.Join(resourcesDir, tc.File))
		if err != nil {
			logger.Warnf("[SKIP] %-12s | Test file not found: %v", tc.File, err)
			continue
		}

		p := parser.NewDivisionParser()
		if err := p.Parse(file); err != nil {
			file.Close()
			logger.Errorf("%-12s | Parse failed: %v", tc.File, err)
			failed = true
			continue
		}
		file.Close()

		if p.Division.Name != tc.Name {
			logger.Errorf("%-12s | Name: got %q, want %q", tc.File, p.Division.Name, tc.Name)
			failed = true
		}
		if p.Division.Designator != tc.Designator {
			logger.Errorf("%-12s | Designator: got %q, want %q", tc.File, p.Division.Designator, tc.Designator)
			failed = true
		}

		if p.Division.Name == tc.Name && p.Division.Designator == tc.Designator {
			logger.Infof("%-12s | Name: %-30s | Designator: %s", tc.File, tc.Name, tc.Designator)
		}
	}

	if failed {
		t.Fail()
	}
}

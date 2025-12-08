// tests/parser/teacher_test.go
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

var expectedTeachers = []struct {
	File       string
	Name       string
	Designator string
}{
	{"n1.html", "W.Szarek", "Sz"},
	{"n10.html", "M.Bochniarz", "Bm"},
	{"n11.html", "M.Bochniarz", "Bo"},
	{"n12.html", "K.Brdej", "Br"},
	{"n13.html", "Ł.Budnik", "Bu"},
	{"n14.html", "A.Chronowska", "Cg"},
	{"n15.html", "R.Ciastoń", "RC"},
	{"n16.html", "E.Czernecka", "EC"},
	{"n17.html", "U.Dara", "Da"},
	{"n18.html", "G.Durałek", "Dg"},
	{"n19.html", "Z.Durlak", "ZD"},
	{"n2.html", "E.Dziedzic", "De"},
	{"n20.html", "D.Dyrek", "Dd"},
	{"n21.html", "K.Dzido", "SC"},
	{"n22.html", "W.Gałach", "WG"},
	{"n23.html", "A.Gołaszewski", "Gx"},
	{"n24.html", "B.Górska", "GÓ"},
	{"n25.html", "M.Górska", "GS"},
	{"n26.html", "R.Gruca", "gr"},
	{"n27.html", "M.Grzegorczyk", "Gm"},
	{"n28.html", "S.Izworski", "iz"},
	{"n29.html", "K.Janusz", "Kj"},
	{"n3.html", "B.Matuła-Stępień", "Mb"},
	{"n30.html", "K.Jaworski", "Jk"},
	{"n31.html", "Ł.Jurczak", "Łj"},
	{"n32.html", "S.Kadyszewska", "Sy"},
	{"n33.html", "E.Kajder", "Ek"},
	{"n34.html", "K.Kamińska", "KŃ"},
	{"n35.html", "G.Kantor", "Kg"},
	{"n36.html", "J.Klag-Pierzchała", "JP"},
	{"n37.html", "R.Kruk", "Kr"},
	{"n38.html", "Ł.Kucharski", "Kł"},
	{"n39.html", "D.Kulig", "DK"},
	{"n4.html", "Z.Zelek", "zz"},
	{"n40.html", "M.Kwiatek-Poręba", "KE"},
	{"n41.html", "T.Liber", "tl"},
	{"n42.html", "M.Lorek", "ML"},
	{"n43.html", "K.Lupa", "kl"},
	{"n44.html", "K.Maj", "Kv"},
	{"n45.html", "T.Mąka", "MĄ"},
	{"n46.html", "H.Małysa-Legutko", "HM"},
	{"n47.html", "J.Michalik", "jm"},
	{"n48.html", "K.Michalik", "Kc"},
	{"n49.html", "J.Michałowska", "MH"},
	{"n5.html", "B.Kapturkiewicz", "BK"},
	{"n50.html", "M.Mikulski", "MU"},
	{"n51.html", "K.Mirek", "KM"},
	{"n52.html", "P.Mordarska", "Mp"},
	{"n53.html", "J.Mularczyk", "MZ"},
	{"n54.html", "J.Nalepa", "Na"},
	{"n55.html", "A.Niemczak", "Nm"},
	{"n56.html", "E.Nowak", "Ne"},
	{"n57.html", "P.Obrzut", "PO"},
	{"n58.html", "v.PPp", "va"},
	{"n59.html", "P.Piszczek", "PP"},
	{"n6.html", "K.Adamek", "Kd"},
	{"n60.html", "M.Popiela", "EP"},
	{"n61.html", "D.Pres", "DP"},
	{"n62.html", "M.Roman", "Ro"},
	{"n63.html", "D.Rosiek-Ogorzałek", "DR"},
	{"n64.html", "D.Sejut-Kocemba", "DS"},
	{"n65.html", "A.Sekuła", "Su"},
	{"n66.html", "M.Siciarz", "Si"},
	{"n67.html", "I.Sroka", "IS"},
	{"n68.html", "I.Stelmach", "Se"},
	{"n69.html", "K.Stopka", "Ks"},
	{"n7.html", "M.Adamek", "md"},
	{"n70.html", "M.Świerczek", "ŚW"},
	{"n71.html", "A.Święs", "AŚ"},
	{"n72.html", "S.Szafraniec", "Sr"},
	{"n73.html", "P.Szczypuła", "Sp"},
	{"n74.html", "J.Talar", "Tj"},
	{"n75.html", "K.Tekieli", "Tk"},
	{"n76.html", "M.Trybuch", "Mt"},
	{"n77.html", "M.Tutka", "Tm"},
	{"n78.html", "J.Wąsowicz", "JW"},
	{"n79.html", "M.Węglarz", "Wm"},
	{"n8.html", "A.Baran", "Ba"},
	{"n80.html", "B.Wideł", "Bw"},
	{"n81.html", "L.Wilczak", "LW"},
	{"n82.html", "M.Wójcik", "Mw"},
	{"n83.html", "K.Wojnarowski", "WJ"},
	{"n84.html", "A.Wójs", "Aw"},
	{"n85.html", "L.Wontorczyk", "Wo"},
	{"n86.html", "S.Wysowska", "Sw"},
	{"n87.html", "K.Ząbkowski", "Zk"},
	{"n88.html", "T.Zawiślan", "Za"},
	{"n89.html", "v.angielski", "AG"},
	{"n9.html", "E.Bereś", "Be"},
	{"n90.html", "v.niemiecki", "VN"},
	{"n91.html", "v.Doradztwo", "VD"},
}

func TestTeacherParser(t *testing.T) {
	file, err := os.Open(filepath.Join(resourcesDir, "n1.html"))
	if err != nil {
		t.Skipf("Test file not found: %v", err)
	}
	defer file.Close()

	p := parser.NewTeacherParser()
	if err := p.Parse(file); err != nil {
		t.Fatalf("Failed to parse teacher: %v", err)
	}

	if p.Teacher.Name == "" {
		t.Error("Teacher name is empty")
	}
	if p.Teacher.Designator == "" {
		t.Error("Teacher designator is empty")
	}
	logger.Infof("Teacher: %s (%s)", p.Teacher.Name, p.Teacher.Designator)
}

func TestAllTeachers(t *testing.T) {
	files, err := filepath.Glob(filepath.Join(resourcesDir, "n*.html"))
	if err != nil {
		t.Fatalf("Failed to glob teacher files: %v", err)
	}
	if len(files) == 0 {
		t.Skip("No teacher files found")
	}

	sort.Strings(files)
	logger.Infof("%s", strings.Repeat("=", 70))
	logger.Infof("TEACHERS (n*.html) - Total: %d files", len(files))
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

		p := parser.NewTeacherParser()
		if err := p.Parse(file); err != nil {
			file.Close()
			logger.Errorf("%-12s | Failed to parse: %v", fileName, err)
			failed = true
			continue
		}
		file.Close()

		if p.Teacher.Name == "" {
			logger.Errorf("%-12s | Teacher name is empty", fileName)
			failed = true
			continue
		}
		if p.Teacher.Designator == "" {
			logger.Errorf("%-12s | Teacher designator is empty", fileName)
			failed = true
			continue
		}

		scheduleCount := countScheduleLessons(p.Teacher.Schedule)
		logger.Infof("%-12s | %-25s | %-6s | %3d lessons",
			fileName, truncate(p.Teacher.Name, 25),
			p.Teacher.Designator, scheduleCount)
	}

	if failed {
		t.Fail()
	}
}

func TestExpectedTeacherValues(t *testing.T) {
	logger.Infof("%s", strings.Repeat("=", 70))
	logger.Info("EXPECTED TEACHER VALUES VERIFICATION")
	logger.Infof("%s", strings.Repeat("=", 70))

	var failed bool
	for _, tc := range expectedTeachers {
		file, err := os.Open(filepath.Join(resourcesDir, tc.File))
		if err != nil {
			logger.Warnf("[SKIP] %-12s | Test file not found: %v", tc.File, err)
			continue
		}

		p := parser.NewTeacherParser()
		if err := p.Parse(file); err != nil {
			file.Close()
			logger.Errorf("%-12s | Parse failed: %v", tc.File, err)
			failed = true
			continue
		}
		file.Close()

		if p.Teacher.Name != tc.Name {
			logger.Errorf("%-12s | Name: got %q, want %q", tc.File, p.Teacher.Name, tc.Name)
			failed = true
		}
		if p.Teacher.Designator != tc.Designator {
			logger.Errorf("%-12s | Designator: got %q, want %q", tc.File, p.Teacher.Designator, tc.Designator)
			failed = true
		}

		if p.Teacher.Name == tc.Name && p.Teacher.Designator == tc.Designator {
			logger.Infof("%-12s | Name: %-25s | Designator: %s", tc.File, tc.Name, tc.Designator)
		}
	}

	if failed {
		t.Fail()
	}
}

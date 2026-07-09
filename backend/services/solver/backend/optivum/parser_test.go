// optivum/parser_test.go

package optivum

import (
	"archive/zip"
	"bytes"
	"os"
	"regexp"
	"testing"
	"testing/fstest"

	arrangov1 "github.com/smegg99/arrango/backend/gen/arrangov1"
	"google.golang.org/protobuf/proto"
)

const sampleDir = "../../data/whole optivum schedule"

// firstParticipant returns the (divisionId, groupId) of a lesson's first
// participant, or (0, 0) when it has none.
func firstParticipant(l *arrangov1.LessonInstance) (uint32, uint32) {
	if len(l.GetParticipants()) == 0 {
		return 0, 0
	}
	p := l.GetParticipants()[0]
	return p.GetDivisionId(), p.GetGroupId()
}

func parseSample(t *testing.T) *Result {
	t.Helper()
	if _, err := os.Stat(sampleDir); err != nil {
		t.Skipf("sample export not present: %v", err)
	}
	result, err := ParseFS(os.DirFS(sampleDir))
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func TestParsesRealExportCounts(t *testing.T) {
	r := parseSample(t)
	if r.Summary.Divisions != 34 {
		t.Errorf("divisions = %d, want 34", r.Summary.Divisions)
	}
	if r.Summary.Teachers != 91 {
		t.Errorf("teachers = %d, want 91", r.Summary.Teachers)
	}
	if r.Summary.Rooms != 42 {
		t.Errorf("rooms = %d, want 42", r.Summary.Rooms)
	}
	if r.Summary.Days != 5 {
		t.Errorf("days = %d, want 5", r.Summary.Days)
	}
	if r.Summary.Periods < 15 {
		t.Errorf("periods = %d, want >= 15", r.Summary.Periods)
	}
	if r.Summary.Lessons == 0 || r.Summary.Subjects == 0 || r.Summary.Groups == 0 {
		t.Errorf("empty lessons/subjects/groups: %+v", r.Summary)
	}
	if len(r.Snapshot.Lessons) != len(r.Model.Lessons) {
		t.Errorf("snapshot %d != lessons %d",
			len(r.Snapshot.Lessons), len(r.Model.Lessons))
	}
}

// o1.html: 5fgT Monday periods 1-2 = j.polski, teacher Kr (n37), room 46
// (s21) — must import as one duration-2 instance starting at period 0.
func TestKnownLessonMergedWithPlacement(t *testing.T) {
	r := parseSample(t)
	var division *arrangov1.Division
	for _, d := range r.Model.Divisions {
		if d.Name == "5fgT" {
			division = d
		}
	}
	if division == nil {
		t.Fatal("division 5fgT not found")
	}
	subjects := map[uint32]string{}
	for _, s := range r.Model.Subjects {
		subjects[s.Id] = s.Name
	}
	teachers := map[uint32]string{}
	for _, teacher := range r.Model.Teachers {
		teachers[teacher.Id] = teacher.Name
	}
	rooms := map[uint32]string{}
	for _, room := range r.Model.Rooms {
		rooms[room.Id] = room.Name
	}
	monday := r.Model.Days[0].Id

	found := false
	for _, l := range r.Model.Lessons {
		divID, _ := firstParticipant(l)
		if divID != division.Id || subjects[l.SubjectId] != "j.polski" {
			continue
		}
		p := l.PreviousPlacement
		if p.DayId == monday && p.StartPeriod == 0 {
			found = true
			if l.Duration != 2 {
				t.Errorf("duration = %d, want 2 (merged block)", l.Duration)
			}
			if got := teachers[l.TeacherId]; !bytes.Contains([]byte(got), []byte("(Kr)")) {
				t.Errorf("teacher = %q, want name containing (Kr)", got)
			}
			if got := rooms[p.RoomId]; got != "46" {
				t.Errorf("room = %q, want 46", got)
			}
			if !l.RequiresTeacher || !l.RequiresRoom || !l.HasPrevious {
				t.Errorf("flags wrong: %+v", l)
			}
			// Room eligibility stays unrestricted on import: the imported
			// room lives only in previous_placement, never as a pin.
			if len(l.AllowedRoomDesignators) != 0 {
				t.Errorf("allowed designators = %v, want empty (unrestricted)",
					l.AllowedRoomDesignators)
			}
		}
	}
	if !found {
		t.Fatal("5fgT j.polski Monday period 0 not found")
	}
}

// Every imported room carries a canonical designator and its s* source stem.
func TestDesignatorsAndSourceRefs(t *testing.T) {
	r := parseSample(t)
	stemRe := regexp.MustCompile(`^s\d+$`)
	for _, room := range r.Model.Rooms {
		if room.Designator == "" {
			t.Errorf("room %q has empty designator", room.Name)
		}
		if !stemRe.MatchString(room.SourceRef) {
			t.Errorf("room %q source_ref = %q, want s<number>",
				room.Name, room.SourceRef)
		}
	}
}

// Every x/N group joins ONE open Split named "N" per division; named groups
// stay split-less (implicit private open split in the solver). This mirrors
// the old partition-string semantics as first-class structure.
func TestSplitsMirrorGroupSuffixes(t *testing.T) {
	r := parseSample(t)
	splitByID := map[uint32]*arrangov1.Split{}
	for _, s := range r.Model.Splits {
		if s.Kind != arrangov1.SplitKind_SPLIT_KIND_OPEN {
			t.Errorf("split %q imported as non-open", s.Name)
		}
		splitByID[s.Id] = s
	}
	xOfN := regexp.MustCompile(`^\d+/(\d+)$`)
	// division id -> label -> split id (must be unique per pair)
	seen := map[uint32]map[string]uint32{}
	for _, g := range r.Model.Groups {
		match := xOfN.FindStringSubmatch(g.Name)
		if match == nil {
			if g.SplitId != 0 {
				t.Errorf("named group %q got split %d, want none", g.Name, g.SplitId)
			}
			continue
		}
		label := match[1]
		if g.SplitId == 0 {
			t.Errorf("x/N group %q has no split", g.Name)
			continue
		}
		s, ok := splitByID[g.SplitId]
		if !ok {
			t.Errorf("group %q references unknown split %d", g.Name, g.SplitId)
			continue
		}
		if s.DivisionId != g.DivisionId {
			t.Errorf("group %q references split of another division", g.Name)
		}
		if s.Name != label {
			t.Errorf("group %q split named %q, want %q", g.Name, s.Name, label)
		}
		if seen[g.DivisionId] == nil {
			seen[g.DivisionId] = map[string]uint32{}
		}
		if prev, ok := seen[g.DivisionId][label]; ok && prev != g.SplitId {
			t.Errorf("division %d label %q maps to two splits", g.DivisionId, label)
		}
		seen[g.DivisionId][label] = g.SplitId
	}
	if len(seen) == 0 {
		t.Error("expected at least one division with split groups")
	}
}

func TestGroupsAndReferencesResolve(t *testing.T) {
	r := parseSample(t)
	groupNames := map[string]bool{}
	groupIDs := map[uint32]bool{}
	for _, g := range r.Model.Groups {
		groupNames[g.Name] = true
		groupIDs[g.Id] = true
	}
	if !groupNames["1/3"] {
		t.Error("expected a 1/3 split group")
	}
	divisions := map[uint32]bool{}
	for _, d := range r.Model.Divisions {
		divisions[d.Id] = true
	}
	subjects := map[uint32]bool{}
	for _, s := range r.Model.Subjects {
		subjects[s.Id] = true
	}
	teachers := map[uint32]bool{}
	for _, teacher := range r.Model.Teachers {
		teachers[teacher.Id] = true
	}
	rooms := map[uint32]bool{}
	for _, room := range r.Model.Rooms {
		rooms[room.Id] = true
	}
	for _, l := range r.Model.Lessons {
		divID, groupID := firstParticipant(l)
		if !divisions[divID] || !subjects[l.SubjectId] {
			t.Fatalf("lesson %d: dangling division/subject", l.Id)
		}
		if groupID != 0 && !groupIDs[groupID] {
			t.Fatalf("lesson %d: dangling group", l.Id)
		}
		if l.RequiresTeacher && !teachers[l.TeacherId] {
			t.Fatalf("lesson %d: dangling teacher", l.Id)
		}
		if l.RequiresRoom && !rooms[l.PreviousPlacement.RoomId] {
			t.Fatalf("lesson %d: dangling room", l.Id)
		}
		if !l.HasPrevious || l.PreviousPlacement == nil {
			t.Fatalf("lesson %d: missing previous placement", l.Id)
		}
	}
}

func TestZipRoundTripEqualsDirParse(t *testing.T) {
	if _, err := os.Stat(sampleDir); err != nil {
		t.Skip("sample export not present")
	}
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	entries, err := os.ReadDir(sampleDir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		data, err := os.ReadFile(sampleDir + "/" + e.Name())
		if err != nil {
			t.Fatal(err)
		}
		f, err := w.Create("plany/" + e.Name())
		if err != nil {
			t.Fatal(err)
		}
		if _, err := f.Write(data); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}

	fromZip, err := ParseZip(buf.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	fromDir := parseSample(t)
	if !proto.Equal(fromZip.Model, fromDir.Model) {
		t.Error("zip parse differs from dir parse")
	}
}

func TestBrokenPagesYieldWarningsNotErrors(t *testing.T) {
	fsys := fstest.MapFS{
		"o1.html": &fstest.MapFile{Data: []byte(`<html><body>
<span class="tytulnapis">1a</span>
<table class="tabela">
<tr><th>Nr</th><th>Godz</th><th>Pon</th></tr>
<tr><td class="nr">abc</td><td class="g">x</td><td class="l">&nbsp;</td></tr>
<tr><td class="nr">1</td><td class="g">7:00</td>
<td class="l"><span class="p">mat</span> <a href="junk" class="n">XX</a></td></tr>
</table></body></html>`)},
	}
	r, err := ParseFS(fsys)
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Warnings) == 0 {
		t.Error("expected warnings for malformed content")
	}
	if r.Summary.Lessons != 1 {
		t.Errorf("lessons = %d, want 1 (mat without valid teacher link)",
			r.Summary.Lessons)
	}
	if r.Model.Lessons[0].RequiresTeacher {
		t.Error("teacher link was junk; lesson must not require a teacher")
	}
}

func TestNoDivisionPagesIsFatal(t *testing.T) {
	if _, err := ParseFS(fstest.MapFS{"readme.txt": &fstest.MapFile{Data: []byte("hi")}}); err == nil {
		t.Fatal("expected error for archive without division pages")
	}
}

// formats/optivum/xml/build_test.go

package xml_test

import (
	"archive/zip"
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	arrangov1 "github.com/smegg99/arrango/backend/gen/arrangov1"
	"golang.org/x/text/encoding/charmap"
	"google.golang.org/protobuf/encoding/prototext"
	"google.golang.org/protobuf/proto"

	"github.com/smegg99/goptivum/backend/formats"
	"github.com/smegg99/goptivum/backend/formats/optivum/pla"
	planxml "github.com/smegg99/goptivum/backend/formats/optivum/xml"
	"github.com/smegg99/goptivum/backend/internal/testsupport"
)

// container wraps fixture XML the way Optivum does: iso-8859-2 bytes inside a
// zlib-compressed container. The Polish letters are the point of the exercise,
// because a reader that transcodes before parsing gets them wrong.
func container(t *testing.T, body string) []byte {
	t.Helper()

	latin2, err := charmap.ISO8859_2.NewEncoder().Bytes([]byte(body))
	if err != nil {
		t.Fatalf("encode fixture as iso-8859-2: %v", err)
	}
	data, err := pla.Encode(pla.File{Magic: pla.Magic, XML: latin2, Compressed: true})
	if err != nil {
		t.Fatalf("wrap fixture in a container: %v", err)
	}
	return data
}

// importDocument reads an in-source document, encoded the way a real file is.
func importDocument(t *testing.T, body string) *formats.Result {
	t.Helper()
	return importSource(t, formats.Open("fixture.pla", container(t, body)))
}

// importFixture reads a committed fixture. The fixtures declare utf-8 and are
// stored as utf-8 so that a diff of them is readable; the iso-8859-2 path a
// real file takes has its own document in this file.
func importFixture(t *testing.T, name string) *formats.Result {
	t.Helper()

	body, err := os.ReadFile(testsupport.Fixture(t, filepath.Join("optivum", name)))
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	data, err := pla.Encode(pla.File{Magic: pla.Magic, XML: body, Compressed: true})
	if err != nil {
		t.Fatalf("wrap fixture %s: %v", name, err)
	}
	return importSource(t, formats.Open(name, data))
}

func importSource(t *testing.T, source formats.Source) *formats.Result {
	t.Helper()

	result, err := planxml.New().Import(context.Background(), source)
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	return result
}

func TestReadsSmallPlan(t *testing.T) {
	result := importFixture(t, "plan-small.xml")
	checkModel(t, result)

	model := result.Model
	if result.HasErrors() {
		for _, finding := range result.Findings {
			if finding.Severity == formats.Error {
				t.Errorf("unexpected error finding %s: %s", finding.Kind, finding.Detail)
			}
		}
	}
	if model.GetName() != "Szkoła Testowa" {
		t.Errorf("school name = %q", model.GetName())
	}

	if got := len(model.GetDays()); got != 2 {
		t.Fatalf("days = %d, want 2", got)
	}
	if got := model.GetDays()[0].GetName(); got != "Poniedziałek" {
		t.Errorf("day 0 name = %q, want the iso-8859-2 spelling", got)
	}
	// The window a division marks bounds the day: three periods on Monday,
	// three on Tuesday because the second class is taught all three.
	for i, want := range []uint32{3, 3} {
		if got := model.GetDays()[i].GetPeriodCount(); got != want {
			t.Errorf("day %d period_count = %d, want %d", i, got, want)
		}
	}
	if got := model.GetPeriods()[0].GetName(); got != "8:00-8:45" {
		t.Errorf("period 0 name = %q, want the bell times trimmed", got)
	}

	if got := len(model.GetYears()); got != 2 {
		t.Errorf("years = %d, want one per level", got)
	}
	if got := model.GetYears()[0]; got.GetLevel() != 1 || got.GetName() != "Rok 1" {
		t.Errorf("year = %+v, want level 1 taken from the division code", got)
	}

	division := divisionNamed(t, model, "1A")
	if division.GetStudentCount() != 26 ||
		division.GetCountSource() != arrangov1.CountSource_COUNT_SOURCE_IMPORTED {
		t.Errorf("division 1A: students = %d source = %v, want 26 imported",
			division.GetStudentCount(), division.GetCountSource())
	}
	if want := "optivum.pla:oddzial:1A"; division.GetSourceRef() != want {
		t.Errorf("division 1A source_ref = %q, want %q", division.GetSourceRef(), want)
	}

	kinds := map[string]arrangov1.SplitKind{}
	for _, split := range model.GetSplits() {
		kinds[split.GetName()] = split.GetKind()
	}
	if kinds["Podział wg płci"] != arrangov1.SplitKind_SPLIT_KIND_FIXED {
		t.Errorf("a split by gender is a fact about a student, so it must be fixed: %v",
			kinds["Podział wg płci"])
	}
	if kinds["Podział na dwie grupy"] != arrangov1.SplitKind_SPLIT_KIND_OPEN {
		t.Errorf("a numbered split is a plan choice, so it must be open: %v",
			kinds["Podział na dwie grupy"])
	}

	if got := len(model.GetGroups()); got != 4 {
		t.Errorf("groups = %d, want 4", got)
	}
	group := groupNamed(t, model, "1/2")
	if group.GetStudentCount() != 13 ||
		group.GetCountSource() != arrangov1.CountSource_COUNT_SOURCE_IMPORTED {
		t.Errorf("group 1/2: students = %d source = %v", group.GetStudentCount(), group.GetCountSource())
	}

	teacher := teacherNamed(t, model, "Łukasz Żółć")
	if want := []string{"101"}; !equal(teacher.GetPreferredRoomDesignators(), want) {
		t.Errorf("teacher rooms = %v, want %v", teacher.GetPreferredRoomDesignators(), want)
	}
	if _, ok := findTeacher(model, "Zofia Śliwa"); !ok {
		t.Error("a surname with Polish letters did not survive the charset")
	}

	rooms := map[string]*arrangov1.Room{}
	for _, room := range model.GetRooms() {
		rooms[room.GetDesignator()] = room
	}
	if got := rooms["102"].GetName(); got != "102" {
		t.Errorf("room 102 name = %q, want the code as a fallback", got)
	}
	if got := rooms["103"]; got.GetCapacitySource() != arrangov1.CountSource_COUNT_SOURCE_UNKNOWN {
		t.Errorf("room 103 capacity source = %v, want unknown for a zero capacity", got.GetCapacitySource())
	}
	if got := rooms["103"].GetBuildingId(); got != 0 {
		t.Errorf("room 103 building = %d, want 0 for a building the file never defines", got)
	}

	subjects := map[string]*arrangov1.Subject{}
	for _, subject := range model.GetSubjects() {
		subjects[subject.GetName()] = subject
	}
	if !subjects["matematyka"].GetPrefersBlocks() {
		t.Error("a subject taught in a two-period block prefers blocks")
	}
	if subjects["język angielski"].GetPrefersBlocks() {
		t.Error("a subject never taught in a block does not prefer them")
	}
	if _, ok := subjects["wf"]; !ok {
		t.Error("a subject without a name keeps its code")
	}

	// Seven rows, one of which is the second period of a block.
	if got := len(model.GetLessons()); got != 6 {
		t.Fatalf("lessons = %d, want 6: a block is one lesson, not two", got)
	}
	block := lessonOf(t, model, "matematyka", "1A")
	if block.GetDuration() != 2 {
		t.Errorf("block duration = %d, want 2", block.GetDuration())
	}
	if !block.GetHasPrevious() || block.GetPreviousPlacement().GetStartPeriod() != 0 {
		t.Errorf("block placement = %v", block.GetPreviousPlacement())
	}
	if got := block.GetAllowedRoomDesignators(); !equal(got, []string{"101"}) {
		t.Errorf("block tier 1 = %v, want the first listed room", got)
	}
	if got := block.GetAllowedRoomDesignatorsTier2(); !equal(got, []string{"102", "103"}) {
		t.Errorf("block tier 2 = %v, want the room set expanded", got)
	}

	// A staple pins the two halves of one split to the same slot. Its own
	// number is 0 in this file, which is a real staple and not an absent one.
	var pinned []*arrangov1.LessonInstance
	for _, lesson := range model.GetLessons() {
		if lesson.GetParallelBlockId() != 0 {
			pinned = append(pinned, lesson)
		}
	}
	if len(pinned) != 2 {
		t.Fatalf("pinned lessons = %d, want the two stapled halves", len(pinned))
	}
	if pinned[0].GetParallelBlockId() != pinned[1].GetParallelBlockId() {
		t.Error("stapled lessons landed in different parallel blocks")
	}

	// The gym set carries the any-room wildcard, so the lesson that prefers
	// it must stay unrestricted: eligibility is the union of the tiers.
	gym := lessonOf(t, model, "wf", "1A")
	if len(gym.GetAllowedRoomDesignators())+len(gym.GetAllowedRoomDesignatorsTier2()) != 0 {
		t.Errorf("a wildcard preference restricted the rooms: %v", gym.GetAllowedRoomDesignators())
	}
	if !gym.GetRequiresRoom() {
		t.Error("a lesson with a room preference still needs a room")
	}

	if got := len(result.Schedule.GetLessons()); got != 5 {
		t.Errorf("snapshot = %d placed lessons, want 5", got)
	}

	// Three blocked slots, each its own run: one teacher, one room, one class.
	targets := map[arrangov1.ExternalBlock_Target]int{}
	for _, block := range model.GetExternalBlocks() {
		targets[block.GetTarget()]++
	}
	want := map[arrangov1.ExternalBlock_Target]int{
		arrangov1.ExternalBlock_TARGET_TEACHER:  1,
		arrangov1.ExternalBlock_TARGET_ROOM:     1,
		arrangov1.ExternalBlock_TARGET_DIVISION: 1,
	}
	for target, count := range want {
		if targets[target] != count {
			t.Errorf("external blocks for %v = %d, want %d", target, targets[target], count)
		}
	}

	if got := len(model.GetDailyLoadRules()); got != 2 {
		t.Errorf("daily load rules = %d, want one per division that states a bound", got)
	}
	for _, rule := range model.GetDailyLoadRules() {
		if rule.GetDivisionId() == division.GetId() &&
			(rule.GetMinPerDay() != 2 || rule.GetMaxPerDay() != 3) {
			t.Errorf("division 1A load rule = %d..%d, want 2..3", rule.GetMinPerDay(), rule.GetMaxPerDay())
		}
	}

	if len(model.GetLessonLinks()) != 0 {
		t.Error("a staple is a parallel block, not a relative-placement link")
	}
	kindCount := map[formats.FindingKind]int{}
	for _, finding := range result.Findings {
		kindCount[finding.Kind]++
	}
	for _, kind := range []formats.FindingKind{
		formats.KindUnmappedElement,
		formats.KindUnmappedAttribute,
		formats.KindGroupOrdinalMismatch,
		formats.KindLessonUnplaced,
		formats.KindSplitKindInferred,
	} {
		if kindCount[kind] == 0 {
			t.Errorf("no %s finding; the fixture contains one", kind)
		}
	}
}

// The reader takes the fields its structs name and nothing else. A real file
// carries a national identity number, a date of birth, an email address and
// the institution's registry number; the check here uses neutral attribute
// names so that those four never enter this repository at all.
func TestSkipsAttributesOutsideTheAllowlist(t *testing.T) {
	result := importFixture(t, "plan-small.xml")

	dump := prototext.Format(result.Model)
	for _, leaked := range []string{"poza lista pol", "notatka poza lista pol"} {
		if strings.Contains(dump, leaked) {
			t.Errorf("an attribute outside the allowlist reached the model: %q", leaked)
		}
	}
	for _, finding := range result.Findings {
		for _, leaked := range []string{"poza lista pol", "notatka poza lista pol"} {
			if strings.Contains(finding.Detail, leaked) {
				t.Errorf("an attribute outside the allowlist reached a finding: %q", leaked)
			}
		}
	}
}

func TestImportIsDeterministic(t *testing.T) {
	first := importFixture(t, "plan-small.xml")
	second := importFixture(t, "plan-small.xml")

	if !proto.Equal(first.Model, second.Model) {
		t.Error("two imports of one file produced different models")
	}
	if !proto.Equal(first.Schedule, second.Schedule) {
		t.Error("two imports of one file produced different timetables")
	}
	if len(first.Findings) != len(second.Findings) {
		t.Errorf("findings = %d then %d", len(first.Findings), len(second.Findings))
	}
}

func TestReportsBrokenReferences(t *testing.T) {
	result := importFixture(t, "plan-broken.xml")
	checkModel(t, result)

	counts := map[formats.FindingKind]map[formats.Severity]int{}
	for _, finding := range result.Findings {
		if counts[finding.Kind] == nil {
			counts[finding.Kind] = map[formats.Severity]int{}
		}
		counts[finding.Kind][finding.Severity]++
	}

	// A lesson naming a division, a subject or a teacher the file never
	// defines cannot be built, and that is the one thing worth an error.
	if got := counts[formats.KindLessonDropped][formats.Error]; got != 3 {
		t.Errorf("dropped lessons = %d, want 3", got)
	}
	if !result.HasErrors() {
		t.Error("HasErrors is false with dropped lessons")
	}
	for _, kind := range []formats.FindingKind{
		formats.KindUnknownReference,
		formats.KindBrokenBlockChain,
		formats.KindUnmappedSlotMark,
		formats.KindBlockRoomChange,
	} {
		if len(counts[kind]) == 0 {
			t.Errorf("no %s finding; the fixture contains one", kind)
		}
	}

	// An unknown group widens the lesson to the whole class: that cannot
	// create a collision, where dropping the lesson would lose demand.
	model := result.Model
	if got := len(model.GetLessons()); got != 5 {
		t.Fatalf("lessons = %d, want 5", got)
	}

	// A block whose second period sits in another room keeps the first room,
	// because a placement holds one.
	var moved *arrangov1.LessonInstance
	for _, lesson := range model.GetLessons() {
		if lesson.GetDuration() == 2 {
			moved = lesson
		}
	}
	if moved == nil {
		t.Fatal("no two-period lesson")
	}
	if got := moved.GetPreviousPlacement().GetRoomId(); got != model.GetRooms()[0].GetId() {
		t.Errorf("block room = %d, want the first room of the block", got)
	}
	for _, lesson := range model.GetLessons() {
		for _, participant := range lesson.GetParticipants() {
			if participant.GetGroupId() != 0 {
				t.Errorf("lesson %d kept an unresolved group", lesson.GetId())
			}
		}
	}

	// pmin above pmax is refused rather than carried into a rule the solver
	// would have to reconcile.
	for _, rule := range model.GetDailyLoadRules() {
		if rule.GetMinPerDay() != 0 || rule.GetMaxPerDay() != 2 {
			t.Errorf("load rule = %d..%d, want the impossible minimum dropped",
				rule.GetMinPerDay(), rule.GetMaxPerDay())
		}
	}

	// An unrecognised split name stays open, which is the permissive choice,
	// and says so.
	if got := model.GetSplits()[0].GetKind(); got != arrangov1.SplitKind_SPLIT_KIND_OPEN {
		t.Errorf("unrecognised split kind = %v, want open", got)
	}
	if len(counts[formats.KindSplitKindInferred]) == 0 {
		t.Error("an inferred split kind was not reported")
	}
}

// Two blocks naming one continuation row would count that period twice, which
// is a class booked against itself. The first block in document order keeps it.
func TestOnePeriodBelongsToOneLessonOnly(t *testing.T) {
	const doubleClaim = `<?xml version="1.0" encoding="iso-8859-2"?>
<plan wersja="12.00.0004">
 <dni><dzien kod="Pn" nazwa="Poniedziałek"/></dni>
 <lekcje><lekcja kod="1" od=" 8:00" do=" 8:45"/><lekcja kod="2" od=" 8:55" do=" 9:40"/></lekcje>
 <nauczyciele><nauczyciel kod="AB" imie="Jan" nazwisko="Nowak"/></nauczyciele>
 <przedmioty><przedmiot kod="mat" nazwa="matematyka"/></przedmioty>
 <sale><sala kod="101" nazwa="Sala 101" poj="30"/></sale>
 <oddzialy><oddzial kod="1A" poziom="4" ch="20" dz="0">
  <grupy/><info><p d="0" g="0" i="*"/><p d="0" g="1" i="*"/></info>
 </oddzial></oddzialy>
 <przydzialy>
  <p kl="1A" nau="AB" prz="mat" sa="101" dz="0" godz="0" bk="2" nb="2" pref="101"/>
  <p kl="1A" nau="AB" prz="mat" sa="101" dz="0" godz="0" bk="2" nb="2" pref="101"/>
  <p kl="1A" nau="AB" prz="mat" sa="101" dz="0" godz="1" pref="101"/>
 </przydzialy>
</plan>`

	result := importDocument(t, doubleClaim)
	checkModel(t, result)

	var periods uint32
	for _, lesson := range result.Model.GetLessons() {
		periods += lesson.GetDuration()
	}
	if periods != 3 {
		t.Errorf("lesson periods = %d, the document has 3 rows", periods)
	}
	broken := 0
	for _, finding := range result.Findings {
		if finding.Kind == formats.KindBrokenBlockChain {
			broken++
		}
	}
	if broken == 0 {
		t.Error("a block that lost its continuation row was not reported")
	}
}

// A split can state the fact about a student in its subject rather than its
// name, and a room set code can hold a space. Both were silent losses: a
// religion split read as a plan choice collides in practice, and a set code
// cut in half drops the room restriction the school stated.
func TestReadsSubjectSplitsAndSpacedSetCodes(t *testing.T) {
	const plan = `<?xml version="1.0" encoding="iso-8859-2"?>
<plan wersja="12.00.0004">
 <dni><dzien kod="Pn" nazwa="Poniedziałek"/></dni>
 <lekcje><lekcja kod="1" od=" 8:00" do=" 8:45"/></lekcje>
 <nauczyciele><nauczyciel kod="AB" imie="Jan" nazwisko="Nowak" sala="sala 101 gabinet"/></nauczyciele>
 <przedmioty><przedmiot kod="rel" nazwa="religia"/></przedmioty>
 <sale><sala kod="sala 101" nazwa="Sala 101" poj="30"/><sala kod="102" nazwa="Sala 102" poj="30"/></sale>
 <zbiory><zbior kod="sale katechetyczne" nazwa="Katecheza" el="sala 101,102"/></zbiory>
 <oddzialy><oddzial kod="1A" poziom="4" ch="20" dz="0">
  <grupy><podzial kod="Podział na dwie grupy" przedmiot="religia">
   <grupa kod="1/2" lu="10" nr="1"/><grupa kod="2/2" lu="10" nr="2"/>
  </podzial></grupy>
  <info><p d="0" g="0" i="*"/></info>
 </oddzial></oddzialy>
 <przydzialy>
  <p kl="1A" gr="1/2" aog="1" nau="AB" prz="rel" sa="sala 101" dz="0" godz="0" bk="1" pref="sale katechetyczne"/>
 </przydzialy>
</plan>`

	result := importDocument(t, plan)
	checkModel(t, result)

	if got := result.Model.GetSplits()[0].GetKind(); got != arrangov1.SplitKind_SPLIT_KIND_FIXED {
		t.Errorf("a split over religia is a fact about a student, so it must be fixed: %v", got)
	}
	if got := len(result.Model.GetLessons()); got != 1 {
		t.Fatalf("lessons = %d, want 1", got)
	}
	lesson := result.Model.GetLessons()[0]
	if got := lesson.GetAllowedRoomDesignators(); !equal(got, []string{"sala 101", "102"}) {
		t.Errorf("allowed rooms = %v, want the spaced set code expanded", got)
	}
	teacher := teacherNamed(t, result.Model, "Jan Nowak")
	if got := teacher.GetPreferredRoomDesignators(); !equal(got, []string{"sala 101"}) {
		t.Errorf("teacher rooms = %v, want the spaced room code read from free text", got)
	}
	for _, finding := range result.Findings {
		if finding.Severity == formats.Error {
			t.Errorf("unexpected error finding %s: %s", finding.Kind, finding.Detail)
		}
	}
}

func TestRejectsDocumentWithoutCalendar(t *testing.T) {
	const noCalendar = `<?xml version="1.0" encoding="iso-8859-2"?>
<plan wersja="12.00.0004">
 <nauczyciele><nauczyciel kod="AB" imie="Jan" nazwisko="Nowak"/></nauczyciele>
 <oddzialy><oddzial kod="1A" poziom="4"/></oddzialy>
</plan>`

	result := importDocument(t, noCalendar)
	if !result.HasErrors() {
		t.Fatal("a file with no days and no periods must be an error finding")
	}
	found := false
	for _, finding := range result.Findings {
		if finding.Kind == formats.KindEmptyCalendar {
			found = true
		}
	}
	if !found {
		t.Error("no empty_calendar finding")
	}
}

// Detection reads the container's own header, so it recognises a file it
// cannot then read: saying "this is an Optivum plan and it is truncated" beats
// saying "this is not an Optivum plan".
func TestDetectsContainerByItsHeader(t *testing.T) {
	whole := container(t, latin2Plan)

	var zipped bytes.Buffer
	writer := zip.NewWriter(&zipped)
	entry, err := writer.Create("export/plan.pla")
	if err != nil {
		t.Fatalf("create zip entry: %v", err)
	}
	if _, err := entry.Write(whole); err != nil {
		t.Fatalf("write zip entry: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close zip: %v", err)
	}

	cases := []struct {
		name    string
		data    []byte
		want    formats.Confidence
		imports bool
	}{
		{"a container", whole, formats.Certain, true},
		{"a container inside a zip", zipped.Bytes(), formats.Certain, true},
		{"a truncated container", whole[:20], formats.Certain, false},
		{"a header promising more magic than it has", []byte{0x40, 0x00, 'P', 'l', 'a', 'n'}, formats.No, false},
		{"something else entirely", []byte("PDF-1.7 and then some bytes"), formats.No, false},
		{"nothing", nil, formats.No, false},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			source := formats.Open("upload.bin", test.data)
			got, err := planxml.New().Detect(source)
			if err != nil {
				t.Fatalf("detect: %v", err)
			}
			if got != test.want {
				t.Errorf("detect = %v, want %v", got, test.want)
			}
			_, err = planxml.New().Import(context.Background(), source)
			if test.imports && err != nil {
				t.Errorf("import: %v", err)
			}
			if !test.imports && err == nil {
				t.Error("import accepted a file it cannot read")
			}
		})
	}
}

func TestRejectsForeignDocument(t *testing.T) {
	const other = `<?xml version="1.0" encoding="utf-8"?><cennik><pozycja/></cennik>`

	source := formats.Open("other.pla", container(t, other))
	if _, err := planxml.New().Import(context.Background(), source); err == nil {
		t.Fatal("a container holding another document imported without an error")
	}
}

func TestImportHonoursContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	source := formats.Open("fixture.pla", container(t, latin2Plan))
	if _, err := planxml.New().Import(ctx, source); err == nil {
		t.Fatal("import ran with a cancelled context")
	}
}

// latin2Plan is the encoding a real file carries: the prolog declares
// iso-8859-2 and the bytes are iso-8859-2. Parsing those bytes through a
// CharsetReader is the only way to read them; transcoding first makes
// encoding/xml refuse the document outright.
const latin2Plan = `<?xml version="1.0" encoding="iso-8859-2"?>
<plan wersja="12.00.0004">
 <placowka nazwa="Szkoła Żółkiewskiego"/>
 <dni><dzien kod="Śr" nazwa="Środa"/></dni>
 <lekcje><lekcja kod="1" od=" 8:00" do=" 8:45"/></lekcje>
 <nauczyciele><nauczyciel kod="ŁŻ" imie="Łukasz" nazwisko="Żółć" sala="" pref=""/></nauczyciele>
 <przedmioty><przedmiot kod="jp" nazwa="język polski"/></przedmioty>
 <sale><sala kod="101" nazwa="Sala 101" poj="30"/></sale>
 <oddzialy><oddzial kod="1A" poziom="4" ch="10" dz="10">
  <grupy/><info><p d="0" g="0" i="*"/></info>
 </oddzial></oddzialy>
 <przydzialy><p kl="1A" nau="ŁŻ" prz="jp" sa="101" dz="0" godz="0" bk="1" pref="101"/></przydzialy>
</plan>`

func TestReadsPolishLettersFromIso88592(t *testing.T) {
	result := importDocument(t, latin2Plan)
	checkModel(t, result)

	if got := result.Model.GetName(); got != "Szkoła Żółkiewskiego" {
		t.Errorf("school name = %q, want the iso-8859-2 spelling", got)
	}
	if got := result.Model.GetDays()[0].GetName(); got != "Środa" {
		t.Errorf("day name = %q, want the iso-8859-2 spelling", got)
	}
	teacherNamed(t, result.Model, "Łukasz Żółć")
	if got := result.Model.GetSubjects()[0].GetName(); got != "język polski" {
		t.Errorf("subject name = %q, want the iso-8859-2 spelling", got)
	}
	// The teacher code itself carries Polish letters, so a mis-decoded code
	// would break the reference from the assignment and lose the lesson.
	if got := len(result.Model.GetLessons()); got != 1 {
		t.Errorf("lessons = %d; a mis-decoded code loses the reference", got)
	}
}

// --- helpers ----------------------------------------------------------

func equal(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

func divisionNamed(t *testing.T, model *arrangov1.SchoolModel, name string) *arrangov1.Division {
	t.Helper()
	for _, division := range model.GetDivisions() {
		if division.GetName() == name {
			return division
		}
	}
	t.Fatalf("no division named %q", name)
	return nil
}

func groupNamed(t *testing.T, model *arrangov1.SchoolModel, name string) *arrangov1.Group {
	t.Helper()
	for _, group := range model.GetGroups() {
		if group.GetName() == name {
			return group
		}
	}
	t.Fatalf("no group named %q", name)
	return nil
}

func findTeacher(model *arrangov1.SchoolModel, name string) (*arrangov1.Teacher, bool) {
	for _, teacher := range model.GetTeachers() {
		if teacher.GetName() == name {
			return teacher, true
		}
	}
	return nil, false
}

func teacherNamed(t *testing.T, model *arrangov1.SchoolModel, name string) *arrangov1.Teacher {
	t.Helper()
	teacher, ok := findTeacher(model, name)
	if !ok {
		t.Fatalf("no teacher named %q", name)
	}
	return teacher
}

// lessonOf finds the lesson of one subject in one division.
func lessonOf(t *testing.T, model *arrangov1.SchoolModel, subject, division string) *arrangov1.LessonInstance {
	t.Helper()

	var subjectID, divisionID uint32
	for _, candidate := range model.GetSubjects() {
		if candidate.GetName() == subject {
			subjectID = candidate.GetId()
		}
	}
	for _, candidate := range model.GetDivisions() {
		if candidate.GetName() == division {
			divisionID = candidate.GetId()
		}
	}
	for _, lesson := range model.GetLessons() {
		if lesson.GetSubjectId() != subjectID {
			continue
		}
		for _, participant := range lesson.GetParticipants() {
			if participant.GetDivisionId() == divisionID {
				return lesson
			}
		}
	}
	t.Fatalf("no lesson of %q in %q", subject, division)
	return nil
}

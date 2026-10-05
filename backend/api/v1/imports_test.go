// api/v1/imports_test.go

package v1_test

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"unicode/utf8"

	arrangov1 "github.com/smegg99/arrango/backend/gen/arrangov1"
	"github.com/smegg99/s99ab/data/db"

	apiv1 "github.com/smegg99/goptivum/backend/api/v1"
	"github.com/smegg99/goptivum/backend/api/v1/gen"
	"github.com/smegg99/goptivum/backend/common/messages"
	"github.com/smegg99/goptivum/backend/formats/goptivum"
	"github.com/smegg99/goptivum/backend/migrations"
	"github.com/smegg99/goptivum/document"
	"github.com/smegg99/goptivum/document/gpf"
)

func preview(t *testing.T, api gen.StrictServerInterface, ctx context.Context, name string, file []byte, fields map[string]string) gen.PreviewImportResponseObject {
	t.Helper()
	response, err := api.PreviewImport(ctx, gen.PreviewImportRequestObject{Body: form(t, "file", name, file, fields)})
	if err != nil {
		t.Fatalf("preview: %v", err)
	}
	return response
}

func importFile(t *testing.T, api gen.StrictServerInterface, ctx context.Context, name string, file []byte, fields map[string]string) gen.ImportPlanResponseObject {
	t.Helper()
	response, err := api.ImportPlan(ctx, gen.ImportPlanRequestObject{Body: form(t, "file", name, file, fields)})
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	return response
}

func imported(t *testing.T, response gen.ImportPlanResponseObject) gen.ImportPlan201JSONResponse {
	t.Helper()
	result, ok := response.(gen.ImportPlan201JSONResponse)
	if !ok {
		t.Fatalf("import returned %T %+v, want 201", response, response)
	}
	return result
}

func previewed(t *testing.T, response gen.PreviewImportResponseObject) gen.PreviewImport200JSONResponse {
	t.Helper()
	result, ok := response.(gen.PreviewImport200JSONResponse)
	if !ok {
		t.Fatalf("preview returned %T %+v, want 200", response, response)
	}
	return result
}

func planCount(t *testing.T, api gen.StrictServerInterface, ctx context.Context) int {
	t.Helper()
	listed, err := api.ListPlans(ctx, gen.ListPlansRequestObject{})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	return listed.(gen.ListPlans200JSONResponse).Total
}

// The preview reads the file exactly as an import does and stores nothing, so
// what a person confirms is what is stored (B16).
func TestAPreviewReadsTheFileAndStoresNothing(t *testing.T) {
	api := newAPI(t)
	ctx := signedIn("identity-1")

	seen := previewed(t, preview(t, api, ctx, "plan-small.pla", fixtureContainer(t), nil))
	if seen.Importer != "optivum.pla" {
		t.Errorf("importer = %q, want optivum.pla", seen.Importer)
	}
	if seen.School == nil || *seen.School != "Szkoła Testowa" || seen.Name != "Szkoła Testowa" {
		t.Errorf("school = %v, name = %q, want the school the file names", seen.School, seen.Name)
	}
	if seen.Counts.Days != 2 || seen.Counts.Classes == 0 || seen.Counts.Teachers == 0 || seen.Counts.Lessons == 0 {
		t.Errorf("counts = %+v, want the fixture's school", seen.Counts)
	}
	if len(seen.Findings) == 0 {
		t.Error("no findings; the fixture drops elements the model has no place for")
	}
	if got := planCount(t, api, ctx); got != 0 {
		t.Fatalf("a preview stored %d plans, want none", got)
	}
	trash, err := api.ListTrash(ctx, gen.ListTrashRequestObject{})
	if err != nil {
		t.Fatalf("trash: %v", err)
	}
	if items := trash.(gen.ListTrash200JSONResponse).Items; len(items) != 0 {
		t.Errorf("a preview left %d Trash items", len(items))
	}

	// The import that follows stores the school the preview counted.
	result := imported(t, importFile(t, api, ctx, "plan-small.pla", fixtureContainer(t), nil))
	stored := storedDocument(t, api, result.PlanId, result.Version)
	model := stored.GetModel()
	want := gen.ImportCounts{
		Days: len(model.GetDays()), Periods: len(model.GetPeriods()), Classes: len(model.GetDivisions()),
		Groups: len(model.GetGroups()), Teachers: len(model.GetTeachers()), Subjects: len(model.GetSubjects()),
		Rooms: len(model.GetRooms()), Lessons: len(model.GetLessons()), Placed: placedLessons(stored.GetSchedule()),
	}
	if seen.Counts != want {
		t.Errorf("preview counts = %+v, stored school = %+v", seen.Counts, want)
	}
	if want.Placed == 0 || want.Placed >= want.Lessons {
		t.Errorf("placed = %d of %d, want the fixture's partly placed timetable", want.Placed, want.Lessons)
	}
	if len(result.Findings) != len(seen.Findings) {
		t.Errorf("import findings = %d, preview findings = %d, want the same", len(result.Findings), len(seen.Findings))
	}
	if got := planCount(t, api, ctx); got != 1 {
		t.Errorf("plans after the import = %d, want 1", got)
	}
}

func placedLessons(schedule *arrangov1.ScheduleSnapshot) int {
	placed := map[uint32]bool{}
	for _, lesson := range schedule.GetLessons() {
		if lesson.GetPlacement() != nil {
			placed[lesson.GetLessonId()] = true
		}
	}
	return len(placed)
}

// A file no importer knows is refused with a key the catalogue words (G19),
// by the preview and the import alike.
func TestAnUnknownFileIsRefusedWithItsKey(t *testing.T) {
	api := newAPI(t)
	ctx := signedIn("identity-1")
	junk := []byte("NAME=\"Arch Linux\"\nID=arch\n")

	refused, ok := importFile(t, api, ctx, "not-a-plan.pla", junk, nil).(gen.ImportPlan415JSONResponse)
	if !ok || refused.Code != "unsupported_format" || refused.Message != messages.KeyImportUnsupportedFormat {
		t.Errorf("import = %+v, want 415 unsupported_format %s", refused, messages.KeyImportUnsupportedFormat)
	}
	seen, ok := preview(t, api, ctx, "not-a-plan.pla", junk, nil).(gen.PreviewImport415JSONResponse)
	if !ok || seen.Message != messages.KeyImportUnsupportedFormat {
		t.Errorf("preview = %+v, want 415 %s", seen, messages.KeyImportUnsupportedFormat)
	}
	for _, lang := range []string{"pl", "en"} {
		if said := messages.Localize(messages.Localizer(lang), messages.KeyImportUnsupportedFormat); said == messages.KeyImportUnsupportedFormat {
			t.Errorf("%s has no %s text", messages.KeyImportUnsupportedFormat, lang)
		}
	}
}

// zipped packs files the way a school zips its export folder.
func zipped(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var out bytes.Buffer
	writer := zip.NewWriter(&out)
	for name, body := range files {
		entry, err := writer.Create(name)
		if err != nil {
			t.Fatalf("zip %s: %v", name, err)
		}
		if _, err := entry.Write([]byte(body)); err != nil {
			t.Fatalf("zip %s: %v", name, err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("zip: %v", err)
	}
	return out.Bytes()
}

// divisionPage is one synthetic o1.html. Each row numbered "x" is a row the
// parser warns about.
func divisionPage(badRows int) string {
	var b strings.Builder
	b.WriteString(`<html><head><meta charset="utf-8"><title>Plan lekcji oddziału - 1a</title></head><body>
<span class="tytulnapis">1a</span><table class="tabela">
<tr><th>Nr</th><th>Godz</th><th>Poniedziałek</th></tr>
<tr><td class="nr">1</td><td class="g">8:00- 8:45</td><td class="l"><span class="p">matematyka</span></td></tr>
`)
	for range badRows {
		b.WriteString(`<tr><td class="nr">x</td><td class="g">9:00- 9:45</td><td class="l">&nbsp;</td></tr>` + "\n")
	}
	b.WriteString("</table></body></html>")
	return b.String()
}

// The same fact reported several times is one finding with its count (G10,
// G16): an HTML export's parse notes are merged, not listed N times.
func TestImportFindingsAreMergedWithACount(t *testing.T) {
	api := newAPI(t)
	ctx := signedIn("identity-1")

	result := imported(t, importFile(t, api, ctx, "plan.zip", zipped(t, map[string]string{
		"plan/plany/o1.html": divisionPage(3),
	}), nil))
	warnings := 0
	seen := map[string]bool{}
	for _, finding := range result.Findings {
		if finding.Count < 1 {
			t.Errorf("%s count = %d, want at least 1", finding.Kind, finding.Count)
		}
		key := finding.Kind + "/" + string(finding.Severity)
		if finding.Entities == nil {
			if seen[key] {
				t.Errorf("%s is listed twice", key)
			}
			seen[key] = true
		}
		if finding.Kind == "source_warning" {
			warnings++
			if finding.Count != 3 {
				t.Errorf("source_warning count = %d, want the 3 rows it stands for", finding.Count)
			}
		}
	}
	if warnings != 1 {
		t.Errorf("source_warning listed %d times, want once", warnings)
	}
}

// lockedFile is a small school locked with a password, cheap to open.
func lockedFile(t *testing.T, passphrase string) []byte {
	t.Helper()
	locked := document.New(&arrangov1.SchoolModel{
		Name:     "Szkoła Zamknięta",
		Days:     []*arrangov1.Day{{Id: 1, Name: "Środa", PeriodCount: 1}},
		Periods:  []*arrangov1.Period{{Id: 2, Name: "8:00"}},
		Teachers: []*arrangov1.Teacher{{Id: 3, Name: "Łucja Ćwik"}},
		Lessons:  []*arrangov1.LessonInstance{{Id: 4, TeacherId: 3, Duration: 1}},
	})
	file, err := gpf.Encode(locked, gpf.WriteOptions{
		Header: gpf.NewHeader().Set(gpf.FieldSchool, "Szkoła Zamknięta"), Passphrase: passphrase, WorkFactor: 10,
	})
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	return file
}

// A password-locked .gpf asks for its password, refuses a wrong one as such,
// and imports with the right one (B17).
func TestAPasswordLockedFileImportsWithItsPassphrase(t *testing.T) {
	api := newAPI(t)
	ctx := signedIn("identity-1")
	file := lockedFile(t, "tajne hasło")

	for _, test := range []struct {
		passphrase string
		key        string
	}{
		{"", gpf.KeyPasswordRequired},
		{"złe hasło", gpf.KeyWrongPassword},
	} {
		fields := map[string]string{"passphrase": test.passphrase}
		refused, ok := preview(t, api, ctx, "zamek.gpf", file, fields).(gen.PreviewImport422JSONResponse)
		if !ok || refused.Message != test.key {
			t.Errorf("preview with %q = %+v, want 422 %s", test.passphrase, refused, test.key)
		}
		stopped, ok := importFile(t, api, ctx, "zamek.gpf", file, fields).(gen.ImportPlan422JSONResponse)
		if !ok || stopped.Message != test.key {
			t.Errorf("import with %q = %+v, want 422 %s", test.passphrase, stopped, test.key)
		}
	}
	if got := planCount(t, api, ctx); got != 0 {
		t.Fatalf("refused imports stored %d plans", got)
	}

	fields := map[string]string{"passphrase": "tajne hasło"}
	seen := previewed(t, preview(t, api, ctx, "zamek.gpf", file, fields))
	if seen.Importer != goptivum.ID || seen.Name != "Szkoła Zamknięta" || seen.Counts.Lessons != 1 {
		t.Errorf("preview = %+v", seen)
	}
	result := imported(t, importFile(t, api, ctx, "zamek.gpf", file, fields))
	stored := storedDocument(t, api, result.PlanId, result.Version)
	if stored.GetModel().GetTeachers()[0].GetName() != "Łucja Ćwik" {
		t.Errorf("stored school = %+v", stored.GetModel())
	}
	plan, err := api.GetPlan(ctx, gen.GetPlanRequestObject{PlanId: result.PlanId})
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got := plan.(gen.GetPlan200JSONResponse); got.Name != "Szkoła Zamknięta" {
		t.Errorf("plan name = %q, want the school's", got.Name)
	}
}

func TestAPassphraseOverItsLimitIsRefused(t *testing.T) {
	api := newAPI(t)
	ctx := signedIn("identity-1")
	fields := map[string]string{"passphrase": strings.Repeat("ż", 1025)}

	refused, ok := importFile(t, api, ctx, "zamek.gpf", lockedFile(t, "x"), fields).(gen.ImportPlan422JSONResponse)
	if !ok || refused.Details == nil || (*refused.Details)["passphrase"] != "too_long" {
		t.Errorf("import = %+v, want 422 passphrase: too_long", refused)
	}
	seen, ok := preview(t, api, ctx, "zamek.gpf", lockedFile(t, "x"), fields).(gen.PreviewImport422JSONResponse)
	if !ok || seen.Details == nil || (*seen.Details)["passphrase"] != "too_long" {
		t.Errorf("preview = %+v, want 422 passphrase: too_long", seen)
	}
}

// A project is named after the school the file names, else after the file;
// a typed name wins, and a derived name is cut to fit rather than refused.
func TestAnImportIsNamedAfterTheSchoolOrTheFile(t *testing.T) {
	api := newAPI(t)
	ctx := signedIn("identity-1")
	page := divisionPage(0)
	withIndex := zipped(t, map[string]string{
		"plan/plany/o1.html": page,
		"plan/index.html":    `<html><head><meta charset="utf-8"><title>Zespół Szkół nr 5. Plan lekcji</title></head></html>`,
	})
	long := strings.Repeat("ź", 250)

	for _, test := range []struct {
		name, file string
		data       []byte
		fields     map[string]string
		want       string
	}{
		{"the school the export names", "eksport.zip", withIndex, nil, "Zespół Szkół nr 5"},
		{"a zip naming no school", "plan-2027.zip", zipped(t, map[string]string{"plany/o1.html": page}), nil, "plan-2027"},
		{"one class page", "o1.html", []byte(page), nil, "o1"},
		{"a typed name", "eksport.zip", withIndex, map[string]string{"name": " Plan próbny "}, "Plan próbny"},
		{"a long file name", long + ".zip", zipped(t, map[string]string{"plany/o1.html": page}), nil, strings.Repeat("ź", 200)},
	} {
		t.Run(test.name, func(t *testing.T) {
			seen := previewed(t, preview(t, api, ctx, test.file, test.data, nil))
			result := imported(t, importFile(t, api, ctx, test.file, test.data, test.fields))
			plan, err := api.GetPlan(ctx, gen.GetPlanRequestObject{PlanId: result.PlanId})
			if err != nil {
				t.Fatalf("get: %v", err)
			}
			got := plan.(gen.GetPlan200JSONResponse)
			if got.Name != test.want {
				t.Errorf("plan name = %q (%d letters), want %q", got.Name, utf8.RuneCountInString(got.Name), test.want)
			}
			if test.fields == nil && seen.Name != test.want {
				t.Errorf("preview name = %q, want %q", seen.Name, test.want)
			}
		})
	}
}

// An import makes a project the caller owns, and a refused one leaves none
// (G9).
func TestAnImportIsTheCallersOwnProjectOrNothing(t *testing.T) {
	api := newAPI(t)
	ctx := signedIn("identity-1")

	refused := importFile(t, api, ctx, "plan-small.pla", fixtureContainer(t), map[string]string{
		"school_year": strings.Repeat("9", 33),
	})
	if !is[gen.ImportPlan422JSONResponse](refused) {
		t.Fatalf("an unusable school year returned %T, want 422", refused)
	}
	if got := planCount(t, api, ctx); got != 0 {
		t.Fatalf("a refused import left %d plans", got)
	}

	result := imported(t, importFile(t, api, ctx, "plan-small.pla", fixtureContainer(t), nil))
	plan, err := api.GetPlan(ctx, gen.GetPlanRequestObject{PlanId: result.PlanId})
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	got := plan.(gen.GetPlan200JSONResponse)
	if got.Owner.Id != "identity-1" || !got.CanEdit {
		t.Errorf("owner = %+v, can_edit = %v, want the caller's own", got.Owner, got.CanEdit)
	}
	if other := signedIn("identity-2"); planCount(t, api, other) != 1 {
		t.Error("another account does not see the imported project")
	}
}

// A handler that refuses an upload over the limit says the limit, so a person
// is told how large a file may be.
func TestAnUploadOverTheLimitSaysTheLimit(t *testing.T) {
	api := planAPIWithLimit(t, 4096)
	ctx := signedIn("identity-1")
	big := bytes.Repeat([]byte("x"), 5000)

	tooLarge, ok := importFile(t, api, ctx, "too-big.pla", big, nil).(gen.ImportPlan413JSONResponse)
	if !ok || tooLarge.Details == nil || (*tooLarge.Details)["Limit"] != "4096" {
		t.Errorf("import = %+v, want 413 with Limit 4096", tooLarge)
	}
	seen, ok := preview(t, api, ctx, "too-big.pla", big, nil).(gen.PreviewImport413JSONResponse)
	if !ok || seen.Details == nil || (*seen.Details)["Limit"] != "4096" {
		t.Errorf("preview = %+v, want 413 with Limit 4096", seen)
	}
	plan := createPlan(t, api, ctx, "Plan 2027")
	saved, ok := save(t, api, ctx, plan.Id, big, map[string]string{"parent": "1", "upload_key": "autosave-0000000001"}).(gen.UploadVersion413JSONResponse)
	if !ok || saved.Details == nil || (*saved.Details)["Limit"] != "4096" {
		t.Errorf("upload = %+v, want 413 with Limit 4096", saved)
	}
}

// The preview sits on the upload group, so the upload cap bounds it: a body
// over the cap is refused before a handler reads it.
func TestThePreviewRouteCarriesTheUploadCap(t *testing.T) {
	server, err := apiv1.New(apiv1.Options{
		Build:            apiv1.BuildInfo{Version: "1.2.3"},
		MinClientVersion: "0.1.0",
		Handlers:         &apiv1.Handlers{API: newPlanAPI(t, db.TestDB(t, migrations.FS))},
		Kratos:           adminFake(t),
		MaxUploadSize:    2048,
	})
	if err != nil {
		t.Fatalf("new server: %v", err)
	}

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, _ := writer.CreateFormFile("file", "too-big.pla")
	part.Write(bytes.Repeat([]byte("x"), 4000))
	writer.Close()
	request := httptest.NewRequest(http.MethodPost, "/api/v1/import/preview", &body)
	request.Header.Set("Content-Type", writer.FormDataContentType())
	request.Header.Set("Authorization", asPlanner)
	recorder := httptest.NewRecorder()
	server.Handler().ServeHTTP(recorder, request)
	if recorder.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("status = %d %s, want 413 from the upload cap", recorder.Code, recorder.Body.String())
	}
}

// /api/info names the upload limit the server enforces, so a client checks a
// file before sending it and names the limit when a 413 carries none.
func TestInfoNamesTheUploadLimit(t *testing.T) {
	for _, test := range []struct {
		configured, want int64
	}{
		{0, apiv1.MaxUploadSize},
		{32 << 20, 32 << 20},
	} {
		server, err := apiv1.New(apiv1.Options{
			Build:            apiv1.BuildInfo{Version: "1.2.3"},
			MinClientVersion: "0.1.0",
			Handlers:         newAPI(t),
			MaxUploadSize:    test.configured,
		})
		if err != nil {
			t.Fatalf("new server: %v", err)
		}
		recorder := httptest.NewRecorder()
		server.Handler().ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/info", nil))
		var info gen.Info
		if err := json.Unmarshal(recorder.Body.Bytes(), &info); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if info.MaximumUploadBytes == nil || *info.MaximumUploadBytes != test.want {
			t.Errorf("configured %d: maximum_upload_bytes = %v, want %d", test.configured, info.MaximumUploadBytes, test.want)
		}
	}
}

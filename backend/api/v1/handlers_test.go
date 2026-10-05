// api/v1/handlers_test.go

package v1_test

import (
	"bytes"
	"context"
	"io"
	"mime/multipart"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/smegg99/s99ab/auth/kratosauth"

	arrangov1 "github.com/smegg99/arrango/backend/gen/arrangov1"
	"google.golang.org/protobuf/proto"

	apiv1 "github.com/smegg99/goptivum/backend/api/v1"
	"github.com/smegg99/goptivum/backend/api/v1/gen"
	"github.com/smegg99/goptivum/backend/common/messages"
	"github.com/smegg99/goptivum/backend/formats/goptivum"
	"github.com/smegg99/goptivum/backend/formats/optivum/pla"
	"github.com/smegg99/goptivum/backend/internal/testsupport"
	"github.com/smegg99/goptivum/document"
	goptivumv1 "github.com/smegg99/goptivum/document/gen/goptivumv1"
	"github.com/smegg99/goptivum/document/gpf"
)

// signedIn is the context a guarded route reaches a handler with. Every route
// that gets here has passed kratosauth.Require.
func signedIn(identity string) context.Context {
	return kratosauth.WithIdentity(context.Background(), &kratosauth.Identity{ID: identity})
}

func createPlan(t *testing.T, api *apiv1.Handlers, ctx context.Context, name string) gen.Plan {
	t.Helper()

	response, err := api.CreatePlan(ctx, gen.CreatePlanRequestObject{
		Body: form(t, "", "", nil, map[string]string{"name": name, "school_year": "2026/2027"}),
	})
	if err != nil {
		t.Fatalf("create plan: %v", err)
	}
	created, ok := response.(gen.CreatePlan201JSONResponse)
	if !ok {
		t.Fatalf("create plan returned %T", response)
	}
	return gen.Plan(created)
}

func TestPlanLifecycle(t *testing.T) {
	api := newAPI(t)
	ctx := signedIn("identity-1")

	plan := createPlan(t, api, ctx, "Plan ZSEM")
	if plan.Id == "" || plan.Name != "Plan ZSEM" {
		t.Fatalf("created %+v", plan)
	}

	listed, err := api.ListPlans(ctx, gen.ListPlansRequestObject{})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	page, ok := listed.(gen.ListPlans200JSONResponse)
	if !ok {
		t.Fatalf("list returned %T", listed)
	}
	if page.Total != 1 || len(page.Items) != 1 || page.Items[0].Id != plan.Id {
		t.Errorf("page = %+v", page)
	}
	if page.Page != 1 || page.Limit != 25 {
		t.Errorf("page %d limit %d, want the defaults the contract states", page.Page, page.Limit)
	}

	read, err := api.GetPlan(ctx, gen.GetPlanRequestObject{PlanId: plan.Id})
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if _, ok := read.(gen.GetPlan200JSONResponse); !ok {
		t.Fatalf("get returned %T", read)
	}

	deleted, err := api.DeletePlan(ctx, gen.DeletePlanRequestObject{PlanId: plan.Id})
	if err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, ok := deleted.(gen.DeletePlan204Response); !ok {
		t.Fatalf("delete returned %T", deleted)
	}
	gone, err := api.GetPlan(ctx, gen.GetPlanRequestObject{PlanId: plan.Id})
	if err != nil {
		t.Fatalf("get after delete: %v", err)
	}
	if _, ok := gone.(gen.GetPlan404JSONResponse); !ok {
		t.Fatalf("get after delete returned %T", gone)
	}
}

// Everyone at the school reads another owner's plan and cannot delete it.
func TestAnotherAccountReadsAPlanAndCannotDeleteIt(t *testing.T) {
	api := newAPI(t)
	mine := signedIn("identity-1")
	theirs := signedIn("identity-2")

	plan := createPlan(t, api, mine, "Plan ZSEM")

	read, err := api.GetPlan(theirs, gen.GetPlanRequestObject{PlanId: plan.Id})
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got, ok := read.(gen.GetPlan200JSONResponse); !ok || got.CanEdit || got.CanDelete || got.Owner.Id != "identity-1" {
		t.Errorf("get returned %T %+v, want the plan, not the caller's to change", read, read)
	}

	deleted, err := api.DeletePlan(theirs, gen.DeletePlanRequestObject{PlanId: plan.Id})
	if err != nil {
		t.Fatalf("delete: %v", err)
	}
	if refused, ok := deleted.(gen.DeletePlan403JSONResponse); !ok || refused.Message != messages.KeyPlansNotYours {
		t.Errorf("delete returned %T %+v, want 403 plans.notYours", deleted, deleted)
	}

	listed, err := api.ListPlans(theirs, gen.ListPlansRequestObject{})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if page := listed.(gen.ListPlans200JSONResponse); page.Total != 1 || page.Items[0].Id != plan.Id || page.Items[0].CanEdit {
		t.Errorf("another account sees %+v", page)
	}
}

// Every handler refuses a caller with no identity, even though the route
// wiring should never let one through.
func TestHandlersRefuseACallerWithoutAnIdentity(t *testing.T) {
	api := newAPI(t)
	ctx := context.Background()

	listed, _ := api.ListPlans(ctx, gen.ListPlansRequestObject{})
	if _, ok := listed.(gen.ListPlans401JSONResponse); !ok {
		t.Errorf("list returned %T", listed)
	}
	created, _ := api.CreatePlan(ctx, gen.CreatePlanRequestObject{Body: form(t, "", "", nil, map[string]string{"name": "x"})})
	if _, ok := created.(gen.CreatePlan401JSONResponse); !ok {
		t.Errorf("create returned %T", created)
	}
	read, _ := api.GetPlan(ctx, gen.GetPlanRequestObject{PlanId: "1"})
	if _, ok := read.(gen.GetPlan401JSONResponse); !ok {
		t.Errorf("get returned %T", read)
	}
	imported, _ := api.ImportPlan(ctx, gen.ImportPlanRequestObject{})
	if _, ok := imported.(gen.ImportPlan401JSONResponse); !ok {
		t.Errorf("import returned %T", imported)
	}
}

func TestCreateRefusesAPlanWithoutAName(t *testing.T) {
	api := newAPI(t)
	ctx := signedIn("identity-1")

	for _, fields := range []map[string]string{{"name": "  "}, {"school_year": "2026/2027"}} {
		response, err := api.CreatePlan(ctx, gen.CreatePlanRequestObject{Body: form(t, "", "", nil, fields)})
		if err != nil {
			t.Fatalf("create: %v", err)
		}
		refused, ok := response.(gen.CreatePlan422JSONResponse)
		if !ok {
			t.Fatalf("create with %v returned %T, want 422", fields, response)
		}
		if refused.Details == nil || (*refused.Details)["name"] != "required" {
			t.Errorf("details = %v, want name: required", refused.Details)
		}
	}
	if listed, _ := api.ListPlans(ctx, gen.ListPlansRequestObject{}); listed.(gen.ListPlans200JSONResponse).Total != 0 {
		t.Error("a refused create left a plan behind")
	}
}

// form builds a multipart body the way a client does.
func form(t *testing.T, fileField, filename string, file []byte, fields map[string]string) *multipart.Reader {
	t.Helper()

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	if file != nil {
		part, err := writer.CreateFormFile(fileField, filename)
		if err != nil {
			t.Fatalf("create part: %v", err)
		}
		if _, err := part.Write(file); err != nil {
			t.Fatalf("write part: %v", err)
		}
	}
	for name, value := range fields {
		if err := writer.WriteField(name, value); err != nil {
			t.Fatalf("write field: %v", err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close writer: %v", err)
	}
	return multipart.NewReader(&body, writer.Boundary())
}

// fixtureContainer is the committed Optivum fixture as a school hands it over.
func fixtureContainer(t *testing.T) []byte {
	t.Helper()

	body, err := os.ReadFile(testsupport.Fixture(t, filepath.Join("optivum", "plan-small.xml")))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	// The fixture declares utf-8 and is stored as utf-8, so it is wrapped as
	// it is; the reader's own tests cover the iso-8859-2 path.
	data, err := pla.Encode(pla.File{Magic: pla.Magic, XML: body, Compressed: true})
	if err != nil {
		t.Fatalf("wrap: %v", err)
	}
	return data
}

// The import path end to end: a school's own file becomes a plan, its first
// version, and typed findings for what did not map.
func TestImportCreatesAPlanAndItsFirstVersion(t *testing.T) {
	api := newAPI(t)
	ctx := signedIn("identity-1")

	response, err := api.ImportPlan(ctx, gen.ImportPlanRequestObject{
		Body: form(t, "file", "plan-small.pla", fixtureContainer(t), map[string]string{
			"school_year": "2026/2027",
		}),
	})
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	result, ok := response.(gen.ImportPlan201JSONResponse)
	if !ok {
		t.Fatalf("import returned %T", response)
	}
	if result.Version != 1 {
		t.Errorf("version = %d, want the plan's first", result.Version)
	}
	if result.Importer != "optivum.pla" {
		t.Errorf("importer = %v, want the one that claimed the file", result.Importer)
	}
	if len(result.Findings) == 0 {
		t.Error("no findings; the fixture drops elements the model has no place for")
	}
	for _, finding := range result.Findings {
		if finding.Severity == "error" {
			t.Errorf("import reported a lost lesson: %s", finding.Kind)
		}
	}

	// The imported document is downloadable, and it is the bytes that were
	// stored.
	downloaded, err := api.DownloadVersion(ctx, gen.DownloadVersionRequestObject{
		PlanId: result.PlanId, Version: result.Version,
	})
	if err != nil {
		t.Fatalf("download: %v", err)
	}
	body, ok := downloaded.(gen.DownloadVersion200ApplicationzstdResponse)
	if !ok {
		t.Fatalf("download returned %T", downloaded)
	}
	data, err := io.ReadAll(body.Body)
	if err != nil {
		t.Fatalf("read document: %v", err)
	}
	doc, err := document.Decode(data)
	if err != nil {
		t.Fatalf("decode document: %v", err)
	}
	if len(doc.GetModel().GetLessons()) == 0 {
		t.Error("the stored document carries no lessons")
	}
	// An import allocates the counter, so the next edit cannot reuse an id.
	if doc.GetNextArrangoId() == 0 {
		t.Error("the stored document hands out id zero")
	}
	// The placements the importer read are stored with the school. Dropping
	// them loses the timetable itself and leaves a model nobody solved.
	if len(doc.GetSchedule().GetLessons()) == 0 {
		t.Error("the stored document carries no placements, so the import lost the timetable")
	}
}

func TestImportRefusesAFileNoImporterKnows(t *testing.T) {
	api := newAPI(t)
	ctx := signedIn("identity-1")

	response, err := api.ImportPlan(ctx, gen.ImportPlanRequestObject{
		Body: form(t, "file", "notes.txt", []byte("this is not a school"), nil),
	})
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if _, ok := response.(gen.ImportPlan415JSONResponse); !ok {
		t.Errorf("import returned %T, want 415", response)
	}

	empty, err := api.ImportPlan(ctx, gen.ImportPlanRequestObject{Body: form(t, "file", "", nil, nil)})
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if _, ok := empty.(gen.ImportPlan422JSONResponse); !ok {
		t.Errorf("an empty upload returned %T, want 422", empty)
	}
}

// The upload path, including the refusal that carries the current version.
func TestUploadVersionRefusesAStaleParent(t *testing.T) {
	api := newAPI(t)
	ctx := signedIn("identity-1")

	imported, err := api.ImportPlan(ctx, gen.ImportPlanRequestObject{
		Body: form(t, "file", "plan-small.pla", fixtureContainer(t), nil),
	})
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	first := imported.(gen.ImportPlan201JSONResponse)

	// Re-upload the same document as version 2, derived from version 1.
	downloaded, err := api.DownloadVersion(ctx, gen.DownloadVersionRequestObject{
		PlanId: first.PlanId, Version: 1,
	})
	if err != nil {
		t.Fatalf("download: %v", err)
	}
	data, err := io.ReadAll(downloaded.(gen.DownloadVersion200ApplicationzstdResponse).Body)
	if err != nil {
		t.Fatalf("read: %v", err)
	}

	second, err := api.UploadVersion(ctx, gen.UploadVersionRequestObject{
		PlanId: first.PlanId,
		Body: form(t, "document", "plan.gpf", data, map[string]string{
			"parent": "1", "valid_from": "2026-09-01", "upload_key": "first-save-000001",
		}),
	})
	if err != nil {
		t.Fatalf("upload: %v", err)
	}
	version, ok := second.(gen.UploadVersion201JSONResponse)
	if !ok {
		t.Fatalf("upload returned %T", second)
	}
	if version.Version != 2 || version.Parent == nil || *version.Parent != 1 {
		t.Errorf("version = %+v", version)
	}
	if version.ValidFrom == nil || version.ValidFrom.Format("2006-01-02") != "2026-09-01" {
		t.Errorf("valid_from = %v", version.ValidFrom)
	}

	// The same parent again, from another save: someone saved first.
	stale, err := api.UploadVersion(ctx, gen.UploadVersionRequestObject{
		PlanId: first.PlanId,
		Body: form(t, "document", "plan.gpf", data, map[string]string{
			"parent": "1", "upload_key": "other-save-000002",
		}),
	})
	if err != nil {
		t.Fatalf("upload: %v", err)
	}
	conflict, ok := stale.(gen.UploadVersion409JSONResponse)
	if !ok {
		t.Fatalf("a stale parent returned %T, want 409", stale)
	}
	if conflict.CurrentVersion != 2 {
		t.Errorf("conflict names version %d, want 2 so the client can say who saved first", conflict.CurrentVersion)
	}
	if conflict.Message != "plans.versionConflict" {
		t.Errorf("conflict message = %q, want the catalogue key plans.versionConflict", conflict.Message)
	}

	listed, err := api.ListVersions(ctx, gen.ListVersionsRequestObject{PlanId: first.PlanId})
	if err != nil {
		t.Fatalf("list versions: %v", err)
	}
	if timeline := listed.(gen.ListVersions200JSONResponse); len(timeline.Items) != 2 {
		t.Errorf("timeline holds %d versions, want 2", len(timeline.Items))
	}
}

func TestUploadRefusesSomethingThatIsNotADocument(t *testing.T) {
	api := newAPI(t)
	ctx := signedIn("identity-1")
	plan := createPlan(t, api, ctx, "Plan ZSEM")

	response, err := api.UploadVersion(ctx, gen.UploadVersionRequestObject{
		PlanId: plan.Id,
		Body: form(t, "document", "plan.gpf", []byte("not a document"), map[string]string{
			"parent": "1", "upload_key": "not-a-document-01",
		}),
	})
	if err != nil {
		t.Fatalf("upload: %v", err)
	}
	if _, ok := response.(gen.UploadVersion422JSONResponse); !ok {
		t.Errorf("upload returned %T, want 422: storing bytes that cannot be read back is silent corruption", response)
	}
}

func TestUnknownPlanIdIsNotFound(t *testing.T) {
	api := newAPI(t)
	ctx := signedIn("identity-1")

	for _, id := range []string{"0", "banana", "-1", strconv.FormatInt(1<<62, 10)} {
		t.Run(id, func(t *testing.T) {
			response, err := api.GetPlan(ctx, gen.GetPlanRequestObject{PlanId: id})
			if err != nil {
				t.Fatalf("get: %v", err)
			}
			if _, ok := response.(gen.GetPlan404JSONResponse); !ok {
				t.Errorf("get returned %T, want 404", response)
			}
		})
	}
}

// A .gpf arrives as a whole envelope and is stored as one. Rebuilding it would
// recompute the id counter, and a file whose sender deleted their
// highest-numbered entity carries a counter above what the model implies:
// recomputing hands out an id that sender already spent.
func TestImportingAGpfKeepsItsIDCounter(t *testing.T) {
	api := newAPI(t)
	ctx := signedIn("identity-1")

	// A sender who deleted their highest-numbered lesson: the counter is above
	// anything the model still mentions.
	sent := document.New(&arrangov1.SchoolModel{
		Name:     "Szkola Nadawcy",
		Days:     []*arrangov1.Day{{Id: 1, Name: "Poniedzialek", PeriodCount: 1}},
		Periods:  []*arrangov1.Period{{Id: 2, Name: "8:00"}},
		Teachers: []*arrangov1.Teacher{{Id: 3, Name: "AB"}},
		Lessons:  []*arrangov1.LessonInstance{{Id: 4, TeacherId: 3, Duration: 1}},
	})
	sent.NextArrangoId = 900

	file, err := gpf.Encode(sent, gpf.WriteOptions{
		Header: gpf.NewHeader().Set(gpf.FieldSchool, "Szkola Nadawcy"),
	})
	if err != nil {
		t.Fatalf("encode: %v", err)
	}

	response, err := api.ImportPlan(ctx, gen.ImportPlanRequestObject{
		Body: form(t, "file", "wrzesien.gpf", file, nil),
	})
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	result, ok := response.(gen.ImportPlan201JSONResponse)
	if !ok {
		t.Fatalf("import returned %T", response)
	}
	if result.Importer != goptivum.ID {
		t.Fatalf("importer = %v, want %s", result.Importer, goptivum.ID)
	}

	stored := storedDocument(t, api, result.PlanId, result.Version)
	if stored.GetNextArrangoId() != 900 {
		t.Errorf("next_arrango_id = %d, want the 900 the file carried",
			stored.GetNextArrangoId())
	}
}

// The desktop's notes (short names, the teacher pool, hour targets and year
// templates) reach an autosaved version and an imported .gpf as they came.
func TestEditorNotesSurviveAnUploadAndAnImport(t *testing.T) {
	api := newAPI(t)
	ctx := signedIn("identity-1")
	sent := document.New(&arrangov1.SchoolModel{
		Name:      "Szkola Nadawcy",
		Teachers:  []*arrangov1.Teacher{{Id: 1, Name: "AB"}, {Id: 2, Name: "CD"}},
		Subjects:  []*arrangov1.Subject{{Id: 3, Name: "Matematyka"}},
		Years:     []*arrangov1.Year{{Id: 4, Level: 1, Priority: 100}},
		Divisions: []*arrangov1.Division{{Id: 5, Name: "1a", YearId: 4}},
		Splits:    []*arrangov1.Split{{Id: 6, Name: "Języki", DivisionId: 5}},
	})
	sent.SubjectShortNames = []*goptivumv1.SubjectShortName{{SubjectId: 3, ShortName: "Mat"}}
	sent.TeacherPool = &goptivumv1.TeacherPool{Subjects: []*goptivumv1.SubjectPool{{SubjectId: 3, TeacherIds: []uint32{2}}}}
	sent.SchoolHourTarget = &goptivumv1.HourTarget{HalfPeriods: 36}
	sent.TeacherHourTargets = []*goptivumv1.TeacherHourTarget{{TeacherId: 2, HalfPeriods: 41}}
	sent.YearTemplates = []*goptivumv1.YearTemplate{{YearId: 4, Rows: []*goptivumv1.TemplateRow{
		{SubjectId: 3, HalfPeriods: 8}, {SubjectId: 3, SplitName: "Języki", HalfPeriods: 3},
	}}}
	kept := func(t *testing.T, stored *goptivumv1.Document) {
		t.Helper()
		if len(stored.GetSubjectShortNames()) != 1 || stored.GetSubjectShortNames()[0].GetShortName() != "Mat" {
			t.Errorf("short names = %v, want Mat for subject 3", stored.GetSubjectShortNames())
		}
		if pool := stored.GetTeacherPool().GetSubjects(); len(pool) != 1 || len(pool[0].GetTeacherIds()) != 1 || pool[0].GetTeacherIds()[0] != 2 {
			t.Errorf("pool = %v, want teacher 2 for subject 3", pool)
		}
		if !proto.Equal(stored.GetSchoolHourTarget(), sent.GetSchoolHourTarget()) {
			t.Errorf("school target = %v, want 36 half periods", stored.GetSchoolHourTarget())
		}
		if targets := stored.GetTeacherHourTargets(); len(targets) != 1 || !proto.Equal(targets[0], sent.GetTeacherHourTargets()[0]) {
			t.Errorf("teacher targets = %v, want teacher 2 at 41 half periods", targets)
		}
		if templates := stored.GetYearTemplates(); len(templates) != 1 || !proto.Equal(templates[0], sent.GetYearTemplates()[0]) {
			t.Errorf("templates = %v, want Rok 1's two rows", templates)
		}
	}

	plan := createPlan(t, api, ctx, "Plan 2027")
	encoded, err := document.Encode(sent)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	version := saved(t, save(t, api, ctx, plan.Id, encoded.Bytes, map[string]string{"parent": "1", "upload_key": "autosave-0000000042"}))
	kept(t, storedDocument(t, api, plan.Id, version.Version))

	file, err := gpf.Encode(sent, gpf.WriteOptions{Header: gpf.NewHeader().Set(gpf.FieldSchool, "Szkola Nadawcy")})
	if err != nil {
		t.Fatalf("gpf: %v", err)
	}
	response, err := api.ImportPlan(ctx, gen.ImportPlanRequestObject{Body: form(t, "file", "wrzesien.gpf", file, nil)})
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	imported, ok := response.(gen.ImportPlan201JSONResponse)
	if !ok {
		t.Fatalf("import returned %T", response)
	}
	kept(t, storedDocument(t, api, imported.PlanId, imported.Version))
}

// storedDocument reads a version back the way a client does.
func storedDocument(t *testing.T, api gen.StrictServerInterface, planID string, version int64) *goptivumv1.Document {
	t.Helper()
	downloaded, err := api.DownloadVersion(signedIn("identity-1"), gen.DownloadVersionRequestObject{
		PlanId: planID, Version: version,
	})
	if err != nil {
		t.Fatalf("download: %v", err)
	}
	body, ok := downloaded.(gen.DownloadVersion200ApplicationzstdResponse)
	if !ok {
		t.Fatalf("download returned %T", downloaded)
	}
	data, err := io.ReadAll(body.Body)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	doc, err := document.Decode(data)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	return doc
}

// A refused .gpf says why. Collapsing every refusal into one reason is what
// makes a person retry the same file three times, so each one arrives as its
// own catalog key with the parameters that key needs.
func TestARefusedGpfSaysWhy(t *testing.T) {
	api := newAPI(t)
	ctx := signedIn("identity-1")

	encrypted, err := gpf.Encode(document.New(&arrangov1.SchoolModel{Name: "Szkola"}),
		gpf.WriteOptions{
			Header:     gpf.NewHeader().Set(gpf.FieldSchool, "Szkola"),
			Passphrase: "correct horse", WorkFactor: 10,
		})
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	damaged := append([]byte(nil), encrypted...)
	// A length past the end of the file, which is malformed rather than locked.
	damaged[12], damaged[13], damaged[14], damaged[15] = 0xFF, 0xFF, 0xFF, 0x00

	for _, one := range []struct {
		name string
		file []byte
		want string
	}{
		{"locked", encrypted, "gpf.passwordRequired"},
		{"damaged", damaged, "gpf.malformed"},
	} {
		t.Run(one.name, func(t *testing.T) {
			response, err := api.ImportPlan(ctx, gen.ImportPlanRequestObject{
				Body: form(t, "file", "wrzesien.gpf", one.file, nil),
			})
			if err != nil {
				t.Fatalf("import: %v", err)
			}
			refused, ok := response.(gen.ImportPlan422JSONResponse)
			if !ok {
				t.Fatalf("import returned %T, want 422", response)
			}
			if refused.Message != one.want {
				t.Errorf("message = %q, want %q", refused.Message, one.want)
			}
		})
	}
}

// The parameters a message needs travel with it, or the desktop renders "this
// file belongs to " and stops.
func TestARefusalCarriesItsParameters(t *testing.T) {
	api := planAPIWithRealm(t, "liceum.pl")

	file, err := gpf.Encode(document.New(&arrangov1.SchoolModel{Name: "Szkola"}),
		gpf.WriteOptions{Header: gpf.NewHeader().Set(gpf.FieldRealm, "szkola.edu.pl")})
	if err != nil {
		t.Fatalf("encode: %v", err)
	}

	response, err := api.ImportPlan(signedIn("identity-1"), gen.ImportPlanRequestObject{
		Body: form(t, "file", "wrzesien.gpf", file, nil),
	})
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	refused, ok := response.(gen.ImportPlan422JSONResponse)
	if !ok {
		t.Fatalf("import returned %T, want 422", response)
	}
	if refused.Message != "gpf.wrongRealm" {
		t.Fatalf("message = %q, want gpf.wrongRealm", refused.Message)
	}
	if refused.Details == nil {
		t.Fatal("the refusal carries no parameters, so it cannot be rendered")
	}
	details := *refused.Details
	if details["File"] != "szkola.edu.pl" || details["Local"] != "liceum.pl" {
		t.Errorf("details = %v, want both schools named", details)
	}
}

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

	apiv1 "github.com/smegg99/goptivum/backend/api/v1"
	"github.com/smegg99/goptivum/backend/api/v1/gen"
	"github.com/smegg99/goptivum/backend/core/documents"
	"github.com/smegg99/goptivum/backend/formats/optivum/pla"
	"github.com/smegg99/goptivum/backend/internal/testsupport"
)

// signedIn is the context a guarded route reaches a handler with. Every route
// that gets here has passed kratosauth.Require.
func signedIn(identity string) context.Context {
	return kratosauth.WithIdentity(context.Background(), &kratosauth.Identity{ID: identity})
}

func createPlan(t *testing.T, api *apiv1.API, ctx context.Context, name string) gen.Plan {
	t.Helper()

	response, err := api.CreatePlan(ctx, gen.CreatePlanRequestObject{
		Body: &gen.PlanCreate{Name: name},
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

// Another owner's plan is not found rather than forbidden: whether it exists
// is not this caller's business.
func TestAnotherOwnersPlanIsNotFound(t *testing.T) {
	api := newAPI(t)
	mine := signedIn("identity-1")
	theirs := signedIn("identity-2")

	plan := createPlan(t, api, mine, "Plan ZSEM")

	read, err := api.GetPlan(theirs, gen.GetPlanRequestObject{PlanId: plan.Id})
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if _, ok := read.(gen.GetPlan404JSONResponse); !ok {
		t.Errorf("get returned %T, want 404", read)
	}

	deleted, err := api.DeletePlan(theirs, gen.DeletePlanRequestObject{PlanId: plan.Id})
	if err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, ok := deleted.(gen.DeletePlan404JSONResponse); !ok {
		t.Errorf("delete returned %T, want 404", deleted)
	}

	listed, err := api.ListPlans(theirs, gen.ListPlansRequestObject{})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if page := listed.(gen.ListPlans200JSONResponse); page.Total != 0 {
		t.Errorf("another owner sees %d plans", page.Total)
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
	created, _ := api.CreatePlan(ctx, gen.CreatePlanRequestObject{Body: &gen.PlanCreate{Name: "x"}})
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

	response, err := api.CreatePlan(ctx, gen.CreatePlanRequestObject{Body: &gen.PlanCreate{Name: "  "}})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, ok := response.(gen.CreatePlan422JSONResponse); !ok {
		t.Errorf("create returned %T, want 422", response)
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
	if result.Importer == nil || *result.Importer != "optivum.pla" {
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
	document, ok := downloaded.(gen.DownloadVersion200ApplicationzstdResponse)
	if !ok {
		t.Fatalf("download returned %T", downloaded)
	}
	data, err := io.ReadAll(document.Body)
	if err != nil {
		t.Fatalf("read document: %v", err)
	}
	model, err := documents.Decode(data)
	if err != nil {
		t.Fatalf("decode document: %v", err)
	}
	if len(model.GetLessons()) == 0 {
		t.Error("the stored document carries no lessons")
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
			"parent": "1", "valid_from": "2026-09-01",
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

	// The same parent again: someone saved first.
	stale, err := api.UploadVersion(ctx, gen.UploadVersionRequestObject{
		PlanId: first.PlanId,
		Body:   form(t, "document", "plan.gpf", data, map[string]string{"parent": "1"}),
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
		Body:   form(t, "document", "plan.gpf", []byte("not a document"), map[string]string{"parent": "0"}),
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

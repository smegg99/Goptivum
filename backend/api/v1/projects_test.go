// api/v1/projects_test.go

package v1_test

import (
	"bytes"
	"context"
	"io"
	"testing"
	"time"

	arrangov1 "github.com/smegg99/arrango/backend/gen/arrangov1"

	apiv1 "github.com/smegg99/goptivum/backend/api/v1"
	"github.com/smegg99/goptivum/backend/api/v1/gen"
	"github.com/smegg99/goptivum/backend/common/messages"
	"github.com/smegg99/goptivum/document"
)

// download reads one version back as the bytes that were stored.
func download(t *testing.T, api *apiv1.Handlers, ctx context.Context, planID string, version int64) []byte {
	t.Helper()
	response, err := api.DownloadVersion(ctx, gen.DownloadVersionRequestObject{PlanId: planID, Version: version})
	if err != nil {
		t.Fatalf("download: %v", err)
	}
	body, ok := response.(gen.DownloadVersion200ApplicationzstdResponse)
	if !ok {
		t.Fatalf("download returned %T", response)
	}
	data, err := io.ReadAll(body.Body)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	return data
}

func versions(t *testing.T, api *apiv1.Handlers, ctx context.Context, planID string) []gen.Version {
	t.Helper()
	response, err := api.ListVersions(ctx, gen.ListVersionsRequestObject{PlanId: planID})
	if err != nil {
		t.Fatalf("list versions: %v", err)
	}
	listed, ok := response.(gen.ListVersions200JSONResponse)
	if !ok {
		t.Fatalf("list versions returned %T", response)
	}
	return listed.Items
}

// A new project without a document is an empty school as version 1, so there
// is always something to open.
func TestCreateWithoutADocumentStoresAnEmptySchoolAsVersionOne(t *testing.T) {
	api := newAPI(t)
	ctx := signedIn("identity-1")

	plan := createPlan(t, api, ctx, "  Plan 2027 ")
	if plan.Name != "Plan 2027" || plan.SchoolYear != "2026/2027" {
		t.Errorf("created %+v, want the name trimmed", plan)
	}

	timeline := versions(t, api, ctx, plan.Id)
	if len(timeline) != 1 || timeline[0].Version != 1 || timeline[0].Parent != nil ||
		timeline[0].CreatedBy != "identity-1" {
		t.Fatalf("timeline = %+v, want version 1 alone", timeline)
	}
	doc, err := document.Decode(download(t, api, ctx, plan.Id, 1))
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if doc.GetModel().GetName() != "Plan 2027" {
		t.Errorf("the empty school is named %q, want the plan's name", doc.GetModel().GetName())
	}
	if len(doc.GetModel().GetLessons()) != 0 || len(doc.GetModel().GetDays()) != 0 || doc.GetSchedule() != nil {
		t.Error("an empty school carries lessons, days or a timetable")
	}
}

// With a document, version 1 is those bytes, unchanged.
func TestCreateWithADocumentStoresItAsVersionOne(t *testing.T) {
	api := newAPI(t)
	ctx := signedIn("identity-1")

	sent, err := document.Encode(document.New(&arrangov1.SchoolModel{
		Name:    "Szkoła Testowa",
		Lessons: []*arrangov1.LessonInstance{{Id: 1, Duration: 1}},
	}))
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	response, err := api.CreatePlan(ctx, gen.CreatePlanRequestObject{
		Body: form(t, "document", "Plan 2027.gpf", sent.Bytes, map[string]string{"name": "Plan 2027"}),
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	created, ok := response.(gen.CreatePlan201JSONResponse)
	if !ok {
		t.Fatalf("create returned %T", response)
	}
	if created.SchoolYear != "" {
		t.Errorf("school year = %q, want none when none was sent", created.SchoolYear)
	}
	if timeline := versions(t, api, ctx, created.Id); len(timeline) != 1 || timeline[0].Version != 1 {
		t.Fatalf("timeline = %+v, want version 1 alone", timeline)
	}
	if stored := download(t, api, ctx, created.Id, 1); !bytes.Equal(stored, sent.Bytes) {
		t.Error("version 1 is not the document that was sent")
	}
}

func TestCreateRefusesADocumentThatIsNotOne(t *testing.T) {
	api := newAPI(t)
	ctx := signedIn("identity-1")

	for _, body := range [][]byte{[]byte("not a document"), {}} {
		response, err := api.CreatePlan(ctx, gen.CreatePlanRequestObject{
			Body: form(t, "document", "plan.gpf", body, map[string]string{"name": "Plan 2027"}),
		})
		if err != nil {
			t.Fatalf("create: %v", err)
		}
		refused, ok := response.(gen.CreatePlan422JSONResponse)
		if !ok {
			t.Fatalf("create with %q returned %T, want 422", body, response)
		}
		if refused.Details == nil || (*refused.Details)["document"] != "unreadable" {
			t.Errorf("details = %v, want document: unreadable", refused.Details)
		}
	}
	listed, err := api.ListPlans(ctx, gen.ListPlansRequestObject{})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if listed.(gen.ListPlans200JSONResponse).Total != 0 {
		t.Error("a refused document left a plan behind")
	}
}

func TestUpdatePlanRenamesAndRefusesABlankName(t *testing.T) {
	api := newAPI(t)
	ctx := signedIn("identity-1")
	plan := createPlan(t, api, ctx, "Plan 2027")

	name := " Plan 2027 test "
	response, err := api.UpdatePlan(ctx, gen.UpdatePlanRequestObject{
		PlanId: plan.Id, Body: &gen.PlanUpdate{Name: &name},
	})
	if err != nil {
		t.Fatalf("rename: %v", err)
	}
	renamed, ok := response.(gen.UpdatePlan200JSONResponse)
	if !ok {
		t.Fatalf("rename returned %T", response)
	}
	if renamed.Name != "Plan 2027 test" || renamed.SchoolYear != "2026/2027" {
		t.Errorf("renamed = %+v, want the name trimmed and the year kept", renamed)
	}
	if renamed.UpdatedAt.Before(plan.UpdatedAt) {
		t.Errorf("updated_at went back: %v", renamed.UpdatedAt)
	}

	year := "2027/2028"
	response, err = api.UpdatePlan(ctx, gen.UpdatePlanRequestObject{
		PlanId: plan.Id, Body: &gen.PlanUpdate{SchoolYear: &year},
	})
	if err != nil {
		t.Fatalf("change year: %v", err)
	}
	if moved := response.(gen.UpdatePlan200JSONResponse); moved.Name != "Plan 2027 test" || moved.SchoolYear != year {
		t.Errorf("moved = %+v, want only the year changed", moved)
	}

	blank := "   "
	response, err = api.UpdatePlan(ctx, gen.UpdatePlanRequestObject{
		PlanId: plan.Id, Body: &gen.PlanUpdate{Name: &blank},
	})
	if err != nil {
		t.Fatalf("blank rename: %v", err)
	}
	refused, ok := response.(gen.UpdatePlan422JSONResponse)
	if !ok {
		t.Fatalf("a blank name returned %T, want 422", response)
	}
	if refused.Message != "error.validation" || refused.Details == nil || (*refused.Details)["name"] != "required" {
		t.Errorf("refusal = %+v, want error.validation with name: required", refused)
	}

	read, err := api.GetPlan(ctx, gen.GetPlanRequestObject{PlanId: plan.Id})
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got := read.(gen.GetPlan200JSONResponse); got.Name != "Plan 2027 test" {
		t.Errorf("after a refused rename the name is %q", got.Name)
	}

	other, err := api.UpdatePlan(signedIn("identity-2"), gen.UpdatePlanRequestObject{
		PlanId: plan.Id, Body: &gen.PlanUpdate{Name: &name},
	})
	if err != nil {
		t.Fatalf("rename by another owner: %v", err)
	}
	if refused, ok := other.(gen.UpdatePlan403JSONResponse); !ok || refused.Message != messages.KeyPlansNotYours {
		t.Errorf("another owner's rename returned %T, want 403 plans.notYours", other)
	}
}

// Delete moves a project to Trash, where it stays restorable until its removal
// date; everywhere else it is gone.
func TestDeleteMovesAPlanToTrashAndRestoreBringsItBack(t *testing.T) {
	api := newAPI(t)
	ctx := signedIn("identity-1")
	plan := createPlan(t, api, ctx, "Do usunięcia")
	kept := createPlan(t, api, ctx, "Plan 2027")

	before := time.Now().UTC().Add(-time.Second)
	deleted, err := api.DeletePlan(ctx, gen.DeletePlanRequestObject{PlanId: plan.Id})
	if err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, ok := deleted.(gen.DeletePlan204Response); !ok {
		t.Fatalf("delete returned %T", deleted)
	}

	if read, _ := api.GetPlan(ctx, gen.GetPlanRequestObject{PlanId: plan.Id}); !is[gen.GetPlan404JSONResponse](read) {
		t.Errorf("get of a trashed plan returned %T", read)
	}
	if listed, _ := api.ListVersions(ctx, gen.ListVersionsRequestObject{PlanId: plan.Id}); !is[gen.ListVersions404JSONResponse](listed) {
		t.Errorf("versions of a trashed plan returned %T", listed)
	}
	if again, _ := api.DeletePlan(ctx, gen.DeletePlanRequestObject{PlanId: plan.Id}); !is[gen.DeletePlan404JSONResponse](again) {
		t.Errorf("deleting twice returned %T", again)
	}
	page, _ := api.ListPlans(ctx, gen.ListPlansRequestObject{})
	if live := page.(gen.ListPlans200JSONResponse); live.Total != 1 || live.Items[0].Id != kept.Id {
		t.Errorf("list = %+v, want only the live plan", live)
	}

	trash := listTrash(t, api, ctx)
	if len(trash) != 1 {
		t.Fatalf("trash holds %d items", len(trash))
	}
	item := trash[0]
	if item.Kind != gen.TrashItemKindProject || item.PlanId != plan.Id || item.Name != "Do usunięcia" || item.SchoolYear != "2026/2027" {
		t.Errorf("item = %+v", item)
	}
	if item.DeletedAt.Before(before) {
		t.Errorf("deleted_at %v is before the delete", item.DeletedAt)
	}
	if want := item.DeletedAt.AddDate(0, 0, trashDays); !item.RemovesAt.Equal(want) {
		t.Errorf("removes_at = %v, want %v", item.RemovesAt, want)
	}
	if theirs := listTrash(t, api, signedIn("identity-2")); len(theirs) != 0 {
		t.Errorf("another owner sees %d items in Trash", len(theirs))
	}

	if stolen, _ := api.RestorePlan(signedIn("identity-2"), gen.RestorePlanRequestObject{PlanId: plan.Id}); !is[gen.RestorePlan403JSONResponse](stolen) {
		t.Errorf("another owner's restore returned %T, want 403", stolen)
	}
	restored, err := api.RestorePlan(ctx, gen.RestorePlanRequestObject{PlanId: plan.Id})
	if err != nil {
		t.Fatalf("restore: %v", err)
	}
	back, ok := restored.(gen.RestorePlan200JSONResponse)
	if !ok || back.Id != plan.Id || back.Name != "Do usunięcia" {
		t.Fatalf("restore returned %T %+v", restored, restored)
	}
	if timeline := versions(t, api, ctx, plan.Id); len(timeline) != 1 {
		t.Errorf("a restored plan has %d versions", len(timeline))
	}
	if trash := listTrash(t, api, ctx); len(trash) != 0 {
		t.Errorf("trash after restore holds %d items", len(trash))
	}
	// Undo may ask twice.
	if twice, _ := api.RestorePlan(ctx, gen.RestorePlanRequestObject{PlanId: plan.Id}); !is[gen.RestorePlan200JSONResponse](twice) {
		t.Errorf("restoring a live plan returned %T", twice)
	}
	if unknown, _ := api.RestorePlan(ctx, gen.RestorePlanRequestObject{PlanId: "banana"}); !is[gen.RestorePlan404JSONResponse](unknown) {
		t.Errorf("restoring an unreadable id returned %T", unknown)
	}
}

// Every plan is born with version 1, so an upload always names its parent.
func TestUploadNeedsAParent(t *testing.T) {
	api := newAPI(t)
	ctx := signedIn("identity-1")
	plan := createPlan(t, api, ctx, "Plan 2027")
	data := download(t, api, ctx, plan.Id, 1)

	for _, test := range []struct {
		fields map[string]string
		code   string
	}{
		{nil, "required"},
		{map[string]string{"parent": " "}, "required"},
		{map[string]string{"parent": "0"}, "invalid"},
		{map[string]string{"parent": "abc"}, "invalid"},
	} {
		response, err := api.UploadVersion(ctx, gen.UploadVersionRequestObject{
			PlanId: plan.Id, Body: form(t, "document", "plan.gpf", data, test.fields),
		})
		if err != nil {
			t.Fatalf("upload: %v", err)
		}
		refused, ok := response.(gen.UploadVersion422JSONResponse)
		if !ok {
			t.Fatalf("upload with %v returned %T, want 422", test.fields, response)
		}
		if refused.Details == nil || (*refused.Details)["parent"] != test.code {
			t.Errorf("upload with %v: details = %v, want parent: %s", test.fields, refused.Details, test.code)
		}
	}
}

func listTrash(t *testing.T, api *apiv1.Handlers, ctx context.Context) []gen.TrashItem {
	t.Helper()
	response, err := api.ListTrash(ctx, gen.ListTrashRequestObject{})
	if err != nil {
		t.Fatalf("list trash: %v", err)
	}
	listed, ok := response.(gen.ListTrash200JSONResponse)
	if !ok {
		t.Fatalf("list trash returned %T", response)
	}
	return listed.Items
}

// is reports whether a response is of one generated type.
func is[T any](response any) bool {
	_, ok := response.(T)
	return ok
}

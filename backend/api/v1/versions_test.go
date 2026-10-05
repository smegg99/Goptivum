// api/v1/versions_test.go

package v1_test

import (
	"bytes"
	"context"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/smegg99/s99ab/auth/kratosauth"
	"github.com/smegg99/s99ab/auth/kratosauth/kratosauthtest"
	"github.com/smegg99/s99ab/data/db"
	"github.com/smegg99/s99ab/data/storage"

	apiv1 "github.com/smegg99/goptivum/backend/api/v1"
	"github.com/smegg99/goptivum/backend/api/v1/gen"
	"github.com/smegg99/goptivum/backend/core/documents"
	"github.com/smegg99/goptivum/backend/core/plans"
	"github.com/smegg99/goptivum/backend/formats"
	"github.com/smegg99/goptivum/backend/migrations"
)

// save uploads data as the next version with the given form fields and
// returns the answer.
func save(t *testing.T, api *apiv1.Handlers, ctx context.Context, planID string, data []byte,
	fields map[string]string) gen.UploadVersionResponseObject {
	t.Helper()
	response, err := api.UploadVersion(ctx, gen.UploadVersionRequestObject{
		PlanId: planID, Body: form(t, "document", "plan.gpf", data, fields),
	})
	if err != nil {
		t.Fatalf("upload: %v", err)
	}
	return response
}

func saved(t *testing.T, response gen.UploadVersionResponseObject) gen.Version {
	t.Helper()
	version, ok := response.(gen.UploadVersion201JSONResponse)
	if !ok {
		t.Fatalf("upload returned %T %+v, want 201", response, response)
	}
	return gen.Version(version)
}

func refusedWith(t *testing.T, response gen.UploadVersionResponseObject, field, code string) {
	t.Helper()
	refused, ok := response.(gen.UploadVersion422JSONResponse)
	if !ok {
		t.Fatalf("upload returned %T, want 422 %s: %s", response, field, code)
	}
	if refused.Details == nil || (*refused.Details)[field] != code {
		t.Errorf("details = %v, want %s: %s", refused.Details, field, code)
	}
}

// A retry whose first answer was lost gets the same version back, not a
// conflict, and stores nothing twice.
func TestAnUploadSentTwiceLandsOnce(t *testing.T) {
	api := newAPI(t)
	ctx := signedIn("identity-1")
	plan := createPlan(t, api, ctx, "Plan 2027")
	data := download(t, api, ctx, plan.Id, 1)
	fields := map[string]string{"parent": "1", "upload_key": "autosave-0000000001"}

	first := saved(t, save(t, api, ctx, plan.Id, data, fields))
	again := saved(t, save(t, api, ctx, plan.Id, data, fields))
	if first.Version != 2 || again.Version != 2 || !again.CreatedAt.Equal(first.CreatedAt) {
		t.Errorf("first %+v, again %+v; want version 2 both times", first, again)
	}
	if listed := versions(t, api, ctx, plan.Id); len(listed) != 2 {
		t.Errorf("the timeline holds %d versions, want 2", len(listed))
	}

	fields["parent"] = "2"
	refusedWith(t, save(t, api, ctx, plan.Id, data, fields), "upload_key", "reused")
}

// The object a refused upload stored goes to the sweep's list instead of
// staying for ever.
func TestARefusedUploadsObjectIsListedForTheSweep(t *testing.T) {
	conn := db.TestDB(t, migrations.FS)
	api := &apiv1.Handlers{API: newPlanAPI(t, conn)}
	ctx := signedIn("identity-1")
	plan := createPlan(t, api, ctx, "Plan 2027")
	data := download(t, api, ctx, plan.Id, 1)
	saved(t, save(t, api, ctx, plan.Id, data, map[string]string{"parent": "1", "upload_key": "autosave-0000000001"}))

	other := createPlan(t, api, ctx, "Plan 2028")
	otherData := download(t, api, ctx, other.Id, 1)
	response := save(t, api, ctx, plan.Id, otherData, map[string]string{"parent": "1", "upload_key": "autosave-0000000002"})
	if !is[gen.UploadVersion409JSONResponse](response) {
		t.Fatalf("a stale parent returned %T, want 409", response)
	}
	var waiting int
	if err := conn.QueryRow(`select count(*) from object_garbage`).Scan(&waiting); err != nil || waiting != 1 {
		t.Errorf("object_garbage holds %d keys, %v; want the refused upload's", waiting, err)
	}
}

func TestAnUploadNeedsAWellFormedKey(t *testing.T) {
	api := newAPI(t)
	ctx := signedIn("identity-1")
	plan := createPlan(t, api, ctx, "Plan 2027")
	data := download(t, api, ctx, plan.Id, 1)

	refusedWith(t, save(t, api, ctx, plan.Id, data, map[string]string{"parent": "1"}), "upload_key", "required")
	refusedWith(t, save(t, api, ctx, plan.Id, data, map[string]string{
		"parent": "1", "upload_key": "short",
	}), "upload_key", "invalid")
}

func TestASummaryIsStoredAndListed(t *testing.T) {
	api := newAPI(t)
	ctx := signedIn("identity-1")
	plan := createPlan(t, api, ctx, "Plan 2027")
	data := download(t, api, ctx, plan.Id, 1)

	version := saved(t, save(t, api, ctx, plan.Id, data, map[string]string{
		"parent": "1", "upload_key": "autosave-0000000001",
		"summary": `{"added":{"day":5,"period":6,"class":3}}`,
	}))
	want := gen.VersionSummary{"added": {"day": 5, "period": 6, "class": 3}}
	if version.Summary == nil || !reflect.DeepEqual(*version.Summary, want) {
		t.Errorf("answered summary = %v, want %v", version.Summary, want)
	}
	listed := versions(t, api, ctx, plan.Id)
	if listed[1].Summary == nil || !reflect.DeepEqual(*listed[1].Summary, want) {
		t.Errorf("listed summary = %v, want %v", listed[1].Summary, want)
	}
	if listed[0].Summary != nil {
		t.Errorf("version 1 lists summary %v, want none", *listed[0].Summary)
	}

	refusedWith(t, save(t, api, ctx, plan.Id, data, map[string]string{
		"parent": "2", "upload_key": "autosave-0000000002", "summary": `"dodano 3 klasy"`,
	}), "summary", "invalid")
}

// A save that does not send the validity keeps the parent's (G11), an empty
// value clears it, and a date that does not parse is refused (G23).
func TestAnUploadKeepsTheParentsValidityUnlessItSaysOtherwise(t *testing.T) {
	api := newAPI(t)
	ctx := signedIn("identity-1")
	plan := createPlan(t, api, ctx, "Plan 2027")
	data := download(t, api, ctx, plan.Id, 1)

	dated := saved(t, save(t, api, ctx, plan.Id, data, map[string]string{
		"parent": "1", "upload_key": "autosave-0000000001",
		"valid_from": "2026-09-01", "valid_to": "2027-06-26",
	}))
	kept := saved(t, save(t, api, ctx, plan.Id, data, map[string]string{
		"parent": "2", "upload_key": "autosave-0000000002",
	}))
	if kept.ValidFrom == nil || kept.ValidFrom.Time != dated.ValidFrom.Time ||
		kept.ValidTo == nil || kept.ValidTo.Time != dated.ValidTo.Time {
		t.Errorf("kept validity = %v..%v, want %v..%v", kept.ValidFrom, kept.ValidTo, dated.ValidFrom, dated.ValidTo)
	}
	cleared := saved(t, save(t, api, ctx, plan.Id, data, map[string]string{
		"parent": "3", "upload_key": "autosave-0000000003", "valid_to": "",
	}))
	if cleared.ValidFrom == nil || cleared.ValidTo != nil {
		t.Errorf("cleared validity = %v..%v, want the start kept and no end", cleared.ValidFrom, cleared.ValidTo)
	}

	for _, field := range []string{"valid_from", "valid_to"} {
		refusedWith(t, save(t, api, ctx, plan.Id, data, map[string]string{
			"parent": "4", "upload_key": "autosave-0000000004", field: "1 września",
		}), field, "invalid")
	}
}

// planAPIWithLimit is the plan half with a small upload limit.
func planAPIWithLimit(t *testing.T, limit int64) *apiv1.Handlers {
	t.Helper()
	api, err := apiv1.NewAPI(
		plans.NewRepository(db.TestDB(t, migrations.FS), db.SQLite, plans.Caps{BackupsKeep: 100, ResultsKeep: 20}),
		documents.NewStore(storage.NewLocalDir(t.TempDir(), "/objects")),
		formats.NewRegistry(),
		nil, apiv1.Settings{TrashDays: fixedTrashDays, MaxUploadSize: limit},
	)
	if err != nil {
		t.Fatalf("new api: %v", err)
	}
	return &apiv1.Handlers{API: api}
}

// A part over the configured limit is 413, not a part cut short and refused
// as unreadable (G21); so is a body the upload cap stops mid-read.
func TestAnUploadOverTheLimitIsTooLarge(t *testing.T) {
	api := planAPIWithLimit(t, 4096)
	ctx := signedIn("identity-1")
	plan := createPlan(t, api, ctx, "Plan 2027")
	data := download(t, api, ctx, plan.Id, 1)
	if len(data) >= 4096 {
		t.Fatalf("the empty school is %d bytes, which this test assumes is under the limit", len(data))
	}

	big := bytes.Repeat([]byte("x"), 5000)
	response := save(t, api, ctx, plan.Id, big, map[string]string{"parent": "1", "upload_key": "autosave-0000000001"})
	refused, ok := response.(gen.UploadVersion413JSONResponse)
	if !ok {
		t.Fatalf("an oversized part returned %T, want 413", response)
	}
	if refused.Message != "error.requestTooLarge" || refused.Code != "too_large" {
		t.Errorf("413 body = %+v", refused)
	}
	saved(t, save(t, api, ctx, plan.Id, data, map[string]string{"parent": "1", "upload_key": "autosave-0000000002"}))

	// The body cap the upload routes carry surfaces as a read error.
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, _ := writer.CreateFormFile("document", "plan.gpf")
	part.Write(big)
	writer.Close()
	capped := http.MaxBytesReader(httptest.NewRecorder(), ioNopCloser{&body}, 4096)
	created, err := api.CreatePlan(ctx, gen.CreatePlanRequestObject{
		Body: multipart.NewReader(capped, writer.Boundary()),
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if !is[gen.CreatePlan413JSONResponse](created) {
		t.Errorf("a body over the cap returned %T, want 413", created)
	}
}

type ioNopCloser struct{ *bytes.Buffer }

func (ioNopCloser) Close() error { return nil }

func TestACheckpointIsNamedDeletedAndRestored(t *testing.T) {
	api := newAPI(t)
	ctx := signedIn("identity-1")
	plan := createPlan(t, api, ctx, "Plan 2027")

	named, err := api.NameCheckpoint(ctx, gen.NameCheckpointRequestObject{
		PlanId: plan.Id, Version: 1, Body: &gen.NameCheckpointJSONRequestBody{Name: "tydzień i klasy"},
	})
	if err != nil {
		t.Fatalf("name: %v", err)
	}
	version, ok := named.(gen.NameCheckpoint200JSONResponse)
	if !ok || version.Checkpoint == nil || *version.Checkpoint != "tydzień i klasy" {
		t.Fatalf("name returned %T %+v", named, named)
	}
	if listed := versions(t, api, ctx, plan.Id); listed[0].Checkpoint == nil {
		t.Error("listVersions does not show the checkpoint")
	}

	blank, err := api.NameCheckpoint(ctx, gen.NameCheckpointRequestObject{
		PlanId: plan.Id, Version: 1, Body: &gen.NameCheckpointJSONRequestBody{Name: "  "},
	})
	if err != nil || !is[gen.NameCheckpoint422JSONResponse](blank) {
		t.Errorf("a blank name returned %T, %v; want 422", blank, err)
	}

	deleted, err := api.DeleteCheckpoint(ctx, gen.DeleteCheckpointRequestObject{PlanId: plan.Id, Version: 1})
	if err != nil || !is[gen.DeleteCheckpoint204Response](deleted) {
		t.Fatalf("delete returned %T, %v", deleted, err)
	}
	if listed := versions(t, api, ctx, plan.Id); len(listed) != 1 || listed[0].Checkpoint != nil {
		t.Errorf("after delete, versions = %+v; want version 1 listed plain", listed)
	}
	trash := listTrash(t, api, ctx)
	if len(trash) != 1 || trash[0].Kind != gen.TrashItemKindCheckpoint || trash[0].PlanId != plan.Id ||
		trash[0].Name != "Plan 2027" || trash[0].Version == nil || *trash[0].Version != 1 ||
		trash[0].Checkpoint == nil || *trash[0].Checkpoint != "tydzień i klasy" {
		t.Fatalf("trash = %+v", trash)
	}
	if want := trash[0].DeletedAt.AddDate(0, 0, trashDays); !trash[0].RemovesAt.Equal(want) {
		t.Errorf("removes at %v, want %v", trash[0].RemovesAt, want)
	}
	again, err := api.DeleteCheckpoint(ctx, gen.DeleteCheckpointRequestObject{PlanId: plan.Id, Version: 1})
	if err != nil || !is[gen.DeleteCheckpoint404JSONResponse](again) {
		t.Errorf("deleting it again returned %T, %v; want 404", again, err)
	}

	for attempt := 0; attempt < 2; attempt++ {
		restored, err := api.RestoreCheckpoint(ctx, gen.RestoreCheckpointRequestObject{PlanId: plan.Id, Version: 1})
		if err != nil {
			t.Fatalf("restore: %v", err)
		}
		back, ok := restored.(gen.RestoreCheckpoint200JSONResponse)
		if !ok || back.Checkpoint == nil || *back.Checkpoint != "tydzień i klasy" {
			t.Errorf("restore %d returned %T %+v", attempt+1, restored, restored)
		}
	}
	if trash := listTrash(t, api, ctx); len(trash) != 0 {
		t.Errorf("after restore, trash = %+v", trash)
	}
}

func TestCheckpointRoutesAreTheOwnersOnly(t *testing.T) {
	api := newAPI(t)
	plan := createPlan(t, api, signedIn("identity-1"), "Plan 2027")
	theirs := signedIn("identity-2")

	named, err := api.NameCheckpoint(theirs, gen.NameCheckpointRequestObject{
		PlanId: plan.Id, Version: 1, Body: &gen.NameCheckpointJSONRequestBody{Name: "moje"},
	})
	if err != nil || !is[gen.NameCheckpoint403JSONResponse](named) {
		t.Errorf("another owner naming returned %T, %v; want 403", named, err)
	}
	deleted, err := api.DeleteCheckpoint(theirs, gen.DeleteCheckpointRequestObject{PlanId: plan.Id, Version: 1})
	if err != nil || !is[gen.DeleteCheckpoint403JSONResponse](deleted) {
		t.Errorf("another owner deleting returned %T, %v; want 403", deleted, err)
	}
	restored, err := api.RestoreCheckpoint(theirs, gen.RestoreCheckpointRequestObject{PlanId: plan.Id, Version: 1})
	if err != nil || !is[gen.RestoreCheckpoint403JSONResponse](restored) {
		t.Errorf("another owner restoring returned %T, %v; want 403", restored, err)
	}
	missing, err := api.NameCheckpoint(signedIn("identity-1"), gen.NameCheckpointRequestObject{
		PlanId: plan.Id, Version: 7, Body: &gen.NameCheckpointJSONRequestBody{Name: "brak"},
	})
	if err != nil || !is[gen.NameCheckpoint404JSONResponse](missing) {
		t.Errorf("naming a missing version returned %T, %v; want 404", missing, err)
	}
}

// The plan's own cap is set through PATCH and bounds what a save keeps.
func TestAPlansBackupsCapIsSetAndApplied(t *testing.T) {
	api := newAPI(t)
	ctx := signedIn("identity-1")
	plan := createPlan(t, api, ctx, "Plan 2027")
	if plan.BackupsKeep != 0 || plan.BackupsKeepDefault != 100 {
		t.Errorf("a new plan has cap %d, default %d; want 0 and the server's 100",
			plan.BackupsKeep, plan.BackupsKeepDefault)
	}

	update := func(keep int) gen.UpdatePlanResponseObject {
		response, err := api.UpdatePlan(ctx, gen.UpdatePlanRequestObject{
			PlanId: plan.Id, Body: &gen.UpdatePlanJSONRequestBody{BackupsKeep: &keep},
		})
		if err != nil {
			t.Fatalf("update: %v", err)
		}
		return response
	}
	for _, keep := range []int{-1, 10001} {
		refused, ok := update(keep).(gen.UpdatePlan422JSONResponse)
		if !ok || refused.Details == nil || (*refused.Details)["backups_keep"] != "invalid" {
			t.Errorf("cap %d: returned %+v, want 422 backups_keep: invalid", keep, refused)
		}
	}
	set, ok := update(1).(gen.UpdatePlan200JSONResponse)
	if !ok || set.BackupsKeep != 1 {
		t.Fatalf("setting a cap of 1 returned %+v", set)
	}

	data := download(t, api, ctx, plan.Id, 1)
	for parent, key := range []string{"autosave-0000000001", "autosave-0000000002", "autosave-0000000003"} {
		saved(t, save(t, api, ctx, plan.Id, data, map[string]string{
			"parent": strconv.Itoa(parent + 1), "upload_key": key,
		}))
	}
	listed := versions(t, api, ctx, plan.Id)
	if len(listed) != 2 || listed[0].Version != 3 || listed[1].Version != 4 {
		t.Errorf("with a cap of 1, versions = %+v; want 3 and 4", listed)
	}

	if dropped, ok := update(0).(gen.UpdatePlan200JSONResponse); !ok || dropped.BackupsKeep != 0 {
		t.Errorf("dropping the cap returned %+v", dropped)
	}
}

// The checkpoint routes exist and sit behind the session check: a caller
// without one is 401, where an unrouted path would be 404.
func TestCheckpointRoutesNeedASession(t *testing.T) {
	fake := kratosauthtest.Fake(t, map[string]kratosauth.Identity{bearer: {ID: "headmaster"}})
	server := newGuardedServer(t, fake, "")

	for _, route := range []struct{ method, path string }{
		{http.MethodPut, "/api/v1/plans/1/versions/1/checkpoint"},
		{http.MethodDelete, "/api/v1/plans/1/versions/1/checkpoint"},
		{http.MethodPost, "/api/v1/plans/1/versions/1/checkpoint/restore"},
	} {
		recorder := httptest.NewRecorder()
		server.Handler().ServeHTTP(recorder, httptest.NewRequest(route.method, route.path, nil))
		if recorder.Code != http.StatusUnauthorized {
			t.Errorf("%s %s = %d, want 401", route.method, route.path, recorder.Code)
		}
	}

	request := httptest.NewRequest(http.MethodPut, "/api/v1/plans/1/versions/1/checkpoint",
		strings.NewReader(`{"name":"tydzień"}`))
	request.Header.Set("Authorization", bearer)
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	server.Handler().ServeHTTP(recorder, request)
	if recorder.Code != http.StatusNotFound || errorKey(t, recorder.Body.Bytes()) != "error.notFound" {
		t.Errorf("naming a version of no plan = %d %s, want 404", recorder.Code, recorder.Body.String())
	}
}

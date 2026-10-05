// api/v1/sharing_test.go

package v1_test

import (
	"bytes"
	"context"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/smegg99/s99ab/auth/kratosauth"
	"github.com/smegg99/s99ab/data/db"

	"github.com/smegg99/goptivum/backend/api/v1/gen"
	"github.com/smegg99/goptivum/backend/common/messages"
	"github.com/smegg99/goptivum/backend/core/plans"
	"github.com/smegg99/goptivum/backend/identity"
	"github.com/smegg99/goptivum/backend/migrations"
)

// uploadAs sends doc as the next version of the plan at path, as one account.
func uploadAs(t *testing.T, office *backOffice, path, credential string, doc []byte) *httptest.ResponseRecorder {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	_ = writer.WriteField("parent", "1")
	_ = writer.WriteField("upload_key", "sharing-test-upload-01")
	part, _ := writer.CreateFormFile("document", "plan.gpf")
	_, _ = part.Write(doc)
	_ = writer.Close()
	request := httptest.NewRequest(http.MethodPost, path, &body)
	request.Header.Set("Content-Type", writer.FormDataContentType())
	request.Header.Set("Authorization", credential)
	recorder := httptest.NewRecorder()
	office.server.Handler().ServeHTTP(recorder, request)
	return recorder
}

// refusedAsNotYours asserts one answer is 403 plans.notYours.
func refusedAsNotYours(t *testing.T, office *backOffice, method, path, body string) {
	t.Helper()
	recorder := call(t, office.server, method, path, asPiotr, body)
	var envelope gen.Error
	if recorder.Code != http.StatusForbidden || json.Unmarshal(recorder.Body.Bytes(), &envelope) != nil ||
		envelope.Message != messages.KeyPlansNotYours {
		t.Errorf("%s %s by another account = %d %s, want 403 %s", method, path,
			recorder.Code, recorder.Body.String(), messages.KeyPlansNotYours)
	}
}

// planByID finds one plan in a listing.
func planByID(t *testing.T, page gen.PlanPage, id string) gen.Plan {
	t.Helper()
	for _, plan := range page.Items {
		if plan.Id == id {
			return plan
		}
	}
	t.Fatalf("plan %s is not listed in %+v", id, page.Items)
	return gen.Plan{}
}

// Karol's test 1: everyone sees and opens every project, and only its owner
// changes it.
func TestEveryAccountReadsEveryPlanAndOnlyTheOwnerChangesIt(t *testing.T) {
	office := newBackOffice(t)
	plan := newProject(t, office, asAnna, "Plan 2027")
	if plan.Owner.Id != "identity-anna" || plan.Owner.Name == nil || *plan.Owner.Name != "Anna Kowalska" ||
		!plan.CanEdit || !plan.CanDelete || plan.Origin != nil {
		t.Errorf("created = %+v", plan)
	}

	seen := planByID(t, answer[gen.PlanPage](t, office, http.MethodGet, "/api/v1/plans", asPiotr, ""), plan.Id)
	if seen.CanEdit || seen.CanDelete || seen.Owner.Id != "identity-anna" ||
		seen.Owner.Name == nil || *seen.Owner.Name != "Anna Kowalska" {
		t.Errorf("Piotr sees %+v, want Anna's plan he cannot change", seen)
	}
	if own := planByID(t, answer[gen.PlanPage](t, office, http.MethodGet, "/api/v1/plans", asAnna, ""), plan.Id); !own.CanEdit || !own.CanDelete {
		t.Errorf("Anna sees her own plan as %+v", own)
	}
	if read := answer[gen.Plan](t, office, http.MethodGet, "/api/v1/plans/"+plan.Id, asPiotr, ""); read.CanEdit || read.Name != "Plan 2027" {
		t.Errorf("Piotr reads %+v", read)
	}
	versions := answer[gen.VersionList](t, office, http.MethodGet, "/api/v1/plans/"+plan.Id+"/versions", asPiotr, "")
	if len(versions.Items) != 1 {
		t.Errorf("Piotr sees versions %+v", versions.Items)
	}
	download := call(t, office.server, http.MethodGet, "/api/v1/plans/"+plan.Id+"/versions/1", asPiotr, "")
	if download.Code != http.StatusOK {
		t.Fatalf("Piotr's download = %d", download.Code)
	}

	base := "/api/v1/plans/" + plan.Id
	refusedAsNotYours(t, office, http.MethodPatch, base, `{"name":"Plan Piotra"}`)
	refusedAsNotYours(t, office, http.MethodPut, base+"/versions/1/checkpoint", `{"name":"moje"}`)
	refusedAsNotYours(t, office, http.MethodDelete, base+"/versions/1/checkpoint", "")
	refusedAsNotYours(t, office, http.MethodPost, base+"/versions/1/checkpoint/restore", "")
	refusedAsNotYours(t, office, http.MethodPost, base+"/restore", "")
	refusedAsNotYours(t, office, http.MethodDelete, base, "")
	recorder := uploadAs(t, office, base+"/versions", asPiotr, download.Body.Bytes())
	if recorder.Code != http.StatusForbidden || !strings.Contains(recorder.Body.String(), messages.KeyPlansNotYours) {
		t.Errorf("Piotr's upload = %d %s, want 403", recorder.Code, recorder.Body.String())
	}

	after := answer[gen.Plan](t, office, http.MethodGet, base, asAnna, "")
	if after.Name != "Plan 2027" {
		t.Errorf("after refused changes the plan is %+v", after)
	}
	if entries := auditPage(t, office, "?kind=project"); entries.Total != 1 {
		t.Errorf("refused changes were audited: %+v", entries.Items)
	}

	// In Trash it is Anna's alone: not found for Piotr, not in his Trash,
	// and not his to restore.
	deleteProject(t, office, asAnna, plan)
	if gone := call(t, office.server, http.MethodGet, base, asPiotr, ""); gone.Code != http.StatusNotFound {
		t.Errorf("Piotr reads a plan in Trash: %d", gone.Code)
	}
	if trash := answer[gen.TrashList](t, office, http.MethodGet, "/api/v1/trash", asPiotr, ""); len(trash.Items) != 0 {
		t.Errorf("Piotr's Trash holds %+v", trash.Items)
	}
	if trash := answer[gen.TrashList](t, office, http.MethodGet, "/api/v1/trash", asAnna, ""); len(trash.Items) != 1 {
		t.Errorf("Anna's Trash holds %+v", trash.Items)
	}
	refusedAsNotYours(t, office, http.MethodPost, base+"/restore", "")
}

// Karol's tests 1, 2 and 5: Fork makes an own copy that says where it came
// from; on one's own plan the same route duplicates it.
func TestForkMakesAnOwnCopyThatSaysWhereItCameFrom(t *testing.T) {
	office := newBackOffice(t)
	ctx := t.Context()
	origin := newProject(t, office, asAnna, "Plan 2027")
	originID := mustID(t, origin.Id)
	first, err := office.index.Latest(ctx, originID)
	if err != nil {
		t.Fatalf("latest: %v", err)
	}
	if _, err := office.index.AddVersion(ctx, plans.Upload{
		PlanID: originID, Parent: 1, ObjectKey: first.ObjectKey, SizeBytes: first.SizeBytes,
		CreatedBy: "identity-anna", UploadKey: "sharing-test-second-01",
	}); err != nil {
		t.Fatalf("second version: %v", err)
	}
	path := "/api/v1/plans/" + origin.Id + "/fork"

	fork := answer[gen.Plan](t, office, http.MethodPost, path, asPiotr, `{"name":"Plan 2027 (fork)"}`)
	if fork.Id == origin.Id || fork.Name != "Plan 2027 (fork)" || fork.SchoolYear != "2026/2027" ||
		fork.Owner.Id != "identity-piotr" || fork.Owner.Name == nil || *fork.Owner.Name != "piotr" ||
		!fork.CanEdit || !fork.CanDelete {
		t.Errorf("fork = %+v", fork)
	}
	if fork.Origin == nil || fork.Origin.PlanId != origin.Id || fork.Origin.Version != 2 ||
		fork.Origin.Name == nil || *fork.Origin.Name != "Plan 2027" {
		t.Errorf("origin = %+v, want Plan 2027 version 2", fork.Origin)
	}
	if renamed := answer[gen.Plan](t, office, http.MethodPatch, "/api/v1/plans/"+fork.Id, asPiotr, `{"name":"Plan Piotra"}`); renamed.Name != "Plan Piotra" {
		t.Errorf("Piotr cannot rename his fork: %+v", renamed)
	}
	if download := call(t, office.server, http.MethodGet, "/api/v1/plans/"+fork.Id+"/versions/1", asPiotr, ""); download.Code != http.StatusOK {
		t.Errorf("the fork's version 1 = %d", download.Code)
	}

	// Karol's test 2: the admin sees whose it is and where it came from.
	seen := planByID(t, answer[gen.PlanPage](t, office, http.MethodGet, "/api/v1/plans", asAnna, ""), fork.Id)
	if seen.CanEdit || seen.Owner.Name == nil || *seen.Owner.Name != "piotr" ||
		seen.Origin == nil || seen.Origin.Name == nil || *seen.Origin.Name != "Plan 2027" || seen.Origin.Version != 2 {
		t.Errorf("Anna sees the fork as %+v", seen)
	}

	// A chosen version, and no name: the server's default.
	older := answer[gen.Plan](t, office, http.MethodPost, path, asPiotr, `{"version":1}`)
	if older.Name != "Plan 2027 (fork)" || older.Origin == nil || older.Origin.Version != 1 {
		t.Errorf("fork of version 1 = %+v", older)
	}

	// Karol's test 5: a duplicate of one's own plan.
	duplicate := answer[gen.Plan](t, office, http.MethodPost, path, asAnna, `{"name":"Plan 2027 kopia"}`)
	if duplicate.Owner.Id != "identity-anna" || !duplicate.CanEdit || duplicate.Origin == nil ||
		duplicate.Origin.PlanId != origin.Id {
		t.Errorf("duplicate = %+v", duplicate)
	}
	deleteProject(t, office, asAnna, duplicate)

	for _, refusal := range []struct {
		name, path, body string
		want             int
	}{
		{"a version the plan lacks", path, `{"version":9}`, http.StatusNotFound},
		{"no such plan", "/api/v1/plans/424242/fork", `{}`, http.StatusNotFound},
		{"a blank name", path, `{"name":"   "}`, http.StatusUnprocessableEntity},
		{"no body", path, "", http.StatusBadRequest},
	} {
		if recorder := call(t, office.server, http.MethodPost, refusal.path, asPiotr, refusal.body); recorder.Code != refusal.want {
			t.Errorf("%s = %d %s, want %d", refusal.name, recorder.Code, recorder.Body.String(), refusal.want)
		}
	}

	page := auditPage(t, office, "?kind=project")
	var forked *gen.AuditEntry
	for i, entry := range page.Items {
		if entry.Action == gen.AuditActionProjectCreated && entry.Target.Id == fork.Id {
			forked = &page.Items[i]
		}
	}
	if forked == nil || forked.Details["source"] != "fork" || forked.Details["origin"] != origin.Id ||
		forked.Details["origin_name"] != "Plan 2027" || forked.Details["version"] != "2" {
		t.Errorf("the fork's audit entry = %+v", forked)
	}

	// The origin in Trash: the copy still says which plan and version, no
	// longer by name, and a plan in Trash cannot be forked.
	deleteProject(t, office, asAnna, origin)
	after := answer[gen.Plan](t, office, http.MethodGet, "/api/v1/plans/"+fork.Id, asPiotr, "")
	if after.Origin == nil || after.Origin.PlanId != origin.Id || after.Origin.Version != 2 || after.Origin.Name != nil {
		t.Errorf("origin after its deletion = %+v", after.Origin)
	}
	if recorder := call(t, office.server, http.MethodPost, path, asPiotr, `{}`); recorder.Code != http.StatusNotFound {
		t.Errorf("fork of a plan in Trash = %d", recorder.Code)
	}
}

// A default name that would be too long keeps the suffix and shortens the
// origin's name.
func TestAForksDefaultNameFitsTheLimit(t *testing.T) {
	office := newBackOffice(t)
	long := strings.Repeat("ż", plans.MaxNameLength)
	origin := newProject(t, office, asAnna, long)
	fork := answer[gen.Plan](t, office, http.MethodPost, "/api/v1/plans/"+origin.Id+"/fork", asPiotr, `{}`)
	if n := len([]rune(fork.Name)); n != plans.MaxNameLength || !strings.HasSuffix(fork.Name, " (fork)") ||
		!strings.HasPrefix(fork.Name, "żż") {
		t.Errorf("default name has %d letters: %q", n, fork.Name)
	}
}

// Karol's test 3: an administrator gives a plan to another account, and the
// previous owner reads it like everyone else.
func TestAnAdministratorTransfersAPlan(t *testing.T) {
	office := newBackOffice(t)
	plan := newProject(t, office, asPiotr, "Plan Piotra")
	path := "/api/v1/plans/" + plan.Id + "/owner"

	if recorder := call(t, office.server, http.MethodPut, path, asPiotr, `{"owner_id":"identity-anna"}`); recorder.Code != http.StatusNotFound {
		t.Errorf("a planner's transfer = %d, want 404", recorder.Code)
	}
	for _, refusal := range []struct{ body, field, code string }{
		{`{"owner_id":"nobody"}`, "owner_id", "unknown"},
		{`{"owner_id":"identity-ewa"}`, "owner_id", "disabled"},
		{`{"owner_id":"  "}`, "owner_id", "required"},
	} {
		recorder := call(t, office.server, http.MethodPut, path, asAnna, refusal.body)
		var envelope gen.Error
		if recorder.Code != http.StatusUnprocessableEntity || json.Unmarshal(recorder.Body.Bytes(), &envelope) != nil ||
			envelope.Details == nil || (*envelope.Details)[refusal.field] != refusal.code {
			t.Errorf("transfer %s = %d %s, want 422 %s: %s", refusal.body, recorder.Code, recorder.Body.String(),
				refusal.field, refusal.code)
		}
	}
	if same := answer[gen.Plan](t, office, http.MethodPut, path, asAnna, `{"owner_id":"identity-piotr"}`); same.Owner.Id != "identity-piotr" {
		t.Errorf("a transfer to the owner = %+v", same)
	}

	moved := answer[gen.Plan](t, office, http.MethodPut, path, asAnna, `{"owner_id":"identity-anna"}`)
	if moved.Owner.Id != "identity-anna" || moved.Owner.Name == nil || *moved.Owner.Name != "Anna Kowalska" || !moved.CanEdit {
		t.Errorf("moved = %+v", moved)
	}
	if read := answer[gen.Plan](t, office, http.MethodGet, "/api/v1/plans/"+plan.Id, asPiotr, ""); read.CanEdit || read.CanDelete {
		t.Errorf("Piotr still may change %+v", read)
	}
	refusedAsNotYours(t, office, http.MethodPatch, "/api/v1/plans/"+plan.Id, `{"name":"Nadal moje"}`)

	page := auditPage(t, office, "?kind=project")
	transfers := 0
	for _, entry := range page.Items {
		if entry.Action != gen.AuditActionProjectTransferred {
			continue
		}
		transfers++
		if entry.Target.Id != plan.Id || entry.Actor.Id != "identity-anna" ||
			entry.Details["from_id"] != "identity-piotr" || entry.Details["from"] != "piotr" ||
			entry.Details["to_id"] != "identity-anna" || entry.Details["to"] != "Anna Kowalska" {
			t.Errorf("transfer entry = %+v", entry)
		}
	}
	if transfers != 1 {
		t.Errorf("%d transfers audited, want 1 (the one that changed the owner)", transfers)
	}

	deleteProject(t, office, asAnna, answer[gen.Plan](t, office, http.MethodGet, "/api/v1/plans/"+plan.Id, asAnna, ""))
	if recorder := call(t, office.server, http.MethodPut, path, asAnna, `{"owner_id":"identity-piotr"}`); recorder.Code != http.StatusNotFound {
		t.Errorf("transfer of a plan in Trash = %d, want 404", recorder.Code)
	}
}

// The owners' names come from Kratos; when it does not answer the plans are
// listed without them, and the caller's own name still comes from its session.
func TestPlansAreListedWhenKratosDoesNotName(t *testing.T) {
	down, err := identity.NewAdmin("http://127.0.0.1:1", time.Second)
	if err != nil {
		t.Fatalf("admin client: %v", err)
	}
	api := newAPI(t)
	api.API = newPlanAPI(t, db.TestDB(t, migrations.FS)).WithAccounts(down)
	anna := kratosauth.WithIdentity(t.Context(), &kratosauth.Identity{
		ID: "identity-anna", Traits: json.RawMessage(`{"username":"anna","name":"Anna Kowalska"}`),
	})
	plan := createPlan(t, api, anna, "Plan 2027")

	listed := func(ctx context.Context) gen.Plan {
		t.Helper()
		response, err := api.ListPlans(ctx, gen.ListPlansRequestObject{})
		page, ok := response.(gen.ListPlans200JSONResponse)
		if err != nil || !ok {
			t.Fatalf("list = %T, %v", response, err)
		}
		return planByID(t, gen.PlanPage(page), plan.Id)
	}
	if seen := listed(signedIn("identity-piotr")); seen.Owner.Id != "identity-anna" || seen.Owner.Name != nil {
		t.Errorf("seen = %+v, want the owner's id without a name", seen)
	}
	if own := listed(anna); own.Owner.Name == nil || *own.Owner.Name != "Anna Kowalska" {
		t.Errorf("own = %+v, want the caller's own name from its session", own)
	}
}

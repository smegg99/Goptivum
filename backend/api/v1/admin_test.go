// api/v1/admin_test.go

package v1_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/smegg99/s99ab/auth/kratosauth"
	"github.com/smegg99/s99ab/auth/kratosauth/kratosauthtest"
	"github.com/smegg99/s99ab/data/db"

	apiv1 "github.com/smegg99/goptivum/backend/api/v1"
	"github.com/smegg99/goptivum/backend/api/v1/gen"
	"github.com/smegg99/goptivum/backend/arrango"
	"github.com/smegg99/goptivum/backend/core/audit"
	"github.com/smegg99/goptivum/backend/identity"
	"github.com/smegg99/goptivum/backend/identity/identitytest"
	"github.com/smegg99/goptivum/backend/migrations"
	"github.com/smegg99/goptivum/solveclient"

	arrangoid "github.com/smegg99/arrango/backend/identity"
)

// Two callers: one an administrator by metadata_public, one a planner who
// claims the role in traits, which is the value a user can write themselves.
const (
	asAdmin   = "Bearer admin-token"
	asPlanner = "Bearer planner-token"
)

func adminFake(t *testing.T) *kratosauth.Client {
	t.Helper()
	return kratosauthtest.Fake(t, map[string]kratosauth.Identity{
		asAdmin: {
			ID:             "the-admin",
			MetadataPublic: json.RawMessage(`{"role":"admin"}`),
		},
		asPlanner: {
			ID:     "the-planner",
			Traits: json.RawMessage(`{"username":"planner","role":"admin"}`),
			// The real one, written where only the admin API can reach.
			MetadataPublic: json.RawMessage(`{"role":"planner"}`),
		},
	})
}

// adminServer wires the administrative half over a fake Kratos admin API and a
// real node registry.
func adminServer(t *testing.T, kratosAdmin string) (*apiv1.Server, *arrango.Registry) {
	t.Helper()

	conn := db.TestDB(t, migrations.FS)
	registry := arrango.NewRegistry(conn, db.SQLite)

	var accounts *identity.Admin
	if kratosAdmin != "" {
		built, err := identity.NewAdmin(kratosAdmin, 2*time.Second)
		if err != nil {
			t.Fatalf("admin client: %v", err)
		}
		accounts = built
	}

	trail := audit.New(conn, db.SQLite)
	server, err := apiv1.New(apiv1.Options{
		Build:            apiv1.BuildInfo{Version: "1.2.3"},
		MinClientVersion: "0.1.0",
		Handlers: &apiv1.Handlers{
			API:        newPlanAPI(t, conn),
			Admin:      apiv1.NewAdmin(accounts, apiv1.Nodes{Registry: registry}).WithAudit(trail),
			Jobs:       apiv1.NewJobs(nil, nil, nil, nil, nil),
			AuditTrail: apiv1.NewAuditTrail(trail),
		},
		Kratos: adminFake(t),
	})
	if err != nil {
		t.Fatalf("new server: %v", err)
	}
	return server, registry
}

func call(t *testing.T, server *apiv1.Server, method, path, credential, body string) *httptest.ResponseRecorder {
	t.Helper()
	var reader *strings.Reader
	if body != "" {
		reader = strings.NewReader(body)
	}
	var request *http.Request
	if reader != nil {
		request = httptest.NewRequest(method, path, reader)
		request.Header.Set("Content-Type", "application/json")
	} else {
		request = httptest.NewRequest(method, path, nil)
	}
	if credential != "" {
		request.Header.Set("Authorization", credential)
	}
	recorder := httptest.NewRecorder()
	server.Handler().ServeHTTP(recorder, request)
	return recorder
}

// The whole authorization boundary, at the HTTP layer this time: a role in
// traits is a role the account wrote for itself, and it must not open the back
// office. Not 403 either, because a 403 confirms the route is real.
func TestAdminRoutesAreClosedToEverybodyElse(t *testing.T) {
	server, _ := adminServer(t, "")

	for _, route := range []struct{ method, path string }{
		{http.MethodGet, "/api/v1/users"},
		{http.MethodPost, "/api/v1/users"},
		{http.MethodGet, "/api/v1/users/someone"},
		{http.MethodPatch, "/api/v1/users/someone"},
		{http.MethodPost, "/api/v1/users/someone/password"},
		{http.MethodGet, "/api/v1/users/someone/sessions"},
		{http.MethodDelete, "/api/v1/users/someone/sessions"},
		{http.MethodDelete, "/api/v1/users/someone/sessions/00000000-0000-4000-8000-000000000001"},
		{http.MethodGet, "/api/v1/security/lockout"},
		{http.MethodPatch, "/api/v1/security/lockout"},
		{http.MethodGet, "/api/v1/security/password-rules"},
		{http.MethodGet, "/api/v1/nodes"},
		{http.MethodPost, "/api/v1/nodes"},
		{http.MethodGet, "/api/v1/nodes/1"},
		{http.MethodPatch, "/api/v1/nodes/1"},
		{http.MethodDelete, "/api/v1/nodes/1"},
		{http.MethodGet, "/api/v1/nodes/1/status"},
		{http.MethodGet, "/api/v1/nodes/1/jobs"},
		{http.MethodGet, "/api/v1/server/settings"},
		{http.MethodPatch, "/api/v1/server/settings"},
		{http.MethodGet, "/api/v1/storage"},
		{http.MethodPost, "/api/v1/storage/trash/empty"},
		{http.MethodPost, "/api/v1/storage/backups/trim"},
		{http.MethodPost, "/api/v1/storage/sweep"},
		{http.MethodGet, "/api/v1/logs"},
		{http.MethodGet, "/api/v1/audit"},
	} {
		t.Run(route.method+" "+route.path, func(t *testing.T) {
			anonymous := call(t, server, route.method, route.path, "", "")
			if anonymous.Code != http.StatusUnauthorized {
				t.Errorf("anonymous = %d, want 401", anonymous.Code)
			}
			planner := call(t, server, route.method, route.path, asPlanner, "{}")
			if planner.Code != http.StatusNotFound {
				t.Errorf("planner claiming admin in traits = %d, want 404", planner.Code)
			}
		})
	}
}

// An administrator reaches the routes, and they answer 404 rather than 500
// while the dependency behind them is not configured.
func TestAdminRoutesAnswerNotFoundWithoutTheirDependency(t *testing.T) {
	server, _ := adminServer(t, "")

	for _, route := range []struct{ method, path string }{
		{http.MethodGet, "/api/v1/users"},
		{http.MethodGet, "/api/v1/users/someone"},
		{http.MethodGet, "/api/v1/users/someone/sessions"},
		{http.MethodDelete, "/api/v1/users/someone/sessions"},
		{http.MethodDelete, "/api/v1/users/someone/sessions/00000000-0000-4000-8000-000000000001"},
		{http.MethodGet, "/api/v1/security/lockout"},
		{http.MethodGet, "/api/v1/security/password-rules"},
		{http.MethodGet, "/api/v1/server/settings"},
		{http.MethodGet, "/api/v1/storage"},
		{http.MethodPost, "/api/v1/storage/sweep"},
		{http.MethodGet, "/api/v1/logs"},
	} {
		recorder := call(t, server, route.method, route.path, asAdmin, "")
		if recorder.Code != http.StatusNotFound {
			t.Errorf("%s %s = %d, want 404 with no identity provider",
				route.method, route.path, recorder.Code)
		}
	}
}

func TestUserLifecycleOverHTTP(t *testing.T) {
	fake, url := newKratosAdmin(t)
	server, _ := adminServer(t, url)

	// Create: the password is generated, returned once, and the account must
	// change it.
	created := call(t, server, http.MethodPost, "/api/v1/users", asAdmin,
		`{"username":"kowalski","email":"kowalski@szkola.test","name":"Jan","role":"planner"}`)
	if created.Code != http.StatusCreated {
		t.Fatalf("create = %d: %s", created.Code, created.Body.String())
	}
	var minted gen.UserCreated
	if err := json.Unmarshal(created.Body.Bytes(), &minted); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if minted.Password == "" {
		t.Fatal("no password in the create response")
	}
	if !minted.User.MustChangePassword {
		t.Error("the new account does not have to change its password")
	}
	if minted.User.Role != "planner" {
		t.Errorf("role = %q, want planner", minted.User.Role)
	}
	// The role must never have reached traits.
	stored := fake.Identities[minted.User.Id]
	traits, _ := stored["traits"].(map[string]any)
	if _, leaked := traits["role"]; leaked {
		t.Error("the role reached traits, which the account can rewrite")
	}

	// Read it back.
	read := call(t, server, http.MethodGet, "/api/v1/users/"+minted.User.Id, asAdmin, "")
	if read.Code != http.StatusOK {
		t.Fatalf("get = %d: %s", read.Code, read.Body.String())
	}

	// Promote and disable in one patch.
	patched := call(t, server, http.MethodPatch, "/api/v1/users/"+minted.User.Id, asAdmin,
		`{"role":"admin","active":false}`)
	if patched.Code != http.StatusOK {
		t.Fatalf("patch = %d: %s", patched.Code, patched.Body.String())
	}
	var updated gen.User
	if err := json.Unmarshal(patched.Body.Bytes(), &updated); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if updated.Role != "admin" || updated.Active {
		t.Errorf("user = %+v, want an admin that is disabled", updated)
	}
	if updated.Username != "kowalski" {
		t.Errorf("the patch changed the username to %q", updated.Username)
	}

	// Reset the password: a full replace, never a patch of the password field.
	reset := call(t, server, http.MethodPost, "/api/v1/users/"+minted.User.Id+"/password", asAdmin, "")
	if reset.Code != http.StatusOK {
		t.Fatalf("reset = %d: %s", reset.Code, reset.Body.String())
	}
	var fresh gen.PasswordReset
	if err := json.Unmarshal(reset.Body.Bytes(), &fresh); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if fresh.Password == "" || fresh.Password == minted.Password {
		t.Error("the reset did not produce a new password")
	}
	if fake.Puts != 1 {
		t.Errorf("puts = %d, want 1: a password patch answers 200 and breaks the credential", fake.Puts)
	}

	// And the listing sees it.
	listed := call(t, server, http.MethodGet, "/api/v1/users?page=1&limit=25", asAdmin, "")
	if listed.Code != http.StatusOK {
		t.Fatalf("list = %d: %s", listed.Code, listed.Body.String())
	}
	var page gen.UserPage
	if err := json.Unmarshal(listed.Body.Bytes(), &page); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if page.Total != 1 || len(page.Items) != 1 {
		t.Errorf("page = %+v, want one account", page)
	}
}

func TestCreateUserRefusesAnUnknownRole(t *testing.T) {
	_, url := newKratosAdmin(t)
	server, _ := adminServer(t, url)

	recorder := call(t, server, http.MethodPost, "/api/v1/users", asAdmin,
		`{"username":"x","email":"x@szkola.test","role":"superuser"}`)
	// The generated binder rejects a value outside the enum before the handler
	// runs, which is the contract doing its job.
	if recorder.Code != http.StatusBadRequest && recorder.Code != http.StatusUnprocessableEntity {
		t.Errorf("status = %d, want the role refused", recorder.Code)
	}
}

func TestNodeRoutesOverHTTP(t *testing.T) {
	server, registry := adminServer(t, "")

	empty := call(t, server, http.MethodGet, "/api/v1/nodes", asAdmin, "")
	if empty.Code != http.StatusOK {
		t.Fatalf("list = %d: %s", empty.Code, empty.Body.String())
	}
	var list gen.NodeList
	if err := json.Unmarshal(empty.Body.Bytes(), &list); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(list.Items) != 0 {
		t.Errorf("a fresh deployment lists %d nodes", len(list.Items))
	}

	node, err := registry.Record(t.Context(), "school-node", paired(t, "127.0.0.1:9443"))
	if err != nil {
		t.Fatalf("record: %v", err)
	}
	id := fmt.Sprintf("%d", node.ID)

	one := call(t, server, http.MethodGet, "/api/v1/nodes/"+id, asAdmin, "")
	if one.Code != http.StatusOK {
		t.Fatalf("get = %d: %s", one.Code, one.Body.String())
	}
	var read gen.Node
	if err := json.Unmarshal(one.Body.Bytes(), &read); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if read.Id != id || !read.Active || read.Fingerprint != node.Fingerprint {
		t.Errorf("node = %+v", read)
	}

	revoked := call(t, server, http.MethodDelete, "/api/v1/nodes/"+id, asAdmin, "")
	if revoked.Code != http.StatusNoContent {
		t.Fatalf("revoke = %d: %s", revoked.Code, revoked.Body.String())
	}
	after := call(t, server, http.MethodGet, "/api/v1/nodes/"+id, asAdmin, "")
	if err := json.Unmarshal(after.Body.Bytes(), &read); err != nil {
		t.Fatalf("decode: %v", err)
	}
	// The row is kept, so a job that names a revoked node still reads.
	if read.Active || read.RevokedAt == nil {
		t.Errorf("node = %+v, want a kept row marked revoked", read)
	}

	missing := call(t, server, http.MethodGet, "/api/v1/nodes/999", asAdmin, "")
	if missing.Code != http.StatusNotFound {
		t.Errorf("a node that does not exist = %d, want 404", missing.Code)
	}
}

// Pairing needs a dialer, and this server was built without one.
func TestPairNodeWithoutADialerIsNotFound(t *testing.T) {
	server, _ := adminServer(t, "")
	recorder := call(t, server, http.MethodPost, "/api/v1/nodes", asAdmin,
		`{"uri":"arrango://127.0.0.1:9443#fp=AAAA&t=ABCD-EFGH-IJKL-MNOP"}`)
	if recorder.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404 with no dialer configured", recorder.Code)
	}
}

// newKratosAdmin is the fake admin API, shared with the identity package's own
// tests so there is one stand-in rather than two that can disagree.
func newKratosAdmin(t *testing.T) (*identitytest.Kratos, string) {
	t.Helper()
	return identitytest.New(t)
}

// paired builds a Paired with a real fingerprint, since the registry stores it
// and parses it back.
func paired(t *testing.T, address string) solveclient.Paired {
	t.Helper()
	self, err := arrangoid.Create(t.TempDir())
	if err != nil {
		t.Fatalf("create identity: %v", err)
	}
	return solveclient.Paired{Address: address, Fingerprint: self.Fingerprint}
}

// Account and node changes are written to the trail with the account's
// username and the node's label as they were.
func TestAccountAndNodeChangesAreAudited(t *testing.T) {
	fake, url := newKratosAdmin(t)
	server, registry := adminServer(t, url)

	created := call(t, server, http.MethodPost, "/api/v1/users", asAdmin,
		`{"username":"planner-test","email":"planner-test@example.invalid","role":"planner"}`)
	if created.Code != http.StatusCreated {
		t.Fatalf("create = %d: %s", created.Code, created.Body.String())
	}
	var minted gen.UserCreated
	if err := json.Unmarshal(created.Body.Bytes(), &minted); err != nil {
		t.Fatalf("decode: %v", err)
	}
	user := "/api/v1/users/" + minted.User.Id
	fake.AddSession(minted.User.Id, time.Now().Add(-time.Minute))
	for _, step := range []struct{ method, path, body string }{
		{http.MethodPatch, user, `{"active":false}`},
		{http.MethodPost, user + "/password", ""},
		{http.MethodDelete, user + "/sessions", ""},
	} {
		if recorder := call(t, server, step.method, step.path, asAdmin, step.body); recorder.Code >= 300 {
			t.Fatalf("%s %s = %d: %s", step.method, step.path, recorder.Code, recorder.Body.String())
		}
	}

	node, err := registry.Record(t.Context(), "school-node", paired(t, "127.0.0.1:9443"))
	if err != nil {
		t.Fatalf("record: %v", err)
	}
	if recorder := call(t, server, http.MethodDelete, fmt.Sprintf("/api/v1/nodes/%d", node.ID), asAdmin, ""); recorder.Code != http.StatusNoContent {
		t.Fatalf("revoke = %d: %s", recorder.Code, recorder.Body.String())
	}

	listed := call(t, server, http.MethodGet, "/api/v1/audit", asAdmin, "")
	if listed.Code != http.StatusOK {
		t.Fatalf("audit = %d: %s", listed.Code, listed.Body.String())
	}
	var page gen.AuditPage
	if err := json.Unmarshal(listed.Body.Bytes(), &page); err != nil {
		t.Fatalf("decode: %v", err)
	}
	type seen struct {
		action gen.AuditAction
		kind   gen.AuditKind
		name   string
	}
	got := []seen{}
	for _, entry := range page.Items {
		name := ""
		if entry.Target.Name != nil {
			name = *entry.Target.Name
		}
		got = append(got, seen{entry.Action, entry.Target.Kind, name})
		if entry.Actor.Id != "the-admin" {
			t.Errorf("actor = %+v, want the administrator", entry.Actor)
		}
	}
	want := []seen{
		{gen.AuditActionNodeRevoked, gen.AuditKindNode, "school-node"},
		{gen.AuditActionUserSignedOut, gen.AuditKindUser, "planner-test"},
		{gen.AuditActionUserPasswordReset, gen.AuditKindUser, "planner-test"},
		{gen.AuditActionUserChanged, gen.AuditKindUser, "planner-test"},
		{gen.AuditActionUserCreated, gen.AuditKindUser, "planner-test"},
	}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("trail = %v\nwant    %v", got, want)
	}
	if details := page.Items[3].Details; details["active"] != "false" || len(details) != 1 {
		t.Errorf("user.changed details = %v", details)
	}
	if details := page.Items[4].Details; details["role"] != "planner" {
		t.Errorf("user.created details = %v", details)
	}
}

func TestLargePageMustNotPanic(t *testing.T) {
	_, url := newKratosAdmin(t)
	server, _ := adminServer(t, url)
	page := int(^uint(0)>>1)/2 + 2
	response := call(t, server, http.MethodGet, fmt.Sprintf("/api/v1/users?page=%d&limit=2", page), asAdmin, "")
	if response.Code != http.StatusOK {
		t.Fatalf("large positive page returned %d, want empty 200 page", response.Code)
	}
}

func TestKratosFieldRefusalIsValidation(t *testing.T) {
	kratos := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			_, _ = w.Write([]byte(`[]`))
			return
		}
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":{"code":400,"reason":"synthetic invalid traits"}}`))
	}))
	t.Cleanup(kratos.Close)
	server, _ := adminServer(t, kratos.URL)
	response := call(t, server, http.MethodPost, "/api/v1/users", asAdmin, `{"username":"synthetic","email":"synthetic@example.invalid","role":"planner"}`)
	if response.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status %d: %s", response.Code, response.Body.String())
	}
	body := envelope(t, response)
	if body.Code != "validation" || body.Details == nil || (*body.Details)["body"] != "invalid" {
		t.Fatalf("refusal: %+v", body)
	}
}

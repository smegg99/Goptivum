// api/v1/users_test.go

package v1_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"testing"
	"time"

	apiv1 "github.com/smegg99/goptivum/backend/api/v1"
	"github.com/smegg99/goptivum/backend/api/v1/gen"
	"github.com/smegg99/goptivum/backend/common/messages"
	"github.com/smegg99/goptivum/backend/identity/identitytest"
)

// createUser makes an account through the route and returns it.
func createUser(t *testing.T, server *apiv1.Server, username, role string) gen.User {
	t.Helper()
	recorder := call(t, server, http.MethodPost, "/api/v1/users", asAdmin,
		fmt.Sprintf(`{"username":%q,"email":%q,"role":%q}`, username, username+"@example.invalid", role))
	if recorder.Code != http.StatusCreated {
		t.Fatalf("create %s = %d: %s", username, recorder.Code, recorder.Body.String())
	}
	var created gen.UserCreated
	if err := json.Unmarshal(recorder.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode: %v", err)
	}
	return created.User
}

func TestListUsersSearchesNameUsernameAndEmail(t *testing.T) {
	_, kratosURL := newKratosAdmin(t)
	server, _ := adminServer(t, kratosURL)
	createUser(t, server, "anna.kowalska", "planner")
	createUser(t, server, "piotr", "planner")
	named := call(t, server, http.MethodPost, "/api/v1/users", asAdmin,
		`{"username":"mw","email":"maria@example.invalid","name":"Maria Wiśniewska","role":"planner"}`)
	if named.Code != http.StatusCreated {
		t.Fatalf("create = %d", named.Code)
	}

	for query, want := range map[string][]string{
		"KOWAL":           {"anna.kowalska"},
		"wiśniew":         {"mw"},
		" maria@example ": {"mw"},
		"example.invalid": {"anna.kowalska", "mw", "piotr"},
		"nobody":          {},
		"":                {"anna.kowalska", "mw", "piotr"},
	} {
		recorder := call(t, server, http.MethodGet, "/api/v1/users?q="+url.QueryEscape(query), asAdmin, "")
		if recorder.Code != http.StatusOK {
			t.Fatalf("q=%q: %d %s", query, recorder.Code, recorder.Body.String())
		}
		var page gen.UserPage
		if err := json.Unmarshal(recorder.Body.Bytes(), &page); err != nil {
			t.Fatalf("decode: %v", err)
		}
		got := map[string]bool{}
		for _, user := range page.Items {
			got[user.Username] = true
		}
		if page.Total != len(want) || len(got) != len(want) {
			t.Errorf("q=%q: total %d, items %v, want %v", query, page.Total, got, want)
			continue
		}
		for _, username := range want {
			if !got[username] {
				t.Errorf("q=%q: %s missing from %v", query, username, got)
			}
		}
	}
}

func TestASecondAccountWithATakenNameIsRefused(t *testing.T) {
	_, kratosURL := newKratosAdmin(t)
	server, _ := adminServer(t, kratosURL)
	createUser(t, server, "planner-test", "planner")

	for body, field := range map[string]string{
		`{"username":"other","email":"PLANNER-TEST@example.invalid","role":"planner"}`: "email",
		`{"username":"Planner-Test","email":"other@example.invalid","role":"planner"}`: "username",
	} {
		recorder := call(t, server, http.MethodPost, "/api/v1/users", asAdmin, body)
		if recorder.Code != http.StatusConflict {
			t.Errorf("%s = %d, want 409: %s", body, recorder.Code, recorder.Body.String())
			continue
		}
		envelope := decodeError(t, recorder)
		if envelope.Code != "conflict" || envelope.Message != messages.KeyUsersAlreadyExists ||
			envelope.Details == nil || (*envelope.Details)["Field"] != field {
			t.Errorf("%s: envelope = %+v, want users.alreadyExists on %s", body, envelope, field)
		}
	}
}

// The recovery that Karol's slice 13 test 6 relies on never having to run:
// the only enabled administrator cannot be demoted or disabled over HTTP.
func TestTheLastEnabledAdminCannotBeDemotedOrDisabled(t *testing.T) {
	fake, kratosURL := newKratosAdmin(t)
	server, _ := adminServer(t, kratosURL)
	head := createUser(t, server, "head", "admin")
	planner := createUser(t, server, "planner", "planner")

	for _, body := range []string{`{"role":"planner"}`, `{"active":false}`, `{"role":"planner","active":false}`} {
		recorder := call(t, server, http.MethodPatch, "/api/v1/users/"+head.Id, asAdmin, body)
		if recorder.Code != http.StatusConflict {
			t.Errorf("%s = %d, want 409: %s", body, recorder.Code, recorder.Body.String())
			continue
		}
		if envelope := decodeError(t, recorder); envelope.Code != "conflict" || envelope.Message != messages.KeyUsersLastAdmin {
			t.Errorf("%s: envelope = %+v, want users.lastAdmin", body, envelope)
		}
	}
	stored := fake.Identities[head.Id]
	if meta, _ := stored["metadata_public"].(map[string]any); meta["role"] != "admin" || stored["state"] != "active" {
		t.Errorf("a refused change reached Kratos: %v", stored)
	}

	// A second administrator makes the same change fine.
	if recorder := call(t, server, http.MethodPatch, "/api/v1/users/"+planner.Id, asAdmin, `{"role":"admin"}`); recorder.Code != http.StatusOK {
		t.Fatalf("promote = %d: %s", recorder.Code, recorder.Body.String())
	}
	if recorder := call(t, server, http.MethodPatch, "/api/v1/users/"+head.Id, asAdmin, `{"role":"planner"}`); recorder.Code != http.StatusOK {
		t.Errorf("demote with a second admin = %d: %s", recorder.Code, recorder.Body.String())
	}
}

func TestDisablingOrResettingSignsTheAccountOutEverywhere(t *testing.T) {
	fake, kratosURL := newKratosAdmin(t)
	server, _ := adminServer(t, kratosURL)
	disabled := createUser(t, server, "disabled", "planner")
	reset := createUser(t, server, "reset", "planner")
	fake.AddSession(disabled.Id, time.Now())
	fake.AddSession(reset.Id, time.Now())

	if recorder := call(t, server, http.MethodPatch, "/api/v1/users/"+disabled.Id, asAdmin, `{"active":false}`); recorder.Code != http.StatusOK {
		t.Fatalf("disable = %d: %s", recorder.Code, recorder.Body.String())
	}
	if recorder := call(t, server, http.MethodPost, "/api/v1/users/"+reset.Id+"/password", asAdmin, ""); recorder.Code != http.StatusOK {
		t.Fatalf("reset = %d: %s", recorder.Code, recorder.Body.String())
	}
	fake.Identities[disabled.Id]["state"] = "active"
	for _, user := range []gen.User{disabled, reset} {
		if live := fake.LiveSessions(user.Id); live != 0 {
			t.Errorf("%s kept %d sessions", user.Username, live)
		}
	}
}

func TestSessionsAreListedAndSignedOut(t *testing.T) {
	fake, kratosURL := newKratosAdmin(t)
	server, _ := adminServer(t, kratosURL)
	planner := createUser(t, server, "planner-test", "planner")
	other := createUser(t, server, "other", "planner")
	// kratosauthtest names the caller's session "session-" plus its id, so a
	// session under that id reads as the caller's own.
	mine := "session-the-admin"
	fake.Sessions = append(fake.Sessions, &identitytest.Session{
		ID: mine, IdentityID: planner.Id, Active: true,
		AuthenticatedAt: time.Date(2026, 9, 27, 7, 0, 0, 0, time.UTC),
		ExpiresAt:       time.Date(2026, 9, 28, 7, 0, 0, 0, time.UTC),
	})
	second := fake.AddSession(planner.Id, time.Date(2026, 9, 27, 8, 0, 0, 0, time.UTC))
	others := fake.AddSession(other.Id, time.Now())

	listed := call(t, server, http.MethodGet, "/api/v1/users/"+planner.Id+"/sessions", asAdmin, "")
	if listed.Code != http.StatusOK {
		t.Fatalf("list = %d: %s", listed.Code, listed.Body.String())
	}
	var sessions gen.UserSessionList
	if err := json.Unmarshal(listed.Body.Bytes(), &sessions); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(sessions.Items) != 2 || sessions.Items[0].Id != second || sessions.Items[1].Id != mine {
		t.Fatalf("sessions = %+v, want two, newest first", sessions.Items)
	}
	if sessions.Items[0].Current || !sessions.Items[1].Current {
		t.Errorf("current = %v, %v; want only the caller's own session", sessions.Items[0].Current, sessions.Items[1].Current)
	}
	if first := sessions.Items[0]; first.IpAddress == nil || first.UserAgent == nil || *first.UserAgent != "goptivum-desktop/test" {
		t.Errorf("session = %+v, want where it was used", first)
	}

	// Another account's session is not reachable through this one.
	if recorder := call(t, server, http.MethodDelete, "/api/v1/users/"+planner.Id+"/sessions/"+others, asAdmin, ""); recorder.Code != http.StatusNotFound {
		t.Errorf("end another account's session = %d, want 404", recorder.Code)
	}
	if recorder := call(t, server, http.MethodDelete, "/api/v1/users/"+planner.Id+"/sessions/"+second, asAdmin, ""); recorder.Code != http.StatusNoContent {
		t.Fatalf("end one = %d: %s", recorder.Code, recorder.Body.String())
	}
	if live := fake.LiveSessions(planner.Id); live != 1 {
		t.Errorf("after ending one: %d left, want 1", live)
	}
	if recorder := call(t, server, http.MethodDelete, "/api/v1/users/"+planner.Id+"/sessions", asAdmin, ""); recorder.Code != http.StatusNoContent {
		t.Fatalf("end all = %d: %s", recorder.Code, recorder.Body.String())
	}
	if live := fake.LiveSessions(planner.Id); live != 0 {
		t.Errorf("after ending all: %d left", live)
	}
	if live := fake.LiveSessions(other.Id); live != 1 {
		t.Errorf("another account lost its session")
	}
	// Signing out an account with nothing left is not an error.
	if recorder := call(t, server, http.MethodDelete, "/api/v1/users/"+planner.Id+"/sessions", asAdmin, ""); recorder.Code != http.StatusNoContent {
		t.Errorf("end all again = %d: %s", recorder.Code, recorder.Body.String())
	}

	for _, route := range []struct{ method, path string }{
		{http.MethodGet, "/api/v1/users/nobody/sessions"},
		{http.MethodDelete, "/api/v1/users/nobody/sessions"},
		{http.MethodDelete, "/api/v1/users/nobody/sessions/" + mine},
	} {
		if recorder := call(t, server, route.method, route.path, asAdmin, ""); recorder.Code != http.StatusNotFound {
			t.Errorf("%s %s = %d, want 404 for an unknown account", route.method, route.path, recorder.Code)
		}
	}
}

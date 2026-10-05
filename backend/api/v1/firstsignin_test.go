// api/v1/firstsignin_test.go

package v1_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/smegg99/s99ab/auth/kratosauth"
	"github.com/smegg99/s99ab/auth/kratosauth/kratosauthtest"
	"github.com/smegg99/s99ab/data/db"

	apiv1 "github.com/smegg99/goptivum/backend/api/v1"
	"github.com/smegg99/goptivum/backend/arrango"
	"github.com/smegg99/goptivum/backend/identity"
	"github.com/smegg99/goptivum/backend/identity/identitytest"
	"github.com/smegg99/goptivum/backend/migrations"
)

// Tokens for an account that has not changed its generated password yet, and
// one that has.
const (
	asFresh    = "Bearer fresh-token"
	asSettled  = "Bearer settled-token"
	freshAdmin = "Bearer fresh-admin-token"
)

// firstSignInServer serves the same surface as the rest, over identities that
// differ only in the first-sign-in flag.
func firstSignInServer(t *testing.T, kratosPublicURL string) *apiv1.Server {
	t.Helper()
	conn := db.TestDB(t, migrations.FS)

	fake := kratosauthtest.Fake(t, map[string]kratosauth.Identity{
		asFresh: {
			ID:             "fresh",
			MetadataPublic: json.RawMessage(`{"role":"planner","must_change_password":true}`),
		},
		asSettled: {
			ID:             "settled",
			MetadataPublic: json.RawMessage(`{"role":"planner"}`),
		},
		freshAdmin: {
			ID:             "fresh-admin",
			MetadataPublic: json.RawMessage(`{"role":"admin","must_change_password":true}`),
		},
	})
	server, err := apiv1.New(apiv1.Options{
		Build:            apiv1.BuildInfo{Version: "1.2.3"},
		MinClientVersion: "0.1.0",
		Handlers: &apiv1.Handlers{
			API:   newPlanAPI(t, conn),
			Admin: apiv1.NewAdmin(nil, apiv1.Nodes{Registry: arrango.NewRegistry(conn, db.SQLite)}),
			Jobs:  apiv1.NewJobs(nil, nil, nil, nil, nil),
		},
		Kratos:          fake,
		KratosPublicURL: kratosPublicURL,
	})
	if err != nil {
		t.Fatalf("new server: %v", err)
	}
	return server
}

// A generated password is printed to a log once and must stop working. Without
// this the flag is decoration: bootstrap sets it and nothing ever asks.
func TestEveryGuardedRouteRefusesUntilThePasswordIsChanged(t *testing.T) {
	server := firstSignInServer(t, "")

	for _, one := range []struct{ method, path, credential string }{
		{http.MethodGet, "/api/v1/plans", asFresh},
		{http.MethodPost, "/api/v1/plans", asFresh},
		{http.MethodGet, "/api/v1/plans/1/versions", asFresh},
		{http.MethodGet, "/api/v1/plans/1/jobs", asFresh},
		{http.MethodGet, "/api/v1/jobs/1", asFresh},
		{http.MethodGet, "/api/v1/plans/1/versions/1/export/goptivum.gpf", asFresh},
		{http.MethodGet, "/api/v1/plans/1/results/1/export/optivum.html", asFresh},
		// The upload group is a sibling of the guarded one, not a child, so it
		// needs the guard in its own right.
		{http.MethodPost, "/api/v1/import", asFresh},
		{http.MethodPost, "/api/v1/import/preview", asFresh},
		{http.MethodPost, "/api/v1/plans/1/versions", asFresh},
		// The admin sub-group inherits it, and must: a first administrator
		// holding a password from a log file is exactly who this is for.
		{http.MethodGet, "/api/v1/users", freshAdmin},
		{http.MethodGet, "/api/v1/nodes", freshAdmin},
	} {
		recorder := call(t, server, one.method, one.path, one.credential, "")
		if recorder.Code != http.StatusForbidden {
			t.Errorf("%s %s = %d, want 403", one.method, one.path, recorder.Code)
			continue
		}
		var envelope struct{ Code, Message string }
		if err := json.Unmarshal(recorder.Body.Bytes(), &envelope); err != nil {
			t.Errorf("%s %s: decode: %v", one.method, one.path, err)
			continue
		}
		// The stable key is in message; code is the coarse kind a client
		// branches on for its error page.
		if envelope.Message != "auth.passwordChangeRequired" {
			t.Errorf("%s %s message = %q, want auth.passwordChangeRequired",
				one.method, one.path, envelope.Message)
		}
	}
}

// The same account, once the flag is gone, is an ordinary account. A guard that
// cannot be satisfied is a locked door with no key.
func TestAnAccountThatChangedItsPasswordIsLetThrough(t *testing.T) {
	server := firstSignInServer(t, "")

	recorder := call(t, server, http.MethodGet, "/api/v1/plans", asSettled, "")
	if recorder.Code != http.StatusOK {
		t.Fatalf("settled account = %d, want 200: %s", recorder.Code, recorder.Body.String())
	}
}

// Refusing everything is only safe because the flow that changes the password
// is not in this group. If the proxy ever moves inside it, the account is
// locked out for good and this is the test that says so.
func TestTheKratosFlowStaysReachableWhileEverythingElseIsRefused(t *testing.T) {
	kratos := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"flow-1","ui":{"action":"/kratos/self-service/settings"}}`))
	}))
	defer kratos.Close()

	server := firstSignInServer(t, kratos.URL)

	// Refused, as every other route is.
	if recorder := call(t, server, http.MethodGet, "/api/v1/plans", asFresh, ""); recorder.Code != http.StatusForbidden {
		t.Fatalf("guarded route = %d, want 403", recorder.Code)
	}

	// Reachable, which is the whole point.
	recorder := call(t, server, http.MethodGet, "/kratos/self-service/settings/browser", asFresh, "")
	if recorder.Code != http.StatusOK {
		t.Fatalf("the settings flow = %d, want 200: %s", recorder.Code, recorder.Body.String())
	}
	if !strings.Contains(recorder.Body.String(), "self-service/settings") {
		t.Errorf("the proxy did not reach Kratos: %s", recorder.Body.String())
	}
}

// Discovery answers before identity exists at all, so it cannot depend on the
// state of an account.
func TestDiscoveryIgnoresTheFirstSignInFlag(t *testing.T) {
	server := firstSignInServer(t, "")
	if recorder := call(t, server, http.MethodGet, "/api/info", asFresh, ""); recorder.Code != http.StatusOK {
		t.Errorf("/api/info = %d, want 200", recorder.Code)
	}
}

// serverWithAccounts wires the Kratos admin API in, which is the branch that
// actually runs in production: without it the guard can refuse but never clear.
func serverWithAccounts(t *testing.T) (*apiv1.Server, *identitytest.Kratos, string) {
	t.Helper()
	conn := db.TestDB(t, migrations.FS)
	fake, base := identitytest.New(t)

	accounts, err := identity.NewAdmin(base, 5*time.Second)
	if err != nil {
		t.Fatalf("admin client: %v", err)
	}
	created, err := accounts.Create(t.Context(), identity.CreateParams{
		Username: "planner", Email: "planner@example.test", Password: "initial-password-1",
		Role: identity.RolePlanner, MustChangePassword: true,
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	kratos := kratosauthtest.Fake(t, map[string]kratosauth.Identity{
		asFresh: {
			ID:             created.ID,
			MetadataPublic: json.RawMessage(`{"role":"planner","must_change_password":true}`),
		},
	})
	server, err := apiv1.New(apiv1.Options{
		Build:            apiv1.BuildInfo{Version: "1.2.3"},
		MinClientVersion: "0.1.0",
		Handlers: &apiv1.Handlers{
			API:   newPlanAPI(t, conn),
			Admin: apiv1.NewAdmin(accounts, apiv1.Nodes{Registry: arrango.NewRegistry(conn, db.SQLite)}),
			Jobs:  apiv1.NewJobs(nil, nil, nil, nil, nil),
		},
		Kratos:   kratos,
		Accounts: accounts,
	})
	if err != nil {
		t.Fatalf("new server: %v", err)
	}
	return server, fake, created.ID
}

// The guard clears itself. An account that changed its password is let through
// on the very request that discovers it, and the flag is written down so the
// next one does not have to ask again.
func TestChangingThePasswordClearsTheFlagAndLetsTheCallerThrough(t *testing.T) {
	server, fake, id := serverWithAccounts(t)

	// Still flagged, so still refused.
	if recorder := call(t, server, http.MethodGet, "/api/v1/plans", asFresh, ""); recorder.Code != http.StatusForbidden {
		t.Fatalf("before the change = %d, want 403", recorder.Code)
	}

	// What a settings flow does: the stored hash moves.
	fake.ChangePassword(id)

	if recorder := call(t, server, http.MethodGet, "/api/v1/plans", asFresh, ""); recorder.Code != http.StatusOK {
		t.Fatalf("after the change = %d, want 200: %s", recorder.Code, recorder.Body.String())
	}
	// And it was written down, not merely tolerated for one request.
	meta, _ := fake.Identities[id]["metadata_public"].(map[string]any)
	if must, _ := meta["must_change_password"].(bool); must {
		t.Error("the flag was not cleared, so every later request asks Kratos again")
	}
}

// The session carries a snapshot of the metadata from when it was issued, so a
// second request in flight while the first clears the flag still says "must
// change". Kratos is the authority and says otherwise.
func TestAStaleSessionSnapshotDoesNotRefuseAnAccountThatAlreadyChanged(t *testing.T) {
	server, fake, id := serverWithAccounts(t)

	// Kratos has already been cleared, exactly as a concurrent request would
	// have left it, while this caller's session still carries the old flag.
	if err := identityAdmin(t, fake).ClearMustChangePassword(t.Context(), id); err != nil {
		t.Fatalf("clear: %v", err)
	}

	recorder := call(t, server, http.MethodGet, "/api/v1/plans", asFresh, "")
	if recorder.Code != http.StatusOK {
		t.Fatalf("stale snapshot = %d, want 200: %s", recorder.Code, recorder.Body.String())
	}
}

// identityAdmin is a second client onto the same fake, for a test that has to
// act on Kratos directly rather than through the server.
func identityAdmin(t *testing.T, fake *identitytest.Kratos) *identity.Admin {
	t.Helper()
	accounts, err := identity.NewAdmin(fake.BaseURL(), 5*time.Second)
	if err != nil {
		t.Fatalf("admin client: %v", err)
	}
	return accounts
}

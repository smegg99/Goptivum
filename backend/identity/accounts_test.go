// identity/accounts_test.go

package identity_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/smegg99/goptivum/backend/identity"
	"github.com/smegg99/goptivum/backend/identity/identitytest"
)

func create(t *testing.T, admin *identity.Admin, username, role string) *identity.Account {
	t.Helper()
	account, err := admin.Create(t.Context(), identity.CreateParams{
		Username: username, Email: username + "@example.invalid",
		Password: "synthetic-password-123", Role: role,
	})
	if err != nil {
		t.Fatalf("create %s: %v", username, err)
	}
	return account
}

func role(value string) *string { return &value }
func active(value bool) *bool   { return &value }

func TestCreateRefusesANameAnotherAccountSignsInWith(t *testing.T) {
	_, url := identitytest.New(t)
	admin := newAdmin(t, url)
	create(t, admin, "anna", identity.RolePlanner)

	for _, test := range []struct {
		username, email, field string
	}{
		{"anna2", "ANNA@example.invalid", "email"},
		{"Anna", "other@example.invalid", "username"},
		// One namespace: a username may not be another account's email.
		{"anna@example.invalid", "third@example.invalid", "username"},
	} {
		_, err := admin.Create(t.Context(), identity.CreateParams{
			Username: test.username, Email: test.email,
			Password: "synthetic-password-123", Role: identity.RolePlanner,
		})
		if !errors.Is(err, identity.ErrExists) {
			t.Errorf("%s / %s: err = %v, want ErrExists", test.username, test.email, err)
			continue
		}
		var taken *identity.Taken
		if !errors.As(err, &taken) || taken.Field != test.field {
			t.Errorf("%s / %s: taken = %+v, want field %s", test.username, test.email, taken, test.field)
		}
	}
}

// A clash the listing did not see, because another create won the race, is
// still Kratos's 409 and still ErrExists, without a field.
func TestCreateReadsKratosConflictAsExists(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			_, _ = w.Write([]byte(`[]`))
			return
		}
		w.WriteHeader(http.StatusConflict)
		_, _ = w.Write([]byte(`{"error":{"code":409,"status":"Conflict"}}`))
	}))
	t.Cleanup(server.Close)
	admin := newAdmin(t, server.URL)

	_, err := admin.Create(t.Context(), identity.CreateParams{
		Username: "anna", Email: "anna@example.invalid", Password: "synthetic-password-123",
	})
	if !errors.Is(err, identity.ErrExists) {
		t.Fatalf("err = %v, want ErrExists", err)
	}
	var taken *identity.Taken
	if errors.As(err, &taken) {
		t.Errorf("a raced clash names a field: %+v", taken)
	}
}

func TestUpdateRefusesToLeaveTheSchoolWithoutAnEnabledAdmin(t *testing.T) {
	fake, url := identitytest.New(t)
	admin := newAdmin(t, url)
	head := create(t, admin, "head", identity.RoleAdmin)
	planner := create(t, admin, "planner", identity.RolePlanner)
	// A disabled administrator cannot sign in, so it does not count.
	former := create(t, admin, "former", identity.RoleAdmin)
	if _, err := admin.Update(t.Context(), former.ID, identity.Change{Active: active(false)}); err != nil {
		t.Fatalf("disable a second admin: %v", err)
	}

	for name, change := range map[string]identity.Change{
		"demote":             {Role: role(identity.RolePlanner)},
		"disable":            {Active: active(false)},
		"demote and disable": {Role: role(identity.RolePlanner), Active: active(false)},
	} {
		patchesBefore := fake.Patches
		if _, err := admin.Update(t.Context(), head.ID, change); !errors.Is(err, identity.ErrLastAdmin) {
			t.Errorf("%s the last enabled admin: err = %v, want ErrLastAdmin", name, err)
		}
		if fake.Patches != patchesBefore {
			t.Errorf("%s: a refused change wrote to Kratos", name)
		}
	}
	after, err := admin.Get(t.Context(), head.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if after.Role != identity.RoleAdmin || !after.Active {
		t.Errorf("the refused changes moved the account: %+v", after)
	}

	// With a second enabled administrator the same change goes through.
	if _, err := admin.Update(t.Context(), planner.ID, identity.Change{Role: role(identity.RoleAdmin)}); err != nil {
		t.Fatalf("promote: %v", err)
	}
	demoted, err := admin.Update(t.Context(), head.ID, identity.Change{Role: role(identity.RolePlanner)})
	if err != nil {
		t.Fatalf("demote with another admin: %v", err)
	}
	if demoted.Role != identity.RolePlanner {
		t.Errorf("role = %q, want planner", demoted.Role)
	}
}

// A school that has no enabled administrator already, which a restored
// database can produce, is not stopped from changing a planner.
func TestUpdateOfAnAccountThatIsNotAnAdminIsNeverRefused(t *testing.T) {
	_, url := identitytest.New(t)
	admin := newAdmin(t, url)
	planner := create(t, admin, "planner", identity.RolePlanner)

	if _, err := admin.Update(t.Context(), planner.ID, identity.Change{Active: active(false)}); err != nil {
		t.Errorf("disable a planner with no admin anywhere: %v", err)
	}
}

func TestUpdateOfAnUnknownAccountIsNotFound(t *testing.T) {
	_, url := identitytest.New(t)
	admin := newAdmin(t, url)
	if _, err := admin.Update(t.Context(), "nobody", identity.Change{Active: active(false)}); !errors.Is(err, identity.ErrNotFound) {
		t.Errorf("err = %v, want ErrNotFound", err)
	}
}

func TestUpdateRefusesAnUnknownRole(t *testing.T) {
	_, url := identitytest.New(t)
	admin := newAdmin(t, url)
	planner := create(t, admin, "planner", identity.RolePlanner)
	if _, err := admin.Update(t.Context(), planner.ID, identity.Change{Role: role("superuser")}); err == nil {
		t.Error("an unknown role was accepted")
	}
}

func TestDisablingEndsEverySessionOfTheAccount(t *testing.T) {
	fake, url := identitytest.New(t)
	admin := newAdmin(t, url)
	create(t, admin, "head", identity.RoleAdmin)
	planner := create(t, admin, "planner", identity.RolePlanner)
	other := create(t, admin, "other", identity.RolePlanner)
	now := time.Now()
	fake.AddSession(planner.ID, now)
	fake.AddSession(planner.ID, now)
	fake.AddSession(other.ID, now)

	if _, err := admin.Update(t.Context(), planner.ID, identity.Change{Active: active(false)}); err != nil {
		t.Fatalf("disable: %v", err)
	}
	// Enabled again in the fake only, so a session merely hidden by the
	// disable would show up here: they must be gone, not hidden.
	fake.Identities[planner.ID]["state"] = "active"
	if live := fake.LiveSessions(planner.ID); live != 0 {
		t.Errorf("%d sessions outlived the disable", live)
	}
	if live := fake.LiveSessions(other.ID); live != 1 {
		t.Errorf("another account lost its session: %d left", live)
	}
}

// Kratos answers 404 when an account with no session is signed out. That is
// nothing to end, not a failure.
func TestDisablingAnAccountWithNoSessionWorks(t *testing.T) {
	_, url := identitytest.New(t)
	admin := newAdmin(t, url)
	planner := create(t, admin, "planner", identity.RolePlanner)
	if _, err := admin.Update(t.Context(), planner.ID, identity.Change{Active: active(false)}); err != nil {
		t.Errorf("disable an account with no session: %v", err)
	}
}

func TestResetPasswordEndsEverySessionAndRaisesTheFlag(t *testing.T) {
	fake, url := identitytest.New(t)
	admin := newAdmin(t, url)
	planner := create(t, admin, "planner", identity.RolePlanner)
	fake.AddSession(planner.ID, time.Now())

	password, err := admin.ResetPassword(t.Context(), planner.ID)
	if err != nil {
		t.Fatalf("reset: %v", err)
	}
	if password == "" {
		t.Error("no password")
	}
	if fake.Puts != 1 {
		t.Errorf("puts = %d, want 1: a password patch answers 200 and breaks the credential", fake.Puts)
	}
	if live := fake.LiveSessions(planner.ID); live != 0 {
		t.Errorf("%d sessions kept signing in with the old password", live)
	}
	after, err := admin.Get(t.Context(), planner.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if !after.MustChangePassword || after.Username != "planner" || after.Role != identity.RolePlanner {
		t.Errorf("after the reset: %+v", after)
	}
	// And with no session left, a second reset still works.
	if _, err := admin.ResetPassword(t.Context(), planner.ID); err != nil {
		t.Errorf("reset with no session: %v", err)
	}
}

func TestSessionsListsWhatStillSignsTheAccountIn(t *testing.T) {
	fake, url := identitytest.New(t)
	admin := newAdmin(t, url)
	planner := create(t, admin, "planner", identity.RolePlanner)
	older := fake.AddSession(planner.ID, time.Date(2026, 9, 26, 8, 0, 0, 0, time.UTC))
	newer := fake.AddSession(planner.ID, time.Date(2026, 9, 27, 8, 0, 0, 0, time.UTC))
	ended := fake.AddSession(planner.ID, time.Date(2026, 9, 25, 8, 0, 0, 0, time.UTC))
	if err := admin.EndSession(t.Context(), planner.ID, ended); err != nil {
		t.Fatalf("end one: %v", err)
	}

	sessions, err := admin.Sessions(t.Context(), planner.ID)
	if err != nil {
		t.Fatalf("sessions: %v", err)
	}
	if len(sessions) != 2 || sessions[0].ID != newer || sessions[1].ID != older {
		t.Fatalf("sessions = %+v, want the two live ones, newest first", sessions)
	}
	first := sessions[0]
	if first.IPAddress != "192.0.2.10:50486" || first.UserAgent != "goptivum-desktop/test" ||
		!first.AuthenticatedAt.Equal(time.Date(2026, 9, 27, 8, 0, 0, 0, time.UTC)) ||
		!first.ExpiresAt.After(first.AuthenticatedAt) {
		t.Errorf("session = %+v", first)
	}

	// A disabled account's sessions no longer sign it in, so none is listed.
	fake.Identities[planner.ID]["state"] = "inactive"
	if sessions, err := admin.Sessions(t.Context(), planner.ID); err != nil || len(sessions) != 0 {
		t.Errorf("disabled account: sessions = %+v, err = %v, want none", sessions, err)
	}
}

func TestEndSessionOnlyEndsTheAccountsOwnSession(t *testing.T) {
	fake, url := identitytest.New(t)
	admin := newAdmin(t, url)
	anna := create(t, admin, "anna", identity.RolePlanner)
	piotr := create(t, admin, "piotr", identity.RolePlanner)
	piotrs := fake.AddSession(piotr.ID, time.Now())

	for _, id := range []string{piotrs, "00000000-0000-4000-8000-999999999999", "not-a-session"} {
		if err := admin.EndSession(t.Context(), anna.ID, id); !errors.Is(err, identity.ErrNotFound) {
			t.Errorf("end %q as anna's: err = %v, want ErrNotFound", id, err)
		}
	}
	if live := fake.LiveSessions(piotr.ID); live != 1 {
		t.Errorf("piotr's session was ended through anna: %d left", live)
	}
	if err := admin.EndSessions(t.Context(), piotr.ID); err != nil {
		t.Fatalf("end piotr's sessions: %v", err)
	}
	if live := fake.LiveSessions(piotr.ID); live != 0 {
		t.Errorf("%d sessions left after ending them all", live)
	}
}

func TestFindByIdentifierTakesAUsernameOrAnEmail(t *testing.T) {
	_, url := identitytest.New(t)
	admin := newAdmin(t, url)
	anna := create(t, admin, "anna", identity.RoleAdmin)
	create(t, admin, "piotr", identity.RolePlanner)

	for _, name := range []string{"anna", "ANNA@example.invalid"} {
		found, err := admin.FindByIdentifier(t.Context(), name)
		if err != nil || found.ID != anna.ID {
			t.Errorf("find %q = %+v, %v; want anna", name, found, err)
		}
	}
	if _, err := admin.FindByIdentifier(t.Context(), "nobody@example.invalid"); !errors.Is(err, identity.ErrNotFound) {
		t.Errorf("unknown name: err = %v, want ErrNotFound", err)
	}
}

func TestStalePasswordCheckMustNotClearNewReset(t *testing.T) {
	fake, url := identitytest.New(t)
	accounts := newAdmin(t, url)
	planner := create(t, accounts, "planner", identity.RolePlanner)
	if err := accounts.SetMustChangePassword(t.Context(), planner.ID, true); err != nil {
		t.Fatal(err)
	}
	old, err := accounts.Get(t.Context(), planner.ID)
	if err != nil {
		t.Fatal(err)
	}
	fake.ChangePassword(planner.ID)
	changed, err := accounts.PasswordChanged(t.Context(), old)
	if err != nil || !changed {
		t.Fatalf("check: %v, %v", changed, err)
	}
	if _, err := accounts.ResetPassword(t.Context(), planner.ID); err != nil {
		t.Fatal(err)
	}
	if cleared, err := accounts.ClearMustChangePassword(t.Context(), planner.ID, old.PasswordMark); err != nil || cleared {
		t.Fatalf("stale flag cleared: %v, %v", cleared, err)
	}
	after, err := accounts.Get(t.Context(), planner.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !after.MustChangePassword {
		t.Errorf("stale password check cleared newer reset requirement; mark=%q", after.PasswordMark)
	}
}

func TestFakePasswordResetMustChangeStoredHash(t *testing.T) {
	fake, url := identitytest.New(t)
	admin := newAdmin(t, url)
	account := create(t, admin, "synthetic-reset-hash", identity.RolePlanner)
	before := fake.Hashes[account.ID]
	if _, err := admin.ResetPassword(t.Context(), account.ID); err != nil {
		t.Fatal(err)
	}
	if after := fake.Hashes[account.ID]; after == before {
		t.Fatalf("fake reset left hash unchanged: %q", after)
	}
}

func TestFakeEveryPasswordChangeMustChangeHash(t *testing.T) {
	fake, url := identitytest.New(t)
	admin := newAdmin(t, url)
	account := create(t, admin, "synthetic-repeated-hash", identity.RolePlanner)
	for n := 1; n <= 4; n++ {
		before := fake.Hashes[account.ID]
		fake.ChangePassword(account.ID)
		if after := fake.Hashes[account.ID]; after == before {
			t.Fatalf("fake password change %d left hash unchanged: %q", n, after)
		}
	}
}

func TestSeparateClientsCannotDemoteEveryAdmin(t *testing.T) {
	_, endpoint := identitytest.New(t)
	first, second := newAdmin(t, endpoint), newAdmin(t, endpoint)
	a := create(t, first, "first", identity.RoleAdmin)
	b := create(t, first, "second", identity.RoleAdmin)
	start, outcomes := make(chan struct{}), make(chan error, 2)
	for _, writer := range []struct {
		client *identity.Admin
		id     string
	}{{first, a.ID}, {second, b.ID}} {
		go func() {
			<-start
			_, err := writer.client.Update(t.Context(), writer.id, identity.Change{Role: role(identity.RolePlanner)})
			outcomes <- err
		}()
	}
	close(start)
	successes, refusals := 0, 0
	for range 2 {
		if err := <-outcomes; err == nil {
			successes++
		} else if errors.Is(err, identity.ErrLastAdmin) {
			refusals++
		} else {
			t.Fatal(err)
		}
	}
	if successes != 1 || refusals != 1 {
		t.Fatalf("demotions: %d succeeded, %d refused", successes, refusals)
	}
}

func TestResetOwnsAccountUntilPasswordMarkIsStored(t *testing.T) {
	fake, _ := identitytest.New(t)
	entered, release := make(chan struct{}), make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPut {
			close(entered)
			<-release
		}
		fake.ServeHTTP(w, r)
	}))
	t.Cleanup(server.Close)
	first, second := newAdmin(t, server.URL), newAdmin(t, server.URL)
	account := create(t, first, "planner", identity.RolePlanner)
	finished := make(chan error, 1)
	go func() { _, err := first.ResetPassword(t.Context(), account.ID); finished <- err }()
	<-entered
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err := second.Update(ctx, account.ID, identity.Change{Role: role(identity.RoleAdmin)})
	close(release)
	if !errors.Is(err, context.Canceled) {
		t.Errorf("concurrent change: %v", err)
	}
	if err := <-finished; err != nil {
		t.Fatal(err)
	}
	changed, err := second.Update(t.Context(), account.ID, identity.Change{Role: role(identity.RoleAdmin)})
	if err != nil {
		t.Fatal(err)
	}
	if changed.Role != identity.RoleAdmin || !changed.MustChangePassword {
		t.Fatalf("reset/update: %+v", changed)
	}
}

func TestCreatePreservesKratosFieldRefusal(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			_, _ = w.Write([]byte(`[]`))
			return
		}
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":{"code":400,"reason":"synthetic invalid traits"}}`))
	}))
	t.Cleanup(server.Close)
	_, err := newAdmin(t, server.URL).Create(t.Context(), identity.CreateParams{Username: "synthetic", Email: "invalid"})
	var invalid *identity.Invalid
	if !errors.As(err, &invalid) {
		t.Fatalf("refusal: %v", err)
	}
}

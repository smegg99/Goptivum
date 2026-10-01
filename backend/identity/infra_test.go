// identity/infra_test.go

package identity_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/smegg99/goptivum/backend/identity"
)

// realKratos is a running oryd/kratos. Nothing here can be proved against a
// fake: the whole mechanism rests on what Kratos does to a credential's
// timestamp when its own settings flow changes a password, and a fake would
// only prove what this test already assumed.
func realKratos(t *testing.T) (*identity.Admin, string) {
	t.Helper()
	admin := os.Getenv("GOPTIVUM_TEST_KRATOS_ADMIN")
	public := os.Getenv("GOPTIVUM_TEST_KRATOS_PUBLIC")
	if admin == "" || public == "" {
		t.Skip("no GOPTIVUM_TEST_KRATOS_ADMIN and GOPTIVUM_TEST_KRATOS_PUBLIC")
	}
	client, err := identity.NewAdmin(admin, 10*time.Second)
	if err != nil {
		t.Fatalf("admin client: %v", err)
	}
	return client, public
}

// The first-sign-in flag is only safe to enforce because it can be satisfied.
// This is the whole loop: raise it, prove it refuses, change the password the
// way a person actually does, prove it now passes, and clear it.
func TestInfraFirstSignInFlagIsSatisfiedByARealPasswordChange(t *testing.T) {
	accounts, public := realKratos(t)
	ctx := context.Background()

	username := fmt.Sprintf("firstsignin-%d", time.Now().UnixNano())
	const initial = "initial-password-123456"
	const changed = "a-brand-new-password-987654"

	created, err := accounts.Create(ctx, identity.CreateParams{
		Username: username, Email: username + "@example.test",
		Password: initial, Role: identity.RolePlanner, MustChangePassword: true,
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	t.Cleanup(func() { _ = accounts.Delete(context.Background(), created.ID) })

	account, err := accounts.Get(ctx, created.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if !account.MustChangePassword {
		t.Fatal("the account was created without the flag")
	}
	// Raising the flag has to record what the credential read, or nothing can
	// ever satisfy it and the account is locked out for good.
	if account.PasswordMark == "" {
		t.Fatal("the flag was raised with nothing to compare a later password against")
	}

	changedNow, err := accounts.PasswordChanged(ctx, account)
	if err != nil {
		t.Fatalf("password changed: %v", err)
	}
	if changedNow {
		t.Error("an account that has done nothing reports a changed password")
	}

	// The real settings flow, over the native API the desktop uses.
	token := signIn(t, public, username, initial)
	settings(t, public, token, changed)

	after, err := accounts.Get(ctx, created.ID)
	if err != nil {
		t.Fatalf("get after: %v", err)
	}
	changedNow, err = accounts.PasswordChanged(ctx, after)
	if err != nil {
		t.Fatalf("password changed after: %v", err)
	}
	if !changedNow {
		t.Fatal("a real password change was not detected, so the account stays locked out")
	}

	if err := accounts.ClearMustChangePassword(ctx, created.ID); err != nil {
		t.Fatalf("clear: %v", err)
	}
	cleared, err := accounts.Get(ctx, created.ID)
	if err != nil {
		t.Fatalf("get cleared: %v", err)
	}
	if cleared.MustChangePassword {
		t.Error("the flag survived being cleared")
	}
	// The stamp goes with it, so a future credential write has nothing stale
	// to be compared against.
	if cleared.PasswordMark != "" {
		t.Errorf("the stamp survived being cleared: %q", cleared.PasswordMark)
	}

	// And the new password is the one that works.
	signIn(t, public, username, changed)
}

// An administrator resetting a password raises the flag again, and the stamp
// has to move with it. Measuring against the old credential would report the
// reset itself as the user's own change and let them straight back in.
func TestInfraAnAdminResetRaisesTheFlagAgainstTheNewCredential(t *testing.T) {
	accounts, _ := realKratos(t)
	ctx := context.Background()

	username := fmt.Sprintf("reset-%d", time.Now().UnixNano())
	created, err := accounts.Create(ctx, identity.CreateParams{
		Username: username, Email: username + "@example.test",
		Password: "initial-password-123456", Role: identity.RolePlanner,
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	t.Cleanup(func() { _ = accounts.Delete(context.Background(), created.ID) })

	// The reset the handler runs: set the password, raise the flag, end the
	// sessions.
	if _, err := accounts.ResetPassword(ctx, created.ID); err != nil {
		t.Fatalf("reset: %v", err)
	}

	account, err := accounts.Get(ctx, created.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	changed, err := accounts.PasswordChanged(ctx, account)
	if err != nil {
		t.Fatalf("password changed: %v", err)
	}
	if changed {
		t.Error("the reset was read as the account changing its own password")
	}
}

// A duplicate, a disable and a reset, against the real thing: the 409, the
// sessions that end, and the 404 Kratos gives for an account with none left.
func TestInfraAccountsAndSessions(t *testing.T) {
	accounts, public := realKratos(t)
	ctx := context.Background()

	username := fmt.Sprintf("sessions-%d", time.Now().UnixNano())
	const password = "sessions-password-123456"
	created, err := accounts.Create(ctx, identity.CreateParams{
		Username: username, Email: username + "@example.test",
		Password: password, Role: identity.RolePlanner,
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	t.Cleanup(func() { _ = accounts.Delete(context.Background(), created.ID) })

	_, err = accounts.Create(ctx, identity.CreateParams{
		Username: username + "-2", Email: strings.ToUpper(username) + "@example.test",
		Password: password, Role: identity.RolePlanner,
	})
	if !errors.Is(err, identity.ErrExists) {
		t.Errorf("a second account with the same email: err = %v, want ErrExists", err)
	}

	found, err := accounts.FindByIdentifier(ctx, strings.ToUpper(username))
	if err != nil || found.ID != created.ID {
		t.Errorf("find by username = %+v, %v", found, err)
	}

	first := signIn(t, public, username, password)
	signIn(t, public, username+"@example.test", password)
	sessions, err := accounts.Sessions(ctx, created.ID)
	if err != nil || len(sessions) != 2 {
		t.Fatalf("sessions = %+v, %v; want two", sessions, err)
	}
	if err := accounts.EndSession(ctx, created.ID, sessions[0].ID); err != nil {
		t.Fatalf("end one: %v", err)
	}
	if left, _ := accounts.Sessions(ctx, created.ID); len(left) != 1 {
		t.Errorf("after ending one: %d left, want 1", len(left))
	}

	if _, err := accounts.Update(ctx, created.ID, identity.Change{Active: ptr(false)}); err != nil {
		t.Fatalf("disable: %v", err)
	}
	if left, _ := accounts.Sessions(ctx, created.ID); len(left) != 0 {
		t.Errorf("after disabling: %d sessions left", len(left))
	}
	if status := whoami(t, public, first); status != http.StatusUnauthorized {
		t.Errorf("whoami with a disabled account's token = %d, want 401", status)
	}
	// Nothing left to end is not a failure.
	if err := accounts.EndSessions(ctx, created.ID); err != nil {
		t.Errorf("end sessions of an account with none: %v", err)
	}

	if _, err := accounts.Update(ctx, created.ID, identity.Change{Active: ptr(true)}); err != nil {
		t.Fatalf("enable: %v", err)
	}
	token := signIn(t, public, username, password)
	if _, err := accounts.ResetPassword(ctx, created.ID); err != nil {
		t.Fatalf("reset: %v", err)
	}
	if status := whoami(t, public, token); status != http.StatusUnauthorized {
		t.Errorf("whoami after a reset = %d, want 401", status)
	}
}

func ptr[T any](value T) *T { return &value }

// whoami is the status Kratos gives a session token.
func whoami(t *testing.T, public, token string) int {
	t.Helper()
	request, err := http.NewRequest(http.MethodGet, public+"/sessions/whoami", nil)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	request.Header.Set("X-Session-Token", token)
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatalf("whoami: %v", err)
	}
	response.Body.Close()
	return response.StatusCode
}

// signIn runs the native API login flow and returns the session token.
func signIn(t *testing.T, public, username, password string) string {
	t.Helper()
	var flow struct {
		ID string `json:"id"`
	}
	get(t, public+"/self-service/login/api", &flow)

	var answer struct {
		SessionToken string `json:"session_token"`
	}
	post(t, public+"/self-service/login?flow="+flow.ID, "", map[string]any{
		"method": "password", "identifier": username, "password": password,
	}, &answer)
	if answer.SessionToken == "" {
		t.Fatalf("no session token for %s", username)
	}
	return answer.SessionToken
}

// settings changes the password through the flow a person uses.
func settings(t *testing.T, public, token, password string) {
	t.Helper()
	var flow struct {
		ID string `json:"id"`
	}
	getWithToken(t, public+"/self-service/settings/api", token, &flow)

	var answer struct {
		State string `json:"state"`
	}
	post(t, public+"/self-service/settings?flow="+flow.ID, token, map[string]any{
		"method": "password", "password": password,
	}, &answer)
	if answer.State != "success" {
		t.Fatalf("settings flow state = %q, want success", answer.State)
	}
}

func get(t *testing.T, url string, into any) { getWithToken(t, url, "", into) }

func getWithToken(t *testing.T, url, token string, into any) {
	t.Helper()
	request, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	do(t, request, into)
}

func post(t *testing.T, url, token string, body map[string]any, into any) {
	t.Helper()
	encoded, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	request, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(encoded))
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	request.Header.Set("Content-Type", "application/json")
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	do(t, request, into)
}

func do(t *testing.T, request *http.Request, into any) {
	t.Helper()
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatalf("%s %s: %v", request.Method, request.URL.Path, err)
	}
	defer response.Body.Close()
	if response.StatusCode >= 400 {
		t.Fatalf("%s %s = %d", request.Method, request.URL.Path, response.StatusCode)
	}
	if err := json.NewDecoder(response.Body).Decode(into); err != nil {
		t.Fatalf("%s %s: decode: %v", request.Method, request.URL.Path, err)
	}
}

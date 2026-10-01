// identity/identity_test.go

package identity_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/smegg99/goptivum/backend/identity"
	"github.com/smegg99/goptivum/backend/identity/identitytest"
)

func newAdmin(t *testing.T, url string) *identity.Admin {
	t.Helper()
	admin, err := identity.NewAdmin(url, 2*time.Second)
	if err != nil {
		t.Fatalf("new admin: %v", err)
	}
	return admin
}

func TestCreateCarriesRoleInMetadataNotTraits(t *testing.T) {
	fake, url := identitytest.New(t)
	admin := newAdmin(t, url)

	account, err := admin.Create(t.Context(), identity.CreateParams{
		Username: "headmaster", Email: "head@szkola.test",
		Password: "a-real-password", Role: identity.RoleAdmin, MustChangePassword: true,
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if account.Role != identity.RoleAdmin || !account.MustChangePassword {
		t.Errorf("account = %+v, want the admin role and the change flag", account)
	}

	stored := fake.Identities[account.ID]
	traits, _ := stored["traits"].(map[string]any)
	if _, leaked := traits["role"]; leaked {
		t.Error("the role reached traits, which a user can rewrite for themselves")
	}
	meta, _ := stored["metadata_public"].(map[string]any)
	if meta["role"] != identity.RoleAdmin {
		t.Errorf("metadata_public = %v, want the role there", meta)
	}
}

// A listing that stops at the first page silently loses accounts, and a lost
// admin is one nobody can find to disable.
func TestListFollowsEveryPage(t *testing.T) {
	fake, url := identitytest.New(t)
	fake.PageSize = 2
	admin := newAdmin(t, url)

	for index := 0; index < 5; index++ {
		if _, err := admin.Create(t.Context(), identity.CreateParams{
			Username: fmt.Sprintf("teacher%d", index),
			Email:    fmt.Sprintf("teacher%d@szkola.test", index),
			Password: "a-real-password", Role: identity.RolePlanner,
		}); err != nil {
			t.Fatalf("create: %v", err)
		}
	}

	accounts, err := admin.List(t.Context())
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(accounts) != 5 {
		t.Fatalf("list returned %d accounts, want 5 across %d pages", len(accounts), 3)
	}
}

func TestUpdateLeavesTraitsAlone(t *testing.T) {
	fake, url := identitytest.New(t)
	admin := newAdmin(t, url)

	account, err := admin.Create(t.Context(), identity.CreateParams{
		Username: "teacher", Email: "teacher@szkola.test",
		Password: "a-real-password", Role: identity.RolePlanner,
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	after, err := admin.Update(t.Context(), account.ID, identity.Change{
		Role: role(identity.RoleAdmin), Active: active(false),
	})
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if after.Role != identity.RoleAdmin {
		t.Errorf("role = %q, want admin", after.Role)
	}
	if after.Active {
		t.Error("account is still active after being disabled")
	}
	if after.Username != "teacher" || after.Email != "teacher@szkola.test" {
		t.Errorf("traits changed: %+v", after)
	}
	if fake.Puts != 0 {
		t.Error("a role or state change used a full replace, which erases what it does not send")
	}
}

func TestGetReportsAMissingAccount(t *testing.T) {
	_, url := identitytest.New(t)
	admin := newAdmin(t, url)

	if _, err := admin.Get(t.Context(), "nobody"); err != identity.ErrNotFound {
		t.Errorf("err = %v, want ErrNotFound", err)
	}
}

// First boot mints one admin. A second boot must not mint another, or a
// restart would print a new password and leave two administrators behind.
func TestBootstrapIsIdempotent(t *testing.T) {
	fake, url := identitytest.New(t)
	admin := newAdmin(t, url)
	params := identity.BootstrapParams{Username: "admin", Email: "admin@goptivum.invalid"}

	first, err := identity.Bootstrap(t.Context(), admin, params)
	if err != nil {
		t.Fatalf("first boot: %v", err)
	}
	if !first.Created || first.Password == "" {
		t.Fatal("first boot created no administrator")
	}
	if first.Account.Role != identity.RoleAdmin || !first.Account.MustChangePassword {
		t.Errorf("account = %+v, want an admin that must change its password", first.Account)
	}

	second, err := identity.Bootstrap(t.Context(), admin, params)
	if err != nil {
		t.Fatalf("second boot: %v", err)
	}
	if second.Created {
		t.Error("the second boot minted a second administrator")
	}
	if second.Password != "" {
		t.Error("the second boot produced a password, which would be printed again")
	}
	if fake.Created != 1 {
		t.Errorf("created = %d, want 1 across two boots", fake.Created)
	}
}

// An unreachable Kratos must be an error the caller reports, never a silent
// second administrator on the next boot.
func TestBootstrapReportsAnUnreachableKratos(t *testing.T) {
	admin, err := identity.NewAdmin("http://127.0.0.1:1", 2*time.Second)
	if err != nil {
		t.Fatalf("new admin: %v", err)
	}
	result, err := identity.Bootstrap(t.Context(), admin,
		identity.BootstrapParams{Username: "admin", Email: "admin@goptivum.invalid"})
	if err == nil {
		t.Fatal("bootstrap succeeded against a dead Kratos")
	}
	if result.Created {
		t.Error("bootstrap reported a creation that did not happen")
	}
}

func TestBootstrapNeedsAUsernameAndEmail(t *testing.T) {
	_, url := identitytest.New(t)
	admin := newAdmin(t, url)

	for _, params := range []identity.BootstrapParams{
		{Username: "", Email: "admin@goptivum.invalid"},
		{Username: "admin", Email: ""},
	} {
		if _, err := identity.Bootstrap(t.Context(), admin, params); err == nil {
			t.Errorf("bootstrap accepted %+v", params)
		}
	}
}

// A fixed default password would put the same one on every school on the
// internet, so this checks the generator is actually a generator.
func TestGeneratedPasswordsDiffer(t *testing.T) {
	seen := map[string]bool{}
	for attempt := 0; attempt < 100; attempt++ {
		password, err := identity.GeneratePassword()
		if err != nil {
			t.Fatalf("generate: %v", err)
		}
		if seen[password] {
			t.Fatalf("generated the same password twice: %q", password)
		}
		seen[password] = true

		if len(password) != 23 {
			t.Fatalf("password %q is %d characters, want 20 symbols in 4 groups", password, len(password))
		}
		for _, symbol := range password {
			if symbol == '-' {
				continue
			}
			if !strings.ContainsRune("abcdefghijkmnpqrstuvwxyz23456789", symbol) {
				t.Fatalf("password %q carries %q, which is not in the alphabet", password, symbol)
			}
		}
	}
}

func TestAdminRefusesAnEmptyBaseURL(t *testing.T) {
	if _, err := identity.NewAdmin("   ", time.Second); err == nil {
		t.Error("an empty admin base URL was accepted")
	}
}

func TestProbeReportsKratosHealth(t *testing.T) {
	healthy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/health/ready" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer healthy.Close()

	probe, err := identity.Probe(healthy.URL, time.Second)
	if err != nil {
		t.Fatalf("probe: %v", err)
	}
	if err := probe(t.Context()); err != nil {
		t.Errorf("a healthy kratos reported %v", err)
	}

	dead, err := identity.Probe("http://127.0.0.1:1", time.Second)
	if err != nil {
		t.Fatalf("probe: %v", err)
	}
	if err := dead(context.Background()); err == nil {
		t.Error("an unreachable kratos reported ready")
	}
}

// A truncated listing hides accounts, and a hidden administrator is one nobody
// can find to disable. Running out of pages must be an error, not a short list.
func TestListRefusesToTruncate(t *testing.T) {
	fake, url := identitytest.New(t)
	fake.PageSize = 1
	admin := newAdmin(t, url)

	// One more account than maxPages can carry at one per page.
	for index := 0; index < 101; index++ {
		if _, err := admin.Create(t.Context(), identity.CreateParams{
			Username: fmt.Sprintf("teacher%03d", index),
			Email:    fmt.Sprintf("teacher%03d@szkola.test", index),
			Password: "a-real-password", Role: identity.RolePlanner,
		}); err != nil {
			t.Fatalf("create: %v", err)
		}
	}

	accounts, err := admin.List(t.Context())
	if err == nil {
		t.Fatalf("list returned %d accounts, want an error rather than a truncated list", len(accounts))
	}
	if !strings.Contains(err.Error(), "truncated") {
		t.Errorf("err = %v, want it to say the list was refused for being truncated", err)
	}
}

// Kratos sends a relative Link target today. An absolute one must not be
// concatenated onto the base URL, which would request a nonsense address.
func TestListFollowsAnAbsoluteLinkTarget(t *testing.T) {
	fake, url := identitytest.New(t)
	fake.PageSize = 2
	fake.AbsoluteLinks = true
	admin := newAdmin(t, url)

	for index := 0; index < 5; index++ {
		if _, err := admin.Create(t.Context(), identity.CreateParams{
			Username: fmt.Sprintf("teacher%d", index),
			Email:    fmt.Sprintf("teacher%d@szkola.test", index),
			Password: "a-real-password", Role: identity.RolePlanner,
		}); err != nil {
			t.Fatalf("create: %v", err)
		}
	}

	accounts, err := admin.List(t.Context())
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(accounts) != 5 {
		t.Errorf("list returned %d accounts, want 5", len(accounts))
	}
}

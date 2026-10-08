// cmd/users_test.go

package cmd

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/smegg99/goptivum/backend/common/messages"
	"github.com/smegg99/goptivum/backend/identity"
	"github.com/smegg99/goptivum/backend/identity/identitytest"
)

// usersConfig is a configuration whose Kratos admin API is the fake.
func usersConfig(t *testing.T, adminURL string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.yaml")
	body := "identity:\n  public_url: http://127.0.0.1:1\n  admin_url: " + adminURL + "\nlogging:\n  level: ERROR\n"
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return path
}

func runUsers(t *testing.T, config string, args ...string) (string, error) {
	t.Helper()
	root, _ := newRootCmd()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs(append([]string{"--config", config, "--lang", "en", "users", "set-role"}, args...))
	err := root.ExecuteContext(context.Background())
	return out.String(), err
}

func accountsIn(t *testing.T, url string) *identity.Admin {
	t.Helper()
	admin, err := identity.NewAdmin(url, 2*time.Second)
	if err != nil {
		t.Fatalf("admin: %v", err)
	}
	return admin
}

func faultKey(err error) string {
	var fault *messages.Fault
	if errors.As(err, &fault) {
		return fault.Key()
	}
	return ""
}

// The recovery path: an administrator who lost the role gets it back from the
// server host, by email or by username in any case, with the server running.
func TestSetRoleGivesTheAdminRoleBack(t *testing.T) {
	fake, url := identitytest.New(t)
	admin := accountsIn(t, url)
	head, err := admin.Create(t.Context(), identity.CreateParams{
		Username: "head", Email: "head@example.invalid", Password: "synthetic-password-123",
		Role: identity.RolePlanner,
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	config := usersConfig(t, url)

	for _, name := range []string{"head@example.invalid", "HEAD"} {
		fake.Identities[head.ID]["metadata_public"] = map[string]any{"role": "planner"}
		out, err := runUsers(t, config, name, "admin")
		if err != nil {
			t.Fatalf("set-role %s: %v\n%s", name, err, out)
		}
		meta, _ := fake.Identities[head.ID]["metadata_public"].(map[string]any)
		if meta["role"] != identity.RoleAdmin {
			t.Errorf("set-role %s: role = %v, want admin", name, meta["role"])
		}
		for _, want := range []string{"role set", "head@example.invalid", "admin", "enabled"} {
			if !strings.Contains(out, want) {
				t.Errorf("set-role %s printed %q, want %q in it", name, out, want)
			}
		}
	}
}

// The same guard as the Users page: the command cannot demote the last
// enabled administrator either.
func TestSetRoleRefusesToDemoteTheLastAdmin(t *testing.T) {
	_, url := identitytest.New(t)
	admin := accountsIn(t, url)
	if _, err := admin.Create(t.Context(), identity.CreateParams{
		Username: "head", Email: "head@example.invalid", Password: "synthetic-password-123",
		Role: identity.RoleAdmin,
	}); err != nil {
		t.Fatalf("create: %v", err)
	}

	_, err := runUsers(t, usersConfig(t, url), "head", "planner")
	if faultKey(err) != messages.KeyUsersLastAdmin {
		t.Errorf("err = %v, want users.lastAdmin", err)
	}
	if errors.Is(err, errUsage) {
		t.Error("a refused change is not a misuse; it must exit 1")
	}
}

func TestSetRoleMisuseIsAUsageError(t *testing.T) {
	_, url := identitytest.New(t)
	config := usersConfig(t, url)
	for name, args := range map[string][]string{
		"unknown role":   {"head@example.invalid", "superuser"},
		"no role":        {"head@example.invalid"},
		"extra argument": {"head@example.invalid", "admin", "again"},
		"nothing at all": {},
		"blank sign-in":  {"  ", "admin"},
	} {
		if _, err := runUsers(t, config, args...); !errors.Is(err, errUsage) {
			t.Errorf("%s: err = %v, want a usage error", name, err)
		}
	}
}

func TestSetRoleSaysWhenNoAccountHasTheName(t *testing.T) {
	_, url := identitytest.New(t)
	_, err := runUsers(t, usersConfig(t, url), "nobody@example.invalid", "admin")
	if faultKey(err) != messages.KeyCliUsersNoAccount {
		t.Errorf("err = %v, want cli.users.noAccount", err)
	}
}

func TestSetRoleNeedsAKratosAdminAddress(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte("identity:\n  public_url: \"\"\n  admin_url: \"\"\n"), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	_, err := runUsers(t, path, "head@example.invalid", "admin")
	if faultKey(err) != messages.KeyCliUsersNoIdentity {
		t.Errorf("err = %v, want cli.users.noIdentity", err)
	}
}

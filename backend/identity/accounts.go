// identity/accounts.go

package identity

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

// ErrLastAdmin refuses a change that would leave no account that is both an
// administrator and enabled.
var ErrLastAdmin = errors.New("identity: the school would have no enabled administrator left")

// ErrExists is a username or email another account already signs in with.
var ErrExists = errors.New("identity: another account already signs in with this username or email")

// Taken is ErrExists when this server found which field clashes: "username"
// or "email".
type Taken struct{ Field string }

func (t *Taken) Error() string { return fmt.Sprintf("%v (%s)", ErrExists, t.Field) }

// Is makes errors.Is(err, ErrExists) true for a Taken.
func (t *Taken) Is(target error) bool { return target == ErrExists }

// refuseTaken names the new account's field another account signs in with.
// Username and email share one namespace in Kratos and compare ignoring case.
func (a *Admin) refuseTaken(ctx context.Context, username, email string) error {
	accounts, err := a.List(ctx)
	if err != nil {
		return err
	}
	for _, field := range []struct{ name, value string }{{"username", username}, {"email", email}} {
		wanted := strings.TrimSpace(field.value)
		if wanted == "" {
			continue
		}
		for _, account := range accounts {
			if strings.EqualFold(account.Username, wanted) || strings.EqualFold(account.Email, wanted) {
				return &Taken{Field: field.name}
			}
		}
	}
	return nil
}

// Change is what an administrator changes about an account. A nil field is
// left as it is.
type Change struct {
	Role   *string
	Active *bool
}

// Update applies a change unless it would leave the school without an enabled
// administrator, and ends every session of an account it disables.
func (a *Admin) Update(ctx context.Context, id string, change Change) (*Account, error) {
	if change.Role != nil && *change.Role != RoleAdmin && *change.Role != RolePlanner {
		return nil, fmt.Errorf("identity: unknown role %q", *change.Role)
	}

	a.changes.Lock()
	defer a.changes.Unlock()

	accounts, err := a.List(ctx)
	if err != nil {
		return nil, err
	}
	if leavesNoAdmin(accounts, id, change) {
		return nil, ErrLastAdmin
	}
	if !holds(accounts, id) {
		return nil, ErrNotFound
	}

	if change.Role != nil {
		if err := a.setRole(ctx, id, *change.Role); err != nil {
			return nil, err
		}
	}
	if change.Active != nil {
		if err := a.setActive(ctx, id, *change.Active); err != nil {
			return nil, err
		}
		if !*change.Active {
			if err := a.EndSessions(ctx, id); err != nil {
				return nil, err
			}
		}
	}
	return a.Get(ctx, id)
}

// ResetPassword replaces the password with a generated one, makes the account
// change it at its next sign-in, and ends the sessions the old one opened.
func (a *Admin) ResetPassword(ctx context.Context, id string) (string, error) {
	password, err := GeneratePassword()
	if err != nil {
		return "", err
	}
	if err := a.setPassword(ctx, id, password); err != nil {
		return "", err
	}
	// Whoever reset it has seen it, so the account has to change it again.
	if err := a.SetMustChangePassword(ctx, id, true); err != nil {
		return "", err
	}
	if err := a.EndSessions(ctx, id); err != nil {
		return "", err
	}
	return password, nil
}

// leavesNoAdmin reports whether the change takes the last enabled
// administrator away. A school with none already is not stopped from changing
// anyone else.
func leavesNoAdmin(accounts []Account, id string, change Change) bool {
	target, others := false, 0
	for _, account := range accounts {
		if !enabledAdmin(account) {
			continue
		}
		if account.ID == id {
			target = true
			continue
		}
		others++
	}
	if !target || others > 0 {
		return false
	}
	after := Account{Role: RoleAdmin, Active: true}
	if change.Role != nil {
		after.Role = *change.Role
	}
	if change.Active != nil {
		after.Active = *change.Active
	}
	return !enabledAdmin(after)
}

func enabledAdmin(account Account) bool { return account.Role == RoleAdmin && account.Active }

func holds(accounts []Account, id string) bool {
	for _, account := range accounts {
		if account.ID == id {
			return true
		}
	}
	return false
}

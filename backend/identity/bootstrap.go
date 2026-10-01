// identity/bootstrap.go

package identity

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"strings"
)

// passwordAlphabet has 32 symbols and no look-alike pairs, so a byte masked to
// five bits selects one without bias and a person reading the password aloud
// cannot confuse l with 1 or O with 0.
const passwordAlphabet = "abcdefghijkmnpqrstuvwxyz23456789"

// passwordSymbols is 20 symbols, which is 100 bits from that alphabet.
const passwordSymbols = 20

// passwordGroup is how many symbols go between dashes.
const passwordGroup = 5

// BootstrapParams names the first account.
type BootstrapParams struct {
	Username string
	Email    string
}

// BootstrapResult is what happened. Password is set only when Created is true,
// and it is the only time this value exists anywhere.
type BootstrapResult struct {
	Created  bool
	Account  *Account
	Password string
}

// Bootstrap mints one admin when the deployment has none.
//
// Idempotency is a question to Kratos rather than a local marker: a second
// boot finds the existing admin and does nothing, and so does a boot against a
// database restored from a backup.
func Bootstrap(ctx context.Context, admin *Admin, params BootstrapParams) (BootstrapResult, error) {
	if admin == nil {
		return BootstrapResult{}, fmt.Errorf("identity: no admin client")
	}
	if strings.TrimSpace(params.Username) == "" || strings.TrimSpace(params.Email) == "" {
		return BootstrapResult{}, fmt.Errorf("identity: bootstrap needs a username and an email")
	}

	existing, err := admin.FirstWithRole(ctx, RoleAdmin)
	switch {
	case err == nil:
		return BootstrapResult{Account: existing}, nil
	case !errors.Is(err, ErrNotFound):
		return BootstrapResult{}, err
	}

	password, err := GeneratePassword()
	if err != nil {
		return BootstrapResult{}, err
	}
	account, err := admin.Create(ctx, CreateParams{
		Username:           params.Username,
		Email:              params.Email,
		Password:           password,
		Role:               RoleAdmin,
		MustChangePassword: true,
	})
	if err != nil {
		return BootstrapResult{}, err
	}
	return BootstrapResult{Created: true, Account: account, Password: password}, nil
}

// GeneratePassword returns about 100 bits of entropy in dashed groups. A fixed
// default password would put the same one on every school on the internet.
func GeneratePassword() (string, error) {
	raw := make([]byte, passwordSymbols)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("identity: read random bytes: %w", err)
	}

	var built strings.Builder
	for index, value := range raw {
		if index > 0 && index%passwordGroup == 0 {
			built.WriteByte('-')
		}
		// The alphabet is exactly 32 long, so five bits map onto it evenly and
		// no value is more likely than another.
		built.WriteByte(passwordAlphabet[value&0x1F])
	}
	return built.String(), nil
}

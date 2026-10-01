// identity/admin.go

// Package identity talks to Kratos's admin API on the loopback port.
//
// kratosauth.Admin reads only, by lookup, with no List and no create, so every
// write and the listing live here until S99AB grows write support upstream.
package identity

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// The only two roles. They live in metadata_public, which Kratos writes only
// through this API; a role in traits would be a self-service promotion.
const (
	RoleAdmin   = "admin"
	RolePlanner = "planner"
)

// Keys inside metadata_public.
const (
	MetaRole               = "role"
	MetaMustChangePassword = "must_change_password"
	// MetaPasswordMark is a digest of the stored password hash as it read when
	// the first-sign-in flag was raised, and lives in metadata_admin.
	//
	// The obvious signal, the credential's own updated_at, is wrong: Kratos
	// rewrites the whole identity on any update, so raising the flag moves that
	// timestamp itself and every account would read as already changed.
	// Measured against v1.3.1. The stored hash moves only when the password
	// does, and bcrypt salts randomly so even re-setting the same password
	// moves it.
	MetaPasswordMark = "password_mark"
)

// StateActive and StateInactive are Kratos's own account states. An inactive
// account cannot sign in; the credential is left alone.
const (
	StateActive   = "active"
	StateInactive = "inactive"
)

// ErrNotFound is a lookup that matched nothing.
var ErrNotFound = errors.New("identity: no such account")

// maxPages bounds the listing so a paging bug cannot spin forever.
const maxPages = 100

// pageSize is Kratos's maximum.
const pageSize = 250

// Account is one Kratos identity, reduced to what this server uses.
type Account struct {
	ID                 string
	Username           string
	Email              string
	Name               string
	Active             bool
	Role               string
	MustChangePassword bool
	// PasswordMark is the digest recorded when the flag was raised, and empty
	// when it never was. It is admin-only metadata and never leaves this
	// server.
	PasswordMark string
}

// Admin calls Kratos's admin API.
type Admin struct {
	base   *url.URL
	client *http.Client
	// changes serialises Update, so two administrators demoting each other at
	// once cannot both pass the last-admin check. It covers this process only.
	changes sync.Mutex
}

// NewAdmin builds a client for the admin base URL, which must never be
// reachable beyond loopback.
func NewAdmin(baseURL string, timeout time.Duration) (*Admin, error) {
	if strings.TrimSpace(baseURL) == "" {
		return nil, fmt.Errorf("identity: admin base URL is required")
	}
	parsed, err := url.Parse(strings.TrimRight(baseURL, "/"))
	if err != nil {
		return nil, fmt.Errorf("identity: parse admin base URL: %w", err)
	}
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	return &Admin{base: parsed, client: &http.Client{Timeout: timeout}}, nil
}

// wireIdentity is Kratos's identity document, only the fields used here.
type wireIdentity struct {
	ID             string              `json:"id,omitempty"`
	SchemaID       string              `json:"schema_id,omitempty"`
	State          string              `json:"state,omitempty"`
	Traits         wireTraits          `json:"traits"`
	MetadataPublic json.RawMessage     `json:"metadata_public,omitempty"`
	MetadataAdmin  json.RawMessage     `json:"metadata_admin,omitempty"`
	Credentials    wireReadCredentials `json:"credentials,omitempty"`
}

// wireReadCredentials is what Kratos returns, which is not what it accepts:
// the write shape carries a plaintext password and the read shape carries
// timestamps and a hash.
type wireReadCredentials struct {
	Password struct {
		Config struct {
			HashedPassword string `json:"hashed_password"`
		} `json:"config"`
	} `json:"password"`
}

type wireTraits struct {
	Username string `json:"username"`
	Email    string `json:"email"`
	Name     string `json:"name,omitempty"`
}

// wireCreate is the identity-creation contract, verified against v1.3.1.
type wireCreate struct {
	SchemaID       string          `json:"schema_id"`
	State          string          `json:"state,omitempty"`
	Traits         wireTraits      `json:"traits"`
	Credentials    wireCredentials `json:"credentials"`
	MetadataPublic json.RawMessage `json:"metadata_public,omitempty"`
	MetadataAdmin  json.RawMessage `json:"metadata_admin,omitempty"`
}

type wireCredentials struct {
	Password wirePassword `json:"password"`
}

type wirePassword struct {
	Config wirePasswordConfig `json:"config"`
}

type wirePasswordConfig struct {
	Password string `json:"password"`
}

// CreateParams is a new account. Role is stored in metadata_public.
type CreateParams struct {
	Username           string
	Email              string
	Name               string
	Password           string
	Role               string
	MustChangePassword bool
	// PasswordMark is the digest recorded when the flag was raised, and empty
	// when it never was. It is admin-only metadata and never leaves this
	// server.
	PasswordMark string
}

// Create mints an account with a password already set. A username or email
// another account already signs in with is ErrExists.
func (a *Admin) Create(ctx context.Context, params CreateParams) (*Account, error) {
	if err := a.refuseTaken(ctx, params.Username, params.Email); err != nil {
		return nil, err
	}
	body := wireCreate{
		SchemaID: "default",
		State:    StateActive,
		Traits: wireTraits{
			Username: params.Username,
			Email:    params.Email,
			Name:     params.Name,
		},
		MetadataPublic: metadata(params.Role, params.MustChangePassword),
	}
	body.Credentials.Password.Config.Password = params.Password

	var created wireIdentity
	if err := a.call(ctx, http.MethodPost, "/admin/identities", body, &created); err != nil {
		return nil, err
	}
	account := toAccount(created)
	if params.MustChangePassword {
		// The stamp can only be read once the credential exists, so the flag is
		// raised again here to measure it. Without it the flag can never be
		// satisfied and the account is locked out for good.
		if err := a.SetMustChangePassword(ctx, account.ID, true); err != nil {
			return nil, err
		}
		account.MustChangePassword = true
	}
	return &account, nil
}

// List returns every account, following Kratos's page token to the end.
//
// Running out of pages is an error rather than a short list: a truncated
// listing hides accounts, and a hidden administrator is one nobody can find to
// disable.
func (a *Admin) List(ctx context.Context) ([]Account, error) {
	accounts := []Account{}
	next := fmt.Sprintf("/admin/identities?page_size=%d", pageSize)
	for page := 0; next != ""; page++ {
		if page >= maxPages {
			return nil, fmt.Errorf("identity: more than %d pages of accounts, refusing a truncated list",
				maxPages)
		}
		var batch []wireIdentity
		link, err := a.callWithHeader(ctx, http.MethodGet, next, nil, &batch)
		if err != nil {
			return nil, err
		}
		for _, one := range batch {
			accounts = append(accounts, toAccount(one))
		}
		next = nextPage(link)
	}
	return accounts, nil
}

// Get is one account by id.
func (a *Admin) Get(ctx context.Context, id string) (*Account, error) {
	var found wireIdentity
	if err := a.call(ctx, http.MethodGet, "/admin/identities/"+url.PathEscape(id), nil, &found); err != nil {
		return nil, err
	}
	account := toAccount(found)
	return &account, nil
}

// FindByIdentifier is the account that signs in with a username or an email.
// Kratos compares sign-in names ignoring case.
func (a *Admin) FindByIdentifier(ctx context.Context, identifier string) (*Account, error) {
	var found []wireIdentity
	path := "/admin/identities?credentials_identifier=" + url.QueryEscape(strings.TrimSpace(identifier))
	if err := a.call(ctx, http.MethodGet, path, nil, &found); err != nil {
		return nil, err
	}
	if len(found) == 0 {
		return nil, ErrNotFound
	}
	account := toAccount(found[0])
	return &account, nil
}

// FirstWithRole reports whether any account already holds a role, which is how
// first-boot decides it has nothing to do.
func (a *Admin) FirstWithRole(ctx context.Context, role string) (*Account, error) {
	accounts, err := a.List(ctx)
	if err != nil {
		return nil, err
	}
	for _, account := range accounts {
		if account.Role == role {
			return &account, nil
		}
	}
	return nil, ErrNotFound
}

// patchOp is one RFC 6902 operation.
type patchOp struct {
	Op    string `json:"op"`
	Path  string `json:"path"`
	Value any    `json:"value,omitempty"`
}

// setRole rewrites only the role. A patch leaves traits untouched, which a
// full replace would not. Update is the way in, because it guards the last
// administrator.
func (a *Admin) setRole(ctx context.Context, id, role string) error {
	if role != RoleAdmin && role != RolePlanner {
		return fmt.Errorf("identity: unknown role %q", role)
	}
	return a.patch(ctx, id, []patchOp{{Op: "add", Path: "/metadata_public/" + MetaRole, Value: role}})
}

// setActive enables or disables signing in. The credential is left in place,
// so re-enabling needs no new password.
func (a *Admin) setActive(ctx context.Context, id string, active bool) error {
	state := StateInactive
	if active {
		state = StateActive
	}
	return a.patch(ctx, id, []patchOp{{Op: "replace", Path: "/state", Value: state}})
}

// SetMustChangePassword raises or clears the first-sign-in flag.
//
// Raising it also records what the password credential reads right now, which
// is the only way the flag can ever be satisfied: Kratos owns the settings
// flow, changes the credential there, and never touches this metadata. A later
// credential timestamp is the evidence, and without a value to compare against
// there is none.
func (a *Admin) SetMustChangePassword(ctx context.Context, id string, must bool) error {
	ops := []patchOp{{Op: "add", Path: "/metadata_public/" + MetaMustChangePassword, Value: must}}
	if must {
		mark, err := a.passwordMark(ctx, id)
		if err != nil {
			return err
		}
		ops = append(ops, patchOp{
			Op: "add", Path: "/metadata_admin/" + MetaPasswordMark, Value: mark,
		})
	}
	return a.patch(ctx, id, ops)
}

// passwordMark is a digest of the stored password hash, which changes when and
// only when the password does.
//
// A digest and not the hash, and in metadata_admin rather than metadata_public,
// so nothing credential-derived is ever readable by the account it belongs to.
func (a *Admin) passwordMark(ctx context.Context, id string) (string, error) {
	var found wireIdentity
	path := "/admin/identities/" + url.PathEscape(id) + "?include_credential=password"
	if err := a.call(ctx, http.MethodGet, path, nil, &found); err != nil {
		return "", err
	}
	hashed := found.Credentials.Password.Config.HashedPassword
	if hashed == "" {
		return "", nil
	}
	sum := sha256.Sum256([]byte(hashed))
	return hex.EncodeToString(sum[:]), nil
}

// PasswordChanged reports whether the account has set its own password since
// the first-sign-in flag was raised.
//
// A missing mark answers no. That is the safe reading: an account that is
// already refused stays refused rather than being let through on an absence of
// evidence.
func (a *Admin) PasswordChanged(ctx context.Context, account *Account) (bool, error) {
	if account == nil || account.PasswordMark == "" {
		return false, nil
	}
	mark, err := a.passwordMark(ctx, account.ID)
	if err != nil {
		return false, err
	}
	return mark != "" && mark != account.PasswordMark, nil
}

// ClearMustChangePassword drops the flag and the mark it was measured against,
// so nothing stale is left for a future credential to be compared to.
func (a *Admin) ClearMustChangePassword(ctx context.Context, id string) error {
	return a.patch(ctx, id, []patchOp{
		{Op: "add", Path: "/metadata_public/" + MetaMustChangePassword, Value: false},
		{Op: "remove", Path: "/metadata_admin/" + MetaPasswordMark},
	})
}

// setPassword replaces the password. ResetPassword is the way in, because it
// also ends the sessions the old password opened.
//
// This is a read-modify-write through PUT on purpose. A JSON patch of
// /credentials/password/config/password answers 200 and destroys the
// credential: neither the old nor the new password then signs in.
func (a *Admin) setPassword(ctx context.Context, id, password string) error {
	var current wireIdentity
	if err := a.call(ctx, http.MethodGet, "/admin/identities/"+url.PathEscape(id), nil, &current); err != nil {
		return err
	}
	// PUT is a full replace, so everything read above is sent back verbatim.
	body := wireCreate{
		SchemaID:       orDefault(current.SchemaID, "default"),
		State:          orDefault(current.State, StateActive),
		Traits:         current.Traits,
		MetadataPublic: current.MetadataPublic,
		// Sent back too, or this replace drops it: the first-sign-in mark lives
		// there and losing it locks the account out for good.
		MetadataAdmin: current.MetadataAdmin,
	}
	body.Credentials.Password.Config.Password = password
	return a.call(ctx, http.MethodPut, "/admin/identities/"+url.PathEscape(id), body, nil)
}

// Delete removes an account outright.
func (a *Admin) Delete(ctx context.Context, id string) error {
	return a.call(ctx, http.MethodDelete, "/admin/identities/"+url.PathEscape(id), nil, nil)
}

func (a *Admin) patch(ctx context.Context, id string, ops []patchOp) error {
	return a.call(ctx, http.MethodPatch, "/admin/identities/"+url.PathEscape(id), ops, nil)
}

func (a *Admin) call(ctx context.Context, method, path string, body, into any) error {
	_, err := a.callWithHeader(ctx, method, path, body, into)
	return err
}

// callWithHeader performs one request and returns the Link header, which is
// how Kratos reports the next page.
func (a *Admin) callWithHeader(ctx context.Context, method, path string, body, into any) (string, error) {
	var payload io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return "", fmt.Errorf("identity: encode request: %w", err)
		}
		payload = bytes.NewReader(encoded)
	}

	request, err := http.NewRequestWithContext(ctx, method, a.resolve(path), payload)
	if err != nil {
		return "", fmt.Errorf("identity: build request: %w", err)
	}
	if body != nil {
		request.Header.Set("Content-Type", contentType(method))
	}

	response, err := a.client.Do(request)
	if err != nil {
		return "", fmt.Errorf("identity: call kratos: %w", err)
	}
	defer response.Body.Close()

	switch {
	case response.StatusCode == http.StatusNotFound:
		return "", ErrNotFound
	case response.StatusCode == http.StatusConflict:
		// Kratos answers 409 to an identity write whose username or email
		// another identity already signs in with, and says no more.
		return "", ErrExists
	case response.StatusCode >= 300:
		// The body carries Kratos's reason; it holds no credential.
		detail, _ := io.ReadAll(io.LimitReader(response.Body, 2048))
		return "", fmt.Errorf("identity: kratos answered %d: %s",
			response.StatusCode, strings.TrimSpace(string(detail)))
	}

	if into != nil {
		if err := json.NewDecoder(response.Body).Decode(into); err != nil {
			return "", fmt.Errorf("identity: decode response: %w", err)
		}
	}
	return response.Header.Get("Link"), nil
}

// resolve turns a path into a URL. Kratos's Link header is relative today, and
// an absolute one must not be concatenated onto the base.
func (a *Admin) resolve(path string) string {
	if strings.HasPrefix(path, "http://") || strings.HasPrefix(path, "https://") {
		return path
	}
	return a.base.String() + path
}

// contentType is JSON everywhere except a patch, which has its own media type.
func contentType(method string) string {
	if method == http.MethodPatch {
		return "application/json-patch+json"
	}
	return "application/json"
}

// nextPage reads the rel="next" target out of a Link header, and returns an
// empty string at the end of the listing.
func nextPage(link string) string {
	for _, part := range strings.Split(link, ",") {
		segments := strings.Split(strings.TrimSpace(part), ";")
		if len(segments) < 2 {
			continue
		}
		target := strings.Trim(strings.TrimSpace(segments[0]), "<>")
		for _, attribute := range segments[1:] {
			if strings.EqualFold(strings.TrimSpace(attribute), `rel="next"`) {
				return target
			}
		}
	}
	return ""
}

// metadata builds the metadata_public document. Only this API can write it,
// which is what makes it the one field worth authorizing on.
func metadata(role string, mustChange bool) json.RawMessage {
	fields := map[string]any{}
	if role != "" {
		fields[MetaRole] = role
	}
	if mustChange {
		fields[MetaMustChangePassword] = true
	}
	if len(fields) == 0 {
		return nil
	}
	encoded, err := json.Marshal(fields)
	if err != nil {
		return nil
	}
	return encoded
}

func toAccount(from wireIdentity) Account {
	account := Account{
		ID:       from.ID,
		Username: from.Traits.Username,
		Email:    from.Traits.Email,
		Name:     from.Traits.Name,
		Active:   from.State != StateInactive,
	}
	if len(from.MetadataPublic) > 0 {
		var meta struct {
			Role               string `json:"role"`
			MustChangePassword bool   `json:"must_change_password"`
		}
		if err := json.Unmarshal(from.MetadataPublic, &meta); err == nil {
			account.Role = meta.Role
			account.MustChangePassword = meta.MustChangePassword
		}
	}
	if len(from.MetadataAdmin) > 0 {
		var private struct {
			PasswordMark string `json:"password_mark"`
		}
		if err := json.Unmarshal(from.MetadataAdmin, &private); err == nil {
			account.PasswordMark = private.PasswordMark
		}
	}
	return account
}

func orDefault(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}

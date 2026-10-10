// api/v1/admin.go

package v1

import (
	"context"
	"errors"
	"strconv"
	"strings"

	openapi_types "github.com/oapi-codegen/runtime/types"
	"github.com/smegg99/s99logger"

	"github.com/smegg99/goptivum/backend/api/v1/gen"
	"github.com/smegg99/goptivum/backend/common/logger"
	"github.com/smegg99/goptivum/backend/common/messages"
	"github.com/smegg99/goptivum/backend/core/audit"
	"github.com/smegg99/goptivum/backend/identity"
)

// Admin implements the administrative half of the contract: the school's
// accounts and the solving nodes it trusts.
//
// Every route here is mounted behind RequireMeta("role", "admin"), so a
// handler never re-checks the role. What it does check is that the dependency
// exists, because a deployment with no identity provider still serves this
// binary.
type Admin struct {
	accounts *identity.Admin
	nodes    Nodes
	// trail records account and node changes; nil records nothing.
	trail *audit.Log
}

// Handlers is the whole contract in one value: the plans, the administrative
// half, the solving half, the school key, the development-only demo route, the
// check route, the export routes, the rule catalogue, the school's rule profiles, the sign-in
// security settings, the server settings, storage, the recent log lines and
// the audit trail, which are separate types because they share no state.
type Handlers struct {
	*API
	*Admin
	*Jobs
	*School
	*Demo
	*Checks
	*Exports
	*Rules
	*Profiles
	*Libraries
	*Security
	*ServerSettings
	*Storage
	*Logs
	*AuditTrail
}

// NewAdmin wires the administrative handlers. A nil dependency is not an
// error: it means that half is not configured, and every route it serves
// answers 404 rather than pretending.
func NewAdmin(accounts *identity.Admin, nodes Nodes) *Admin {
	return &Admin{accounts: accounts, nodes: nodes}
}

// WithAudit records account and node changes in trail.
func (a *Admin) WithAudit(trail *audit.Log) *Admin {
	a.trail = trail
	return a
}

// userTarget is an account as the audit trail names it.
func userTarget(account identity.Account) audit.Target {
	return audit.Target{Kind: audit.User, ID: account.ID, Name: account.Username}
}

// ListUsers pages the school's accounts, narrowed by q when it is given.
func (a *Admin) ListUsers(ctx context.Context, request gen.ListUsersRequestObject) (gen.ListUsersResponseObject, error) {
	if a.accounts == nil {
		return gen.ListUsers404JSONResponse{NotFoundJSONResponse: gen.NotFoundJSONResponse(notFound())}, nil
	}
	accounts, err := a.accounts.List(ctx)
	if err != nil {
		return nil, err
	}
	if request.Params.Q != nil {
		accounts = matching(accounts, *request.Params.Q)
	}

	page, limit := pageOf(request.Params.Page, request.Params.Limit)
	items := []gen.User{}
	for _, account := range pageSlice(accounts, page, limit) {
		items = append(items, toUser(account))
	}
	return gen.ListUsers200JSONResponse{
		Items: items, Page: page, Limit: limit, Total: len(accounts),
	}, nil
}

// CreateUser mints an account with a generated password.
func (a *Admin) CreateUser(ctx context.Context, request gen.CreateUserRequestObject) (gen.CreateUserResponseObject, error) {
	if a.accounts == nil {
		return gen.CreateUser404JSONResponse{NotFoundJSONResponse: gen.NotFoundJSONResponse(notFound())}, nil
	}
	body := request.Body
	if body == nil {
		return gen.CreateUser422JSONResponse{ValidationJSONResponse: gen.ValidationJSONResponse(validation("body", "required"))}, nil
	}
	username := strings.TrimSpace(body.Username)
	email := strings.TrimSpace(string(body.Email))
	if username == "" {
		return gen.CreateUser422JSONResponse{ValidationJSONResponse: gen.ValidationJSONResponse(validation("username", "required"))}, nil
	}
	if email == "" {
		return gen.CreateUser422JSONResponse{ValidationJSONResponse: gen.ValidationJSONResponse(validation("email", "required"))}, nil
	}
	role, ok := roleOf(body.Role)
	if !ok {
		return gen.CreateUser422JSONResponse{ValidationJSONResponse: gen.ValidationJSONResponse(validation("role", "unknown"))}, nil
	}

	// Generated rather than chosen: an administrator typing passwords for a
	// whole school picks the same one twice.
	password, err := identity.GeneratePassword()
	if err != nil {
		return nil, err
	}
	name := ""
	if body.Name != nil {
		name = *body.Name
	}
	account, err := a.accounts.Create(ctx, identity.CreateParams{
		Username: username, Email: email, Name: name,
		Password: password, Role: role, MustChangePassword: true,
	})
	var refused *identity.Invalid
	if errors.As(err, &refused) {
		return gen.CreateUser422JSONResponse{ValidationJSONResponse: gen.ValidationJSONResponse(validation("body", "invalid"))}, nil
	}
	if errors.Is(err, identity.ErrExists) {
		return gen.CreateUser409JSONResponse(taken(err)), nil
	}
	if err != nil {
		return nil, err
	}

	audited(ctx, a.trail, audit.UserCreated, userTarget(*account), map[string]string{"role": account.Role})
	// The only response that carries a password, and the only time this value
	// exists anywhere.
	return gen.CreateUser201JSONResponse{User: toUser(*account), Password: password}, nil
}

// GetUser reads one account.
func (a *Admin) GetUser(ctx context.Context, request gen.GetUserRequestObject) (gen.GetUserResponseObject, error) {
	if a.accounts == nil {
		return gen.GetUser404JSONResponse{NotFoundJSONResponse: gen.NotFoundJSONResponse(notFound())}, nil
	}
	account, err := a.accounts.Get(ctx, request.UserId)
	if errors.Is(err, identity.ErrNotFound) {
		return gen.GetUser404JSONResponse{NotFoundJSONResponse: gen.NotFoundJSONResponse(notFound())}, nil
	}
	if err != nil {
		return nil, err
	}
	return gen.GetUser200JSONResponse(toUser(*account)), nil
}

// UpdateUser sets a role, disables an account or re-enables one. A change that
// would leave no enabled administrator is refused whole.
func (a *Admin) UpdateUser(ctx context.Context, request gen.UpdateUserRequestObject) (gen.UpdateUserResponseObject, error) {
	if a.accounts == nil {
		return gen.UpdateUser404JSONResponse{NotFoundJSONResponse: gen.NotFoundJSONResponse(notFound())}, nil
	}
	body := request.Body
	if body == nil || (body.Role == nil && body.Active == nil) {
		return gen.UpdateUser422JSONResponse{ValidationJSONResponse: gen.ValidationJSONResponse(validation("body", "empty"))}, nil
	}
	change := identity.Change{Active: body.Active}
	if body.Role != nil {
		role, ok := roleOf(*body.Role)
		if !ok {
			return gen.UpdateUser422JSONResponse{ValidationJSONResponse: gen.ValidationJSONResponse(validation("role", "unknown"))}, nil
		}
		change.Role = &role
	}

	account, err := a.accounts.Update(ctx, request.UserId, change)
	switch {
	case errors.Is(err, identity.ErrNotFound):
		return gen.UpdateUser404JSONResponse{NotFoundJSONResponse: gen.NotFoundJSONResponse(notFound())}, nil
	case errors.Is(err, identity.ErrLastAdmin):
		return gen.UpdateUser409JSONResponse(gen.Error{Code: "conflict", Message: messages.KeyUsersLastAdmin}), nil
	case err != nil:
		return nil, err
	}
	if body.Active != nil && !*body.Active {
		signedOut(ctx, request.UserId, "disabled")
	}
	details := map[string]string{}
	if change.Role != nil {
		details["role"] = *change.Role
	}
	if change.Active != nil {
		details["active"] = strconv.FormatBool(*change.Active)
	}
	audited(ctx, a.trail, audit.UserChanged, userTarget(*account), details)
	return gen.UpdateUser200JSONResponse(toUser(*account)), nil
}

// ResetUserPassword replaces a password with a generated one and ends the
// sessions the old one opened.
func (a *Admin) ResetUserPassword(ctx context.Context, request gen.ResetUserPasswordRequestObject) (gen.ResetUserPasswordResponseObject, error) {
	if a.accounts == nil {
		return gen.ResetUserPassword404JSONResponse{NotFoundJSONResponse: gen.NotFoundJSONResponse(notFound())}, nil
	}
	account, err := a.accounts.Get(ctx, request.UserId)
	if errors.Is(err, identity.ErrNotFound) {
		return gen.ResetUserPassword404JSONResponse{NotFoundJSONResponse: gen.NotFoundJSONResponse(notFound())}, nil
	} else if err != nil {
		return nil, err
	}

	password, err := a.accounts.ResetPassword(ctx, request.UserId)
	if err != nil {
		return nil, err
	}
	signedOut(ctx, request.UserId, "password_reset")
	audited(ctx, a.trail, audit.UserPasswordReset, userTarget(*account), nil)
	return gen.ResetUserPassword200JSONResponse{Password: password}, nil
}

// taken is the 409 for a username or email another account signs in with,
// naming the field when this server found which one.
func taken(err error) gen.Error {
	out := gen.Error{Code: "conflict", Message: messages.KeyUsersAlreadyExists}
	var clash *identity.Taken
	if errors.As(err, &clash) {
		out.Details = &map[string]string{"Field": clash.Field}
	}
	return out
}

// matching keeps the accounts whose username, email or name contains the
// query, ignoring case. A blank query keeps them all.
func matching(accounts []identity.Account, query string) []identity.Account {
	needle := strings.ToLower(strings.TrimSpace(query))
	if needle == "" {
		return accounts
	}
	kept := []identity.Account{}
	for _, account := range accounts {
		for _, field := range []string{account.Username, account.Email, account.Name} {
			if strings.Contains(strings.ToLower(field), needle) {
				kept = append(kept, account)
				break
			}
		}
	}
	return kept
}

// signedOut logs that an account lost every session, and who did it.
func signedOut(ctx context.Context, userID, reason string) {
	by, _ := callerID(ctx)
	logger.Log.Info(s99logger.NewEvent(logger.EventUserSessionsEnded,
		s99logger.String("user_id", userID), s99logger.String("reason", reason),
		s99logger.String("by", by)))
}

// roleOf maps the contract's role onto the one the identity package writes.
func roleOf(role gen.Role) (string, bool) {
	switch string(role) {
	case identity.RoleAdmin:
		return identity.RoleAdmin, true
	case identity.RolePlanner:
		return identity.RolePlanner, true
	default:
		return "", false
	}
}

func toUser(account identity.Account) gen.User {
	user := gen.User{
		Id:                 account.ID,
		Username:           account.Username,
		Email:              openapi_types.Email(account.Email),
		Role:               gen.Role(account.Role),
		Active:             account.Active,
		MustChangePassword: account.MustChangePassword,
	}
	if account.Name != "" {
		name := account.Name
		user.Name = &name
	}
	return user
}

// pageSlice takes one page out of a list already read whole.
func pageSlice[T any](items []T, page, limit int) []T {
	from := pageOffset(page, limit)
	if from >= len(items) {
		return nil
	}
	to := from + min(limit, len(items)-from)
	return items[from:to]
}

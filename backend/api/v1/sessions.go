// api/v1/sessions.go

package v1

import (
	"context"
	"errors"

	"github.com/smegg99/s99ab/auth/kratosauth"

	"github.com/smegg99/goptivum/backend/api/v1/gen"
	"github.com/smegg99/goptivum/backend/core/audit"
	"github.com/smegg99/goptivum/backend/identity"
)

// ListUserSessions lists the sessions that still sign an account in.
func (a *Admin) ListUserSessions(ctx context.Context, request gen.ListUserSessionsRequestObject) (gen.ListUserSessionsResponseObject, error) {
	account, err := a.account(ctx, request.UserId)
	if err != nil {
		return nil, err
	}
	if account == nil {
		return gen.ListUserSessions404JSONResponse{NotFoundJSONResponse: gen.NotFoundJSONResponse(notFound())}, nil
	}
	sessions, err := a.accounts.Sessions(ctx, request.UserId)
	if err != nil {
		return nil, err
	}
	current := ""
	if caller, ok := kratosauth.FromContext(ctx); ok {
		current = caller.SessionID
	}
	items := make([]gen.UserSession, 0, len(sessions))
	for _, session := range sessions {
		items = append(items, toUserSession(session, current))
	}
	return gen.ListUserSessions200JSONResponse{Items: items}, nil
}

// RevokeUserSessions signs an account out everywhere.
func (a *Admin) RevokeUserSessions(ctx context.Context, request gen.RevokeUserSessionsRequestObject) (gen.RevokeUserSessionsResponseObject, error) {
	account, err := a.account(ctx, request.UserId)
	if err != nil {
		return nil, err
	}
	if account == nil {
		return gen.RevokeUserSessions404JSONResponse{NotFoundJSONResponse: gen.NotFoundJSONResponse(notFound())}, nil
	}
	if err := a.accounts.EndSessions(ctx, request.UserId); err != nil {
		return nil, err
	}
	signedOut(ctx, request.UserId, "signed_out")
	audited(ctx, a.trail, audit.UserSignedOut, userTarget(*account), nil)
	return gen.RevokeUserSessions204Response{}, nil
}

// RevokeUserSession ends one of an account's sessions.
func (a *Admin) RevokeUserSession(ctx context.Context, request gen.RevokeUserSessionRequestObject) (gen.RevokeUserSessionResponseObject, error) {
	missing := gen.RevokeUserSession404JSONResponse{NotFoundJSONResponse: gen.NotFoundJSONResponse(notFound())}
	account, err := a.account(ctx, request.UserId)
	if err != nil {
		return nil, err
	}
	if account == nil {
		return missing, nil
	}
	err = a.accounts.EndSession(ctx, request.UserId, request.SessionId)
	if errors.Is(err, identity.ErrNotFound) {
		return missing, nil
	}
	if err != nil {
		return nil, err
	}
	audited(ctx, a.trail, audit.UserSessionEnded, userTarget(*account),
		map[string]string{"session": request.SessionId})
	return gen.RevokeUserSession204Response{}, nil
}

// account reads an account, nil with no error when it does not exist or this
// deployment has no identity provider.
func (a *Admin) account(ctx context.Context, id string) (*identity.Account, error) {
	if a.accounts == nil {
		return nil, nil
	}
	account, err := a.accounts.Get(ctx, id)
	if errors.Is(err, identity.ErrNotFound) {
		return nil, nil
	}
	return account, err
}

func toUserSession(session identity.Session, current string) gen.UserSession {
	out := gen.UserSession{
		Id: session.ID, Current: session.ID == current,
		AuthenticatedAt: session.AuthenticatedAt, ExpiresAt: session.ExpiresAt,
	}
	if session.IPAddress != "" {
		address := session.IPAddress
		out.IpAddress = &address
	}
	if session.UserAgent != "" {
		agent := session.UserAgent
		out.UserAgent = &agent
	}
	return out
}

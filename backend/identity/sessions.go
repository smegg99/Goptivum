// identity/sessions.go

package identity

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"time"
)

// Session is one session an account signs in with.
type Session struct {
	ID              string
	AuthenticatedAt time.Time
	ExpiresAt       time.Time
	// IPAddress and UserAgent are Kratos's newest device record for the
	// session; empty when it recorded none.
	IPAddress string
	UserAgent string
}

// wireSession is Kratos's session document, only the fields used here.
type wireSession struct {
	ID              string    `json:"id"`
	Active          bool      `json:"active"`
	AuthenticatedAt time.Time `json:"authenticated_at"`
	ExpiresAt       time.Time `json:"expires_at"`
	Devices         []struct {
		IPAddress string `json:"ip_address"`
		UserAgent string `json:"user_agent"`
	} `json:"devices"`
	Identity *struct {
		ID string `json:"id"`
	} `json:"identity"`
}

// sessionID is Kratos's session id shape. Kratos answers 400, not 404, to
// anything else, and to a caller both mean there is no such session.
var sessionID = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

// Sessions are the sessions that still sign the account in, newest sign-in
// first. An unknown account has none.
//
// Kratos's own active filter reads the session row only, and a disabled
// account's sessions keep that flag, so the serialised active flag, which also
// reads the account's state and the expiry, decides here.
func (a *Admin) Sessions(ctx context.Context, id string) ([]Session, error) {
	sessions := []Session{}
	next := fmt.Sprintf("/admin/identities/%s/sessions?active=true&page_size=%d", url.PathEscape(id), pageSize)
	for page := 0; next != ""; page++ {
		if page >= maxPages {
			return nil, fmt.Errorf("identity: more than %d pages of sessions, refusing a truncated list", maxPages)
		}
		var batch []wireSession
		link, err := a.callWithHeader(ctx, http.MethodGet, next, nil, &batch)
		if err != nil {
			return nil, err
		}
		for _, one := range batch {
			if one.Active {
				sessions = append(sessions, toSession(one))
			}
		}
		next = nextPage(link)
	}
	sort.SliceStable(sessions, func(i, j int) bool {
		return sessions[i].AuthenticatedAt.After(sessions[j].AuthenticatedAt)
	})
	return sessions, nil
}

// EndSessions signs the account out everywhere.
//
// Kratos answers 404 both for an unknown account and for one with no session
// left, so a 404 here is nothing to end: callers look the account up first.
func (a *Admin) EndSessions(ctx context.Context, id string) error {
	err := a.call(ctx, http.MethodDelete, "/admin/identities/"+url.PathEscape(id)+"/sessions", nil, nil)
	if errors.Is(err, ErrNotFound) {
		return nil
	}
	return err
}

// EndSession ends one session of the account. A session of another account,
// or none at all, is ErrNotFound: Kratos's own call names no account.
func (a *Admin) EndSession(ctx context.Context, identityID, id string) error {
	if !sessionID.MatchString(id) {
		return ErrNotFound
	}
	var found wireSession
	if err := a.call(ctx, http.MethodGet, "/admin/sessions/"+url.PathEscape(id)+"?expand=identity", nil, &found); err != nil {
		return err
	}
	if found.Identity == nil || found.Identity.ID != identityID {
		return ErrNotFound
	}
	return a.call(ctx, http.MethodDelete, "/admin/sessions/"+url.PathEscape(id), nil, nil)
}

func toSession(from wireSession) Session {
	session := Session{ID: from.ID, AuthenticatedAt: from.AuthenticatedAt.UTC(), ExpiresAt: from.ExpiresAt.UTC()}
	if count := len(from.Devices); count > 0 {
		last := from.Devices[count-1]
		session.IPAddress, session.UserAgent = last.IPAddress, last.UserAgent
	}
	return session
}

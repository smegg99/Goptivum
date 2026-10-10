// identity/identitytest/fake.go

// Package identitytest is a Kratos admin API stand-in for this repository's
// tests.
//
// Its own package so the identity package never imports testing, the same
// reason kratosauthtest is separate from kratosauth. It answers only the calls
// identity.Admin makes, in the shapes checked against a real v1.3.1 first.
package identitytest

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"
)

// Session is one session the fake holds.
type Session struct {
	ID              string
	IdentityID      string
	Active          bool
	AuthenticatedAt time.Time
	ExpiresAt       time.Time
	IPAddress       string
	UserAgent       string
}

// Kratos is the admin API, reduced to what this package calls. It is a
// stand-in for exactly the calls whose shapes were checked against a real
// v1.3.1 first.
type Kratos struct {
	T          *testing.T
	Identities map[string]map[string]any
	Created    int
	Patches    int
	Puts       int
	PageSize   int
	// AbsoluteLinks makes the Link header carry a whole URL, which a client
	// that concatenates onto its base URL turns into a nonsense address.
	AbsoluteLinks bool
	// Hashes is the stored password hash per identity, served only when a
	// reader asks for include_credential=password the way the real one does.
	// The first-sign-in mark is a digest of this, so a test changes a password
	// by changing this value.
	Hashes          map[string]string
	passwordChanges uint64
	// Sessions are every session, ended ones included until a delete of the
	// identity's sessions removes them, the way the real one does.
	Sessions []*Session
	base     string
	// mu guards the maps: a handler under test may call from several
	// goroutines.
	mu sync.Mutex
}

// AddSession signs an identity in and returns the session id.
func (f *Kratos) AddSession(identityID string, authenticatedAt time.Time) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	id := fmt.Sprintf("00000000-0000-4000-8000-%012d", len(f.Sessions)+1)
	f.Sessions = append(f.Sessions, &Session{
		ID: id, IdentityID: identityID, Active: true,
		AuthenticatedAt: authenticatedAt, ExpiresAt: authenticatedAt.Add(24 * time.Hour),
		IPAddress: "192.0.2.10:50486", UserAgent: "goptivum-desktop/test",
	})
	return id
}

// LiveSessions counts the sessions that still sign an identity in.
func (f *Kratos) LiveSessions(identityID string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	live := 0
	for _, session := range f.Sessions {
		if session.IdentityID == identityID && f.liveLocked(session) {
			live++
		}
	}
	return live
}

// liveLocked is the session's active flag as Kratos serialises it: false for
// an ended session and for every session of a disabled identity.
func (f *Kratos) liveLocked(session *Session) bool {
	identity, ok := f.Identities[session.IdentityID]
	return session.Active && ok && identity["state"] != "inactive"
}

// BaseURL is where this fake listens, for a test that needs a second client.
func (f *Kratos) BaseURL() string { return f.base }

// ChangePassword is what a settings flow does to the stored hash. bcrypt salts
// randomly, so a real change always produces a different value even for the
// same password.
func (f *Kratos) ChangePassword(id string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.changePasswordLocked(id)
}

func New(t *testing.T) (*Kratos, string) {
	t.Helper()
	fake := &Kratos{T: t, Identities: map[string]map[string]any{},
		Hashes: map[string]string{}, PageSize: 250}
	server := httptest.NewServer(fake)
	t.Cleanup(server.Close)
	fake.base = server.URL
	return fake, server.URL
}

func (f *Kratos) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()

	if sessionID, ok := strings.CutPrefix(r.URL.Path, "/admin/sessions/"); ok {
		f.session(w, r, sessionID)
		return
	}
	path, ok := strings.CutPrefix(r.URL.Path, "/admin/identities")
	if !ok {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	id := strings.TrimPrefix(path, "/")
	if owner, ok := strings.CutSuffix(id, "/sessions"); ok {
		f.identitySessions(w, r, owner)
		return
	}

	switch {
	case r.Method == http.MethodPost && id == "":
		f.create(w, r)
	case r.Method == http.MethodGet && id == "":
		f.list(w, r)
	case r.Method == http.MethodGet:
		f.one(w, r, id)
	case r.Method == http.MethodPatch:
		f.patch(w, r, id)
	case r.Method == http.MethodPut:
		f.put(w, r, id)
	case r.Method == http.MethodDelete:
		delete(f.Identities, id)
		w.WriteHeader(http.StatusNoContent)
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

// notFound is the body the real one sends with a 404.
func notFound(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusNotFound)
	_, _ = w.Write([]byte(`{"error":{"code":404,"status":"Not Found","message":"Unable to locate the resource"}}`))
}

// identitySessions lists or ends one identity's sessions. An unknown identity
// lists nothing, and deleting the sessions of an identity that has none is a
// 404: both measured against v1.3.1.
func (f *Kratos) identitySessions(w http.ResponseWriter, r *http.Request, identityID string) {
	switch r.Method {
	case http.MethodGet:
		onlyActive := r.URL.Query().Get("active") == "true"
		sessions := []map[string]any{}
		for _, session := range f.Sessions {
			if session.IdentityID != identityID || (onlyActive && !session.Active) {
				continue
			}
			sessions = append(sessions, f.sessionBody(session, true))
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(sessions)
	case http.MethodDelete:
		kept := []*Session{}
		for _, session := range f.Sessions {
			if session.IdentityID != identityID {
				kept = append(kept, session)
			}
		}
		if len(kept) == len(f.Sessions) {
			notFound(w)
			return
		}
		f.Sessions = kept
		w.WriteHeader(http.StatusNoContent)
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

// session reads one session, with its identity only when asked to expand it,
// or ends it.
func (f *Kratos) session(w http.ResponseWriter, r *http.Request, id string) {
	var found *Session
	for _, session := range f.Sessions {
		if session.ID == id {
			found = session
		}
	}
	if found == nil {
		notFound(w)
		return
	}
	switch r.Method {
	case http.MethodGet:
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(f.sessionBody(found, r.URL.Query().Get("expand") == "identity"))
	case http.MethodDelete:
		found.Active = false
		w.WriteHeader(http.StatusNoContent)
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (f *Kratos) sessionBody(session *Session, withIdentity bool) map[string]any {
	body := map[string]any{
		"id":               session.ID,
		"active":           f.liveLocked(session),
		"authenticated_at": session.AuthenticatedAt.Format(time.RFC3339Nano),
		"issued_at":        session.AuthenticatedAt.Format(time.RFC3339Nano),
		"expires_at":       session.ExpiresAt.Format(time.RFC3339Nano),
		"devices": []map[string]any{{
			"id": "device-" + session.ID, "ip_address": session.IPAddress,
			"user_agent": session.UserAgent, "location": "",
		}},
		"identity": nil,
	}
	if withIdentity {
		body["identity"] = map[string]any{"id": session.IdentityID}
	}
	return body
}

func (f *Kratos) create(w http.ResponseWriter, r *http.Request) {
	var body map[string]any
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	credentials, ok := body["credentials"].(map[string]any)
	if !ok {
		f.T.Errorf("create carried no credentials, so the account would have no password")
	} else {
		password, _ := credentials["password"].(map[string]any)
		config, _ := password["config"].(map[string]any)
		if value, _ := config["password"].(string); value == "" {
			f.T.Error("create carried an empty password")
		}
	}

	// Both traits are password identifiers, compared ignoring case, and one
	// clash is enough: measured against v1.3.1.
	traits, _ := body["traits"].(map[string]any)
	for _, name := range []string{"username", "email"} {
		wanted, _ := traits[name].(string)
		if wanted != "" && f.identifierTakenLocked(wanted) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusConflict)
			_, _ = w.Write([]byte(`{"error":{"code":409,"status":"Conflict","reason":"This identity conflicts with another identity that already exists.","message":"The resource could not be created due to a conflict"}}`))
			return
		}
	}

	f.Created++
	id := fmt.Sprintf("id-%d", f.Created)
	body["id"] = id
	if _, has := body["state"]; !has {
		body["state"] = "active"
	}
	delete(body, "credentials")
	f.Hashes[id] = "$2a$12$initial"
	f.Identities[id] = body

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(body)
}

// identifierTakenLocked reports whether any identity signs in with this name.
func (f *Kratos) identifierTakenLocked(name string) bool {
	for _, identity := range f.Identities {
		if identifierOf(identity, name) {
			return true
		}
	}
	return false
}

func identifierOf(identity map[string]any, name string) bool {
	traits, _ := identity["traits"].(map[string]any)
	for _, field := range []string{"username", "email"} {
		if value, _ := traits[field].(string); value != "" && strings.EqualFold(value, name) {
			return true
		}
	}
	return false
}

// list pages, because a school with more accounts than one page is the case
// where a client that ignores the Link header silently loses people. A
// credentials_identifier narrows it to the identity signing in with that
// name, ignoring case, the way the real one does.
func (f *Kratos) list(w http.ResponseWriter, r *http.Request) {
	identifier := r.URL.Query().Get("credentials_identifier")
	ids := []string{}
	for id, identity := range f.Identities {
		if identifier != "" && !identifierOf(identity, identifier) {
			continue
		}
		ids = append(ids, id)
	}
	// Deterministic order, since a map is not one.
	sort.Strings(ids)

	from := 0
	if token := r.URL.Query().Get("page_token"); token != "" {
		for index, id := range ids {
			if id == token {
				from = index
				break
			}
		}
	}
	to := from + f.PageSize
	if to > len(ids) {
		to = len(ids)
	}

	page := []map[string]any{}
	for _, id := range ids[from:to] {
		page = append(page, f.Identities[id])
	}
	if to < len(ids) {
		prefix := ""
		if f.AbsoluteLinks {
			prefix = f.base
		}
		w.Header().Set("Link",
			fmt.Sprintf(`<%s/admin/identities?page_token=%s>; rel="next"`, prefix, ids[to]))
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(page)
}

func (f *Kratos) one(w http.ResponseWriter, r *http.Request, id string) {
	found, ok := f.Identities[id]
	if !ok {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	// The hash is served only when it is asked for, the way the real one does
	// it: a reader that does not ask must not receive credential material.
	body := found
	if r.URL.Query().Get("include_credential") == "password" {
		body = map[string]any{}
		for key, value := range found {
			body[key] = value
		}
		body["credentials"] = map[string]any{
			"password": map[string]any{
				"config": map[string]any{"hashed_password": f.Hashes[id]},
			},
		}
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(body)
}

func (f *Kratos) patch(w http.ResponseWriter, r *http.Request, id string) {
	if media := r.Header.Get("Content-Type"); media != "application/json-patch+json" {
		f.T.Errorf("patch content type = %q, want application/json-patch+json", media)
	}
	found, ok := f.Identities[id]
	if !ok {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	var operations []struct {
		Op    string `json:"op"`
		Path  string `json:"path"`
		Value any    `json:"value"`
	}
	if err := json.NewDecoder(r.Body).Decode(&operations); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	f.Patches++
	for _, operation := range operations {
		switch {
		case operation.Path == "/state":
			found["state"] = operation.Value
		case strings.HasPrefix(operation.Path, "/metadata_public/"):
			meta, _ := found["metadata_public"].(map[string]any)
			if meta == nil {
				meta = map[string]any{}
			}
			meta[strings.TrimPrefix(operation.Path, "/metadata_public/")] = operation.Value
			found["metadata_public"] = meta
		case strings.HasPrefix(operation.Path, "/metadata_admin/"):
			// Admin metadata is where the first-sign-in mark lives, and the
			// real Kratos never shows it to the account it belongs to.
			meta, _ := found["metadata_admin"].(map[string]any)
			if meta == nil {
				meta = map[string]any{}
			}
			key := strings.TrimPrefix(operation.Path, "/metadata_admin/")
			if operation.Op == "remove" {
				delete(meta, key)
			} else {
				meta[key] = operation.Value
			}
			found["metadata_admin"] = meta
		default:
			f.T.Errorf("unexpected patch path %q", operation.Path)
		}
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(found)
}

func (f *Kratos) changePasswordLocked(id string) {
	f.passwordChanges++
	f.Hashes[id] = fmt.Sprintf("$2a$12$changed%d", f.passwordChanges)
}

func (f *Kratos) put(w http.ResponseWriter, r *http.Request, id string) {
	if _, ok := f.Identities[id]; !ok {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	var body map[string]any
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	// PUT is a full replace, so a caller that forgets a field loses it. This
	// asserts the client sends everything back.
	if body["traits"] == nil {
		f.T.Error("put carried no traits, which a full replace would erase")
	}
	if body["metadata_public"] == nil {
		f.T.Error("put carried no metadata_public, which would erase the role")
	}
	f.Puts++
	body["id"] = id
	f.changePasswordLocked(id)
	delete(body, "credentials")
	f.Identities[id] = body

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(body)
}

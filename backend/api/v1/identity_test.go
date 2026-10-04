// api/v1/identity_test.go

package v1_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/smegg99/s99ab/auth/kratosauth"
	"github.com/smegg99/s99ab/auth/kratosauth/kratosauthtest"

	apiv1 "github.com/smegg99/goptivum/backend/api/v1"
)

// bearer is the credential the desktop sends, and the key the fake matches on:
// it compares the whole Authorization header, with no scheme stripped.
const bearer = "Bearer ory_st_headmaster"

func newGuardedServer(t *testing.T, sessions *kratosauth.Client, publicURL string) *apiv1.Server {
	t.Helper()

	server, err := apiv1.New(apiv1.Options{
		Build:                    apiv1.BuildInfo{Version: "1.2.3"},
		MinClientVersion:         "0.1.0",
		Handlers:                 newAPI(t),
		Kratos:                   sessions,
		KratosPublicURL:          publicURL,
		KratosRateLimitPerMinute: 600,
	})
	if err != nil {
		t.Fatalf("new server: %v", err)
	}
	return server
}

// errorKey is the stable key a client switches on, never the sentence.
func errorKey(t *testing.T, body []byte) string {
	t.Helper()
	// The client switches on the message key; code is the coarse kind.
	var envelope struct {
		Message string `json:"message"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		t.Fatalf("decode error envelope %q: %v", string(body), err)
	}
	return envelope.Message
}

// The gate: no session at all is 401, and it says which of the two failures it
// is, because a client retries one and re-authenticates for the other.
func TestAnonymousOnAGuardedRouteIsUnauthorized(t *testing.T) {
	fake := kratosauthtest.Fake(t, map[string]kratosauth.Identity{
		bearer: {ID: "headmaster"},
	})
	server := newGuardedServer(t, fake, "")
	if !server.ServesPlans() {
		t.Fatal("plan routes are absent with Kratos configured")
	}

	recorder := httptest.NewRecorder()
	server.Handler().ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/plans", nil))

	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401 for a caller with no session", recorder.Code)
	}
	if key := errorKey(t, recorder.Body.Bytes()); key != "auth.required" {
		t.Errorf("message = %q, want auth.required", key)
	}
}

// A session the fake knows reaches the handler, which is what proves the 401
// above is about the credential and not about the route being broken.
func TestABearerTokenReachesTheHandler(t *testing.T) {
	fake := kratosauthtest.Fake(t, map[string]kratosauth.Identity{
		bearer: {ID: "headmaster"},
	})
	server := newGuardedServer(t, fake, "")

	request := httptest.NewRequest(http.MethodGet, "/api/v1/plans", nil)
	request.Header.Set("Authorization", bearer)
	recorder := httptest.NewRecorder()
	server.Handler().ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d body %s, want 200 for a signed-in caller",
			recorder.Code, recorder.Body.String())
	}
}

// Fail closed. An identity provider that does not answer is an outage, and the
// one thing it must never become is an anonymous request that proceeds.
func TestUnreachableKratosIsUnavailableNotAnonymous(t *testing.T) {
	// Port 1 on loopback refuses at once, so this is an outage without a wait.
	dead, err := kratosauth.New(kratosauth.Options{
		PublicURL: "http://127.0.0.1:1",
		Timeout:   2 * time.Second,
	})
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	server := newGuardedServer(t, dead, "")

	recorder := httptest.NewRecorder()
	server.Handler().ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/plans", nil))

	if recorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503 while Kratos is unreachable", recorder.Code)
	}
	if key := errorKey(t, recorder.Body.Bytes()); key != "auth.unavailable" {
		t.Errorf("message = %q, want auth.unavailable", key)
	}
}

// Discovery and liveness must survive the identity provider being down, or a
// client cannot learn what is wrong with the server it cannot sign in to.
func TestDiscoverySurvivesAnUnreachableKratos(t *testing.T) {
	dead, err := kratosauth.New(kratosauth.Options{
		PublicURL: "http://127.0.0.1:1",
		Timeout:   2 * time.Second,
	})
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	server := newGuardedServer(t, dead, "")

	for _, path := range []string{"/api/info", "/api/v1/health"} {
		recorder := httptest.NewRecorder()
		server.Handler().ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, path, nil))
		if recorder.Code != http.StatusOK {
			t.Errorf("%s = %d, want 200 with Kratos down", path, recorder.Code)
		}
	}
}

// The whole trust boundary in one test. Traits are what a self-service flow
// writes, so a role there would be a self-service promotion to administrator.
func TestARoleInTraitsDoesNotOpenTheGate(t *testing.T) {
	fake := kratosauthtest.Fake(t, map[string]kratosauth.Identity{
		"Bearer in-traits": {
			ID:     "liar",
			Traits: json.RawMessage(`{"username":"liar","role":"admin"}`),
		},
		"Bearer in-metadata": {
			ID:             "real",
			MetadataPublic: json.RawMessage(`{"role":"admin"}`),
		},
	})

	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.Use(kratosauth.Session(fake))
	engine.GET("/admin", kratosauth.Require(), kratosauth.RequireMeta("role", "admin"),
		func(c *gin.Context) { c.Status(http.StatusOK) })

	for _, one := range []struct {
		name, credential string
		want             int
	}{
		// Not 403: a 403 would confirm the route is real to a caller probing it.
		{"role in traits", "Bearer in-traits", http.StatusNotFound},
		{"role in metadata_public", "Bearer in-metadata", http.StatusOK},
		{"no session", "", http.StatusUnauthorized},
	} {
		t.Run(one.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, "/admin", nil)
			if one.credential != "" {
				request.Header.Set("Authorization", one.credential)
			}
			recorder := httptest.NewRecorder()
			engine.ServeHTTP(recorder, request)
			if recorder.Code != one.want {
				t.Errorf("status = %d, want %d", recorder.Code, one.want)
			}
		})
	}
}

// Kratos serves at its own root, so forwarding the prefix is a 404 that reads
// like a broken proxy when the fault is one missing TrimPrefix.
func TestKratosProxyStripsThePrefix(t *testing.T) {
	var seen string
	kratos := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ui":{"action":"http://example.test/kratos/self-service/login"}}`))
	}))
	defer kratos.Close()

	fake := kratosauthtest.Fake(t, map[string]kratosauth.Identity{})
	server := newGuardedServer(t, fake, kratos.URL)
	if !server.ServesIdentity() {
		t.Fatal("the proxy is not mounted with a public URL configured")
	}

	recorder := httptest.NewRecorder()
	server.Handler().ServeHTTP(recorder,
		httptest.NewRequest(http.MethodGet, "/kratos/self-service/login/api", nil))

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 through the proxy", recorder.Code)
	}
	if seen != "/self-service/login/api" {
		t.Errorf("kratos received %q, want /self-service/login/api with no prefix", seen)
	}
}

// The route that issues a session cannot require one, so the proxy is a
// sibling of the versioned group rather than a child of it.
func TestKratosProxyNeedsNoSession(t *testing.T) {
	kratos := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer kratos.Close()

	// A dead session client: if the proxy sat under Session this would 503.
	dead, err := kratosauth.New(kratosauth.Options{
		PublicURL: "http://127.0.0.1:1",
		Timeout:   2 * time.Second,
	})
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	server := newGuardedServer(t, dead, kratos.URL)

	recorder := httptest.NewRecorder()
	server.Handler().ServeHTTP(recorder,
		httptest.NewRequest(http.MethodPost, "/kratos/self-service/login", nil))
	if recorder.Code != http.StatusOK {
		t.Errorf("status = %d, want 200: a login route must answer without a session", recorder.Code)
	}
}

// apimount's limiter lives on the versioned group and never sees the proxy, so
// the proxy carries its own. An unguessable password is no help against an
// unlimited number of guesses.
func TestKratosProxyIsRateLimited(t *testing.T) {
	kratos := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer kratos.Close()

	server, err := apiv1.New(apiv1.Options{
		Handlers:                 newAPI(t),
		KratosPublicURL:          kratos.URL,
		KratosRateLimitPerMinute: 1,
	})
	if err != nil {
		t.Fatalf("new server: %v", err)
	}

	codes := []int{}
	for attempt := 0; attempt < 3; attempt++ {
		recorder := httptest.NewRecorder()
		server.Handler().ServeHTTP(recorder,
			httptest.NewRequest(http.MethodPost, "/kratos/self-service/login", nil))
		codes = append(codes, recorder.Code)
	}
	if codes[0] != http.StatusOK {
		t.Fatalf("first attempt = %d, want 200", codes[0])
	}
	if codes[len(codes)-1] != http.StatusTooManyRequests {
		t.Errorf("attempts = %v, want the last one to be 429", codes)
	}
}

// No public URL, no proxy. A server with no identity provider must not answer
// on a path that implies it has one.
func TestKratosProxyIsAbsentWithoutAPublicURL(t *testing.T) {
	server := newServer(t)
	if server.ServesIdentity() {
		t.Fatal("the proxy is mounted with no public URL")
	}

	recorder := httptest.NewRecorder()
	server.Handler().ServeHTTP(recorder,
		httptest.NewRequest(http.MethodGet, "/kratos/self-service/login/api", nil))
	if recorder.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404 with no identity provider", recorder.Code)
	}
}

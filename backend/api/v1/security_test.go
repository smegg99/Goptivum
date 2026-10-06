// api/v1/security_test.go

package v1_test

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	apiv1 "github.com/smegg99/goptivum/backend/api/v1"
	"github.com/smegg99/goptivum/backend/api/v1/gen"
	"github.com/smegg99/goptivum/backend/common/config"
	"github.com/smegg99/goptivum/backend/common/messages"
	"github.com/smegg99/goptivum/backend/core/lockout"
)

// kratosLogin is Kratos's public side as far as a sign-in goes: the password
// "right" signs in, "gone" is an expired flow, "broken" is Kratos failing,
// and anything else is the 400 Kratos gives wrong credentials.
func kratosLogin(t *testing.T) (string, *atomic.Int32) {
	t.Helper()
	var submitted atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusOK)
			return
		}
		submitted.Add(1)
		password := ""
		if strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
			var body struct {
				Password string `json:"password"`
			}
			_ = json.NewDecoder(r.Body).Decode(&body)
			password = body.Password
		} else if err := r.ParseForm(); err == nil {
			password = r.PostForm.Get("password")
		}
		switch password {
		case "right":
			_, _ = w.Write([]byte(`{"session_token":"ory_st_synthetic"}`))
		case "gone":
			w.WriteHeader(http.StatusGone)
		case "broken":
			w.WriteHeader(http.StatusBadGateway)
		default:
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"ui":{"messages":[{"id":4000006}]}}`))
		}
	}))
	t.Cleanup(server.Close)
	return server.URL, &submitted
}

// securityServer is a server with the lockout in its proxy and the Security
// routes over a settings file of its own.
func securityServer(t *testing.T, kratosPublic, kratosConfig string) (*apiv1.Server, *config.SettingsStore) {
	t.Helper()
	settings, err := config.OpenSettings(filepath.Join(t.TempDir(), "config.yaml"))
	if err != nil {
		t.Fatalf("settings: %v", err)
	}
	guard := lockout.New(apiv1.LimitsOf(settings.Current().Lockout), nil)
	handlers := newAPI(t)
	handlers.Security = apiv1.NewSecurity(settings, guard, kratosConfig)
	server, err := apiv1.New(apiv1.Options{
		Build:                    apiv1.BuildInfo{Version: "1.2.3"},
		MinClientVersion:         "0.1.0",
		Handlers:                 handlers,
		Kratos:                   adminFake(t),
		KratosPublicURL:          kratosPublic,
		KratosRateLimitPerMinute: 600,
		Lockout:                  guard,
	})
	if err != nil {
		t.Fatalf("new server: %v", err)
	}
	return server, settings
}

// signIn submits a native login flow through the proxy from one address.
func signIn(t *testing.T, server *apiv1.Server, address, identifier, password string) *httptest.ResponseRecorder {
	t.Helper()
	body, _ := json.Marshal(map[string]string{"method": "password", "identifier": identifier, "password": password})
	request := httptest.NewRequest(http.MethodPost, "/kratos/self-service/login?flow=synthetic", strings.NewReader(string(body)))
	request.Header.Set("Content-Type", "application/json")
	request.RemoteAddr = address + ":40000"
	recorder := httptest.NewRecorder()
	server.Handler().ServeHTTP(recorder, request)
	return recorder
}

func setLockout(t *testing.T, server *apiv1.Server, body string) gen.Lockout {
	t.Helper()
	recorder := call(t, server, http.MethodPatch, "/api/v1/security/lockout", asAdmin, body)
	if recorder.Code != http.StatusOK {
		t.Fatalf("patch lockout %s = %d: %s", body, recorder.Code, recorder.Body.String())
	}
	var lockout gen.Lockout
	if err := json.Unmarshal(recorder.Body.Bytes(), &lockout); err != nil {
		t.Fatalf("decode: %v", err)
	}
	return lockout
}

func blockedEnvelope(t *testing.T, recorder *httptest.ResponseRecorder) gen.Error {
	t.Helper()
	if recorder.Code != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want 429: %s", recorder.Code, recorder.Body.String())
	}
	return decodeError(t, recorder)
}

func TestRepeatedFailedSignInsBlockTheNameWhetherOrNotItExists(t *testing.T) {
	kratos, submitted := kratosLogin(t)
	server, _ := securityServer(t, kratos, "")
	// Applies now: the next sign-in already counts against the new limit.
	if got := setLockout(t, server, `{"account_failures":2}`); got.Applies != gen.Now || got.AccountFailures != 2 {
		t.Fatalf("lockout = %+v, want 2 applying now", got)
	}

	answers := map[string]*httptest.ResponseRecorder{}
	for _, name := range []string{"anna", "ghost-who-has-no-account"} {
		for range 2 {
			if recorder := signIn(t, server, "192.0.2.10", name, "wrong"); recorder.Code != http.StatusBadRequest {
				t.Fatalf("%s: a failure through the proxy = %d, want Kratos's 400", name, recorder.Code)
			}
		}
		before := submitted.Load()
		answers[name] = signIn(t, server, "192.0.2.10", name, "right")
		if submitted.Load() != before {
			t.Errorf("%s: a blocked sign-in reached Kratos", name)
		}
	}

	real, ghost := answers["anna"], answers["ghost-who-has-no-account"]
	envelope := blockedEnvelope(t, real)
	if envelope.Code != "rate_limited" || envelope.Message != messages.KeyAuthSignInBlocked ||
		envelope.Details == nil || (*envelope.Details)["Seconds"] != "900" {
		t.Errorf("envelope = %+v, want auth.signInBlocked with 900 seconds left", envelope)
	}
	if real.Header().Get("Retry-After") != "900" {
		t.Errorf("Retry-After = %q, want 900", real.Header().Get("Retry-After"))
	}
	// The same answer for a name no account has.
	if ghost.Code != real.Code || ghost.Body.String() != real.Body.String() ||
		ghost.Header().Get("Retry-After") != real.Header().Get("Retry-After") {
		t.Errorf("the block tells names apart:\n%d %s\n%d %s", real.Code, real.Body, ghost.Code, ghost.Body)
	}

	// Only submissions are refused: a new flow can still be started.
	flow := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/kratos/self-service/login/api", nil)
	request.RemoteAddr = "192.0.2.10:40000"
	server.Handler().ServeHTTP(flow, request)
	if flow.Code != http.StatusOK {
		t.Errorf("starting a flow while blocked = %d, want 200", flow.Code)
	}
}

// The address counts every name, and a forwarded-for header from a peer that
// is not a trusted proxy does not move it.
func TestFailuresFromOneAddressBlockIt(t *testing.T) {
	kratos, _ := kratosLogin(t)
	server, _ := securityServer(t, kratos, "")
	setLockout(t, server, `{"address_failures":3}`)

	for index, name := range []string{"a", "b", "c"} {
		body := `{"identifier":"` + name + `","password":"wrong"}`
		request := httptest.NewRequest(http.MethodPost, "/kratos/self-service/login?flow=x", strings.NewReader(body))
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("X-Forwarded-For", "198.51.100."+string(rune('1'+index)))
		request.RemoteAddr = "192.0.2.20:40000"
		recorder := httptest.NewRecorder()
		server.Handler().ServeHTTP(recorder, request)
		if recorder.Code != http.StatusBadRequest {
			t.Fatalf("failure %d = %d", index, recorder.Code)
		}
	}
	blockedEnvelope(t, signIn(t, server, "192.0.2.20", "somebody-new", "right"))
	if recorder := signIn(t, server, "192.0.2.21", "somebody-new", "right"); recorder.Code != http.StatusOK {
		t.Errorf("another address = %d, want 200", recorder.Code)
	}
}

func TestASuccessfulSignInClearsTheName(t *testing.T) {
	kratos, _ := kratosLogin(t)
	server, _ := securityServer(t, kratos, "")
	setLockout(t, server, `{"account_failures":2}`)

	signIn(t, server, "192.0.2.30", "anna", "wrong")
	if recorder := signIn(t, server, "192.0.2.30", "anna", "right"); recorder.Code != http.StatusOK {
		t.Fatalf("sign in = %d", recorder.Code)
	}
	signIn(t, server, "192.0.2.30", "anna", "wrong")
	if recorder := signIn(t, server, "192.0.2.30", "anna", "right"); recorder.Code != http.StatusOK {
		t.Errorf("one failure after a success blocked the name: %d", recorder.Code)
	}
}

// An expired flow and a failing Kratos are nobody's wrong password.
func TestOnlyRefusedCredentialsCount(t *testing.T) {
	kratos, _ := kratosLogin(t)
	server, _ := securityServer(t, kratos, "")
	setLockout(t, server, `{"account_failures":1}`)

	for _, password := range []string{"gone", "broken"} {
		signIn(t, server, "192.0.2.40", "anna", password)
	}
	if recorder := signIn(t, server, "192.0.2.40", "anna", "right"); recorder.Code != http.StatusOK {
		t.Fatalf("an expired flow or a Kratos failure counted: %d", recorder.Code)
	}
	signIn(t, server, "192.0.2.40", "anna", "wrong")
	blockedEnvelope(t, signIn(t, server, "192.0.2.40", "anna", "right"))
}

// A browser form and Kratos's older field name carry the name too.
func TestTheNameIsReadFromAFormAndTheOlderField(t *testing.T) {
	kratos, _ := kratosLogin(t)
	server, _ := securityServer(t, kratos, "")
	setLockout(t, server, `{"account_failures":2}`)

	form := url.Values{"method": {"password"}, "identifier": {"Anna"}, "password": {"wrong"}}.Encode()
	request := httptest.NewRequest(http.MethodPost, "/kratos/self-service/login?flow=x", strings.NewReader(form))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.RemoteAddr = "192.0.2.50:40000"
	server.Handler().ServeHTTP(httptest.NewRecorder(), request)

	older := `{"method":"password","password_identifier":"anna","password":"wrong"}`
	request = httptest.NewRequest(http.MethodPost, "/kratos/self-service/login?flow=x", strings.NewReader(older))
	request.Header.Set("Content-Type", "application/json")
	request.RemoteAddr = "192.0.2.51:40000"
	recorder := httptest.NewRecorder()
	server.Handler().ServeHTTP(recorder, request)
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("the older field = %d", recorder.Code)
	}
	blockedEnvelope(t, signIn(t, server, "192.0.2.52", "ANNA", "right"))
}

// The body is read before Kratos gets it, so it is bounded; the whole body
// still reaches Kratos unchanged.
func TestTheSignInBodyIsBoundedAndForwardedWhole(t *testing.T) {
	var received string
	kratos := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		received = string(raw)
		w.WriteHeader(http.StatusBadRequest)
	}))
	t.Cleanup(kratos.Close)
	server, _ := securityServer(t, kratos.URL, "")

	sent, _ := json.Marshal(map[string]string{"method": "password", "identifier": "anna", "password": "wrong"})
	body := string(sent)
	recorder := signIn(t, server, "192.0.2.60", "anna", "wrong")
	if recorder.Code != http.StatusBadRequest || received != body {
		t.Errorf("forwarded %q (%d), want %q", received, recorder.Code, body)
	}

	huge := httptest.NewRequest(http.MethodPost, "/kratos/self-service/login?flow=x",
		strings.NewReader(`{"identifier":"`+strings.Repeat("a", 70<<10)+`"}`))
	huge.Header.Set("Content-Type", "application/json")
	received = ""
	recorder = httptest.NewRecorder()
	server.Handler().ServeHTTP(recorder, huge)
	if recorder.Code != http.StatusRequestEntityTooLarge || received != "" {
		t.Errorf("a 70 KiB sign-in = %d and reached Kratos: %v", recorder.Code, received != "")
	}
}

func TestLockoutSettingsAreReadCheckedAndKept(t *testing.T) {
	server, settings := securityServer(t, "", "")

	recorder := call(t, server, http.MethodGet, "/api/v1/security/lockout", asAdmin, "")
	if recorder.Code != http.StatusOK {
		t.Fatalf("get = %d: %s", recorder.Code, recorder.Body.String())
	}
	var read gen.Lockout
	if err := json.Unmarshal(recorder.Body.Bytes(), &read); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if read != (gen.Lockout{AccountFailures: 5, AddressFailures: 20, BlockMinutes: 15, Applies: gen.Now}) {
		t.Errorf("lockout = %+v, want the defaults applying now", read)
	}

	for body, field := range map[string]string{
		`{}`:                        "body",
		`{"account_failures":0}`:    "account_failures",
		`{"account_failures":101}`:  "account_failures",
		`{"address_failures":1001}`: "address_failures",
		`{"block_minutes":0}`:       "block_minutes",
		`{"block_minutes":1441}`:    "block_minutes",
	} {
		recorder := call(t, server, http.MethodPatch, "/api/v1/security/lockout", asAdmin, body)
		if recorder.Code != http.StatusUnprocessableEntity {
			t.Errorf("%s = %d, want 422", body, recorder.Code)
			continue
		}
		envelope := decodeError(t, recorder)
		if envelope.Details == nil || (*envelope.Details)[field] == "" {
			t.Errorf("%s: details = %v, want %s named", body, envelope.Details, field)
		}
	}

	changed := setLockout(t, server, `{"address_failures":40,"block_minutes":60}`)
	if changed.AccountFailures != 5 || changed.AddressFailures != 40 || changed.BlockMinutes != 60 {
		t.Errorf("after the change = %+v, want the untouched field kept", changed)
	}
	if saved := settings.Current().Lockout; saved.AddressFailures != 40 || saved.BlockMinutes != 60 {
		t.Errorf("the settings file holds %+v", saved)
	}
	raw, err := os.ReadFile(settings.Path())
	if err != nil || !strings.Contains(string(raw), "block_minutes: 60") {
		t.Errorf("settings file = %q, %v", raw, err)
	}
}

func TestPasswordRulesAreShownAsKratosHasThem(t *testing.T) {
	path := filepath.Join(t.TempDir(), "kratos.yml")
	if err := os.WriteFile(path, []byte("selfservice:\n  methods:\n    password:\n      config:\n        min_password_length: 12\n        haveibeenpwned_enabled: false\n"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	for name, test := range map[string]struct {
		file string
		want gen.PasswordRules
	}{
		"no file named": {"", gen.PasswordRules{Known: false}},
		"unreadable":    {filepath.Join(t.TempDir(), "missing.yml"), gen.PasswordRules{Known: false}},
		"read": {path, gen.PasswordRules{Known: true, MinLength: ptr(12), IdentifierSimilarity: ptr(true),
			BreachCheck: ptr(false), MaxBreaches: ptr(0)}},
	} {
		t.Run(name, func(t *testing.T) {
			server, _ := securityServer(t, "", test.file)
			recorder := call(t, server, http.MethodGet, "/api/v1/security/password-rules", asAdmin, "")
			if recorder.Code != http.StatusOK {
				t.Fatalf("get = %d: %s", recorder.Code, recorder.Body.String())
			}
			var rules gen.PasswordRules
			if err := json.Unmarshal(recorder.Body.Bytes(), &rules); err != nil {
				t.Fatalf("decode: %v", err)
			}
			got, _ := json.Marshal(rules)
			want, _ := json.Marshal(test.want)
			if string(got) != string(want) {
				t.Errorf("rules = %s, want %s", got, want)
			}
		})
	}
}

func ptr[T any](value T) *T { return &value }

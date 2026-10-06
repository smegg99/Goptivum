// api/v1/school_test.go

package v1_test

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"filippo.io/age"
	"github.com/smegg99/s99ab/auth/kratosauth"
	"github.com/smegg99/s99ab/auth/kratosauth/kratosauthtest"
	"github.com/smegg99/s99ab/data/db"

	arrangov1 "github.com/smegg99/arrango/backend/gen/arrangov1"

	apiv1 "github.com/smegg99/goptivum/backend/api/v1"
	"github.com/smegg99/goptivum/backend/api/v1/gen"
	"github.com/smegg99/goptivum/backend/core/escrow"
	"github.com/smegg99/goptivum/backend/migrations"
	"github.com/smegg99/goptivum/document"
	"github.com/smegg99/goptivum/document/gpf"
)

// schoolServer serves the key routes over a deployment that either holds a key
// or does not.
func schoolServer(t *testing.T, passphrase string) *apiv1.Server {
	t.Helper()
	conn := db.TestDB(t, migrations.FS)
	keys := escrow.NewStore(conn, db.SQLite, passphrase).WithWorkFactor(10)
	if keys.Enabled() {
		if _, err := keys.Ensure(t.Context()); err != nil {
			t.Fatalf("ensure: %v", err)
		}
	}
	server, err := apiv1.New(apiv1.Options{
		Build:            apiv1.BuildInfo{Version: "1.2.3"},
		MinClientVersion: "0.1.0",
		Handlers: &apiv1.Handlers{
			API:    newPlanAPI(t, conn),
			Admin:  apiv1.NewAdmin(nil, apiv1.Nodes{}),
			Jobs:   apiv1.NewJobs(nil, nil, nil, nil, nil),
			School: apiv1.NewSchool(keys),
		},
		Kratos: kratosauthtest.Fake(t, map[string]kratosauth.Identity{
			asSettled: {ID: "planner", MetadataPublic: json.RawMessage(`{"role":"planner"}`)},
		}),
	})
	if err != nil {
		t.Fatalf("new server: %v", err)
	}
	return server
}

// A planner reads what the school's files are encrypted to, and can ask for the
// key itself. One key for the whole school was the decision, so this needs a
// session and no role.
func TestAPlannerCanReadAndReleaseTheSchoolKey(t *testing.T) {
	server := schoolServer(t, "the school's escrow passphrase")

	recorder := call(t, server, http.MethodGet, "/api/v1/school/key", asSettled, "")
	if recorder.Code != http.StatusOK {
		t.Fatalf("get = %d: %s", recorder.Code, recorder.Body.String())
	}
	var public struct {
		Configured bool    `json:"configured"`
		Recipient  *string `json:"recipient"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &public); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !public.Configured || public.Recipient == nil || !strings.HasPrefix(*public.Recipient, "age1") {
		t.Fatalf("public key = %+v", public)
	}
	// The public half is what files are encrypted to, so it must not be the
	// secret by accident.
	if strings.Contains(*public.Recipient, "SECRET") {
		t.Fatal("the public endpoint returned a secret")
	}

	released := call(t, server, http.MethodPost, "/api/v1/school/key/release", asSettled, "")
	if released.Code != http.StatusOK {
		t.Fatalf("release = %d: %s", released.Code, released.Body.String())
	}
	var private struct {
		Identity  string `json:"identity"`
		Recipient string `json:"recipient"`
	}
	if err := json.Unmarshal(released.Body.Bytes(), &private); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !strings.HasPrefix(private.Identity, "AGE-SECRET-KEY-") {
		t.Errorf("identity = %q, want the private half", private.Identity)
	}
	// Both halves must be the same key, or the desktop encrypts to one and
	// decrypts with another.
	if private.Recipient != *public.Recipient {
		t.Errorf("release returned a different key: %q and %q", private.Recipient, *public.Recipient)
	}
}

// The private half must never sit in a response a proxy or a browser might
// cache, which is why releasing it is a POST and not a GET.
func TestTheKeyIsNotReleasedOnAGet(t *testing.T) {
	server := schoolServer(t, "the school's escrow passphrase")

	recorder := call(t, server, http.MethodGet, "/api/v1/school/key/release", asSettled, "")
	if recorder.Code == http.StatusOK {
		t.Fatalf("a GET released the key: %s", recorder.Body.String())
	}
	if strings.Contains(recorder.Body.String(), "AGE-SECRET-KEY-") {
		t.Fatal("a GET returned the private key")
	}
}

// A deployment with no escrow says so rather than failing, because not
// configuring a passphrase is a real choice a small school makes.
func TestWithoutEscrowTheSchoolKeyRoutesAnswerHonestly(t *testing.T) {
	server := schoolServer(t, "")

	recorder := call(t, server, http.MethodGet, "/api/v1/school/key", asSettled, "")
	if recorder.Code != http.StatusOK {
		t.Fatalf("get = %d: %s", recorder.Code, recorder.Body.String())
	}
	if !strings.Contains(recorder.Body.String(), `"configured":false`) {
		t.Errorf("body = %s, want configured false", recorder.Body.String())
	}

	released := call(t, server, http.MethodPost, "/api/v1/school/key/release", asSettled, "")
	if released.Code != http.StatusNotFound {
		t.Errorf("release = %d, want 404 when there is no key", released.Code)
	}
}

// Anonymous gets nothing. The key opens every file the school has encrypted.
func TestTheSchoolKeyNeedsASession(t *testing.T) {
	server := schoolServer(t, "the school's escrow passphrase")
	for _, one := range []struct{ method, path string }{
		{http.MethodGet, "/api/v1/school/key"},
		{http.MethodPost, "/api/v1/school/key/release"},
	} {
		recorder := call(t, server, one.method, one.path, "", "")
		if recorder.Code != http.StatusUnauthorized {
			t.Errorf("%s %s anonymous = %d, want 401", one.method, one.path, recorder.Code)
		}
	}
}

// The key's state is read without unlocking it (G7): a server whose escrow
// passphrase no longer opens the key still says it has one, and since when.
func TestTheSchoolKeyStateIsReadWithoutUnlockingIt(t *testing.T) {
	conn := db.TestDB(t, migrations.FS)
	key, err := escrow.NewStore(conn, db.SQLite, "the passphrase the key was made with").WithWorkFactor(10).Ensure(t.Context())
	if err != nil {
		t.Fatalf("ensure: %v", err)
	}
	changed := escrow.NewStore(conn, db.SQLite, "a passphrase that does not open it").WithWorkFactor(10)
	server, err := apiv1.New(apiv1.Options{
		Build:            apiv1.BuildInfo{Version: "1.2.3"},
		MinClientVersion: "0.1.0",
		Handlers: &apiv1.Handlers{
			API:    newPlanAPI(t, conn),
			Admin:  apiv1.NewAdmin(nil, apiv1.Nodes{}),
			Jobs:   apiv1.NewJobs(nil, nil, nil, nil, nil),
			School: apiv1.NewSchool(changed),
		},
		Kratos: kratosauthtest.Fake(t, map[string]kratosauth.Identity{
			asSettled: {ID: "planner", MetadataPublic: json.RawMessage(`{"role":"planner"}`)},
		}),
	})
	if err != nil {
		t.Fatalf("new server: %v", err)
	}

	recorder := call(t, server, http.MethodGet, "/api/v1/school/key", asSettled, "")
	if recorder.Code != http.StatusOK {
		t.Fatalf("get = %d: %s", recorder.Code, recorder.Body.String())
	}
	var state gen.SchoolKey
	if err := json.Unmarshal(recorder.Body.Bytes(), &state); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !state.Configured || state.Recipient == nil || *state.Recipient != key.Recipient ||
		state.CreatedAt == nil || !state.CreatedAt.Equal(key.CreatedAt) {
		t.Errorf("state = %+v, want the key made at %s", state, key.CreatedAt)
	}
}

// The desktop keeps the released key on the device, so its local copies,
// .gpf files encrypted to the school's key, open offline. The identity the
// route releases opens a file encrypted to the recipient the key route
// names, with the document module alone.
func TestTheReleasedKeyOpensACopyEncryptedToTheSchool(t *testing.T) {
	server := schoolServer(t, "the school's escrow passphrase")

	recorder := call(t, server, http.MethodGet, "/api/v1/school/key", asSettled, "")
	var public gen.SchoolKey
	if err := json.Unmarshal(recorder.Body.Bytes(), &public); err != nil || public.Recipient == nil {
		t.Fatalf("key = %s (%v)", recorder.Body.String(), err)
	}
	recipient, err := age.ParseX25519Recipient(*public.Recipient)
	if err != nil {
		t.Fatalf("recipient: %v", err)
	}
	copied, err := gpf.Encode(document.New(&arrangov1.SchoolModel{Name: "Szkoła"}), gpf.WriteOptions{
		Header: gpf.NewHeader().Set(gpf.FieldSchool, "Szkoła"), Recipients: []age.Recipient{recipient},
	})
	if err != nil {
		t.Fatalf("encode: %v", err)
	}

	released := call(t, server, http.MethodPost, "/api/v1/school/key/release", asSettled, "")
	if released.Code != http.StatusOK {
		t.Fatalf("release = %d: %s", released.Code, released.Body.String())
	}
	var private gen.SchoolKeyRelease
	if err := json.Unmarshal(released.Body.Bytes(), &private); err != nil {
		t.Fatalf("decode: %v", err)
	}
	identity, err := age.ParseX25519Identity(private.Identity)
	if err != nil {
		t.Fatalf("identity: %v", err)
	}
	opened, err := gpf.Decode(copied, gpf.ReadOptions{Identities: []age.Identity{identity}})
	if err != nil {
		t.Fatalf("the released key does not open the copy: %v", err)
	}
	if opened.Mode != gpf.ModeIdentity || opened.Document.GetModel().GetName() != "Szkoła" {
		t.Errorf("opened %q in mode %q", opened.Document.GetModel().GetName(), opened.Mode)
	}
}

// A key the escrow passphrase no longer opens cannot be released. The answer
// is the contract's 503, so the desktop keeps the key it holds and asks later.
func TestAKeyThatDoesNotUnsealIsUnavailable(t *testing.T) {
	conn := db.TestDB(t, migrations.FS)
	if _, err := escrow.NewStore(conn, db.SQLite, "the passphrase the key was made with").
		WithWorkFactor(10).Ensure(t.Context()); err != nil {
		t.Fatalf("ensure: %v", err)
	}
	changed := escrow.NewStore(conn, db.SQLite, "a passphrase that does not open it").WithWorkFactor(10)
	server, err := apiv1.New(apiv1.Options{
		Build:            apiv1.BuildInfo{Version: "1.2.3"},
		MinClientVersion: "0.1.0",
		Handlers: &apiv1.Handlers{
			API:    newPlanAPI(t, conn),
			Admin:  apiv1.NewAdmin(nil, apiv1.Nodes{}),
			Jobs:   apiv1.NewJobs(nil, nil, nil, nil, nil),
			School: apiv1.NewSchool(changed),
		},
		Kratos: kratosauthtest.Fake(t, map[string]kratosauth.Identity{
			asSettled: {ID: "planner", MetadataPublic: json.RawMessage(`{"role":"planner"}`)},
		}),
	})
	if err != nil {
		t.Fatalf("new server: %v", err)
	}

	released := call(t, server, http.MethodPost, "/api/v1/school/key/release", asSettled, "")
	if released.Code != http.StatusServiceUnavailable {
		t.Fatalf("release = %d: %s, want 503", released.Code, released.Body.String())
	}
	if !strings.Contains(released.Body.String(), `"message":"error.unavailable"`) {
		t.Errorf("body = %s, want error.unavailable", released.Body.String())
	}
	if strings.Contains(released.Body.String(), "AGE-SECRET-KEY-") {
		t.Fatal("the refusal carries a key")
	}
}

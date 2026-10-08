// api/v1/server_test.go

package v1_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/smegg99/s99ab/data/db"
	"github.com/smegg99/s99ab/data/storage"

	apiv1 "github.com/smegg99/goptivum/backend/api/v1"
	"github.com/smegg99/goptivum/backend/api/v1/gen"
	"github.com/smegg99/goptivum/backend/arrango"
	"github.com/smegg99/goptivum/backend/core/documents"
	"github.com/smegg99/goptivum/backend/core/plans"
	"github.com/smegg99/goptivum/backend/formats"
	"github.com/smegg99/goptivum/backend/formats/goptivum"
	optivumhtml "github.com/smegg99/goptivum/backend/formats/optivum/html"
	planxml "github.com/smegg99/goptivum/backend/formats/optivum/xml"
	"github.com/smegg99/goptivum/backend/migrations"
)

// trashDays is the default retention, so a test's removal dates read like the
// real ones.
const trashDays = 30

// fixedTrashDays is trashDays as the handlers read it.
func fixedTrashDays() int { return trashDays }

// newAPI is the plan half plus an administrative half with nothing configured,
// which is what a deployment without an identity provider or a paired node has.
func newAPI(t *testing.T) *apiv1.Handlers {
	t.Helper()
	return &apiv1.Handlers{
		API:   newPlanAPI(t, db.TestDB(t, migrations.FS)),
		Admin: apiv1.NewAdmin(nil, apiv1.Nodes{}),
		Jobs:  apiv1.NewJobs(nil, nil, nil, nil, nil),
	}
}

// planAPIWithRealm is newPlanAPI for a test that needs the server to belong to
// a particular school, which is what a .gpf refusal compares against.
func planAPIWithRealm(t *testing.T, realm string) *apiv1.API {
	t.Helper()
	api, err := apiv1.NewAPI(
		plans.NewRepository(db.TestDB(t, migrations.FS), db.SQLite, plans.Caps{BackupsKeep: 100, ResultsKeep: 20}),
		documents.NewStore(storage.NewLocalDir(t.TempDir(), "/objects")),
		formats.NewRegistry(goptivum.NewImporter(realm)),
		nil, apiv1.Settings{TrashDays: fixedTrashDays},
	)
	if err != nil {
		t.Fatalf("new api: %v", err)
	}
	return api
}

// planAPIOver is the plan half over an index, a store and a runner a test
// already holds.
func planAPIOver(t *testing.T, index *plans.Repository, store *documents.Store, runner *arrango.Runner) *apiv1.API {
	t.Helper()
	api, err := apiv1.NewAPI(index, store,
		formats.NewRegistry(planxml.New(), optivumhtml.NewImporter(), goptivum.NewImporter("")),
		runner, apiv1.Settings{TrashDays: fixedTrashDays})
	if err != nil {
		t.Fatalf("new api: %v", err)
	}
	return api
}

// newPlanAPI is the plan half over one connection, so a test that also needs
// the node registry can share the database with it.
func newPlanAPI(t *testing.T, conn *sql.DB) *apiv1.API {
	t.Helper()

	api, err := apiv1.NewAPI(
		plans.NewRepository(conn, db.SQLite, plans.Caps{BackupsKeep: 100, ResultsKeep: 20}),
		documents.NewStore(storage.NewLocalDir(t.TempDir(), "/objects")),
		formats.NewRegistry(planxml.New(), optivumhtml.NewImporter(), goptivum.NewImporter("")),
		nil, apiv1.Settings{TrashDays: fixedTrashDays},
	)
	if err != nil {
		t.Fatalf("new api: %v", err)
	}
	return api
}

// stopped is a dependency that is not there, which is the state this server
// has to keep answering discovery in.
func stopped(context.Context) error {
	return fmt.Errorf("stopped")
}

func newServer(t *testing.T) *apiv1.Server {
	t.Helper()

	server, err := apiv1.New(apiv1.Options{
		Build:            apiv1.BuildInfo{Version: "1.2.3", Commit: "abc", BuildTime: "now"},
		MinClientVersion: "0.1.0",
		Handlers:         newAPI(t),
		Database:         stopped,
		Storage:          stopped,
	})
	if err != nil {
		t.Fatalf("new server: %v", err)
	}
	return server
}

// The milestone's gate: a client has to be able to identify this server and
// learn the API version while everything the server depends on is down.
func TestInfoAnswersWithEveryDependencyStopped(t *testing.T) {
	server := newServer(t)

	recorder := httptest.NewRecorder()
	server.Handler().ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/info", nil))

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 with every dependency stopped", recorder.Code)
	}
	var info gen.Info
	if err := json.Unmarshal(recorder.Body.Bytes(), &info); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if info.Product != gen.Goptivum {
		t.Errorf("product = %q, want goptivum: it is the only conclusive identification", info.Product)
	}
	if info.ApiVersion != apiv1.APIVersion {
		t.Errorf("api_version = %q, want %q", info.ApiVersion, apiv1.APIVersion)
	}
	if info.MinClientVersion != "0.1.0" || info.ServerVersion != "1.2.3" {
		t.Errorf("versions = %q and %q", info.MinClientVersion, info.ServerVersion)
	}
	// A deployment reachable at one address publishes no list at all, rather
	// than an empty one: a client reads the two the same way, and the shorter
	// answer is the honest one.
	if info.Addresses != nil {
		t.Errorf("addresses = %v, want absent when the school published none", *info.Addresses)
	}
}

// A school with a public address publishes it here, which is the only way a
// client that connected over the local network can learn it.
//
// Without this a laptop carried out of the building has an address that no
// longer resolves and nothing to fall back to, and someone has to be told the
// public one by hand.
//
// The addresses are read on every request, so a change on Backend > Server
// is published at once.
func TestInfoPublishesTheOtherAddressesTheSchoolConfigured(t *testing.T) {
	published := []string{"https://plan.szkola.edu.pl"}
	server, err := apiv1.New(apiv1.Options{
		Build:            apiv1.BuildInfo{Version: "1.2.3"},
		MinClientVersion: "0.1.0",
		Addresses:        func() []string { return published },
		Handlers:         newAPI(t),
	})
	if err != nil {
		t.Fatalf("new server: %v", err)
	}

	info := func() gen.Info {
		t.Helper()
		recorder := httptest.NewRecorder()
		server.Handler().ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/info", nil))
		if recorder.Code != http.StatusOK {
			t.Fatalf("status = %d", recorder.Code)
		}
		var info gen.Info
		if err := json.Unmarshal(recorder.Body.Bytes(), &info); err != nil {
			t.Fatalf("decode: %v", err)
		}
		return info
	}
	if got := info().Addresses; got == nil || len(*got) != 1 || (*got)[0] != "https://plan.szkola.edu.pl" {
		t.Errorf("addresses = %v, want the configured public address", got)
	}

	published = []string{"https://plan.szkola.edu.pl", "http://192.0.2.10:8833"}
	if got := info().Addresses; got == nil || len(*got) != 2 || (*got)[1] != "http://192.0.2.10:8833" {
		t.Errorf("addresses after a change = %v, want both", got)
	}
	published = nil
	if got := info().Addresses; got != nil {
		t.Errorf("addresses after removing them = %v, want absent", *got)
	}
}

// The path carries no version, because a client cannot ask which version to
// use at a versioned path.
func TestInfoIsNotUnderTheVersionedPrefix(t *testing.T) {
	server := newServer(t)

	recorder := httptest.NewRecorder()
	server.Handler().ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/info", nil))
	if recorder.Code != http.StatusNotFound {
		t.Errorf("/api/v1/info = %d, want 404: the discovery path is unversioned", recorder.Code)
	}
}

// Readiness is honest about a stopped dependency, and discovery is not.
func TestReadinessReportsWhatDiscoveryIgnores(t *testing.T) {
	server := newServer(t)

	recorder := httptest.NewRecorder()
	server.Handler().ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/ready", nil))
	if recorder.Code != http.StatusServiceUnavailable {
		t.Errorf("/api/v1/ready = %d, want 503 while a dependency is stopped", recorder.Code)
	}

	recorder = httptest.NewRecorder()
	server.Handler().ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/health", nil))
	if recorder.Code != http.StatusOK {
		t.Errorf("/api/v1/health = %d, want 200: liveness is not readiness", recorder.Code)
	}
}

// Uploads alone is not the upload half of Guarded. Without an identity
// provider every plan route stays unregistered, because the alternative is
// mounting file routes open to anyone on the school network.
func TestPlanRoutesAreAbsentWithoutAnIdentityProvider(t *testing.T) {
	server := newServer(t)
	if server.ServesPlans() {
		t.Fatal("plan routes are registered with no Kratos configured")
	}

	for _, route := range []struct {
		method, path string
	}{
		{http.MethodGet, "/api/v1/plans"},
		{http.MethodPost, "/api/v1/plans"},
		{http.MethodPost, "/api/v1/plans/demo"},
		{http.MethodGet, "/api/v1/plans/1"},
		{http.MethodPatch, "/api/v1/plans/1"},
		{http.MethodDelete, "/api/v1/plans/1"},
		{http.MethodPost, "/api/v1/plans/1/restore"},
		{http.MethodGet, "/api/v1/trash"},
		{http.MethodGet, "/api/v1/plans/1/versions"},
		{http.MethodPost, "/api/v1/plans/1/versions"},
		{http.MethodGet, "/api/v1/plans/1/versions/1"},
		{http.MethodPut, "/api/v1/plans/1/versions/1/checkpoint"},
		{http.MethodDelete, "/api/v1/plans/1/versions/1/checkpoint"},
		{http.MethodPost, "/api/v1/plans/1/versions/1/checkpoint/restore"},
		{http.MethodGet, "/api/v1/rules"},
		{http.MethodGet, "/api/v1/rules/profiles"},
		{http.MethodPost, "/api/v1/rules/profiles"},
		{http.MethodPatch, "/api/v1/rules/profiles/1"},
		{http.MethodDelete, "/api/v1/rules/profiles/1"},
		{http.MethodPost, "/api/v1/import"},
		{http.MethodPost, "/api/v1/import/preview"},
	} {
		t.Run(route.method+" "+route.path, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			server.Handler().ServeHTTP(recorder, httptest.NewRequest(route.method, route.path, nil))
			if recorder.Code != http.StatusNotFound {
				t.Errorf("status = %d, want 404 while there are no identities", recorder.Code)
			}
		})
	}
}

func TestServerRefusesToStartWithoutHandlers(t *testing.T) {
	if _, err := apiv1.New(apiv1.Options{}); err == nil {
		t.Error("a server with no handlers started")
	}
	if _, err := apiv1.NewAPI(nil, nil, nil, nil, apiv1.Settings{TrashDays: fixedTrashDays}); err == nil {
		t.Error("handlers with no dependencies were accepted")
	}
}

// Readiness has to notice storage, not only the database.
//
// It did not: Storage was never wired, so this server answered "ready" with its
// object store unreachable, and the first anyone knew was a failed upload. A
// document is half of what this product stores.
func TestReadinessNoticesBrokenStorage(t *testing.T) {
	working := func(context.Context) error { return nil }
	broken := errors.New("storage: connection refused")

	for _, one := range []struct {
		name    string
		storage apiv1.Readiness
		want    int
	}{
		{"storage answers", working, http.StatusOK},
		{"storage is down", func(context.Context) error { return broken }, http.StatusServiceUnavailable},
	} {
		t.Run(one.name, func(t *testing.T) {
			server, err := apiv1.New(apiv1.Options{
				Build:            apiv1.BuildInfo{Version: "1.2.3"},
				MinClientVersion: "0.1.0",
				Handlers:         newAPI(t),
				Database:         working,
				Storage:          one.storage,
			})
			if err != nil {
				t.Fatalf("new server: %v", err)
			}
			recorder := httptest.NewRecorder()
			server.Handler().ServeHTTP(recorder,
				httptest.NewRequest(http.MethodGet, "/api/v1/ready", nil))
			if recorder.Code != one.want {
				t.Errorf("/api/v1/ready = %d, want %d: %s",
					recorder.Code, one.want, recorder.Body.String())
			}
		})
	}
}

// The probe reaches the backend without writing to it. A readiness check that
// writes leaves rubbish behind on every poll.
func TestTheStorageProbeAnswersWithoutWriting(t *testing.T) {
	directory := t.TempDir()
	store := documents.NewStore(storage.NewLocalDir(directory, "/objects"))

	if err := store.Probe(t.Context()); err != nil {
		t.Fatalf("probe: %v", err)
	}
	entries, err := os.ReadDir(directory)
	if err != nil {
		t.Fatalf("read dir: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("the probe left %d entries behind", len(entries))
	}
}

// api/v1/server_test.go

package v1_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/smegg99/s99ab/data/db"
	"github.com/smegg99/s99ab/data/storage"

	apiv1 "github.com/smegg99/goptivum/backend/api/v1"
	"github.com/smegg99/goptivum/backend/api/v1/gen"
	"github.com/smegg99/goptivum/backend/core/documents"
	"github.com/smegg99/goptivum/backend/core/plans"
	"github.com/smegg99/goptivum/backend/formats"
	optivumhtml "github.com/smegg99/goptivum/backend/formats/optivum/html"
	planxml "github.com/smegg99/goptivum/backend/formats/optivum/xml"
	"github.com/smegg99/goptivum/backend/migrations"
)

func newAPI(t *testing.T) *apiv1.API {
	t.Helper()

	conn := db.TestDB(t, migrations.FS)
	api, err := apiv1.NewAPI(
		plans.NewRepository(conn, db.SQLite),
		documents.NewStore(storage.NewLocalDir(t.TempDir(), "/objects")),
		formats.NewRegistry(planxml.New(), optivumhtml.NewImporter()),
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
		{http.MethodGet, "/api/v1/plans/1"},
		{http.MethodDelete, "/api/v1/plans/1"},
		{http.MethodGet, "/api/v1/plans/1/versions"},
		{http.MethodPost, "/api/v1/plans/1/versions"},
		{http.MethodGet, "/api/v1/plans/1/versions/1"},
		{http.MethodPost, "/api/v1/import"},
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
	if _, err := apiv1.NewAPI(nil, nil, nil); err == nil {
		t.Error("handlers with no dependencies were accepted")
	}
}

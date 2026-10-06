// api/v1/demo_test.go

package v1_test

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"net"
	"net/http"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"

	"github.com/smegg99/s99ab/auth/kratosauth"
	"github.com/smegg99/s99ab/auth/kratosauth/kratosauthtest"
	"github.com/smegg99/s99ab/data/db"
	"github.com/smegg99/s99ab/data/storage"

	arrangoid "github.com/smegg99/arrango/backend/identity"

	arrangorpcv1 "github.com/smegg99/arrango/backend/gen/arrangorpcv1"
	arrangov1 "github.com/smegg99/arrango/backend/gen/arrangov1"

	apiv1 "github.com/smegg99/goptivum/backend/api/v1"
	"github.com/smegg99/goptivum/backend/api/v1/gen"
	"github.com/smegg99/goptivum/backend/arrango"
	"github.com/smegg99/goptivum/backend/core/documents"
	"github.com/smegg99/goptivum/backend/core/plans"
	"github.com/smegg99/goptivum/backend/migrations"
	"github.com/smegg99/goptivum/document"
	"github.com/smegg99/goptivum/solveclient"
)

// demoNode speaks the node's wire, mutual TLS included, and answers only the
// demo school.
type demoNode struct {
	arrangorpcv1.UnimplementedSolverServiceServer
	asked chan *arrangorpcv1.DemoRequest
}

func (n *demoNode) GetDemoSchool(_ context.Context, request *arrangorpcv1.DemoRequest) (*arrangov1.SchoolModel, error) {
	n.asked <- request
	return &arrangov1.SchoolModel{
		Name:    "Szkoła Demonstracyjna",
		Lessons: []*arrangov1.LessonInstance{{Id: 1, Duration: 1}, {Id: 2, Duration: 1}},
	}, nil
}

// startDemoNode serves on loopback until the test ends and says how to pin it.
func startDemoNode(t *testing.T) (*demoNode, solveclient.Paired) {
	t.Helper()
	self, err := arrangoid.Create(t.TempDir())
	if err != nil {
		t.Fatalf("node identity: %v", err)
	}
	server := grpc.NewServer(grpc.Creds(credentials.NewTLS(&tls.Config{
		Certificates: []tls.Certificate{self.Certificate},
		ClientAuth:   tls.RequireAnyClientCert,
		MinVersion:   tls.VersionTLS13,
	})))
	node := &demoNode{asked: make(chan *arrangorpcv1.DemoRequest, 4)}
	arrangorpcv1.RegisterSolverServiceServer(server, node)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(server.Stop)
	return node, solveclient.Paired{Address: listener.Addr().String(), Fingerprint: self.Fingerprint}
}

type demoServer struct {
	server   *apiv1.Server
	registry *arrango.Registry
	plans    *plans.Repository
}

func newDemoServer(t *testing.T, enabled bool) demoServer {
	t.Helper()
	conn := db.TestDB(t, migrations.FS)
	registry := arrango.NewRegistry(conn, db.SQLite)
	index := plans.NewRepository(conn, db.SQLite, plans.Caps{BackupsKeep: 100, ResultsKeep: 20})
	store := documents.NewStore(storage.NewLocalDir(t.TempDir(), "/objects"))
	self, err := solveclient.LoadIdentity(t.TempDir())
	if err != nil {
		t.Fatalf("identity: %v", err)
	}
	dialer, err := solveclient.NewDialer(self, "0.1.0")
	if err != nil {
		t.Fatalf("dialer: %v", err)
	}
	api := planAPIOver(t, index, store, nil)
	server, err := apiv1.New(apiv1.Options{
		Build:            apiv1.BuildInfo{Version: "1.2.3"},
		MinClientVersion: "0.1.0",
		Handlers: &apiv1.Handlers{
			API:   api,
			Admin: apiv1.NewAdmin(nil, apiv1.Nodes{Registry: registry}),
			Jobs:  apiv1.NewJobs(nil, nil, nil, nil, nil),
			Demo:  apiv1.NewDemo(api, registry, dialer),
		},
		Kratos:     kratosauthtest.Fake(t, map[string]kratosauth.Identity{asOwner: {ID: "owner"}}),
		DemoSchool: enabled,
	})
	if err != nil {
		t.Fatalf("new server: %v", err)
	}
	return demoServer{server: server, registry: registry, plans: index}
}

// A real school's server has no route that fetches a synthetic school.
func TestTheDemoRouteIsAbsentUnlessConfigured(t *testing.T) {
	demo := newDemoServer(t, false)
	if demo.server.ServesDemo() {
		t.Fatal("the demo route is registered with dev.demo_school off")
	}
	// A node that would answer, so only the missing route can refuse.
	_, paired := startDemoNode(t)
	if _, err := demo.registry.Record(t.Context(), "node", paired); err != nil {
		t.Fatalf("record: %v", err)
	}
	recorder := call(t, demo.server, http.MethodPost, "/api/v1/plans/demo", asOwner,
		`{"name":"M0 weryfikacja","school_year":"2026/2027"}`)
	if recorder.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404 while the route is off: %s", recorder.Code, recorder.Body.String())
	}
	envelope(t, recorder)
	if listed, err := demo.plans.List(t.Context()); err != nil || len(listed) != 0 {
		t.Errorf("a plan was made anyway: %+v, %v", listed, err)
	}
}

func TestTheDemoRouteStoresTheNodesSchoolAsANewPlan(t *testing.T) {
	demo := newDemoServer(t, true)
	if !demo.server.ServesDemo() {
		t.Fatal("the demo route is not registered with dev.demo_school on")
	}

	// No node yet: the key says why, and nothing is stored.
	refused := call(t, demo.server, http.MethodPost, "/api/v1/plans/demo", asOwner,
		`{"name":"M0 weryfikacja","school_year":"2026/2027"}`)
	if refused.Code != http.StatusConflict {
		t.Fatalf("with no node = %d: %s", refused.Code, refused.Body.String())
	}
	if body := envelope(t, refused); body.Code != "conflict" || body.Message != "solve.nodeUnavailable" {
		t.Errorf("envelope = %+v", body)
	}

	node, paired := startDemoNode(t)
	if _, err := demo.registry.Record(t.Context(), "node", paired); err != nil {
		t.Fatalf("record: %v", err)
	}
	blank := call(t, demo.server, http.MethodPost, "/api/v1/plans/demo", asOwner, `{"name":" "}`)
	if blank.Code != http.StatusUnprocessableEntity {
		t.Errorf("a blank name = %d: %s", blank.Code, blank.Body.String())
	}

	created := call(t, demo.server, http.MethodPost, "/api/v1/plans/demo", asOwner,
		`{"name":"M0 weryfikacja","school_year":"2026/2027"}`)
	if created.Code != http.StatusCreated {
		t.Fatalf("create = %d: %s", created.Code, created.Body.String())
	}
	var plan gen.Plan
	if err := json.Unmarshal(created.Body.Bytes(), &plan); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if plan.Name != "M0 weryfikacja" || plan.SchoolYear != "2026/2027" {
		t.Errorf("plan = %+v, want the name and year from the request", plan)
	}

	asked := <-node.asked
	if asked.GetPreset() != arrangorpcv1.DemoPreset_DEMO_PRESET_PRODUCTION || asked.GetSeed() != 0 {
		t.Errorf("the node was asked %+v, want the production preset with seed 0", asked)
	}

	downloaded := call(t, demo.server, http.MethodGet, "/api/v1/plans/"+plan.Id+"/versions/1", asOwner, "")
	if downloaded.Code != http.StatusOK {
		t.Fatalf("download = %d", downloaded.Code)
	}
	doc, err := document.Decode(downloaded.Body.Bytes())
	if err != nil {
		t.Fatalf("decode document: %v", err)
	}
	if doc.GetModel().GetName() != "Szkoła Demonstracyjna" || len(doc.GetModel().GetLessons()) != 2 {
		t.Errorf("version 1 holds %q with %d lessons, want the node's school",
			doc.GetModel().GetName(), len(doc.GetModel().GetLessons()))
	}
	if doc.GetNextArrangoId() <= 2 {
		t.Errorf("next_arrango_id = %d, want it above every id the school uses", doc.GetNextArrangoId())
	}
}

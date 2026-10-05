// api/v1/checks_test.go

package v1_test

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"net"
	"net/http"
	"slices"
	"strconv"
	"sync"
	"testing"
	"time"

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
	"github.com/smegg99/goptivum/backend/common/messages"
	"github.com/smegg99/goptivum/backend/core/checks"
	"github.com/smegg99/goptivum/backend/core/documents"
	"github.com/smegg99/goptivum/backend/core/plans"
	"github.com/smegg99/goptivum/backend/migrations"
	"github.com/smegg99/goptivum/document"
	"github.com/smegg99/goptivum/solveclient"
)

// checkNode speaks the node's wire, mutual TLS included, and answers only
// GetNodeInfo and Check.
type checkNode struct {
	arrangorpcv1.UnimplementedSolverServiceServer
	arrangorpcv1.UnimplementedNodeServiceServer

	capabilities []string
	report       *arrangorpcv1.CheckReport
	// hold, when set, runs before each answer; call counts from 1.
	hold func(ctx context.Context, call int) error

	asked chan struct{}
	mu    sync.Mutex
	calls []*arrangorpcv1.CheckRequest
}

func (n *checkNode) GetNodeInfo(context.Context, *arrangorpcv1.NodeInfoRequest) (*arrangorpcv1.NodeInfoResponse, error) {
	return &arrangorpcv1.NodeInfoResponse{NodeApiVersion: "1.0.0", Capabilities: n.capabilities}, nil
}

func (n *checkNode) Check(ctx context.Context, request *arrangorpcv1.CheckRequest) (*arrangorpcv1.CheckReport, error) {
	n.mu.Lock()
	n.calls = append(n.calls, request)
	call := len(n.calls)
	n.mu.Unlock()
	n.asked <- struct{}{}
	if n.hold != nil {
		if err := n.hold(ctx, call); err != nil {
			return nil, err
		}
	}
	if n.report != nil {
		return n.report, nil
	}
	return &arrangorpcv1.CheckReport{}, nil
}

func (n *checkNode) requests() []*arrangorpcv1.CheckRequest {
	n.mu.Lock()
	defer n.mu.Unlock()
	return slices.Clone(n.calls)
}

func (n *checkNode) waitAsked(t *testing.T) {
	t.Helper()
	select {
	case <-n.asked:
	case <-time.After(5 * time.Second):
		t.Fatal("the node was never asked to check")
	}
}

func startCheckNode(t *testing.T, node *checkNode) solveclient.Paired {
	t.Helper()
	node.asked = make(chan struct{}, 16)
	return serveNode(t, node)
}

// fakeNodeServer answers the node's two services; a fake embeds their Unimplemented servers.
type fakeNodeServer interface {
	arrangorpcv1.SolverServiceServer
	arrangorpcv1.NodeServiceServer
}

// serveNode serves a fake node over mutual TLS on loopback until the test ends.
func serveNode(t *testing.T, node fakeNodeServer) solveclient.Paired {
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
	arrangorpcv1.RegisterSolverServiceServer(server, node)
	arrangorpcv1.RegisterNodeServiceServer(server, node)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(server.Stop)
	return solveclient.Paired{Address: listener.Addr().String(), Fingerprint: self.Fingerprint}
}

type checking struct {
	server   *apiv1.Server
	registry *arrango.Registry
	plans    *plans.Repository
	store    *documents.Store
}

func newCheckServer(t *testing.T, deadline time.Duration) checking {
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
			API:    api,
			Admin:  apiv1.NewAdmin(nil, apiv1.Nodes{Registry: registry}),
			Jobs:   apiv1.NewJobs(nil, nil, nil, nil, nil),
			Checks: apiv1.NewChecks(api, checks.New(arrango.NewChecker(registry, dialer).Check, deadline)),
		},
		Kratos: kratosauthtest.Fake(t, map[string]kratosauth.Identity{
			asOwner: {ID: "owner"}, asStranger: {ID: "stranger"},
		}),
	})
	if err != nil {
		t.Fatalf("new server: %v", err)
	}
	return checking{server: server, registry: registry, plans: index, store: store}
}

// planWithSchool stores a school with one lesson as version 1 of an owner's plan.
func (c checking) planWithSchool(t *testing.T, name string) int64 {
	t.Helper()
	encoded, err := document.Encode(document.New(&arrangov1.SchoolModel{
		Name: name, Lessons: []*arrangov1.LessonInstance{{Id: 7, Duration: 1}},
	}))
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	key, err := c.store.Put(t.Context(), encoded)
	if err != nil {
		t.Fatalf("put: %v", err)
	}
	plan, _, err := c.plans.Create(t.Context(), plans.Plan{
		OwnerIdentityID: "owner", Name: name, SchoolYear: "2026/2027",
	}, plans.Upload{ObjectKey: key, SizeBytes: encoded.Size, CreatedBy: "owner"})
	if err != nil {
		t.Fatalf("create plan: %v", err)
	}
	return plan.ID
}

// nextVersion saves a different school as the plan's next version.
func (c checking) nextVersion(t *testing.T, planID, parent int64, name string) int64 {
	t.Helper()
	encoded, err := document.Encode(document.New(&arrangov1.SchoolModel{Name: name}))
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	key, err := c.store.Put(t.Context(), encoded)
	if err != nil {
		t.Fatalf("put: %v", err)
	}
	version, err := c.plans.AddVersion(t.Context(), plans.Upload{
		PlanID: planID, Parent: parent, ObjectKey: key, SizeBytes: encoded.Size, CreatedBy: "owner",
	})
	if err != nil {
		t.Fatalf("add version: %v", err)
	}
	return version.Version
}

func checkPath(planID, version int64) string {
	return "/api/v1/plans/" + strconv.FormatInt(planID, 10) + "/versions/" + strconv.FormatInt(version, 10) + "/check"
}

func strictCheck(t *testing.T, body []byte) gen.Check {
	t.Helper()
	var check gen.Check
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&check); err != nil {
		t.Fatalf("answer %q is not a Check: %v", body, err)
	}
	return check
}

func TestACheckAnswersInTheContractsWordsAndIsCached(t *testing.T) {
	c := newCheckServer(t, checks.Deadline)
	node := &checkNode{capabilities: []string{"solve", "check"}, report: &arrangorpcv1.CheckReport{
		Hard: []*arrangov1.Conflict{{
			Kind: arrangov1.Conflict_KIND_TEACHER_OVERLOADED, Message: "debug text",
			Entities: []*arrangov1.EntityRef{{Kind: arrangov1.EntityKind_ENTITY_KIND_TEACHER, Id: 12}},
			Needed:   31, Available: 30,
		}},
		Advisory: []*arrangov1.Conflict{{
			Kind:     arrangov1.Conflict_KIND_AVAILABILITY_HOLE,
			Entities: []*arrangov1.EntityRef{{Kind: arrangov1.EntityKind_ENTITY_KIND_EXTERNAL_BLOCK, Id: 50}},
		}},
	}}
	if _, err := c.registry.Record(t.Context(), "check", startCheckNode(t, node)); err != nil {
		t.Fatalf("record: %v", err)
	}
	planID := c.planWithSchool(t, "Plan 2027")

	recorder := call(t, c.server, http.MethodGet, checkPath(planID, 1), asOwner, "")
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", recorder.Code, recorder.Body.String())
	}
	check := strictCheck(t, recorder.Body.Bytes())
	if check.PlanId != strconv.FormatInt(planID, 10) || check.Version != 1 ||
		check.Errors != 1 || check.Warnings != 1 || check.Notes != 0 || check.Ignored != 0 || check.Score != nil {
		t.Fatalf("check = %+v", check)
	}
	overloaded := check.Findings[0]
	if overloaded.Part != gen.Input || overloaded.Kind != "teacher_overloaded" || overloaded.Severity != gen.CheckFindingSeverityError ||
		overloaded.Needed == nil || *overloaded.Needed != 31 || overloaded.Available == nil || *overloaded.Available != 30 ||
		overloaded.Entities == nil || (*overloaded.Entities)[0] != (gen.EntityRef{Kind: gen.Teacher, Id: 12}) {
		t.Errorf("first finding = %+v", overloaded)
	}
	if hole := check.Findings[1]; hole.Part != gen.Input || hole.Kind != "availability_hole" || hole.Severity != gen.CheckFindingSeverityWarning {
		t.Errorf("second finding = %+v", hole)
	}
	if bytes.Contains(recorder.Body.Bytes(), []byte("debug text")) {
		t.Error("the node's English message reached the answer")
	}

	asked := node.requests()
	if len(asked) != 1 || asked[0].GetModel().GetName() != "Plan 2027" || asked[0].GetSnapshot() != nil {
		t.Fatalf("the node was asked %v, want the stored school without a timetable", asked)
	}
	if again := call(t, c.server, http.MethodGet, checkPath(planID, 1), asOwner, ""); again.Code != http.StatusOK {
		t.Fatalf("second check status = %d", again.Code)
	}
	if len(node.requests()) != 1 {
		t.Errorf("a repeated check reached the node %d times in all, want once", len(node.requests()))
	}
}

func TestACheckNeedsAPairedNode(t *testing.T) {
	c := newCheckServer(t, checks.Deadline)
	planID := c.planWithSchool(t, "Plan 2027")
	recorder := call(t, c.server, http.MethodGet, checkPath(planID, 1), asOwner, "")
	if recorder.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409: %s", recorder.Code, recorder.Body.String())
	}
	if body := envelope(t, recorder); body.Message != messages.KeySolveNodeUnavailable {
		t.Errorf("message = %q, want %q", body.Message, messages.KeySolveNodeUnavailable)
	}
}

func TestANodeThatCannotCheckIsOutdated(t *testing.T) {
	c := newCheckServer(t, checks.Deadline)
	node := &checkNode{capabilities: []string{"solve", "validate", "score"}}
	if _, err := c.registry.Record(t.Context(), "old", startCheckNode(t, node)); err != nil {
		t.Fatalf("record: %v", err)
	}
	planID := c.planWithSchool(t, "Plan 2027")

	recorder := call(t, c.server, http.MethodGet, checkPath(planID, 1), asOwner, "")
	if recorder.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409: %s", recorder.Code, recorder.Body.String())
	}
	body := envelope(t, recorder)
	if body.Message != messages.KeyNodeOutdated || body.Details == nil || (*body.Details)["Capability"] != "check" {
		t.Errorf("envelope = %+v, want %s naming check", body, messages.KeyNodeOutdated)
	}
	if len(node.requests()) != 0 {
		t.Error("an outdated node was asked to check")
	}
}

func TestASlowNodeIsWaitingForTheSolver(t *testing.T) {
	c := newCheckServer(t, 200*time.Millisecond)
	node := &checkNode{capabilities: []string{"check"}, hold: func(ctx context.Context, _ int) error {
		<-ctx.Done()
		return ctx.Err()
	}}
	if _, err := c.registry.Record(t.Context(), "busy", startCheckNode(t, node)); err != nil {
		t.Fatalf("record: %v", err)
	}
	planID := c.planWithSchool(t, "Plan 2027")

	recorder := call(t, c.server, http.MethodGet, checkPath(planID, 1), asOwner, "")
	if recorder.Code != http.StatusGatewayTimeout {
		t.Fatalf("status = %d, want 504: %s", recorder.Code, recorder.Body.String())
	}
	if body := envelope(t, recorder); body.Message != messages.KeyCheckWaitingForSolver {
		t.Errorf("message = %q, want %q", body.Message, messages.KeyCheckWaitingForSolver)
	}
}

func TestANewerVersionsCheckSupersedesTheOlder(t *testing.T) {
	c := newCheckServer(t, checks.Deadline)
	node := &checkNode{capabilities: []string{"check"}, hold: func(ctx context.Context, call int) error {
		if call == 1 {
			<-ctx.Done()
			return ctx.Err()
		}
		return nil
	}}
	if _, err := c.registry.Record(t.Context(), "check", startCheckNode(t, node)); err != nil {
		t.Fatalf("record: %v", err)
	}
	planID := c.planWithSchool(t, "Plan 2027")
	second := c.nextVersion(t, planID, 1, "Plan 2027, edited")

	older := make(chan int, 1)
	var olderBody []byte
	go func() {
		recorder := call(t, c.server, http.MethodGet, checkPath(planID, 1), asOwner, "")
		olderBody = recorder.Body.Bytes()
		older <- recorder.Code
	}()
	node.waitAsked(t)

	if recorder := call(t, c.server, http.MethodGet, checkPath(planID, second), asOwner, ""); recorder.Code != http.StatusOK {
		t.Fatalf("newer check status = %d: %s", recorder.Code, recorder.Body.String())
	}
	select {
	case code := <-older:
		var body gen.Error
		if code != http.StatusConflict || json.Unmarshal(olderBody, &body) != nil || body.Message != messages.KeyCheckSuperseded {
			t.Errorf("older check = %d %s, want 409 %s", code, olderBody, messages.KeyCheckSuperseded)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the older check never answered")
	}
}

func TestACheckIsEveryonesAndNeedsAVersionAndASession(t *testing.T) {
	c := newCheckServer(t, checks.Deadline)
	planID := c.planWithSchool(t, "Plan 2027")
	// Another account reads the plan, so it is answered as the owner is.
	owned := call(t, c.server, http.MethodGet, checkPath(planID, 1), asOwner, "")
	if other := call(t, c.server, http.MethodGet, checkPath(planID, 1), asStranger, ""); other.Code != owned.Code ||
		other.Code == http.StatusNotFound || other.Code == http.StatusForbidden {
		t.Errorf("another account's check = %d, the owner's %d", other.Code, owned.Code)
	}
	for _, attempt := range []struct {
		name       string
		path, auth string
		want       int
	}{
		{"no such version", checkPath(planID, 9), asOwner, http.StatusNotFound},
		{"no such plan", checkPath(planID+1, 1), asOwner, http.StatusNotFound},
		{"no session", checkPath(planID, 1), "", http.StatusUnauthorized},
	} {
		recorder := call(t, c.server, http.MethodGet, attempt.path, attempt.auth, "")
		if recorder.Code != attempt.want {
			t.Errorf("%s: status = %d, want %d: %s", attempt.name, recorder.Code, attempt.want, recorder.Body.String())
			continue
		}
		envelope(t, recorder)
	}
}

func TestBothComparisonRoutesUseCapturedMainRulesAndSeparateCache(t *testing.T) {
	c := newCheckServer(t, checks.Deadline)
	node := &checkNode{capabilities: []string{"check"}, report: &arrangorpcv1.CheckReport{
		Validation: &arrangov1.ValidationReport{},
		Hard:       []*arrangov1.Conflict{{Kind: arrangov1.Conflict_KIND_INVALID_MODEL}},
		Score:      &arrangov1.ScoreReport{OverallQuality: 96},
	}}
	if _, err := c.registry.Record(t.Context(), "check", startCheckNode(t, node)); err != nil {
		t.Fatal(err)
	}
	planID := c.planWithSchool(t, "Result school")
	first, err := c.plans.Version(t.Context(), planID, 1)
	if err != nil {
		t.Fatal(err)
	}
	doc := document.New(&arrangov1.SchoolModel{Name: "Main school", RuleConfig: &arrangov1.RuleConfig{Profile: "relaxed"}})
	encoded, err := document.Encode(doc)
	if err != nil {
		t.Fatal(err)
	}
	key, err := c.store.Put(t.Context(), encoded)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.plans.AddVersion(t.Context(), plans.Upload{PlanID: planID, Parent: 1, ObjectKey: key, SizeBytes: encoded.Size, CreatedBy: "owner"}); err != nil {
		t.Fatal(err)
	}
	profile, err := arrango.EncodeRules(&arrangov1.RuleConfig{Profile: "dobry_plan"})
	if err != nil {
		t.Fatal(err)
	}
	result, err := c.plans.AddResult(t.Context(), plans.NewResult{PlanID: planID, SourceVersion: 1, ObjectKey: first.ObjectKey, SizeBytes: first.SizeBytes, StartedBy: "owner", RuleProfile: &plans.RuleProfile{Name: "dobry_plan", Rules: profile}})
	if err != nil {
		t.Fatal(err)
	}
	resultPath := "/api/v1/plans/" + strconv.FormatInt(planID, 10) + "/results/" + strconv.FormatInt(result.ID, 10) + "/check"
	for _, path := range []string{checkPath(planID, 1), resultPath} {
		response := call(t, c.server, http.MethodGet, path+"?rules_version=2", asStranger, "")
		if response.Code != http.StatusOK {
			t.Fatalf("%s: %d %s", path, response.Code, response.Body.String())
		}
		check := strictCheck(t, response.Body.Bytes())
		if check.Score == nil || check.Score.Quality != 96 || check.Score.Rating != 45 {
			t.Fatalf("score: %+v", check.Score)
		}
	}
	requests := node.requests()
	if len(requests) != 1 || requests[0].GetModel().GetName() != "Result school" || requests[0].GetModel().GetRuleConfig() != nil || requests[0].GetRuleConfig().GetProfile() != "relaxed" {
		t.Fatalf("comparison requests: %v", requests)
	}
	if response := call(t, c.server, http.MethodGet, resultPath, asOwner, ""); response.Code != http.StatusOK {
		t.Fatal(response.Body.String())
	}
	requests = node.requests()
	if len(requests) != 2 || requests[1].GetRuleConfig().GetProfile() != "dobry_plan" {
		t.Fatalf("stored-profile requests: %v", requests)
	}
	for _, path := range []string{checkPath(planID, 1), resultPath} {
		if response := call(t, c.server, http.MethodGet, path+"?rules_version=999", asOwner, ""); response.Code != http.StatusNotFound {
			t.Errorf("missing rules version: %d", response.Code)
		}
		if response := call(t, c.server, http.MethodGet, path+"?rules_version=0", asOwner, ""); response.Code != http.StatusBadRequest {
			t.Errorf("invalid rules version: %d", response.Code)
		}
	}
}

// api/v1/rules_test.go

package v1_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/smegg99/s99ab/auth/kratosauth"
	"github.com/smegg99/s99ab/auth/kratosauth/kratosauthtest"
	"github.com/smegg99/s99ab/data/db"

	arrangorpcv1 "github.com/smegg99/arrango/backend/gen/arrangorpcv1"
	arrangov1 "github.com/smegg99/arrango/backend/gen/arrangov1"

	apiv1 "github.com/smegg99/goptivum/backend/api/v1"
	"github.com/smegg99/goptivum/backend/api/v1/gen"
	"github.com/smegg99/goptivum/backend/arrango"
	"github.com/smegg99/goptivum/backend/common/messages"
	"github.com/smegg99/goptivum/backend/migrations"
	"github.com/smegg99/goptivum/solveclient"
)

const rulesPath = "/api/v1/rules"

// ruleNode speaks the node's wire and answers only GetNodeInfo and GetRuleCatalogue.
type ruleNode struct {
	arrangorpcv1.UnimplementedSolverServiceServer
	arrangorpcv1.UnimplementedNodeServiceServer

	capabilities []string
	catalogue    *arrangorpcv1.RuleCatalogue
	// hold, when set, runs before the answer.
	hold func(ctx context.Context) error

	mu    sync.Mutex
	calls int
}

func (n *ruleNode) GetNodeInfo(context.Context, *arrangorpcv1.NodeInfoRequest) (*arrangorpcv1.NodeInfoResponse, error) {
	return &arrangorpcv1.NodeInfoResponse{NodeApiVersion: "1.0.0", Capabilities: n.capabilities}, nil
}

func (n *ruleNode) GetRuleCatalogue(ctx context.Context, _ *arrangorpcv1.RuleCatalogueRequest) (*arrangorpcv1.RuleCatalogue, error) {
	n.mu.Lock()
	n.calls++
	n.mu.Unlock()
	if n.hold != nil {
		if err := n.hold(ctx); err != nil {
			return nil, err
		}
	}
	return n.catalogue, nil
}

func (n *ruleNode) asked() int {
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.calls
}

type ruling struct {
	server   *apiv1.Server
	registry *arrango.Registry
}

func newRulesServer(t *testing.T, deadline time.Duration) ruling {
	t.Helper()
	registry := arrango.NewRegistry(db.TestDB(t, migrations.FS), db.SQLite)
	self, err := solveclient.LoadIdentity(t.TempDir())
	if err != nil {
		t.Fatalf("identity: %v", err)
	}
	dialer, err := solveclient.NewDialer(self, "0.1.0")
	if err != nil {
		t.Fatalf("dialer: %v", err)
	}
	server, err := apiv1.New(apiv1.Options{
		Build:            apiv1.BuildInfo{Version: "1.2.3"},
		MinClientVersion: "0.1.0",
		Handlers: &apiv1.Handlers{
			Rules: apiv1.NewRules(arrango.NewRules(registry, dialer).Catalogue, deadline),
		},
		Kratos: kratosauthtest.Fake(t, map[string]kratosauth.Identity{asOwner: {ID: "owner"}}),
	})
	if err != nil {
		t.Fatalf("new server: %v", err)
	}
	return ruling{server: server, registry: registry}
}

func (r ruling) pair(t *testing.T, label string, node *ruleNode) {
	t.Helper()
	if _, err := r.registry.Record(t.Context(), label, serveNode(t, node)); err != nil {
		t.Fatalf("record: %v", err)
	}
}

// A synthetic slice of the solver's table and profiles, in the node's own types.
func sampleCatalogue() *arrangorpcv1.RuleCatalogue {
	return &arrangorpcv1.RuleCatalogue{
		Rules: []*arrangorpcv1.RuleDefinition{
			{Key: "gap_windows", DefaultWeight: 100000, DefaultParam: 0, ParamKind: "none", DefaultMode: arrangov1.RuleMode_RULE_MODE_HARD, Hardable: true, ByYear: true, ByDivision: true},
			{Key: "teacher_gaps", DefaultWeight: 45, DefaultParam: 0, ParamKind: "none", DefaultMode: arrangov1.RuleMode_RULE_MODE_SOFT, Hardable: true, ByTeacher: true},
			{Key: "anti_split_shift", DefaultWeight: 60, DefaultParam: 2, ParamKind: "edge_periods", DefaultMode: arrangov1.RuleMode_RULE_MODE_OFF, Hardable: true, ByTeacher: true},
			{Key: "stability", DefaultWeight: 15, DefaultParam: 0, ParamKind: "none", DefaultMode: arrangov1.RuleMode_RULE_MODE_SOFT},
		},
		Profiles: []*arrangorpcv1.RuleProfile{
			{Name: "default"},
			{Name: "dobry_plan", Overrides: []*arrangov1.RuleOverride{
				{Rule: "gap_windows", Mode: arrangov1.RuleMode_RULE_MODE_HARD},
				{Rule: "anti_split_shift", Mode: arrangov1.RuleMode_RULE_MODE_SOFT, Weight: 60, Param: 2},
			}},
			{Name: "gentle", Overrides: []*arrangov1.RuleOverride{
				{Rule: "teacher_gaps", Mode: arrangov1.RuleMode_RULE_MODE_DEFAULT, Weight: 5},
			}},
		},
	}
}

func strictCatalogue(t *testing.T, body []byte) gen.RuleCatalogue {
	t.Helper()
	var catalogue gen.RuleCatalogue
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&catalogue); err != nil {
		t.Fatalf("answer %q is not a RuleCatalogue: %v", body, err)
	}
	return catalogue
}

func TestTheRuleCatalogueAnswersInTheContractsWords(t *testing.T) {
	r := newRulesServer(t, apiv1.RulesDeadline)
	node := &ruleNode{capabilities: []string{"check", "rule_catalogue", "rule_metadata"}, catalogue: sampleCatalogue()}
	r.pair(t, "rules", node)

	recorder := call(t, r.server, http.MethodGet, rulesPath, asOwner, "")
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", recorder.Code, recorder.Body.String())
	}
	catalogue := strictCatalogue(t, recorder.Body.Bytes())

	wantRules := []gen.RuleDefinition{
		{Key: "gap_windows", DefaultWeight: 100000, DefaultParam: 0, ParamKind: gen.None, DefaultMode: gen.Hard, Hardable: true, ByYear: true, ByDivision: true},
		{Key: "teacher_gaps", DefaultWeight: 45, DefaultParam: 0, ParamKind: gen.None, DefaultMode: gen.Soft, Hardable: true, ByTeacher: true},
		{Key: "anti_split_shift", DefaultWeight: 60, DefaultParam: 2, ParamKind: gen.EdgePeriods, DefaultMode: gen.Off, Hardable: true, ByTeacher: true},
		{Key: "stability", DefaultWeight: 15, DefaultParam: 0, ParamKind: gen.None, DefaultMode: gen.Soft},
	}
	if len(catalogue.Rules) != len(wantRules) {
		t.Fatalf("rules = %+v, want %+v", catalogue.Rules, wantRules)
	}
	for i, want := range wantRules {
		if catalogue.Rules[i] != want {
			t.Errorf("rule %d = %+v, want %+v", i, catalogue.Rules[i], want)
		}
	}

	if len(catalogue.Profiles) != 3 {
		t.Fatalf("profiles = %+v, want default, dobry_plan and gentle", catalogue.Profiles)
	}
	if first := catalogue.Profiles[0]; first.Name != "default" || len(first.Overrides) != 0 {
		t.Errorf("first profile = %+v, want default with no overrides", first)
	}
	dobry := catalogue.Profiles[1].Overrides
	if len(dobry) != 2 ||
		dobry[0].Rule != "gap_windows" || dobry[0].Mode == nil || *dobry[0].Mode != gen.Hard ||
		dobry[0].Weight != nil || dobry[0].Param != nil ||
		dobry[1].Rule != "anti_split_shift" || dobry[1].Mode == nil || *dobry[1].Mode != gen.Soft ||
		dobry[1].Weight == nil || *dobry[1].Weight != 60 || dobry[1].Param == nil || *dobry[1].Param != 2 {
		t.Errorf("dobry_plan overrides = %+v", dobry)
	}
	gentle := catalogue.Profiles[2].Overrides
	if len(gentle) != 1 || gentle[0].Mode != nil || gentle[0].Weight == nil || *gentle[0].Weight != 5 {
		t.Errorf("gentle overrides = %+v, want a weight with the mode left as it is", gentle)
	}
	if bytes.Contains(recorder.Body.Bytes(), []byte(`"weight":0`)) || bytes.Contains(recorder.Body.Bytes(), []byte(`"param":0`)) {
		t.Errorf("a kept weight or param is written as 0: %s", recorder.Body.String())
	}
	if node.asked() != 1 {
		t.Errorf("the node was asked %d time(s), want once", node.asked())
	}
}

func TestTheRuleCatalogueNeedsASession(t *testing.T) {
	r := newRulesServer(t, apiv1.RulesDeadline)
	node := &ruleNode{capabilities: []string{"rule_catalogue", "rule_metadata"}, catalogue: sampleCatalogue()}
	r.pair(t, "rules", node)

	if recorder := call(t, r.server, http.MethodGet, rulesPath, "", ""); recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401: %s", recorder.Code, recorder.Body.String())
	}
	if node.asked() != 0 {
		t.Error("the node was asked for a caller with no session")
	}
}

func TestTheRuleCatalogueNeedsAPairedNode(t *testing.T) {
	r := newRulesServer(t, apiv1.RulesDeadline)
	recorder := call(t, r.server, http.MethodGet, rulesPath, asOwner, "")
	if recorder.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409: %s", recorder.Code, recorder.Body.String())
	}
	if body := envelope(t, recorder); body.Message != messages.KeySolveNodeUnavailable {
		t.Errorf("message = %q, want %q", body.Message, messages.KeySolveNodeUnavailable)
	}
}

func TestANodeThatCannotListItsRulesIsOutdated(t *testing.T) {
	r := newRulesServer(t, apiv1.RulesDeadline)
	node := &ruleNode{capabilities: []string{"solve", "check"}, catalogue: sampleCatalogue()}
	r.pair(t, "old", node)

	recorder := call(t, r.server, http.MethodGet, rulesPath, asOwner, "")
	if recorder.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409: %s", recorder.Code, recorder.Body.String())
	}
	body := envelope(t, recorder)
	if body.Message != messages.KeyNodeOutdated || body.Details == nil || (*body.Details)["Capability"] != "rule_catalogue" {
		t.Errorf("envelope = %+v, want %s naming rule_catalogue", body, messages.KeyNodeOutdated)
	}
	if node.asked() != 0 {
		t.Error("an outdated node was asked for its rules")
	}
}

func TestASlowNodeLeavesTheRulesWaitingForTheSolver(t *testing.T) {
	r := newRulesServer(t, 200*time.Millisecond)
	r.pair(t, "busy", &ruleNode{capabilities: []string{"rule_catalogue", "rule_metadata"}, hold: func(ctx context.Context) error {
		<-ctx.Done()
		return ctx.Err()
	}})

	recorder := call(t, r.server, http.MethodGet, rulesPath, asOwner, "")
	if recorder.Code != http.StatusGatewayTimeout {
		t.Fatalf("status = %d, want 504: %s", recorder.Code, recorder.Body.String())
	}
	if body := envelope(t, recorder); body.Message != messages.KeyRulesWaitingForSolver {
		t.Errorf("message = %q, want %q", body.Message, messages.KeyRulesWaitingForSolver)
	}
}

// The node promises a rule's default is a real mode; one that is not is a
// fault, never a guessed mode.
func TestARuleWithNoDefaultModeIsAnInternalError(t *testing.T) {
	r := newRulesServer(t, apiv1.RulesDeadline)
	catalogue := sampleCatalogue()
	catalogue.Rules[1].DefaultMode = arrangov1.RuleMode_RULE_MODE_DEFAULT
	r.pair(t, "broken", &ruleNode{capabilities: []string{"rule_catalogue", "rule_metadata"}, catalogue: catalogue})

	recorder := call(t, r.server, http.MethodGet, rulesPath, asOwner, "")
	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500: %s", recorder.Code, recorder.Body.String())
	}
	if body := envelope(t, recorder); body.Message != messages.KeyErrorInternal {
		t.Errorf("message = %q, want %q", body.Message, messages.KeyErrorInternal)
	}
}

// Malformed catalogue metadata must not reach the editor as usable defaults.
func TestRuleCatalogueRejectsInvalidDefaults(t *testing.T) {
	for _, one := range []struct {
		name   string
		mutate func(*arrangorpcv1.RuleDefinition)
	}{
		{"unknown kind", func(rule *arrangorpcv1.RuleDefinition) { rule.ParamKind = "unknown" }},
		{"zero weight", func(rule *arrangorpcv1.RuleDefinition) { rule.DefaultWeight = 0 }},
		{"unused parameter", func(rule *arrangorpcv1.RuleDefinition) { rule.DefaultParam = 2 }},
	} {
		t.Run(one.name, func(t *testing.T) {
			r := newRulesServer(t, apiv1.RulesDeadline)
			catalogue := sampleCatalogue()
			one.mutate(catalogue.Rules[0])
			r.pair(t, "malformed", &ruleNode{capabilities: []string{"rule_catalogue", "rule_metadata"}, catalogue: catalogue})
			response := call(t, r.server, http.MethodGet, rulesPath, asOwner, "")
			if response.Code != http.StatusInternalServerError {
				t.Fatalf("catalogue status = %d, body %s", response.Code, response.Body.String())
			}
		})
	}
}

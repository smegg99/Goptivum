// arrango/profile_test.go

package arrango_test

import (
	"testing"

	"google.golang.org/protobuf/proto"

	arrangorpcv1 "github.com/smegg99/arrango/backend/gen/arrangorpcv1"
	arrangov1 "github.com/smegg99/arrango/backend/gen/arrangov1"

	"github.com/smegg99/goptivum/backend/arrango"
	"github.com/smegg99/goptivum/solveclient"
)

// plansOwnRules is what a plan's document says: a built-in and a scoped
// override a request's rules alone could not reset.
func plansOwnRules() *arrangov1.RuleConfig {
	return &arrangov1.RuleConfig{Profile: "dobry_plan", Overrides: []*arrangov1.RuleOverride{
		{Rule: "teacher_gaps", Mode: arrangov1.RuleMode_RULE_MODE_HARD, TeacherId: 7},
	}}
}

func schoolProfile() *solveclient.Profile {
	return &solveclient.Profile{Name: "Nasza szkoła", ID: 77, Rules: &arrangov1.RuleConfig{Overrides: []*arrangov1.RuleOverride{
		{Rule: "teacher_gaps", Mode: arrangov1.RuleMode_RULE_MODE_OFF},
	}}}
}

// A solve with a profile sends the model with no rules of its own and the
// profile as the request's rules, so the profile replaces the plan's rules;
// the job and its result record the profile and the rules sent.
func TestASolveWithAProfileSendsItsRulesInPlaceOfThePlans(t *testing.T) {
	node := startFakeNode(t, &fakeNode{frames: []*arrangorpcv1.SolveUpdate{
		doneFrame(arrangorpcv1.SolveStatus_SOLVE_STATUS_FEASIBLE, placed(1, 2)),
	}})
	runner, registry, jobs, repository, store := newRunner(t)
	ctx := t.Context()
	recorded, err := registry.Record(ctx, "solving", node.paired())
	if err != nil {
		t.Fatalf("record: %v", err)
	}
	plan := newPlan(t, repository, store)

	model := lessons(2, 2)
	model.RuleConfig = plansOwnRules()
	solve := order(plan.ID, recorded.ID, model)
	solve.Profile = schoolProfile()
	job, err := runner.Start(ctx, solve)
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	waitForTerminal(t, jobs, job.ID)

	sent := node.solveRequests()
	if len(sent) != 1 {
		t.Fatalf("the node was asked %d solves, want 1", len(sent))
	}
	if rules := sent[0].GetModel().GetRuleConfig(); rules != nil {
		t.Errorf("the model carried the plan's rules %v, want none", rules)
	}
	if !proto.Equal(sent[0].GetRuleConfig(), schoolProfile().Rules) {
		t.Errorf("the request's rules = %v, want the profile's %v", sent[0].GetRuleConfig(), schoolProfile().Rules)
	}

	closed, err := jobs.Get(ctx, job.ID)
	if err != nil || closed.ResultID == nil {
		t.Fatalf("job = %+v, %v, want one with a result", closed, err)
	}
	if got := closed.Profile; got == nil || got.Name != "Nasza szkoła" || got.ID != 77 || !proto.Equal(got.Rules, schoolProfile().Rules) {
		t.Errorf("the job records %+v", got)
	}

	kept, err := repository.Result(ctx, plan.ID, *closed.ResultID)
	if err != nil {
		t.Fatalf("result: %v", err)
	}
	if kept.RuleProfile == nil || kept.RuleProfile.Name != "Nasza szkoła" || kept.RuleProfile.ID != 77 {
		t.Fatalf("the result records %+v", kept.RuleProfile)
	}
	rules, err := arrango.DecodeRules(kept.RuleProfile.Rules)
	if err != nil || !proto.Equal(rules, schoolProfile().Rules) {
		t.Errorf("the result's rules = %v, %v, want the profile's", rules, err)
	}
}

// Without a profile a solve judges by the plan's own rules, as it always did,
// and nothing records a profile.
func TestASolveWithoutAProfileUsesThePlansRules(t *testing.T) {
	node := startFakeNode(t, &fakeNode{frames: []*arrangorpcv1.SolveUpdate{
		doneFrame(arrangorpcv1.SolveStatus_SOLVE_STATUS_FEASIBLE, placed(1, 2)),
	}})
	runner, registry, jobs, repository, store := newRunner(t)
	ctx := t.Context()
	recorded, err := registry.Record(ctx, "solving", node.paired())
	if err != nil {
		t.Fatalf("record: %v", err)
	}
	plan := newPlan(t, repository, store)

	model := lessons(2, 2)
	model.RuleConfig = plansOwnRules()
	job, err := runner.Start(ctx, order(plan.ID, recorded.ID, model))
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	waitForTerminal(t, jobs, job.ID)

	sent := node.solveRequests()
	if len(sent) != 1 || !proto.Equal(sent[0].GetModel().GetRuleConfig(), plansOwnRules()) || sent[0].GetRuleConfig() != nil {
		t.Fatalf("sent %v, want the plan's rules in the model and none in the request", sent)
	}
	closed, err := jobs.Get(ctx, job.ID)
	if err != nil || closed.Profile != nil || closed.ResultID == nil {
		t.Fatalf("job = %+v, %v, want no profile and a result", closed, err)
	}
	kept, err := repository.Result(ctx, plan.ID, *closed.ResultID)
	if err != nil || kept.RuleProfile != nil {
		t.Errorf("result = %+v, %v, want no profile", kept, err)
	}
}

// The node's Validate takes no request rules, so an improve's baseline is
// judged by a copy of the model that carries the profile as its own rules;
// the model the solve sends still carries none.
func TestAnImproveWithAProfileIsCheckedByTheProfilesRules(t *testing.T) {
	node := startFakeNode(t, &fakeNode{})
	registry, jobs, repository, store := newRegistry(t)
	ctx := t.Context()
	recorded, err := registry.Record(ctx, "solving", node.paired())
	if err != nil {
		t.Fatalf("record: %v", err)
	}
	dispatcher := newDispatcher(t, registry, jobs)
	plan := newPlan(t, repository, store)

	builtin := &solveclient.Profile{Name: "relaxed", Rules: &arrangov1.RuleConfig{Profile: "relaxed"}}
	model := lessons(2, 2)
	model.RuleConfig = plansOwnRules()
	improve := arrango.Order{
		PlanID: plan.ID, Version: 1, NodeID: recorded.ID, StartedBy: owner, Profile: builtin,
		Request: &arrangorpcv1.SolveRequest{Model: model, Config: &arrangorpcv1.SolverConfig{}},
	}
	if _, _, err := dispatcher.Begin(ctx, improve); err != nil {
		t.Fatalf("begin: %v", err)
	}
	validated := node.validateCalls()
	if len(validated) != 1 {
		t.Fatalf("validated %d times, want once", len(validated))
	}
	if !proto.Equal(validated[0].GetModel().GetRuleConfig(), builtin.Rules) {
		t.Errorf("the baseline was judged by %v, want the profile %v", validated[0].GetModel().GetRuleConfig(), builtin.Rules)
	}
	if improve.Request.GetModel().GetRuleConfig() != nil || !proto.Equal(improve.Request.GetRuleConfig(), builtin.Rules) {
		t.Errorf("the solve would send model rules %v and request rules %v",
			improve.Request.GetModel().GetRuleConfig(), improve.Request.GetRuleConfig())
	}
}

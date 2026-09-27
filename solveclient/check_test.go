// solveclient/check_test.go

package solveclient_test

import (
	"errors"
	"slices"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	arrangorpcv1 "github.com/smegg99/arrango/backend/gen/arrangorpcv1"
	arrangov1 "github.com/smegg99/arrango/backend/gen/arrangov1"

	"github.com/smegg99/goptivum/solveclient"
)

// A check reaches a node that can run it and carries its report back.
func TestCheckAsksANodeThatCan(t *testing.T) {
	node := startFakeNode(t, &fakeNode{report: &arrangorpcv1.CheckReport{
		Hard: []*arrangov1.Conflict{{Kind: arrangov1.Conflict_KIND_MISSING_LESSON}},
	}})
	dialer, _ := newDialerAs(t, "0.1.0")
	report, err := node.reach(t, dialer).Check(t.Context(), &arrangorpcv1.CheckRequest{Model: &arrangov1.SchoolModel{}})
	if err != nil {
		t.Fatalf("check: %v", err)
	}
	if len(report.GetHard()) != 1 || node.checkCalls() != 1 {
		t.Errorf("report = %v after %d calls", report, node.checkCalls())
	}
}

// A node whose build lacks the RPC is outdated, named, and never asked.
func TestCheckRefusesANodeWithoutTheCapability(t *testing.T) {
	node := startFakeNode(t, &fakeNode{capabilities: []string{"solve"}})
	dialer, _ := newDialerAs(t, "0.1.0")
	conn := node.reach(t, dialer)

	for _, call := range []struct {
		name       string
		capability string
		run        func() error
	}{
		{"check", solveclient.CapabilityCheck, func() error {
			_, err := conn.Check(t.Context(), &arrangorpcv1.CheckRequest{Model: &arrangov1.SchoolModel{}})
			return err
		}},
		{"rule catalogue", solveclient.CapabilityRuleCatalogue, func() error {
			_, err := conn.RuleCatalogue(t.Context())
			return err
		}},
	} {
		t.Run(call.name, func(t *testing.T) {
			var outdated *solveclient.ErrOutdated
			if err := call.run(); !errors.As(err, &outdated) {
				t.Fatalf("err = %v, want ErrOutdated", err)
			}
			if outdated.Capability != call.capability || outdated.Node != "fake" {
				t.Errorf("outdated = %+v", outdated)
			}
			if outdated.Key() != solveclient.KeyNodeOutdated || outdated.Data()["Capability"] != call.capability {
				t.Errorf("keyed as %s %v", outdated.Key(), outdated.Data())
			}
		})
	}
	if node.checkCalls() != 0 {
		t.Errorf("an outdated node was asked %d checks", node.checkCalls())
	}
}

// A node with no room says so apart from one that failed, and one that is
// going away reads as unreachable.
func TestCheckTellsARefusalFromAFailure(t *testing.T) {
	for _, one := range []struct {
		name string
		err  error
		want error
	}{
		{"no room", status.Error(codes.ResourceExhausted, "busy"), solveclient.ErrRefused},
		{"going away", status.Error(codes.Unavailable, "restart"), solveclient.ErrUnreachable},
	} {
		t.Run(one.name, func(t *testing.T) {
			node := startFakeNode(t, &fakeNode{checkErr: one.err})
			dialer, _ := newDialerAs(t, "0.1.0")
			_, err := node.reach(t, dialer).Check(t.Context(), &arrangorpcv1.CheckRequest{Model: &arrangov1.SchoolModel{}})
			if !errors.Is(err, one.want) {
				t.Fatalf("err = %v, want %v", err, one.want)
			}
		})
	}
}

func TestRuleCatalogueComesFromTheNode(t *testing.T) {
	node := startFakeNode(t, &fakeNode{})
	dialer, _ := newDialerAs(t, "0.1.0")
	catalogue, err := node.reach(t, dialer).RuleCatalogue(t.Context())
	if err != nil {
		t.Fatalf("catalogue: %v", err)
	}
	if len(catalogue.GetProfiles()) != 1 || catalogue.GetProfiles()[0].GetName() != "default" {
		t.Errorf("catalogue = %v", catalogue)
	}
}

// Every refusal a person reads carries a key the importer's catalogue words.
func TestRefusalsCarryTheirKeys(t *testing.T) {
	for _, refusal := range []solveclient.Refusal{
		&solveclient.ErrOutdated{Node: "n", Capability: "check"},
		&solveclient.ErrModelInvalid{Conflicts: []*arrangov1.Conflict{{}, {}}},
		&solveclient.ErrIncompleteBaseline{Lessons: 10, Unplaced: 3},
	} {
		if !slices.Contains(solveclient.RefusalKeys(), refusal.Key()) {
			t.Errorf("%T carries %q, which RefusalKeys does not list", refusal, refusal.Key())
		}
	}
	invalid := &solveclient.ErrModelInvalid{Conflicts: []*arrangov1.Conflict{
		{Kind: arrangov1.Conflict_KIND_MISSING_LESSON}, {Kind: arrangov1.Conflict_KIND_MISSING_LESSON},
	}}
	if invalid.Data()["Count"] != 2 || len(invalid.Kinds()) != 1 {
		t.Errorf("data %v kinds %v, want 2 conflicts of one kind", invalid.Data(), invalid.Kinds())
	}
	baseline := &solveclient.ErrIncompleteBaseline{Lessons: 10, Unplaced: 3}
	if baseline.Data()["Unplaced"] != 3 || baseline.Data()["Lessons"] != 10 {
		t.Errorf("data = %v", baseline.Data())
	}
}

// A profile replaces the model's rules for one request, never adds to them.
func TestAProfileReplacesTheModelsRules(t *testing.T) {
	request := &arrangorpcv1.SolveRequest{Model: &arrangov1.SchoolModel{
		RuleConfig: &arrangov1.RuleConfig{Profile: "relaxed"},
	}}
	profile := &solveclient.Profile{Name: "default", Rules: &arrangov1.RuleConfig{Profile: "default"}}
	profile.Apply(request)
	if request.GetModel().GetRuleConfig() != nil {
		t.Errorf("the model kept its rules: %v", request.GetModel().GetRuleConfig())
	}
	if request.GetRuleConfig().GetProfile() != "default" {
		t.Errorf("request rules = %v", request.GetRuleConfig())
	}
}

// A catalogue without parameter metadata cannot drive the current editor.
func TestRuleCatalogueRefusesMissingMetadata(t *testing.T) {
	node := startFakeNode(t, &fakeNode{capabilities: []string{"rule_catalogue"}})
	dialer, _ := newDialerAs(t, "0.1.0")
	_, err := node.reach(t, dialer).RuleCatalogue(t.Context())
	var outdated *solveclient.ErrOutdated
	if !errors.As(err, &outdated) || outdated.Capability != solveclient.CapabilityRuleMetadata {
		t.Fatalf("catalogue = %v, want missing rule_metadata", err)
	}
}

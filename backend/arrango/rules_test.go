// arrango/rules_test.go

package arrango_test

import (
	"errors"
	"testing"

	"google.golang.org/protobuf/proto"

	arrangorpcv1 "github.com/smegg99/arrango/backend/gen/arrangorpcv1"
	arrangov1 "github.com/smegg99/arrango/backend/gen/arrangov1"

	"github.com/smegg99/goptivum/backend/arrango"
	"github.com/smegg99/goptivum/backend/common/messages"
	"github.com/smegg99/goptivum/solveclient"
)

func newRules(t *testing.T) (*arrango.Rules, *arrango.Registry) {
	t.Helper()
	registry, _, _, _ := newRegistry(t)
	self, err := solveclient.LoadIdentity(t.TempDir())
	if err != nil {
		t.Fatalf("identity: %v", err)
	}
	dialer, err := solveclient.NewDialer(self, "0.1.0")
	if err != nil {
		t.Fatalf("dialer: %v", err)
	}
	return arrango.NewRules(registry, dialer), registry
}

func TestTheRuleCatalogueComesFromTheFirstActiveNode(t *testing.T) {
	rules, registry := newRules(t)
	ctx := t.Context()

	if _, err := rules.Catalogue(ctx); !errors.Is(err, arrango.ErrNoNode) {
		t.Fatalf("with no node = %v, want ErrNoNode", err)
	}

	want := &arrangorpcv1.RuleCatalogue{
		Rules: []*arrangorpcv1.RuleDefinition{{
			Key: "teacher_gaps", DefaultMode: arrangov1.RuleMode_RULE_MODE_SOFT, Hardable: true, ByTeacher: true,
		}},
		Profiles: []*arrangorpcv1.RuleProfile{{Name: "default"}},
	}
	node := startFakeNode(t, &fakeNode{catalogue: want})
	if _, err := registry.Record(ctx, "rules", node.paired()); err != nil {
		t.Fatalf("record: %v", err)
	}
	// Paired later, so never asked.
	if _, err := registry.Record(ctx, "later", paired(t, "127.0.0.1:1")); err != nil {
		t.Fatalf("record: %v", err)
	}

	got, err := rules.Catalogue(ctx)
	if err != nil {
		t.Fatalf("catalogue: %v", err)
	}
	if !proto.Equal(got, want) {
		t.Errorf("catalogue = %v, want the node's %v", got, want)
	}
	if calls := node.catalogueCalls(); calls != 1 {
		t.Errorf("the node was asked %d time(s), want once", calls)
	}
}

// A node built before the catalogue RPC answers everything else, so it is
// refused by name before the call rather than failing inside it.
func TestTheRuleCatalogueRefusesANodeWithoutTheCapability(t *testing.T) {
	rules, registry := newRules(t)
	ctx := t.Context()
	node := startFakeNode(t, &fakeNode{capabilities: []string{"solve", "validate", "check"}})
	if _, err := registry.Record(ctx, "old", node.paired()); err != nil {
		t.Fatalf("record: %v", err)
	}

	_, err := rules.Catalogue(ctx)
	var outdated *solveclient.ErrOutdated
	if !errors.As(err, &outdated) || outdated.Capability != "rule_catalogue" {
		t.Fatalf("err = %v, want ErrOutdated naming rule_catalogue", err)
	}
	var fault messages.Keyed
	if !errors.As(err, &fault) || fault.Key() != messages.KeyNodeOutdated ||
		fault.Data()["Capability"] != "rule_catalogue" {
		t.Errorf("fault = %v, want %s with the capability", fault, messages.KeyNodeOutdated)
	}
	if calls := node.catalogueCalls(); calls != 0 {
		t.Errorf("the node was asked for its rules %d time(s)", calls)
	}
}

func TestTheRuleCatalogueSaysWhenTheNodeDoesNotAnswer(t *testing.T) {
	rules, registry := newRules(t)
	ctx := t.Context()
	// Nothing listens on port 1.
	if _, err := registry.Record(ctx, "gone", paired(t, "127.0.0.1:1")); err != nil {
		t.Fatalf("record: %v", err)
	}
	if _, err := rules.Catalogue(ctx); !errors.Is(err, solveclient.ErrUnreachable) {
		t.Fatalf("err = %v, want ErrUnreachable", err)
	}
}

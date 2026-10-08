// arrango/demo_test.go

package arrango_test

import (
	"errors"
	"testing"

	arrangorpcv1 "github.com/smegg99/arrango/backend/gen/arrangorpcv1"
	arrangov1 "github.com/smegg99/arrango/backend/gen/arrangov1"

	"github.com/smegg99/goptivum/backend/arrango"
	"github.com/smegg99/goptivum/solveclient"
)

func TestDemoSchoolAsksTheFirstActiveNodeForTheProductionSchool(t *testing.T) {
	registry, _, _, _ := newRegistry(t)
	ctx := t.Context()
	self, err := solveclient.LoadIdentity(t.TempDir())
	if err != nil {
		t.Fatalf("identity: %v", err)
	}
	dialer, err := solveclient.NewDialer(self, "0.1.0")
	if err != nil {
		t.Fatalf("dialer: %v", err)
	}

	if _, err := arrango.DemoSchool(ctx, registry, dialer); !errors.Is(err, arrango.ErrNoNode) {
		t.Fatalf("with no node = %v, want ErrNoNode", err)
	}

	// Paired first and revoked: skipped.
	revoked, err := registry.Record(ctx, "revoked", paired(t, "127.0.0.1:1"))
	if err != nil {
		t.Fatalf("record: %v", err)
	}
	if err := registry.Revoke(ctx, revoked.ID); err != nil {
		t.Fatalf("revoke: %v", err)
	}
	if _, err := arrango.DemoSchool(ctx, registry, dialer); !errors.Is(err, arrango.ErrNoNode) {
		t.Fatalf("with only a revoked node = %v, want ErrNoNode", err)
	}

	node := startFakeNode(t, &fakeNode{demo: &arrangov1.SchoolModel{Name: "Demo"}})
	if _, err := registry.Record(ctx, "demo", node.paired()); err != nil {
		t.Fatalf("record: %v", err)
	}
	// Paired later, so never asked.
	if _, err := registry.Record(ctx, "later", paired(t, "127.0.0.1:1")); err != nil {
		t.Fatalf("record: %v", err)
	}

	model, err := arrango.DemoSchool(ctx, registry, dialer)
	if err != nil {
		t.Fatalf("demo school: %v", err)
	}
	if model.GetName() != "Demo" {
		t.Errorf("model = %q, want the node's", model.GetName())
	}
	asked := node.demoCalls()
	if len(asked) != 1 || asked[0].GetPreset() != arrangorpcv1.DemoPreset_DEMO_PRESET_PRODUCTION ||
		asked[0].GetSeed() != 0 {
		t.Errorf("the node was asked %+v, want one production request with seed 0", asked)
	}
}

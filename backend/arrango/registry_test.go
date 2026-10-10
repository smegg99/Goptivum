// arrango/registry_test.go

package arrango_test

import (
	"errors"
	"testing"

	"github.com/smegg99/goptivum/backend/arrango"
	"github.com/smegg99/goptivum/solveclient"
)

func withCeilings(p solveclient.Paired, solves, workers int) solveclient.Paired {
	p.Ceilings = &solveclient.Ceilings{Solves: solves, Workers: workers}
	return p
}

// A new node starts inside what its operator gave this server: as many solves
// at once as the node allows, and the conservative worker default below the
// node's worker ceiling.
func TestANewNodeStartsInsideTheNodesCeilings(t *testing.T) {
	registry, _, _, _ := newRegistry(t)
	ctx := t.Context()

	for _, one := range []struct {
		name   string
		paired solveclient.Paired
		want   arrango.Limits
	}{
		{"paired with --max-solves 2", withCeilings(paired(t, "127.0.0.1:1"), 2, 12), arrango.Limits{Solves: 2, Workers: int(solveclient.DefaultWorkers)}},
		{"a node with no limit of its own", withCeilings(paired(t, "127.0.0.1:2"), 0, 16), arrango.Limits{Solves: 1, Workers: int(solveclient.DefaultWorkers)}},
		{"a small worker ceiling", withCeilings(paired(t, "127.0.0.1:3"), 1, 2), arrango.Limits{Solves: 1, Workers: 2}},
		{"a node that reports no ceilings", paired(t, "127.0.0.1:4"), arrango.Limits{Solves: 1, Workers: int(solveclient.DefaultWorkers)}},
	} {
		t.Run(one.name, func(t *testing.T) {
			node, err := registry.Record(ctx, "node", one.paired)
			if err != nil {
				t.Fatalf("record: %v", err)
			}
			if node.Limits != one.want {
				t.Errorf("limits = %+v, want %+v", node.Limits, one.want)
			}
			stored, err := registry.Get(ctx, node.ID)
			if err != nil || stored.Limits != one.want {
				t.Errorf("stored limits = %+v, %v, want %+v", stored.Limits, err, one.want)
			}
		})
	}
}

// Pairing a revoked node's fingerprint again keeps its id, so its jobs and
// history stay its own, and keeps its name and limits when nothing new is
// given, lowering a limit the node's new ceiling no longer allows.
func TestRePairingARevokedNodeKeepsItsIDNameAndLimits(t *testing.T) {
	registry, _, _, _ := newRegistry(t)
	ctx := t.Context()

	first := withCeilings(paired(t, "127.0.0.1:19443"), 3, 12)
	node, err := registry.Record(ctx, "node", first)
	if err != nil {
		t.Fatalf("record: %v", err)
	}
	name, limits := "Serwer szkolny", arrango.Limits{Solves: 3, Workers: 8}
	if _, err := registry.Update(ctx, node.ID, arrango.NodeChange{Label: &name, Solves: &limits.Solves, Workers: &limits.Workers}); err != nil {
		t.Fatalf("update: %v", err)
	}
	if err := registry.Revoke(ctx, node.ID); err != nil {
		t.Fatalf("revoke: %v", err)
	}

	again, err := registry.Record(ctx, "", withCeilings(solveclient.Paired{Address: "10.0.0.5:9443", Fingerprint: first.Fingerprint}, 2, 12))
	if err != nil {
		t.Fatalf("record again: %v", err)
	}
	if again.ID != node.ID {
		t.Errorf("id = %d, want the node's own %d", again.ID, node.ID)
	}
	if again.Revoked() || again.Label != "Serwer szkolny" || again.Address != "10.0.0.5:9443" {
		t.Errorf("node = %+v, want it trusted again at the new address under its own name", again)
	}
	if want := (arrango.Limits{Solves: 2, Workers: 8}); again.Limits != want {
		t.Errorf("limits = %+v, want %+v: lowered to the new ceiling, never raised", again.Limits, want)
	}
	stored, err := registry.Get(ctx, node.ID)
	if err != nil || stored.Label != again.Label || stored.Limits != again.Limits || stored.Revoked() {
		t.Errorf("stored = %+v, %v, want what Record answered", stored, err)
	}
}

func TestUpdateRenamesAndSetsLimits(t *testing.T) {
	registry, _, _, _ := newRegistry(t)
	ctx := t.Context()
	node, err := registry.Record(ctx, "node", paired(t, "127.0.0.1:1"))
	if err != nil {
		t.Fatalf("record: %v", err)
	}

	name := "Serwer szkolny"
	renamed, err := registry.Update(ctx, node.ID, arrango.NodeChange{Label: &name})
	if err != nil {
		t.Fatalf("rename: %v", err)
	}
	if renamed.Label != name || renamed.Limits != node.Limits {
		t.Errorf("renamed = %+v, want the name changed and the limits kept", renamed)
	}

	limits := arrango.Limits{Solves: 2, Workers: 6}
	limited, err := registry.Update(ctx, node.ID, arrango.NodeChange{Solves: &limits.Solves, Workers: &limits.Workers})
	if err != nil {
		t.Fatalf("set limits: %v", err)
	}
	if limited.Label != name || limited.Limits != limits {
		t.Errorf("limited = %+v, want the name kept and the limits set", limited)
	}

	if _, err := registry.Update(ctx, 999, arrango.NodeChange{Label: &name}); !errors.Is(err, arrango.ErrNoNode) {
		t.Errorf("updating a node that does not exist = %v, want ErrNoNode", err)
	}
}

// A limit is checked against the node's ceilings, and the refusal names the
// ceiling it went over.
func TestALimitAboveTheNodesCeilingIsRefused(t *testing.T) {
	ceilings := solveclient.Ceilings{Solves: 2, Workers: 12}
	if err := (arrango.Limits{Solves: 2, Workers: 12}).AllowedBy(ceilings); err != nil {
		t.Errorf("limits at the ceilings: %v", err)
	}

	var above *arrango.ErrAboveCeiling
	err := (arrango.Limits{Solves: 3, Workers: 4}).AllowedBy(ceilings)
	if !errors.As(err, &above) || above.Workers || above.Ceiling != 2 {
		t.Errorf("3 solves at once: err = %v, want above the ceiling of 2 solves", err)
	}
	err = (arrango.Limits{Solves: 1, Workers: 16}).AllowedBy(ceilings)
	if !errors.As(err, &above) || !above.Workers || above.Ceiling != 12 {
		t.Errorf("16 workers: err = %v, want above the ceiling of 12 workers", err)
	}

	// No limit of the node's own on solves at once.
	if err := (arrango.Limits{Solves: 64, Workers: 8}).AllowedBy(solveclient.Ceilings{Solves: 0, Workers: 8}); err != nil {
		t.Errorf("a node with no solves ceiling: %v", err)
	}
}

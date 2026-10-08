// arrango/probe_test.go

package arrango_test

import (
	"errors"
	"testing"
	"time"

	"github.com/smegg99/goptivum/solveclient"

	"github.com/smegg99/goptivum/backend/arrango"
)

// A recorded node is probed by what the registry stored for it: its versions,
// its host and the ceilings its operator gave this server.
func TestProbeReadsARecordedNode(t *testing.T) {
	fake := startFakeNode(t, &fakeNode{status: policyStatus(2, 12)})
	registry, _, _, _ := newRegistry(t)
	recorded, err := registry.Record(t.Context(), "szkolny", fake.paired())
	if err != nil {
		t.Fatalf("record: %v", err)
	}

	probe, err := arrango.Probe(t.Context(), newTestDialer(t), recorded)
	if err != nil {
		t.Fatalf("probe: %v", err)
	}
	if probe.NodeAPI != "1.0.0" || probe.Capacity.Cores != 8 {
		t.Errorf("probe = %+v", probe)
	}
	if probe.Capacity.Ceilings == nil || *probe.Capacity.Ceilings != (solveclient.Ceilings{Solves: 2, Workers: 12, Seconds: 900}) {
		t.Errorf("ceilings = %+v, want 2 solves and 12 workers", probe.Capacity.Ceilings)
	}
}

// A silent node answers within admission's bound, as unreachable.
func TestProbeOfASilentNodeIsUnreachableInSeconds(t *testing.T) {
	registry, _, _, _ := newRegistry(t)
	recorded, err := registry.Record(t.Context(), "silent", paired(t, "127.0.0.1:1"))
	if err != nil {
		t.Fatalf("record: %v", err)
	}

	started := time.Now()
	_, err = arrango.Probe(t.Context(), newTestDialer(t), recorded)
	if !errors.Is(err, solveclient.ErrUnreachable) {
		t.Fatalf("err = %v, want ErrUnreachable", err)
	}
	if took := time.Since(started); took > 5*time.Second {
		t.Errorf("a silent node took %s to report", took)
	}
}

// solveclient/status_test.go

package solveclient_test

import (
	"context"
	"errors"
	nodecapacity "github.com/smegg99/arrango/backend/capacity"
	"testing"
	"time"

	"github.com/smegg99/arrango/backend/config"
	arrangoid "github.com/smegg99/arrango/backend/identity"

	arrangorpcv1 "github.com/smegg99/arrango/backend/gen/arrangorpcv1"

	"github.com/smegg99/goptivum/solveclient"
)

const gb = uint64(1) << 30

// pairedWith pairs a new caller with the live node under a policy.
func pairedWith(t *testing.T, node *liveNode, policy config.Policy) (*solveclient.Dialer, *solveclient.Identity, solveclient.Paired) {
	t.Helper()
	dialer, self := newDialerAs(t, "0.1.0")
	paired, err := pairWithin(t, dialer, node.link(t, policy, time.Minute))
	if err != nil {
		t.Fatalf("pair: %v", err)
	}
	return dialer, self, paired
}

// probe reaches a paired node by what the caller stored and asks it about itself.
func probe(t *testing.T, dialer *solveclient.Dialer, label, address, fingerprint string) (solveclient.Probe, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	conn, err := dialer.Reach(ctx, label, address, fingerprint)
	if err != nil {
		return solveclient.Probe{}, err
	}
	defer conn.Close()
	return conn.Probe(ctx)
}

// The status is read live: the node's versions, its host and the ceilings its
// operator gave the caller.
func TestProbeReadsTheNodeNow(t *testing.T) {
	node := startLiveNode(t)
	dialer, _, paired := pairedWith(t, node, config.Policy{MaxConcurrentSolves: 2, WorkerCeiling: 12})

	answer, err := probe(t, dialer, "szkolny", paired.Address, paired.Fingerprint.String())
	if err != nil {
		t.Fatalf("probe: %v", err)
	}
	if answer.NodeAPI != "1.0.0" || answer.Solver != "9.9.9" || answer.MinClient != "0.1.0" {
		t.Errorf("versions = %+v", answer)
	}
	if !answer.Can(solveclient.CapabilityPeerPolicy) || answer.Can(solveclient.CapabilityCheck) {
		t.Errorf("capabilities = %v, want the node's own list", answer.Capabilities)
	}
	if answer.Capacity.Cores != 8 || answer.Capacity.FreeBytes != 16<<30 {
		t.Errorf("capacity = %+v", answer.Capacity)
	}
	if answer.Capacity.Ceilings == nil || *answer.Capacity.Ceilings != (solveclient.Ceilings{Solves: 2, Workers: 12}) {
		t.Errorf("ceilings = %+v, want 2 solves and 12 workers", answer.Capacity.Ceilings)
	}
}

// TLS rejects a revoked certificate before gRPC can return an authentication
// status. Report an unavailable link without guessing from transport prose.

func TestProbeOfARevokedTLSConnectionIsUnreachable(t *testing.T) {
	node := startLiveNode(t)
	dialer, self, paired := pairedWith(t, node, config.DefaultPolicy())
	if err := node.peers.Revoke(self.Fingerprint()); err != nil {
		t.Fatalf("revoke on the node: %v", err)
	}

	_, err := probe(t, dialer, "szkolny", paired.Address, paired.Fingerprint.String())
	if !errors.Is(err, solveclient.ErrUnreachable) {
		t.Fatalf("err = %v, want ErrUnreachable", err)
	}
	if errors.Is(err, solveclient.ErrNotPaired) {
		t.Error("transport failure guessed an authentication status")
	}
}

func TestProbeOfASilentNodeIsUnreachable(t *testing.T) {
	other, err := arrangoid.Create(t.TempDir())
	if err != nil {
		t.Fatalf("identity: %v", err)
	}
	dialer, _ := newDialerAs(t, "0.1.0")

	_, err = probe(t, dialer, "silent", closedAddress(t), other.Fingerprint.String())
	if !errors.Is(err, solveclient.ErrUnreachable) {
		t.Fatalf("err = %v, want ErrUnreachable", err)
	}
	if errors.Is(err, solveclient.ErrNotPaired) || errors.Is(err, solveclient.ErrPinMismatch) {
		t.Errorf("err = %v, want only unreachable", err)
	}
}

// Another certificate at the node's address is named as such, and is still
// unreachable for a solve.
func TestReachingAnotherCertificateIsAMismatch(t *testing.T) {
	node := startLiveNode(t)
	other, err := arrangoid.Create(t.TempDir())
	if err != nil {
		t.Fatalf("identity: %v", err)
	}
	dialer, _ := newDialerAs(t, "0.1.0")

	_, err = probe(t, dialer, "moved", node.server.Address(), other.Fingerprint.String())
	if !errors.Is(err, solveclient.ErrPinMismatch) || !errors.Is(err, solveclient.ErrUnreachable) {
		t.Fatalf("err = %v, want ErrPinMismatch within ErrUnreachable", err)
	}
}

// A stored fingerprint that does not parse is a broken record, not a node that
// is down, and nothing is dialled.
func TestReachingAnUnreadableFingerprintDialsNothing(t *testing.T) {
	dialer, _ := newDialerAs(t, "0.1.0")
	_, err := dialer.Reach(t.Context(), "broken", closedAddress(t), "not-a-fingerprint")
	if err == nil || errors.Is(err, solveclient.ErrUnreachable) {
		t.Fatalf("err = %v, want an unreadable fingerprint", err)
	}
}

// A node whose build does not report its policy is reachable, with no ceilings.
func TestProbeOfANodeWithoutPeerPolicyHasNoCeilings(t *testing.T) {
	fake := startFakeNode(t, &fakeNode{})
	dialer, _ := newDialerAs(t, "0.1.0")

	answer, err := probe(t, dialer, "older", fake.address, fake.pin.String())
	if err != nil {
		t.Fatalf("probe: %v", err)
	}
	if answer.Capacity.Ceilings != nil {
		t.Errorf("ceilings = %+v, want none", answer.Capacity.Ceilings)
	}
	if answer.Capacity.Cores != 8 {
		t.Errorf("cores = %d", answer.Capacity.Cores)
	}
}

// Free memory is available minus committed, and a node that has committed more
// than it has available reports zero rather than an enormous number: unsigned
// subtraction below zero wraps, and the wrapped value would admit a solve onto
// a host with nothing left.
func TestCapacityOfNeverUnderflows(t *testing.T) {
	capacity := solveclient.CapacityOf(&arrangorpcv1.NodeStatusResponse{
		Cores:          24,
		AvailableBytes: 4 * gb,
		CommittedBytes: 9 * gb,
	})
	if capacity.FreeBytes != 0 {
		t.Errorf("FreeBytes = %d, want 0 when more is committed than available",
			capacity.FreeBytes)
	}
	if capacity.AffordableWorkers != 0 {
		t.Errorf("AffordableWorkers = %d, want 0", capacity.AffordableWorkers)
	}
	if !capacity.Known {
		t.Error("a node that answered is not marked as having answered")
	}
}

// The whole answer is carried, not just the number admission clamps on: what a
// host is doing is what an operator reads when a solve was slower than they
// expected.
func TestCapacityOfCarriesTheNodesAnswer(t *testing.T) {
	capacity := solveclient.CapacityOf(&arrangorpcv1.NodeStatusResponse{
		Cores:          24,
		AvailableBytes: 20 * gb,
		CommittedBytes: 4 * gb,
		RunningSolves:  2,
		LoadAverage_1M: 3.5,
		Calibration:    &arrangorpcv1.HostCalibration{Calibrated: true, Speed: 1.4},
	})
	if capacity.FreeBytes != 16*gb {
		t.Errorf("FreeBytes = %d, want 16 GB", capacity.FreeBytes)
	}
	// 16 GB free, 2 GB base, 900 MB each: 15 workers.
	if capacity.AffordableWorkers != 15 {
		t.Errorf("AffordableWorkers = %d, want 15", capacity.AffordableWorkers)
	}
	if capacity.RunningSolves != 2 || capacity.Cores != 24 {
		t.Errorf("capacity = %+v", capacity)
	}
	if capacity.LoadAverage1m != 3.5 || capacity.Speed != 1.4 {
		t.Errorf("load %v speed %v", capacity.LoadAverage1m, capacity.Speed)
	}
}

// A node with no calibration yet is not an error: speed is a note for the log,
// never an input to the clamp.
func TestCapacityOfToleratesNoCalibration(t *testing.T) {
	capacity := solveclient.CapacityOf(&arrangorpcv1.NodeStatusResponse{
		Cores: 8, AvailableBytes: 16 * gb,
	})
	if capacity.Speed != 0 {
		t.Errorf("Speed = %v, want zero when the host was never calibrated", capacity.Speed)
	}
	if capacity.AffordableWorkers != 8 {
		t.Errorf("AffordableWorkers = %d, want the core count", capacity.AffordableWorkers)
	}
}

// The node's own memory arithmetic, mirrored here so a solve is clamped before
// it is sent rather than queued or refused after a whole school is on the wire.
func TestAffordableWorkersMirrorsTheNode(t *testing.T) {
	for _, one := range []struct {
		name  string
		free  uint64
		cores uint32
		want  uint32
	}{
		// Below the 2 GB base cost, nothing fits at all.
		{"nothing free", 0, 24, 0},
		{"under the base cost", gb, 24, 0},
		// 900 MB per worker on top of the base.
		{"one worker", 2*gb + 900<<20, 24, 1},
		{"eight workers", 2*gb + 8*(900<<20), 24, 8},
		// Cores bound it even when memory would allow more.
		{"cores are the bound", 64 * gb, 4, 4},
		// A node reporting no cores must not turn into an unbounded answer.
		{"no cores reported", 64 * gb, 0, 0},
	} {
		t.Run(one.name, func(t *testing.T) {
			if got := solveclient.AffordableWorkers(one.free, one.cores); got != one.want {
				t.Errorf("AffordableWorkers(%d, %d) = %d, want %d",
					one.free, one.cores, got, one.want)
			}
		})
	}
}

func TestCapacityMustMatchNodeArithmetic(t *testing.T) {
	free := uint64(2)<<30 + uint64(100)<<20
	host := nodecapacity.Host{Cores: 8, AvailableBytes: free}
	if got, want := int(solveclient.AffordableWorkers(free, 8)), host.AffordableWorkers(); got != want {
		t.Fatalf("client admits %d workers, actual node admits %d at %d free bytes", got, want, free)
	}
}

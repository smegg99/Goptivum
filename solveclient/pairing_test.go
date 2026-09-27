// solveclient/pairing_test.go

package solveclient_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/smegg99/arrango/backend/config"
	arrangoid "github.com/smegg99/arrango/backend/identity"
	"github.com/smegg99/arrango/backend/pairing"

	"github.com/smegg99/goptivum/solveclient"
)

// The identity is created once and kept: a node pins the caller's
// certificate, so replacing it means every node must pair again.
func TestIdentityIsStableAcrossLoads(t *testing.T) {
	dir := t.TempDir()
	first, err := solveclient.LoadIdentity(dir)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	second, err := solveclient.LoadIdentity(dir)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if !first.Fingerprint().Equal(second.Fingerprint()) {
		t.Error("loading the same directory produced a different identity")
	}
	if _, err := solveclient.LoadIdentity(""); err == nil {
		t.Error("an empty identity directory was accepted")
	}
}

// Create writes into the directory but does not make one, so a fresh install
// would fail on its first pairing.
func TestIdentityDirectoryIsCreatedOnFirstUse(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "does", "not", "exist", "yet")
	self, err := solveclient.LoadIdentity(dir)
	if err != nil {
		t.Fatalf("first use in a missing directory: %v", err)
	}
	if self.Fingerprint().String() == "" {
		t.Error("no fingerprint after creating an identity")
	}
	info, err := os.Stat(dir)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	// A private key lives here.
	if mode := info.Mode().Perm(); mode != 0o700 {
		t.Errorf("identity directory mode = %o, want 700", mode)
	}
}

// The property the whole pairing design rests on: the token never leaves this
// process when the presented certificate is not the pinned one.
func TestPairRefusesAMismatchWithoutSendingTheToken(t *testing.T) {
	fake := newImpostor(t)

	// A fingerprint that is not the impostor's, which is what a pairing URI
	// from the real node would carry.
	real, err := arrangoid.Create(t.TempDir())
	if err != nil {
		t.Fatalf("identity: %v", err)
	}
	dialer, _ := newDialerAs(t, "0.1.0")

	// Shaped like a real one: 10 bytes of base32 in groups of four. A token the
	// parser rejects would never reach the wire for the wrong reason.
	const token = "ABCD-EFGH-IJKL-MNOP"
	uri := fmt.Sprintf("arrango://%s#fp=%s&t=%s", fake.address, real.Fingerprint, token)

	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	if _, err := dialer.Pair(ctx, uri); !errors.Is(err, solveclient.ErrPinMismatch) {
		t.Fatalf("err = %v, want ErrPinMismatch", err)
	}
	if bytes := fake.read.Load(); bytes != 0 {
		t.Errorf("the impostor read %d application bytes; it must learn nothing", bytes)
	}
}

func TestPairRefusesSomethingThatIsNotAPairingURI(t *testing.T) {
	dialer, _ := newDialerAs(t, "0.1.0")
	for _, uri := range []string{
		"", "https://example.test", "arrango://nohostport",
		"arrango://127.0.0.1:9443",
		"arrango://127.0.0.1:9443#fp=nonsense&t=ABCD-EFGH-IJKL-MNOP",
		"arrango://127.0.0.1:9443#t=not-a-token",
	} {
		if _, err := dialer.Pair(t.Context(), uri); err == nil {
			t.Errorf("%q was accepted as a pairing URI", uri)
		}
	}
}

// Each way pairing fails is told apart, so the person pasting the link learns
// what to do about it.
func TestPairingSaysWhyItFailed(t *testing.T) {
	node := startLiveNode(t)
	other, err := arrangoid.Create(t.TempDir())
	if err != nil {
		t.Fatalf("identity: %v", err)
	}

	for _, one := range []struct {
		name string
		uri  func(t *testing.T) string
		want error
	}{
		{"text that is not a link", func(*testing.T) string { return "arrango://nothing" }, solveclient.ErrNotALink},
		{"another scheme", func(*testing.T) string { return "https://example.test" }, solveclient.ErrNotALink},
		{"nothing at the address", func(t *testing.T) string {
			return pairing.URI{Address: closedAddress(t), Fingerprint: node.ident.Fingerprint, Token: "ABCD-EFGH-IJKL-MNOP"}.String()
		}, solveclient.ErrUnreachable},
		{"another certificate at the address", func(*testing.T) string {
			return pairing.URI{Address: node.server.Address(), Fingerprint: other.Fingerprint, Token: "ABCD-EFGH-IJKL-MNOP"}.String()
		}, solveclient.ErrPinMismatch},
		// No token is pending, so the node closes its handshake to strangers.
		{"a token the node never issued", func(*testing.T) string { return node.unminted() }, solveclient.ErrLinkSpent},
		// Only an expired token is pending, and the node drops it on sight.
		{"an expired link", func(t *testing.T) string { return node.link(t, config.DefaultPolicy(), -time.Second) }, solveclient.ErrLinkSpent},
	} {
		t.Run(one.name, func(t *testing.T) {
			dialer, _ := newDialerAs(t, "0.1.0")
			_, err := pairWithin(t, dialer, one.uri(t))
			if !errors.Is(err, one.want) {
				t.Fatalf("err = %v, want %v", err, one.want)
			}
		})
	}
}

// A link another caller already used is spent, whether or not another token
// holds the node's pairing window open.
func TestAUsedLinkIsSpent(t *testing.T) {
	node := startLiveNode(t)
	uri := node.link(t, config.DefaultPolicy(), time.Minute)

	first, _ := newDialerAs(t, "0.1.0")
	if _, err := pairWithin(t, first, uri); err != nil {
		t.Fatalf("first pairing: %v", err)
	}

	second, _ := newDialerAs(t, "0.1.0")
	if _, err := pairWithin(t, second, uri); !errors.Is(err, solveclient.ErrLinkSpent) {
		t.Errorf("the window closed: err = %v, want ErrLinkSpent", err)
	}

	// With another link pending the handshake goes through and the token
	// check refuses instead.
	node.link(t, config.DefaultPolicy(), time.Minute)
	third, _ := newDialerAs(t, "0.1.0")
	if _, err := pairWithin(t, third, uri); !errors.Is(err, solveclient.ErrLinkSpent) {
		t.Errorf("the window open: err = %v, want ErrLinkSpent", err)
	}
}

func TestPairingBelowTheNodesFloorSaysTheCallerIsTooOld(t *testing.T) {
	node := startLiveNode(t)
	dialer, _ := newDialerAs(t, "0.0.1")
	_, err := pairWithin(t, dialer, node.link(t, config.DefaultPolicy(), time.Minute))
	if !errors.Is(err, solveclient.ErrClientTooOld) {
		t.Fatalf("err = %v, want ErrClientTooOld", err)
	}
}

// Pairing reads the limits the node's operator gave the caller, so the caller
// starts inside them.
func TestPairingReadsTheNodesCeilingsForTheCaller(t *testing.T) {
	node := startLiveNode(t)

	for _, one := range []struct {
		name   string
		policy config.Policy
		want   solveclient.Ceilings
	}{
		{"paired with --max-solves 2 --worker-ceiling 12", config.Policy{MaxConcurrentSolves: 2, WorkerCeiling: 12}, solveclient.Ceilings{Solves: 2, Workers: 12}},
		// Zero is no limit of the node's own, and the host's cores decide the workers.
		{"paired with no limits", config.Policy{}, solveclient.Ceilings{Solves: 0, Workers: 8}},
	} {
		t.Run(one.name, func(t *testing.T) {
			dialer, _ := newDialerAs(t, "0.1.0")
			paired, err := pairWithin(t, dialer, node.link(t, one.policy, time.Minute))
			if err != nil {
				t.Fatalf("pair: %v", err)
			}
			if paired.Ceilings == nil || *paired.Ceilings != one.want {
				t.Errorf("ceilings = %+v, want %+v", paired.Ceilings, one.want)
			}
			if paired.NodeAPI != "1.0.0" || paired.Solver != "9.9.9" {
				t.Errorf("versions = %q, %q", paired.NodeAPI, paired.Solver)
			}
		})
	}
}

// A node whose build does not report a policy pairs all the same, with no
// ceilings known.
func TestPairingANodeWithoutPeerPolicyKnowsNoCeilings(t *testing.T) {
	fake := startFakeNode(t, &fakeNode{})
	dialer, _ := newDialerAs(t, "0.1.0")
	uri := pairing.URI{Address: fake.address, Fingerprint: fake.pin, Token: "ABCD-EFGH-IJKL-MNOP"}.String()
	paired, err := pairWithin(t, dialer, uri)
	if err != nil {
		t.Fatalf("pair: %v", err)
	}
	if paired.Ceilings != nil {
		t.Errorf("ceilings = %+v, want none from a node that does not report them", paired.Ceilings)
	}
}

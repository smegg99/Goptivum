// solveclient/nodes_test.go

package solveclient_test

import (
	"context"
	"crypto/tls"
	"net"
	"os"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"

	"github.com/smegg99/arrango/backend/config"
	arrangoid "github.com/smegg99/arrango/backend/identity"
	"github.com/smegg99/arrango/backend/pairing"
	"github.com/smegg99/arrango/backend/peers"
	"github.com/smegg99/arrango/backend/transport"

	arrangorpcv1 "github.com/smegg99/arrango/backend/gen/arrangorpcv1"
	arrangov1 "github.com/smegg99/arrango/backend/gen/arrangov1"

	"github.com/smegg99/goptivum/solveclient"
)

// liveNode is Arrango's own transport: its handshake, its allowlist and its
// one-time tokens, so a refusal here is the one a hosted node gives.
type liveNode struct {
	server *transport.Server
	ident  *arrangoid.Identity
	peers  *peers.Store
	tokens *pairing.Store
}

// policyNode answers NodeService the way Arrango's does for these two calls:
// versions, and the caller's own policy in the status.
type policyNode struct {
	arrangorpcv1.UnimplementedNodeServiceServer
}

func (policyNode) GetNodeInfo(context.Context, *arrangorpcv1.NodeInfoRequest) (*arrangorpcv1.NodeInfoResponse, error) {
	return &arrangorpcv1.NodeInfoResponse{
		NodeApiVersion: "1.0.0", MinClientVersion: "0.1.0", SolverVersion: "9.9.9",
		Capabilities: []string{"solve", "peer_policy"},
	}, nil
}

func (policyNode) GetNodeStatus(ctx context.Context, _ *arrangorpcv1.NodeStatusRequest) (*arrangorpcv1.NodeStatusResponse, error) {
	out := &arrangorpcv1.NodeStatusResponse{Cores: 8, AvailableBytes: 16 << 30}
	if caller, ok := transport.CallerFrom(ctx); ok && !caller.Operator {
		out.Policy = &arrangorpcv1.PeerPolicy{
			MaxConcurrentSolves: uint32(caller.Peer.Policy.MaxConcurrentSolves),
			WorkerCeiling:       uint32(caller.Peer.Policy.WorkerCeiling),
		}
	}
	return out, nil
}

func startLiveNode(t *testing.T) *liveNode {
	t.Helper()
	// Short, because the node refuses a socket directory whose path leaves no
	// room for a socket name, and t.TempDir carries the test's name.
	dir, err := os.MkdirTemp("", "arr")
	if err != nil {
		t.Fatalf("temp dir: %v", err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	cfg := &config.Config{DataDir: dir, Listen: "127.0.0.1:0"}
	if err := cfg.Resolve(); err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if err := cfg.EnsureDirs(); err != nil {
		t.Fatalf("ensure dirs: %v", err)
	}
	ident, err := arrangoid.Create(cfg.Path(config.IdentityDir))
	if err != nil {
		t.Fatalf("identity: %v", err)
	}
	allow := peers.New(cfg.Path(config.PeersDir))
	tokens := pairing.New(cfg.Path(config.PairingDir))
	server, err := transport.New(cfg, ident, allow, tokens)
	if err != nil {
		t.Fatalf("transport: %v", err)
	}
	server.Register(func(registrar grpc.ServiceRegistrar) {
		arrangorpcv1.RegisterNodeServiceServer(registrar, policyNode{})
	})
	go func() { _ = server.ListenAndServe() }()
	t.Cleanup(func() { _ = server.Close() })
	return &liveNode{server: server, ident: ident, peers: allow, tokens: tokens}
}

// link mints a pairing link the way `arrango-node pair` does.
func (n *liveNode) link(t *testing.T, policy config.Policy, ttl time.Duration) string {
	t.Helper()
	token, err := n.tokens.Mint("goptivum", policy, ttl)
	if err != nil {
		t.Fatalf("mint: %v", err)
	}
	return pairing.URI{Address: n.server.Address(), Fingerprint: n.ident.Fingerprint, Token: token}.String()
}

// unminted is a well-formed link to the node whose token the node never issued.
func (n *liveNode) unminted() string {
	return pairing.URI{Address: n.server.Address(), Fingerprint: n.ident.Fingerprint, Token: "ABCD-EFGH-IJKL-MNOP"}.String()
}

// fakeNode is a node that really speaks the wire: mutual TLS with a pinned
// fingerprint, and a Solve stream a test controls frame by frame.
type fakeNode struct {
	arrangorpcv1.UnimplementedSolverServiceServer
	arrangorpcv1.UnimplementedNodeServiceServer

	// frames is what Solve sends before it ends, holds or breaks.
	frames []*arrangorpcv1.SolveUpdate
	// hold blocks Solve after the frames, so a test can cancel mid-stream.
	hold bool
	// solveErr, when set and not holding, breaks the stream after the frames.
	solveErr error
	// capabilities is what GetNodeInfo reports. Nil reports a node that can check and list its rules.
	capabilities []string
	// report is what Check answers. Nil answers a report with no findings.
	report *arrangorpcv1.CheckReport
	// checkErr, when set, is what Check answers instead of a report.
	checkErr error

	address string
	pin     arrangoid.Fingerprint

	// mu guards what the node records, which its own goroutines can race with a test.
	mu      sync.Mutex
	solved  []*arrangorpcv1.SolveRequest
	checked []*arrangorpcv1.CheckRequest
}

// startFakeNode serves on loopback until the test ends.
func startFakeNode(t *testing.T, node *fakeNode) *fakeNode {
	t.Helper()
	self, err := arrangoid.Create(t.TempDir())
	if err != nil {
		t.Fatalf("node identity: %v", err)
	}
	server := grpc.NewServer(grpc.Creds(credentials.NewTLS(&tls.Config{
		Certificates: []tls.Certificate{self.Certificate},
		// Any client cert: this fake pins nothing, because what is under test
		// is the client's behaviour and not the node's admission.
		ClientAuth: tls.RequireAnyClientCert,
		MinVersion: tls.VersionTLS13,
	})))
	arrangorpcv1.RegisterSolverServiceServer(server, node)
	arrangorpcv1.RegisterNodeServiceServer(server, node)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(server.Stop)
	node.address = listener.Addr().String()
	node.pin = self.Fingerprint
	return node
}

// Solve sends the frames it was given, then ends, breaks or holds the stream
// open so the caller can cancel it.
func (f *fakeNode) Solve(request *arrangorpcv1.SolveRequest, stream arrangorpcv1.SolverService_SolveServer) error {
	f.mu.Lock()
	f.solved = append(f.solved, request)
	f.mu.Unlock()
	for _, frame := range f.frames {
		if err := stream.Send(frame); err != nil {
			return err
		}
	}
	if !f.hold {
		return f.solveErr
	}
	<-stream.Context().Done()
	return stream.Context().Err()
}

func (f *fakeNode) solveRequests() []*arrangorpcv1.SolveRequest {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.solved)
}

func (f *fakeNode) GetNodeStatus(context.Context, *arrangorpcv1.NodeStatusRequest) (*arrangorpcv1.NodeStatusResponse, error) {
	return &arrangorpcv1.NodeStatusResponse{Cores: 8, AvailableBytes: 16 << 30}, nil
}

func (f *fakeNode) GetNodeInfo(context.Context, *arrangorpcv1.NodeInfoRequest) (*arrangorpcv1.NodeInfoResponse, error) {
	capabilities := f.capabilities
	if capabilities == nil {
		capabilities = []string{"solve", "validate", "check", "rule_catalogue", "rule_metadata"}
	}
	return &arrangorpcv1.NodeInfoResponse{NodeApiVersion: "1.0.0", Capabilities: capabilities}, nil
}

func (f *fakeNode) Check(_ context.Context, request *arrangorpcv1.CheckRequest) (*arrangorpcv1.CheckReport, error) {
	f.mu.Lock()
	f.checked = append(f.checked, request)
	f.mu.Unlock()
	if f.checkErr != nil {
		return nil, f.checkErr
	}
	if f.report != nil {
		return f.report, nil
	}
	return &arrangorpcv1.CheckReport{}, nil
}

func (f *fakeNode) checkCalls() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.checked)
}

func (f *fakeNode) GetRuleCatalogue(context.Context, *arrangorpcv1.RuleCatalogueRequest) (*arrangorpcv1.RuleCatalogue, error) {
	return &arrangorpcv1.RuleCatalogue{Profiles: []*arrangorpcv1.RuleProfile{{Name: "default"}}}, nil
}

// reach dials the fake the way a caller dials a node it paired with.
func (f *fakeNode) reach(t *testing.T, dialer *solveclient.Dialer) *solveclient.Conn {
	t.Helper()
	conn, err := dialer.Reach(t.Context(), "fake", f.address, f.pin.String())
	if err != nil {
		t.Fatalf("reach: %v", err)
	}
	t.Cleanup(func() { conn.Close() })
	return conn
}

func newDialerAs(t *testing.T, clientVersion string) (*solveclient.Dialer, *solveclient.Identity) {
	t.Helper()
	self, err := solveclient.LoadIdentity(t.TempDir())
	if err != nil {
		t.Fatalf("identity: %v", err)
	}
	dialer, err := solveclient.NewDialer(self, clientVersion)
	if err != nil {
		t.Fatalf("dialer: %v", err)
	}
	return dialer, self
}

// closedAddress is a loopback port nothing listens on.
func closedAddress(t *testing.T) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	address := listener.Addr().String()
	listener.Close()
	return address
}

func pairWithin(t *testing.T, dialer *solveclient.Dialer, uri string) (solveclient.Paired, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	return dialer.Pair(ctx, uri)
}

// solutionFrame is a frame carrying a schedule, the way a real solver's
// SOLUTION phase does.
func solutionFrame(lessonID uint32, objective int64) *arrangorpcv1.SolveUpdate {
	return &arrangorpcv1.SolveUpdate{
		Phase:     arrangorpcv1.SolvePhase_SOLVE_PHASE_SOLUTION,
		Status:    arrangorpcv1.SolveStatus_SOLVE_STATUS_FEASIBLE,
		Objective: objective,
		Snapshot: &arrangov1.ScheduleSnapshot{
			Lessons: []*arrangov1.ScheduledLesson{{
				LessonId:  lessonID,
				Placement: &arrangov1.Placement{DayId: 1, StartPeriod: 1},
			}},
		},
	}
}

// doneFrame is the frame a real solve ends with, carrying what it cost.
func doneFrame(status arrangorpcv1.SolveStatus) *arrangorpcv1.SolveUpdate {
	return &arrangorpcv1.SolveUpdate{
		Phase: arrangorpcv1.SolvePhase_SOLVE_PHASE_DONE, Status: status,
		Config:       &arrangorpcv1.SolverConfig{NumWorkers: 3},
		PeakRssBytes: 1536 << 20, CpuSeconds: 42.5,
	}
}

// impostor is a TLS listener with its own certificate, recording whether any
// application byte ever arrived.
type impostor struct {
	address string
	read    atomic.Int64
}

func newImpostor(t *testing.T) *impostor {
	t.Helper()
	self, err := arrangoid.Create(t.TempDir())
	if err != nil {
		t.Fatalf("impostor identity: %v", err)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { listener.Close() })

	fake := &impostor{address: listener.Addr().String()}
	config := &tls.Config{
		Certificates: []tls.Certificate{self.Certificate},
		MinVersion:   tls.VersionTLS13,
		// Accept whatever the caller presents: the point is to prove the caller
		// never gets far enough to present anything that matters.
		ClientAuth: tls.RequestClientCert,
	}
	go func() {
		for {
			raw, err := listener.Accept()
			if err != nil {
				return
			}
			go func() {
				defer raw.Close()
				conn := tls.Server(raw, config)
				if err := conn.HandshakeContext(context.Background()); err != nil {
					return
				}
				buffer := make([]byte, 1024)
				n, _ := conn.Read(buffer)
				fake.read.Add(int64(n))
			}()
		}
	}()
	return fake
}

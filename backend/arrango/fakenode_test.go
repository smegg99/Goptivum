// arrango/fakenode_test.go

package arrango_test

import (
	"context"
	"crypto/tls"
	"net"
	"slices"
	"sync"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"

	arrangoid "github.com/smegg99/arrango/backend/identity"
	"github.com/smegg99/goptivum/solveclient"

	arrangorpcv1 "github.com/smegg99/arrango/backend/gen/arrangorpcv1"
	arrangov1 "github.com/smegg99/arrango/backend/gen/arrangov1"
)

// fakeNode is a node that really speaks the wire: mutual TLS with a pinned
// fingerprint, and a Solve stream a test controls frame by frame.
//
// Without one, every test here dispatches to a port that refuses, which
// exercises the dial and nothing after it. The whole stream lifecycle, and a
// cancel in the middle of it, is only reachable through something that
// actually streams.
type fakeNode struct {
	arrangorpcv1.UnimplementedSolverServiceServer
	arrangorpcv1.UnimplementedNodeServiceServer

	// frames is what Solve sends before it waits.
	frames []*arrangorpcv1.SolveUpdate
	// hold blocks Solve after the frames, so a test can cancel mid-stream.
	hold bool
	// solveErr, when set and not holding, breaks the stream after the frames.
	solveErr error
	// status is what GetNodeStatus reports.
	status *arrangorpcv1.NodeStatusResponse
	// validation is what Validate reports. Nil means the school is fine.
	validation *arrangov1.ValidationReport
	// validateErr, when set, is what Validate answers instead.
	validateErr error
	// demo is what GetDemoSchool answers. Nil answers Unimplemented.
	demo *arrangov1.SchoolModel
	// capabilities is what GetNodeInfo reports. Nil reports a node that can check and list its rules.
	capabilities []string
	// report is what Check answers. Nil answers a report with no findings.
	report *arrangorpcv1.CheckReport
	// checkErr, when set, is what Check answers instead of a report.
	checkErr error
	// catalogue is what GetRuleCatalogue answers. Nil answers an empty catalogue.
	catalogue *arrangorpcv1.RuleCatalogue

	address string
	pin     arrangoid.Fingerprint
	// stop shuts the node down the way a restart does: every stream is cut.
	stop func()

	// mu guards what the node records, which its own goroutines can race with a test.
	mu sync.Mutex
	// validated records every Validate the node was actually asked, so a test
	// can prove both that a check ran and what schedule it ran against.
	validated []*arrangorpcv1.ValidateRequest
	// demoAsked records every GetDemoSchool request.
	demoAsked []*arrangorpcv1.DemoRequest
	// checked records every Check request.
	checked []*arrangorpcv1.CheckRequest
	// catalogued counts the GetRuleCatalogue calls.
	catalogued int
	// solved records every Solve request the node was asked to open.
	solved []*arrangorpcv1.SolveRequest
}

// solveCalls reports how many solves reached this node.
func (f *fakeNode) solveCalls() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.solved)
}

// solveRequests reports the Solve requests this node received.
func (f *fakeNode) solveRequests() []*arrangorpcv1.SolveRequest {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.solved)
}

// validateCalls reports the Validate requests this node received.
func (f *fakeNode) validateCalls() []*arrangorpcv1.ValidateRequest {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.validated)
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
	node.stop = server.Stop
	return node
}

// paired is what the registry stores for this node.
func (f *fakeNode) paired() solveclient.Paired {
	return solveclient.Paired{Address: f.address, Fingerprint: f.pin}
}

// Solve sends the frames it was given, then either ends or holds the stream
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
	// Until the caller goes away. A real solve holds the stream for minutes.
	<-stream.Context().Done()
	return stream.Context().Err()
}

// GetNodeStatus answers what the test configured, so capacity negotiation has
// something real to negotiate with.
func (f *fakeNode) GetNodeStatus(_ context.Context, _ *arrangorpcv1.NodeStatusRequest) (*arrangorpcv1.NodeStatusResponse, error) {
	if f.status != nil {
		return f.status, nil
	}
	return &arrangorpcv1.NodeStatusResponse{
		Cores: 8, AvailableBytes: 16 << 30, CommittedBytes: 0,
		Policy: &arrangorpcv1.PeerPolicy{MaxSolveSeconds: 86400},
	}, nil
}

// Validate answers what the test configured. A nil report is a school with
// nothing wrong with it.
func (f *fakeNode) Validate(_ context.Context, request *arrangorpcv1.ValidateRequest) (*arrangov1.ValidationReport, error) {
	f.mu.Lock()
	f.validated = append(f.validated, request)
	f.mu.Unlock()
	if f.validateErr != nil {
		return nil, f.validateErr
	}
	if f.validation != nil {
		return f.validation, nil
	}
	return &arrangov1.ValidationReport{Valid: true}, nil
}

// GetDemoSchool answers the school the test configured.
func (f *fakeNode) GetDemoSchool(ctx context.Context, request *arrangorpcv1.DemoRequest) (*arrangov1.SchoolModel, error) {
	f.mu.Lock()
	f.demoAsked = append(f.demoAsked, request)
	f.mu.Unlock()
	if f.demo == nil {
		return f.UnimplementedSolverServiceServer.GetDemoSchool(ctx, request)
	}
	return f.demo, nil
}

// demoCalls reports the GetDemoSchool requests this node received.
func (f *fakeNode) demoCalls() []*arrangorpcv1.DemoRequest {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.demoAsked)
}

// GetNodeInfo reports the capabilities the test configured.
func (f *fakeNode) GetNodeInfo(_ context.Context, _ *arrangorpcv1.NodeInfoRequest) (*arrangorpcv1.NodeInfoResponse, error) {
	capabilities := f.capabilities
	if capabilities == nil {
		capabilities = []string{"solve", "validate", "check", "rule_catalogue", "rule_metadata", "solve_time_ceiling", "stall_stop"}
	}
	return &arrangorpcv1.NodeInfoResponse{NodeApiVersion: "1.0.0", Capabilities: capabilities}, nil
}

// Check answers the report the test configured.
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

// checkCalls reports the Check requests this node received.
func (f *fakeNode) checkCalls() []*arrangorpcv1.CheckRequest {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.checked)
}

// GetRuleCatalogue answers the catalogue the test configured.
func (f *fakeNode) GetRuleCatalogue(context.Context, *arrangorpcv1.RuleCatalogueRequest) (*arrangorpcv1.RuleCatalogue, error) {
	f.mu.Lock()
	f.catalogued++
	f.mu.Unlock()
	if f.catalogue != nil {
		return f.catalogue, nil
	}
	return &arrangorpcv1.RuleCatalogue{}, nil
}

// catalogueCalls reports how many GetRuleCatalogue calls reached this node.
func (f *fakeNode) catalogueCalls() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.catalogued
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
		WallTimeSeconds: float64(objective) / 10,
	}
}

// doneFrame is the frame a real solve ends with, carrying what it cost.
func doneFrame(status arrangorpcv1.SolveStatus, snapshot *arrangov1.ScheduleSnapshot) *arrangorpcv1.SolveUpdate {
	frame := &arrangorpcv1.SolveUpdate{
		Phase: arrangorpcv1.SolvePhase_SOLVE_PHASE_DONE, Status: status, Snapshot: snapshot,
		Config:       &arrangorpcv1.SolverConfig{NumWorkers: 3},
		PeakRssBytes: 1536 << 20, CpuSeconds: 42.5,
	}
	if snapshot != nil {
		frame.Score = &arrangov1.ScoreReport{OverallQuality: 91}
		frame.Verdict = &arrangorpcv1.ScheduleVerdict{Tier: arrangorpcv1.ScheduleVerdict_TIER_PRISTINE}
	}
	return frame
}

// waitFor polls until the condition holds or the test gives up, for a stream
// that runs on its own goroutine.
func waitFor(t *testing.T, what string, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if ok() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

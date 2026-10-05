// api/v1/deleteplan_test.go

package v1_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"strconv"
	"sync"
	"testing"
	"time"

	arrangorpcv1 "github.com/smegg99/arrango/backend/gen/arrangorpcv1"
	arrangov1 "github.com/smegg99/arrango/backend/gen/arrangov1"

	"github.com/smegg99/goptivum/backend/api/v1/gen"
	"github.com/smegg99/goptivum/backend/arrango"
	"github.com/smegg99/goptivum/solveclient"
)

// solveNode speaks the node's wire, mutual TLS included. A solve waits for
// release when there is one, sends frames, and then either ends or holds the
// stream until the caller goes away, which it reports. It answers its status,
// says it can check, checks with an empty report, and validates a timetable
// as valid unless told otherwise.
type solveNode struct {
	arrangorpcv1.UnimplementedSolverServiceServer
	arrangorpcv1.UnimplementedNodeServiceServer
	// release, when set, holds each solve until the test closes it.
	release chan struct{}
	frames  []*arrangorpcv1.SolveUpdate
	hold    bool
	// cancelled is closed when a held solve's caller went away.
	cancelled chan struct{}
	once      sync.Once
	// status, when set, is what GetNodeStatus answers.
	status *arrangorpcv1.NodeStatusResponse
	// validation, when set, is what Validate reports about an improve's timetable.
	validation *arrangov1.ValidationReport

	mu      sync.Mutex
	checked []*arrangorpcv1.CheckRequest
	solved  []*arrangorpcv1.SolveRequest
}

func (n *solveNode) Solve(request *arrangorpcv1.SolveRequest, stream arrangorpcv1.SolverService_SolveServer) error {
	n.mu.Lock()
	n.solved = append(n.solved, request)
	n.mu.Unlock()
	if n.release != nil {
		select {
		case <-n.release:
		case <-stream.Context().Done():
			return stream.Context().Err()
		}
	}
	for _, frame := range n.frames {
		if err := stream.Send(frame); err != nil {
			return err
		}
	}
	if !n.hold {
		return nil
	}
	<-stream.Context().Done()
	n.once.Do(func() { close(n.cancelled) })
	return stream.Context().Err()
}

func (n *solveNode) GetNodeStatus(context.Context, *arrangorpcv1.NodeStatusRequest) (*arrangorpcv1.NodeStatusResponse, error) {
	if n.status != nil {
		return n.status, nil
	}
	return &arrangorpcv1.NodeStatusResponse{Cores: 8, AvailableBytes: 16 << 30, Policy: &arrangorpcv1.PeerPolicy{MaxSolveSeconds: 86400}}, nil
}

func (n *solveNode) GetNodeInfo(context.Context, *arrangorpcv1.NodeInfoRequest) (*arrangorpcv1.NodeInfoResponse, error) {
	return &arrangorpcv1.NodeInfoResponse{NodeApiVersion: "1.0.0", Capabilities: []string{"solve", "check", "solve_time_ceiling", "stall_stop", "preflight_refusal", "rule_metadata"}}, nil
}

func (n *solveNode) Check(_ context.Context, request *arrangorpcv1.CheckRequest) (*arrangorpcv1.CheckReport, error) {
	n.mu.Lock()
	n.checked = append(n.checked, request)
	n.mu.Unlock()
	return &arrangorpcv1.CheckReport{}, nil
}

func (n *solveNode) Validate(context.Context, *arrangorpcv1.ValidateRequest) (*arrangov1.ValidationReport, error) {
	if n.validation != nil {
		return n.validation, nil
	}
	return &arrangov1.ValidationReport{Valid: true}, nil
}

func (n *solveNode) checks() []*arrangorpcv1.CheckRequest {
	n.mu.Lock()
	defer n.mu.Unlock()
	return slices.Clone(n.checked)
}

// solves reports the Solve requests this node received.
func (n *solveNode) solves() []*arrangorpcv1.SolveRequest {
	n.mu.Lock()
	defer n.mu.Unlock()
	return slices.Clone(n.solved)
}

// timetableFrame is a solution frame placing lesson 1.
func timetableFrame(phase arrangorpcv1.SolvePhase) *arrangorpcv1.SolveUpdate {
	return &arrangorpcv1.SolveUpdate{
		Phase:  phase,
		Status: arrangorpcv1.SolveStatus_SOLVE_STATUS_FEASIBLE,
		Snapshot: &arrangov1.ScheduleSnapshot{Lessons: []*arrangov1.ScheduledLesson{{
			LessonId: 1, Placement: &arrangov1.Placement{DayId: 1, StartPeriod: 1},
		}}},
		Score:   &arrangov1.ScoreReport{OverallQuality: 88},
		Verdict: &arrangorpcv1.ScheduleVerdict{Tier: arrangorpcv1.ScheduleVerdict_TIER_WARNINGS},
	}
}

// startNode serves a node on loopback until the test ends and says how to pin it.
func startNode(t *testing.T, node *solveNode) solveclient.Paired {
	t.Helper()
	node.cancelled = make(chan struct{})
	return serveNode(t, node)
}

// startSolveNode serves a node whose solves send one timetable once released
// and then run until cancelled, the way a long one runs.
func startSolveNode(t *testing.T) (*solveNode, solveclient.Paired) {
	t.Helper()
	node := &solveNode{
		release: make(chan struct{}),
		frames:  []*arrangorpcv1.SolveUpdate{timetableFrame(arrangorpcv1.SolvePhase_SOLVE_PHASE_SOLUTION)},
		hold:    true,
	}
	return node, startNode(t, node)
}

// solvingWithATimetable dispatches a solve of a new plan and returns once the
// solve holds a timetable: its frame was published, and the runner keeps a
// frame before it reads the next one, so a cancel from here on stores it.
func solvingWithATimetable(t *testing.T, s solving) (planID int64, job int64, node *solveNode) {
	t.Helper()
	planID, version := s.ownedPlanWithVersion(t)
	node, paired := startSolveNode(t)
	recorded, err := s.registry.Record(t.Context(), "streaming", paired)
	if err != nil {
		t.Fatalf("record: %v", err)
	}

	body := fmt.Sprintf(`{"node_id":"%d","plan_version":%d,"ignore_previous_placements":true}`,
		recorded.ID, version)
	dispatched := call(t, s.server, http.MethodPost, fmt.Sprintf("/api/v1/plans/%d/jobs", planID), asOwner, body)
	if dispatched.Code != http.StatusAccepted {
		t.Fatalf("dispatch = %d: %s", dispatched.Code, dispatched.Body.String())
	}
	var created gen.Job
	if err := json.Unmarshal(dispatched.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode: %v", err)
	}
	job, err = strconv.ParseInt(created.Id, 10, 64)
	if err != nil {
		t.Fatalf("job id: %v", err)
	}

	broker, live := s.runner.Broker(job)
	if !live {
		t.Fatal("the solve is not running")
	}
	events, stop := broker.Subscribe()
	defer stop()
	close(node.release)
	deadline := time.After(10 * time.Second)
	for {
		select {
		case event, open := <-events:
			if !open {
				t.Fatal("the solve ended before its timetable arrived")
			}
			var update arrango.Update
			if err := event.Unmarshal(&update); err != nil {
				t.Fatalf("decode event: %v", err)
			}
			if update.Phase == arrangorpcv1.SolvePhase_SOLVE_PHASE_SOLUTION.String() {
				return planID, job, node
			}
		case <-deadline:
			t.Fatal("no timetable arrived")
		}
	}
}

// Deleting a project stops its solve, keeps the timetable the solve had as one
// of the project's results, and only then moves the project to Trash; the
// result comes back with the project.
func TestDeletingAPlanKeepsTheTimetableItsSolveHadBeforeTrash(t *testing.T) {
	s := newSolvingServer(t)
	planID, job, node := solvingWithATimetable(t, s)

	deleted := call(t, s.server, http.MethodDelete, fmt.Sprintf("/api/v1/plans/%d", planID), asOwner, "")
	if deleted.Code != http.StatusNoContent {
		t.Fatalf("delete = %d: %s", deleted.Code, deleted.Body.String())
	}
	closed, err := s.jobs.Get(t.Context(), job)
	if err != nil {
		t.Fatalf("get job: %v", err)
	}
	if closed.Status != arrango.StatusCancelled {
		t.Errorf("status = %q, want cancelled", closed.Status)
	}
	if closed.ResultID == nil {
		t.Fatal("the timetable the solve had was not stored before the plan went to Trash")
	}
	select {
	case <-node.cancelled:
	case <-time.After(5 * time.Second):
		t.Error("the node never saw the cancel")
	}

	restored := call(t, s.server, http.MethodPost, fmt.Sprintf("/api/v1/plans/%d/restore", planID), asOwner, "")
	if restored.Code != http.StatusOK {
		t.Fatalf("restore = %d: %s", restored.Code, restored.Body.String())
	}
	listed := call(t, s.server, http.MethodGet, fmt.Sprintf("/api/v1/plans/%d/results", planID), asOwner, "")
	if listed.Code != http.StatusOK {
		t.Fatalf("results = %d: %s", listed.Code, listed.Body.String())
	}
	var results gen.SolveResultList
	if err := json.Unmarshal(listed.Body.Bytes(), &results); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(results.Items) != 1 || results.Items[0].Id != strconv.FormatInt(*closed.ResultID, 10) {
		t.Errorf("results = %+v, want result %d", results.Items, *closed.ResultID)
	}
}

// A delete whose request ends while it waits for a solve to close answers 503
// and leaves the project where it was: nothing went to Trash.
func TestADeleteThatStopsWaitingLeavesThePlanLive(t *testing.T) {
	held := make(chan struct{})
	var release sync.Once
	letGo := func() { release.Do(func() { close(held) }) }
	t.Cleanup(letGo)
	s := newSolvingServerWith(t, func() { <-held })
	planID, job, _ := solvingWithATimetable(t, s)

	ctx, cancel := context.WithTimeout(t.Context(), 300*time.Millisecond)
	defer cancel()
	request := httptest.NewRequestWithContext(ctx, http.MethodDelete, fmt.Sprintf("/api/v1/plans/%d", planID), nil)
	request.Header.Set("Authorization", asOwner)
	deleted := httptest.NewRecorder()
	s.server.Handler().ServeHTTP(deleted, request)
	if deleted.Code != http.StatusServiceUnavailable {
		t.Fatalf("delete = %d, want 503 while the solve is still storing: %s", deleted.Code, deleted.Body.String())
	}
	if _, err := s.plans.Get(t.Context(), planID); err != nil {
		t.Errorf("the plan is not live after a delete that did not finish: %v", err)
	}

	letGo()
	var closed arrango.Job
	for attempt := 0; closed.FinishedAt == nil; attempt++ {
		if attempt == 400 {
			t.Fatal("the job never finished")
		}
		waitABit()
		row, err := s.jobs.Get(t.Context(), job)
		if err != nil {
			t.Fatalf("get job: %v", err)
		}
		closed = row
	}
	if closed.Status != arrango.StatusCancelled || closed.ResultID == nil {
		t.Errorf("job = %q with result %v, want cancelled with its timetable kept", closed.Status, closed.ResultID)
	}
}

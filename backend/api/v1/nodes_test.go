// api/v1/nodes_test.go

package v1_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	arrangorpcv1 "github.com/smegg99/arrango/backend/gen/arrangorpcv1"

	"github.com/smegg99/goptivum/backend/api/v1/gen"
	"github.com/smegg99/goptivum/backend/arrango"
	"github.com/smegg99/goptivum/backend/common/messages"
	"github.com/smegg99/goptivum/solveclient"
)

// policed is a node status carrying the policy its operator gave this server.
func policed(solves, workers uint32) *arrangorpcv1.NodeStatusResponse {
	return &arrangorpcv1.NodeStatusResponse{
		Cores: 8, AvailableBytes: 16 << 30, CommittedBytes: 4 << 30,
		Policy: &arrangorpcv1.PeerPolicy{MaxConcurrentSolves: solves, WorkerCeiling: workers, MaxSolveSeconds: 900},
	}
}

func nodePath(id int64, rest string) string {
	return fmt.Sprintf("/api/v1/nodes/%d%s", id, rest)
}

func strictNodeStatus(t *testing.T, recorder *httptest.ResponseRecorder) gen.NodeStatus {
	t.Helper()
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", recorder.Code, recorder.Body.String())
	}
	var status gen.NodeStatus
	decoder := json.NewDecoder(bytes.NewReader(recorder.Body.Bytes()))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&status); err != nil {
		t.Fatalf("decode %s: %v", recorder.Body.String(), err)
	}
	return status
}

func decodeNode(t *testing.T, recorder *httptest.ResponseRecorder) gen.Node {
	t.Helper()
	var node gen.Node
	if err := json.Unmarshal(recorder.Body.Bytes(), &node); err != nil {
		t.Fatalf("decode %s: %v", recorder.Body.String(), err)
	}
	return node
}

func decodeError(t *testing.T, recorder *httptest.ResponseRecorder) gen.Error {
	t.Helper()
	var envelope gen.Error
	if err := json.Unmarshal(recorder.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode %s: %v", recorder.Body.String(), err)
	}
	return envelope
}

func silentAddress(t *testing.T) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	address := listener.Addr().String()
	listener.Close()
	return address
}

// The status is read from the node now; running solves are this server's own.
func TestANodesStatusIsReadLive(t *testing.T) {
	s := newSolvingServer(t)
	node, paired := startSolveNode(t)
	node.status = policed(2, 12)
	recorded, err := s.registry.Record(t.Context(), "streaming", paired)
	if err != nil {
		t.Fatalf("record: %v", err)
	}

	idle := strictNodeStatus(t, call(t, s.server, http.MethodGet, nodePath(recorded.ID, "/status"), asAdmin, ""))
	if !idle.Reachable || idle.Problem != nil || idle.RunningSolves != 0 {
		t.Errorf("idle status = %+v", idle)
	}
	if idle.Cores == nil || *idle.Cores != 8 || idle.FreeMemoryBytes == nil || *idle.FreeMemoryBytes != 12<<30 {
		t.Errorf("host = cores %v, free %v, want 8 and 12 GiB", idle.Cores, idle.FreeMemoryBytes)
	}
	if idle.NodeApiVersion == nil || *idle.NodeApiVersion != "1.0.0" {
		t.Errorf("node api version = %v", idle.NodeApiVersion)
	}
	if idle.Ceilings == nil || idle.Ceilings.SolvesAtOnce == nil || *idle.Ceilings.SolvesAtOnce != 2 ||
		idle.Ceilings.WorkersPerSolve != 12 {
		t.Errorf("ceilings = %+v, want 2 solves and 12 workers", idle.Ceilings)
	}

	planID, version := s.ownedPlanWithVersion(t)
	if dispatched := s.dispatch(t, planID, version, recorded.ID); dispatched.Code != http.StatusAccepted {
		t.Fatalf("dispatch = %d: %s", dispatched.Code, dispatched.Body.String())
	}
	busy := strictNodeStatus(t, call(t, s.server, http.MethodGet, nodePath(recorded.ID, "/status"), asAdmin, ""))
	if busy.RunningSolves != 1 {
		t.Errorf("running solves = %d, want 1", busy.RunningSolves)
	}
}

func TestAnUnreachableNodesStatusSaysSo(t *testing.T) {
	s := newSolvingServer(t)
	recorded, err := s.registry.Record(t.Context(), "silent", paired(t, silentAddress(t)))
	if err != nil {
		t.Fatalf("record: %v", err)
	}

	status := strictNodeStatus(t, call(t, s.server, http.MethodGet, nodePath(recorded.ID, "/status"), asAdmin, ""))
	if status.Reachable || status.Problem == nil || *status.Problem != gen.NodeUnreachable {
		t.Errorf("status = %+v, want unreachable", status)
	}
	if status.Cores != nil || status.Ceilings != nil || status.NodeApiVersion != nil {
		t.Errorf("status = %+v, want nothing the node did not say", status)
	}
}

// A revoked node is not asked at all.
func TestARevokedNodesStatusIsNotAsked(t *testing.T) {
	s := newSolvingServer(t)
	node := &solveNode{}
	recorded, err := s.registry.Record(t.Context(), "revoked", startNode(t, node))
	if err != nil {
		t.Fatalf("record: %v", err)
	}
	if err := s.registry.Revoke(t.Context(), recorded.ID); err != nil {
		t.Fatalf("revoke: %v", err)
	}

	status := strictNodeStatus(t, call(t, s.server, http.MethodGet, nodePath(recorded.ID, "/status"), asAdmin, ""))
	if status.Reachable || status.Problem != nil || status.Cores != nil {
		t.Errorf("status = %+v, want a node that was not asked", status)
	}
	if missing := call(t, s.server, http.MethodGet, "/api/v1/nodes/999/status", asAdmin, ""); missing.Code != http.StatusNotFound {
		t.Errorf("an unknown node's status = %d, want 404", missing.Code)
	}
}

// The name is the registry's, so every job read after a rename carries it.
func TestRenamingANodeRenamesItInItsJobsAndHistory(t *testing.T) {
	s := newSolvingServer(t)
	nodeID := s.node(t)
	planID, version := s.ownedPlanWithVersion(t)
	dispatched := s.dispatch(t, planID, version, nodeID)
	if dispatched.Code != http.StatusAccepted {
		t.Fatalf("dispatch = %d: %s", dispatched.Code, dispatched.Body.String())
	}
	job := decodeJob(t, dispatched)
	waitForFinished(t, s, job.Id)

	renamed := call(t, s.server, http.MethodPatch, nodePath(nodeID, ""), asAdmin, `{"label":"  Serwer szkolny "}`)
	if renamed.Code != http.StatusOK {
		t.Fatalf("rename = %d: %s", renamed.Code, renamed.Body.String())
	}
	if node := decodeNode(t, renamed); node.Label != "Serwer szkolny" {
		t.Errorf("label = %q", node.Label)
	}

	read := decodeJob(t, call(t, s.server, http.MethodGet, "/api/v1/jobs/"+job.Id, asOwner, ""))
	if read.NodeLabel != "Serwer szkolny" {
		t.Errorf("the job's node = %q, want the new name", read.NodeLabel)
	}
	var history gen.JobPage
	if err := json.Unmarshal(call(t, s.server, http.MethodGet, nodePath(nodeID, "/jobs"), asAdmin, "").Body.Bytes(), &history); err != nil {
		t.Fatalf("decode history: %v", err)
	}
	if len(history.Items) != 1 || history.Items[0].NodeLabel != "Serwer szkolny" {
		t.Errorf("history = %+v, want the job under the new name", history.Items)
	}
}

// Karol: solves at once for a node never exceed what its pairing allows, and
// the refusal names the ceiling.
func TestALimitAboveTheNodesCeilingIsRefusedNamingIt(t *testing.T) {
	s := newSolvingServer(t)
	nodeID := s.pair(t, "node", &solveNode{status: policed(2, 12)})

	for _, one := range []struct {
		body, message, ceiling string
	}{
		{`{"solves_at_once":3}`, messages.KeyNodeSolvesAboveCeiling, "2"},
		{`{"workers_per_solve":16}`, messages.KeyNodeWorkersAboveCeiling, "12"},
	} {
		refused := call(t, s.server, http.MethodPatch, nodePath(nodeID, ""), asAdmin, one.body)
		if refused.Code != http.StatusUnprocessableEntity {
			t.Fatalf("%s = %d: %s", one.body, refused.Code, refused.Body.String())
		}
		envelope := decodeError(t, refused)
		if envelope.Message != one.message || envelope.Details == nil || (*envelope.Details)["Ceiling"] != one.ceiling {
			t.Errorf("%s = %+v, want %s naming %s", one.body, envelope, one.message, one.ceiling)
		}
	}

	accepted := call(t, s.server, http.MethodPatch, nodePath(nodeID, ""), asAdmin, `{"solves_at_once":2,"workers_per_solve":2}`)
	if accepted.Code != http.StatusOK {
		t.Fatalf("limits at the ceiling = %d: %s", accepted.Code, accepted.Body.String())
	}
	if node := decodeNode(t, accepted); node.SolvesAtOnce != 2 || node.WorkersPerSolve != 2 {
		t.Errorf("node = %+v, want 2 and 2", node)
	}

	// And the solve gets the workers per solve.
	planID, version := s.ownedPlanWithVersion(t)
	dispatched := s.dispatch(t, planID, version, nodeID)
	if dispatched.Code != http.StatusAccepted {
		t.Fatalf("dispatch = %d: %s", dispatched.Code, dispatched.Body.String())
	}
	if job := decodeJob(t, dispatched); job.NumWorkers != 2 {
		t.Errorf("workers = %d, want the node's 2", job.NumWorkers)
	}
}

// A limit cannot be checked against a node that does not say its ceilings, so
// it is not changed; a name needs nothing from the node.
func TestLimitsNeedTheNodesCeilings(t *testing.T) {
	s := newSolvingServer(t)
	silent, err := s.registry.Record(t.Context(), "silent", paired(t, silentAddress(t)))
	if err != nil {
		t.Fatalf("record: %v", err)
	}
	older := s.pair(t, "older", &solveNode{status: &arrangorpcv1.NodeStatusResponse{Cores: 8, AvailableBytes: 16 << 30}})
	revoked := s.pair(t, "revoked", &solveNode{status: policed(2, 12)})
	if err := s.registry.Revoke(t.Context(), revoked); err != nil {
		t.Fatalf("revoke: %v", err)
	}

	for _, one := range []struct {
		name    string
		id      int64
		message string
	}{
		{"a silent node", silent.ID, messages.KeyNodeUnreachable},
		{"a node that does not report its ceilings", older, messages.KeyNodeOutdated},
		{"a revoked node", revoked, messages.KeyNodeRevoked},
	} {
		refused := call(t, s.server, http.MethodPatch, nodePath(one.id, ""), asAdmin, `{"solves_at_once":1}`)
		if refused.Code != http.StatusConflict {
			t.Errorf("%s = %d: %s", one.name, refused.Code, refused.Body.String())
			continue
		}
		if envelope := decodeError(t, refused); envelope.Message != one.message {
			t.Errorf("%s: message = %q, want %q", one.name, envelope.Message, one.message)
		}
	}
	outdated := decodeError(t, call(t, s.server, http.MethodPatch, nodePath(older, ""), asAdmin, `{"workers_per_solve":2}`))
	if outdated.Details == nil || (*outdated.Details)["Capability"] != "peer_policy" {
		t.Errorf("outdated = %+v, want the capability named", outdated)
	}

	if renamed := call(t, s.server, http.MethodPatch, nodePath(silent.ID, ""), asAdmin, `{"label":"Stary"}`); renamed.Code != http.StatusOK {
		t.Errorf("renaming a silent node = %d: %s", renamed.Code, renamed.Body.String())
	}
}

func TestUpdateNodeRefusesWhatIsNotAChange(t *testing.T) {
	s := newSolvingServer(t)
	nodeID := s.pair(t, "node", &solveNode{status: policed(2, 12)})

	for _, one := range []struct{ body, field, code string }{
		{`{}`, "body", "empty"},
		{`{"label":"   "}`, "label", "required"},
		{`{"label":"` + strings.Repeat("x", 65) + `"}`, "label", "too_long"},
		{`{"solves_at_once":0}`, "solves_at_once", "invalid"},
		{`{"solves_at_once":65}`, "solves_at_once", "invalid"},
		{`{"workers_per_solve":0}`, "workers_per_solve", "invalid"},
	} {
		refused := call(t, s.server, http.MethodPatch, nodePath(nodeID, ""), asAdmin, one.body)
		if refused.Code != http.StatusUnprocessableEntity {
			t.Errorf("%s = %d: %s", one.body, refused.Code, refused.Body.String())
			continue
		}
		envelope := decodeError(t, refused)
		if envelope.Message != messages.KeyErrorValidation || envelope.Details == nil || (*envelope.Details)[one.field] != one.code {
			t.Errorf("%s = %+v, want %s: %s", one.body, envelope, one.field, one.code)
		}
	}
	if missing := call(t, s.server, http.MethodPatch, "/api/v1/nodes/999", asAdmin, `{"label":"x"}`); missing.Code != http.StatusNotFound {
		t.Errorf("an unknown node = %d, want 404", missing.Code)
	}
}

// History is what this server ran on the node, whoever started it, with the
// workers, memory and CPU time the node reported.
func TestANodesHistoryListsItsJobsWithWhatTheyCost(t *testing.T) {
	s := newSolvingServer(t)
	done := timetableFrame(arrangorpcv1.SolvePhase_SOLVE_PHASE_DONE)
	done.Config = &arrangorpcv1.SolverConfig{NumWorkers: 3}
	done.PeakRssBytes, done.CpuSeconds = 1536<<20, 42.5
	nodeID := s.pair(t, "node", &solveNode{frames: []*arrangorpcv1.SolveUpdate{
		timetableFrame(arrangorpcv1.SolvePhase_SOLVE_PHASE_SOLUTION), done,
	}})

	planID, version := s.ownedPlanWithVersion(t)
	dispatched := s.dispatch(t, planID, version, nodeID)
	if dispatched.Code != http.StatusAccepted {
		t.Fatalf("dispatch = %d: %s", dispatched.Code, dispatched.Body.String())
	}
	waitForFinished(t, s, decodeJob(t, dispatched).Id)

	theirs, _ := s.planOf(t, "stranger", "Plan liceum")
	if err := s.plans.Trash(t.Context(), theirs); err != nil {
		t.Fatalf("trash: %v", err)
	}
	if _, err := s.jobs.Create(t.Context(), arrango.NewJob{PlanID: theirs, Version: 1, NodeID: nodeID, Workers: 4,
		StartedBy: arrango.Starter{ID: "stranger"}}); err != nil {
		t.Fatalf("create job: %v", err)
	}

	listed := call(t, s.server, http.MethodGet, nodePath(nodeID, "/jobs?page=1&limit=1"), asAdmin, "")
	if listed.Code != http.StatusOK {
		t.Fatalf("history = %d: %s", listed.Code, listed.Body.String())
	}
	var first gen.JobPage
	if err := json.Unmarshal(listed.Body.Bytes(), &first); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if first.Total != 2 || len(first.Items) != 1 || first.Items[0].PlanName != "Plan liceum" {
		t.Errorf("first page = %+v, want the stranger's job in Trash, newest, of 2", first)
	}
	// The administrator owns neither project, so cancels neither solve.
	if strings.Contains(listed.Body.String(), `"can_cancel":true`) {
		t.Errorf("the history offers the administrator a cancel: %s", listed.Body.String())
	}

	var second gen.JobPage
	if err := json.Unmarshal(call(t, s.server, http.MethodGet, nodePath(nodeID, "/jobs?page=2&limit=1"), asAdmin, "").Body.Bytes(), &second); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(second.Items) != 1 {
		t.Fatalf("second page = %+v", second)
	}
	solved := second.Items[0]
	if solved.Status != gen.Done || solved.NumWorkers != 3 || solved.PlanName != "wrzesien" {
		t.Errorf("job = status %s, workers %d, plan %q", solved.Status, solved.NumWorkers, solved.PlanName)
	}
	if solved.PeakRssBytes == nil || *solved.PeakRssBytes != 1536<<20 || solved.CpuSeconds == nil || *solved.CpuSeconds != 42.5 {
		t.Errorf("cost = peak %v, cpu %v", solved.PeakRssBytes, solved.CpuSeconds)
	}

	if missing := call(t, s.server, http.MethodGet, "/api/v1/nodes/999/jobs", asAdmin, ""); missing.Code != http.StatusNotFound {
		t.Errorf("an unknown node's history = %d, want 404", missing.Code)
	}
}

// Revoking a node stops its running solve first, keeping the timetable it had,
// and answers only once the job is closed.
func TestRevokingANodeStopsItsSolvesBeforeItAnswers(t *testing.T) {
	s := newSolvingServer(t)
	_, job, _ := solvingWithATimetable(t, s)
	running, err := s.jobs.Get(t.Context(), job)
	if err != nil {
		t.Fatalf("get: %v", err)
	}

	revoked := call(t, s.server, http.MethodDelete, nodePath(running.NodeID, ""), asAdmin, "")
	if revoked.Code != http.StatusNoContent {
		t.Fatalf("revoke = %d: %s", revoked.Code, revoked.Body.String())
	}
	closed, err := s.jobs.Get(t.Context(), job)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if closed.Status != arrango.StatusCancelled || closed.ResultID == nil {
		t.Errorf("job = %s, result %v, want cancelled with its timetable kept", closed.Status, closed.ResultID)
	}
	node := decodeNode(t, call(t, s.server, http.MethodGet, nodePath(running.NodeID, ""), asAdmin, ""))
	if node.Active {
		t.Error("the node is still active")
	}
}

// Each refusal carries its own key.
func TestPairingRefusalsSayWhy(t *testing.T) {
	s := newSolvingServer(t)
	unreachable := fmt.Sprintf("arrango://%s#fp=%s&t=ABCD-EFGH-IJKL-MNOP",
		silentAddress(t), paired(t, "127.0.0.1:1").Fingerprint)

	for _, one := range []struct{ uri, message string }{
		{"arrango://nothing", messages.KeyNodeNotALink},
		{"not even a link", messages.KeyNodeNotALink},
		{unreachable, messages.KeyNodeUnreachable},
	} {
		body, _ := json.Marshal(map[string]string{"uri": one.uri})
		refused := call(t, s.server, http.MethodPost, "/api/v1/nodes", asAdmin, string(body))
		if refused.Code != http.StatusUnprocessableEntity {
			t.Errorf("%q = %d: %s", one.uri, refused.Code, refused.Body.String())
			continue
		}
		if envelope := decodeError(t, refused); envelope.Message != one.message {
			t.Errorf("%q: message = %q, want %q", one.uri, envelope.Message, one.message)
		}
	}
	listed := call(t, s.server, http.MethodGet, "/api/v1/nodes", asAdmin, "")
	var nodes gen.NodeList
	if err := json.Unmarshal(listed.Body.Bytes(), &nodes); err != nil || len(nodes.Items) != 0 {
		t.Errorf("nodes = %+v, %v, want nothing added", nodes.Items, err)
	}
}

// Pairing reads the node's ceilings, and pairing it again keeps its id, its
// name and its limits.
func TestPairingAgainKeepsTheNodesNameAndLimits(t *testing.T) {
	s := newSolvingServer(t)
	paired := startNode(t, &solveNode{status: policed(2, 12)})
	uri, _ := json.Marshal(map[string]string{
		"uri": fmt.Sprintf("arrango://%s#fp=%s&t=ABCD-EFGH-IJKL-MNOP", paired.Address, paired.Fingerprint),
	})

	created := call(t, s.server, http.MethodPost, "/api/v1/nodes", asAdmin, string(uri))
	if created.Code != http.StatusCreated {
		t.Fatalf("pair = %d: %s", created.Code, created.Body.String())
	}
	var first gen.NodePaired
	if err := json.Unmarshal(created.Body.Bytes(), &first); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if first.Node.Label != "node" || first.Node.SolvesAtOnce != 2 || first.Node.WorkersPerSolve != int(solveclient.DefaultWorkers) {
		t.Errorf("new node = %+v, want the default name and the node's 2 solves at once", first.Node)
	}

	if renamed := call(t, s.server, http.MethodPatch, "/api/v1/nodes/"+first.Node.Id, asAdmin,
		`{"label":"Serwer szkolny","solves_at_once":1}`); renamed.Code != http.StatusOK {
		t.Fatalf("rename = %d: %s", renamed.Code, renamed.Body.String())
	}
	if revoked := call(t, s.server, http.MethodDelete, "/api/v1/nodes/"+first.Node.Id, asAdmin, ""); revoked.Code != http.StatusNoContent {
		t.Fatalf("revoke = %d: %s", revoked.Code, revoked.Body.String())
	}

	again := call(t, s.server, http.MethodPost, "/api/v1/nodes", asAdmin, string(uri))
	if again.Code != http.StatusCreated {
		t.Fatalf("pair again = %d: %s", again.Code, again.Body.String())
	}
	var second gen.NodePaired
	if err := json.Unmarshal(again.Body.Bytes(), &second); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if second.Node.Id != first.Node.Id || second.Node.Label != "Serwer szkolny" ||
		second.Node.SolvesAtOnce != 1 || !second.Node.Active {
		t.Errorf("paired again = %+v, want the same id, name and limits, trusted again", second.Node)
	}
}

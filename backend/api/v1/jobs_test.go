// api/v1/jobs_test.go

package v1_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/smegg99/s99ab/auth/kratosauth"
	"github.com/smegg99/s99ab/auth/kratosauth/kratosauthtest"
	"github.com/smegg99/s99ab/data/db"
	"github.com/smegg99/s99ab/data/storage"
	"github.com/smegg99/s99ab/net/sse"

	arrangov1 "github.com/smegg99/arrango/backend/gen/arrangov1"

	apiv1 "github.com/smegg99/goptivum/backend/api/v1"
	"github.com/smegg99/goptivum/backend/api/v1/gen"
	"github.com/smegg99/goptivum/backend/arrango"
	"github.com/smegg99/goptivum/backend/common/messages"
	"github.com/smegg99/goptivum/backend/core/checks"
	"github.com/smegg99/goptivum/backend/core/documents"
	"github.com/smegg99/goptivum/backend/core/library"
	"github.com/smegg99/goptivum/backend/core/plans"
	"github.com/smegg99/goptivum/backend/core/profiles"
	"github.com/smegg99/goptivum/backend/core/results"
	"github.com/smegg99/goptivum/backend/migrations"
	"github.com/smegg99/goptivum/document"
	"github.com/smegg99/goptivum/solveclient"
)

// asOwner is the planner who owns the plan; asStranger is somebody else with a
// perfectly good session.
const (
	asOwner    = "Bearer owner-token"
	asStranger = "Bearer stranger-token"
)

type solving struct {
	server   *apiv1.Server
	runner   *arrango.Runner
	registry *arrango.Registry
	jobs     *arrango.Jobs
	plans    *plans.Repository
	store    *documents.Store
}

func newSolvingServer(t *testing.T) solving {
	t.Helper()
	return newSolvingServerWith(t, nil)
}

// newSolvingServerWith calls beforeStore ahead of each timetable a solve
// stores, so a test can hold one back while a delete waits for it.
func newSolvingServerWith(t *testing.T, beforeStore func()) solving {
	t.Helper()

	conn := db.TestDB(t, migrations.FS)
	registry := arrango.NewRegistry(conn, db.SQLite)
	jobRows := arrango.NewJobs(conn, db.SQLite)
	index := plans.NewRepository(conn, db.SQLite, plans.Caps{BackupsKeep: 100, ResultsKeep: 20})
	store := documents.NewStore(storage.NewLocalDir(t.TempDir(), "/objects"))
	ruleProfiles := profiles.NewRepository(conn, db.SQLite)

	self, err := solveclient.LoadIdentity(t.TempDir())
	if err != nil {
		t.Fatalf("identity: %v", err)
	}
	dialer, err := solveclient.NewDialer(self, "0.1.0")
	if err != nil {
		t.Fatalf("dialer: %v", err)
	}
	// The server's own persist and live check, so a cancelled solve stores
	// the timetable it had and a delete's ordering is what is under test.
	keep := results.Persist(index, store)
	persist := func(ctx context.Context, job arrango.Job, found arrango.Result) (int64, error) {
		if beforeStore != nil {
			beforeStore()
		}
		return keep(ctx, job, found)
	}
	dispatcher, err := arrango.NewDispatcher(dialer, registry, jobRows, persist, index.Live)
	if err != nil {
		t.Fatalf("dispatcher: %v", err)
	}
	runner := arrango.NewRunner(dispatcher, jobRows)
	t.Cleanup(func() { _ = runner.Close() })

	fake := kratosauthtest.Fake(t, map[string]kratosauth.Identity{
		asOwner:    {ID: "owner", Traits: json.RawMessage(`{"username":"owner","name":"Anna Kowalska"}`)},
		asStranger: {ID: "stranger", Traits: json.RawMessage(`{"username":"stranger"}`)},
		asAdmin:    {ID: "the-admin", MetadataPublic: json.RawMessage(`{"role":"admin"}`)},
	})
	// The same index, store and runner as the solving half, so a delete sees
	// the solves it has to stop and a result's check reads what a solve kept.
	api := planAPIOver(t, index, store, runner)
	server, err := apiv1.New(apiv1.Options{
		Build:            apiv1.BuildInfo{Version: "1.2.3"},
		MinClientVersion: "0.1.0",
		Handlers: &apiv1.Handlers{
			API: api,
			Admin: apiv1.NewAdmin(nil, apiv1.Nodes{
				Registry: registry, Dialer: dialer, Runner: runner, Jobs: jobRows, Plans: index,
			}),
			Jobs:      apiv1.NewJobs(index, store, runner, jobRows, ruleProfiles),
			Checks:    apiv1.NewChecks(api, checks.New(arrango.NewChecker(registry, dialer).Check, checks.Deadline)),
			Profiles:  apiv1.NewProfiles(ruleProfiles),
			Libraries: apiv1.NewLibraries(library.NewRepository(conn, db.SQLite, index)),
		},
		Kratos: fake,
	})
	if err != nil {
		t.Fatalf("new server: %v", err)
	}
	return solving{server: server, runner: runner, registry: registry,
		jobs: jobRows, plans: index, store: store}
}

// ownedPlanWithVersion stores a tiny school so a dispatch has something real to
// load and send.
func (s solving) ownedPlanWithVersion(t *testing.T) (int64, int64) {
	t.Helper()
	return s.planOf(t, "owner", "wrzesien")
}

// planOf stores the tiny school as version 1 of a plan of one owner.
func (s solving) planOf(t *testing.T, owner, name string) (int64, int64) {
	t.Helper()
	ctx := t.Context()

	encoded, err := document.Encode(document.New(lessonModel(2)))
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	key, err := s.store.Put(ctx, encoded)
	if err != nil {
		t.Fatalf("put: %v", err)
	}
	plan, version, err := s.plans.Create(ctx, plans.Plan{
		OwnerIdentityID: owner, Name: name, SchoolYear: "2026/2027",
	}, plans.Upload{ObjectKey: key, SizeBytes: encoded.Size, CreatedBy: owner})
	if err != nil {
		t.Fatalf("create plan: %v", err)
	}
	return plan.ID, version.Version
}

// node pairs a node whose solves end at once with nothing, so a dispatch is
// accepted and its job fails.
func (s solving) node(t *testing.T) int64 {
	t.Helper()
	return s.pair(t, "node", &solveNode{})
}

// pair records a node served by this fake.
func (s solving) pair(t *testing.T, label string, node *solveNode) int64 {
	t.Helper()
	recorded, err := s.registry.Record(t.Context(), label, startNode(t, node))
	if err != nil {
		t.Fatalf("record: %v", err)
	}
	return recorded.ID
}

// dispatch asks for a from-scratch solve of a version, on a node when one is
// named, and returns the answer.
func (s solving) dispatch(t *testing.T, planID, version, nodeID int64) *httptest.ResponseRecorder {
	t.Helper()
	node := ""
	if nodeID != 0 {
		node = fmt.Sprintf(`"node_id":"%d",`, nodeID)
	}
	body := fmt.Sprintf(`{%s"plan_version":%d,"ignore_previous_placements":true}`, node, version)
	return call(t, s.server, http.MethodPost, fmt.Sprintf("/api/v1/plans/%d/jobs", planID), asOwner, body)
}

func decodeJob(t *testing.T, recorder *httptest.ResponseRecorder) gen.Job {
	t.Helper()
	var job gen.Job
	if err := json.Unmarshal(recorder.Body.Bytes(), &job); err != nil {
		t.Fatalf("decode %s: %v", recorder.Body.String(), err)
	}
	return job
}

func TestDispatchAnswersWithTheJobAndNotTheSchedule(t *testing.T) {
	solving := newSolvingServer(t)
	planID, version := solving.ownedPlanWithVersion(t)
	nodeID := solving.node(t)

	body := fmt.Sprintf(`{"node_id":"%d","plan_version":%d,"total_time_seconds":20,"num_workers":0,"ignore_previous_placements":true}`,
		nodeID, version)
	recorder := call(t, solving.server, http.MethodPost,
		fmt.Sprintf("/api/v1/plans/%d/jobs", planID), asOwner, body)
	if recorder.Code != http.StatusAccepted {
		t.Fatalf("dispatch = %d: %s", recorder.Code, recorder.Body.String())
	}

	var job gen.Job
	if err := json.Unmarshal(recorder.Body.Bytes(), &job); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if job.Status != gen.Pending && job.Status != gen.Running {
		t.Errorf("status = %q, want a job that is under way", job.Status)
	}
	// The clamp already happened: a solve is never dispatched with zero.
	if job.NumWorkers == 0 {
		t.Error("the job was accepted with zero workers, which means all cores")
	}
	if job.ResultId != nil {
		t.Error("a job that has not run yet names a result")
	}
	if job.StartedBy.Id != "owner" || job.StartedBy.Name != "Anna Kowalska" ||
		job.NodeLabel != "node" || job.PlanName != "wrzesien" {
		t.Errorf("job = %+v", job)
	}
	if job.PlanVersion != version {
		t.Errorf("plan_version = %d, want %d", job.PlanVersion, version)
	}

	// The authority answers the same job.
	read := call(t, solving.server, http.MethodGet, "/api/v1/jobs/"+job.Id, asOwner, "")
	if read.Code != http.StatusOK {
		t.Fatalf("get = %d: %s", read.Code, read.Body.String())
	}

	listed := call(t, solving.server, http.MethodGet,
		fmt.Sprintf("/api/v1/plans/%d/jobs", planID), asOwner, "")
	if listed.Code != http.StatusOK {
		t.Fatalf("list = %d: %s", listed.Code, listed.Body.String())
	}
	var list gen.JobList
	if err := json.Unmarshal(listed.Body.Bytes(), &list); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(list.Items) != 1 {
		t.Errorf("the plan lists %d jobs, want 1", len(list.Items))
	}
}

// Everyone reads another owner's jobs; only the owner starts and cancels them.
func TestAnotherAccountReadsJobsButNeitherStartsNorCancelsThem(t *testing.T) {
	solving := newSolvingServer(t)
	planID, version := solving.ownedPlanWithVersion(t)
	nodeID := solving.node(t)

	body := fmt.Sprintf(`{"node_id":"%d","plan_version":%d,"ignore_previous_placements":true}`, nodeID, version)
	dispatched := call(t, solving.server, http.MethodPost,
		fmt.Sprintf("/api/v1/plans/%d/jobs", planID), asOwner, body)
	if dispatched.Code != http.StatusAccepted {
		t.Fatalf("dispatch = %d: %s", dispatched.Code, dispatched.Body.String())
	}
	var job gen.Job
	if err := json.Unmarshal(dispatched.Body.Bytes(), &job); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !job.CanCancel {
		t.Errorf("the owner may not cancel %+v", job)
	}

	for _, path := range []string{
		fmt.Sprintf("/api/v1/plans/%d/jobs", planID),
		"/api/v1/jobs/" + job.Id,
		"/api/v1/jobs/" + job.Id + "/events",
	} {
		recorder := call(t, solving.server, http.MethodGet, path, asStranger, "")
		if recorder.Code != http.StatusOK {
			t.Errorf("GET %s as a stranger = %d, want 200", path, recorder.Code)
		}
		if strings.Contains(recorder.Body.String(), `"can_cancel":true`) {
			t.Errorf("GET %s tells a stranger it may cancel: %s", path, recorder.Body.String())
		}
	}
	for _, one := range []struct{ method, path, body string }{
		{http.MethodDelete, "/api/v1/jobs/" + job.Id, ""},
		{http.MethodPost, fmt.Sprintf("/api/v1/plans/%d/jobs", planID), body},
	} {
		recorder := call(t, solving.server, one.method, one.path, asStranger, one.body)
		if recorder.Code != http.StatusForbidden || envelope(t, recorder).Message != messages.KeyPlansNotYours {
			t.Errorf("%s %s as a stranger = %d %s, want 403 plans.notYours", one.method, one.path,
				recorder.Code, recorder.Body.String())
		}
	}
	listed := call(t, solving.server, http.MethodGet, fmt.Sprintf("/api/v1/plans/%d/jobs", planID), asOwner, "")
	var list gen.JobList
	if err := json.Unmarshal(listed.Body.Bytes(), &list); err != nil || len(list.Items) != 1 {
		t.Fatalf("the plan's jobs = %s, want the owner's one", listed.Body.String())
	}
}

func TestDispatchToARevokedNodeIsAConflict(t *testing.T) {
	solving := newSolvingServer(t)
	planID, version := solving.ownedPlanWithVersion(t)
	nodeID := solving.node(t)
	if err := solving.registry.Revoke(t.Context(), nodeID); err != nil {
		t.Fatalf("revoke: %v", err)
	}

	body := fmt.Sprintf(`{"node_id":"%d","plan_version":%d,"ignore_previous_placements":true}`, nodeID, version)
	recorder := call(t, solving.server, http.MethodPost,
		fmt.Sprintf("/api/v1/plans/%d/jobs", planID), asOwner, body)
	if recorder.Code != http.StatusConflict {
		t.Errorf("status = %d, want 409 for a node that cannot take it: %s",
			recorder.Code, recorder.Body.String())
	}
}

func TestDispatchOnAVersionThatDoesNotExistIsNotFound(t *testing.T) {
	solving := newSolvingServer(t)
	planID, _ := solving.ownedPlanWithVersion(t)
	nodeID := solving.node(t)

	body := fmt.Sprintf(`{"node_id":"%d","plan_version":99,"ignore_previous_placements":true}`, nodeID)
	recorder := call(t, solving.server, http.MethodPost,
		fmt.Sprintf("/api/v1/plans/%d/jobs", planID), asOwner, body)
	if recorder.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404: %s", recorder.Code, recorder.Body.String())
	}
}

// A late subscriber must settle rather than hang on a stream that will never
// speak, so a finished job answers with one terminal frame and closes.
func TestWatchingAFinishedJobAnswersOneTerminalFrame(t *testing.T) {
	solving := newSolvingServer(t)
	planID, version := solving.ownedPlanWithVersion(t)
	nodeID := solving.node(t)

	body := fmt.Sprintf(`{"node_id":"%d","plan_version":%d,"ignore_previous_placements":true}`, nodeID, version)
	dispatched := call(t, solving.server, http.MethodPost,
		fmt.Sprintf("/api/v1/plans/%d/jobs", planID), asOwner, body)
	var job gen.Job
	if err := json.Unmarshal(dispatched.Body.Bytes(), &job); err != nil {
		t.Fatalf("decode: %v", err)
	}
	waitForFinished(t, solving, job.Id)

	request := httptest.NewRequest(http.MethodGet, "/api/v1/jobs/"+job.Id+"/events", nil)
	request.Header.Set("Authorization", asOwner)
	recorder := httptest.NewRecorder()
	solving.server.Handler().ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("watch = %d: %s", recorder.Code, recorder.Body.String())
	}
	if media := recorder.Header().Get("Content-Type"); !strings.HasPrefix(media, "text/event-stream") {
		t.Errorf("content type = %q, want text/event-stream", media)
	}
	stream := recorder.Body.String()
	if !strings.Contains(stream, `"terminal":true`) {
		t.Errorf("the stream carried no terminal frame: %s", stream)
	}
	// The row's status goes in job_status. status carries the solver's enum and
	// nothing else, so a client never has to guess which vocabulary it is
	// reading.
	if !strings.Contains(stream, `"job_status":"failed"`) {
		t.Errorf("the terminal frame does not carry the job's own status: %s", stream)
	}
	if strings.Contains(stream, `"status":"failed"`) {
		t.Errorf("the job's status leaked into the solver's field: %s", stream)
	}
}

func TestJobRoutesNeedASession(t *testing.T) {
	solving := newSolvingServer(t)
	for _, one := range []struct{ method, path string }{
		{http.MethodGet, "/api/v1/plans/1/jobs"},
		{http.MethodPost, "/api/v1/plans/1/jobs"},
		{http.MethodGet, "/api/v1/jobs/1"},
		{http.MethodDelete, "/api/v1/jobs/1"},
		{http.MethodGet, "/api/v1/jobs/1/events"},
	} {
		recorder := call(t, solving.server, one.method, one.path, "", "")
		if recorder.Code != http.StatusUnauthorized {
			t.Errorf("%s %s anonymous = %d, want 401", one.method, one.path, recorder.Code)
		}
	}
}

// waitForFinished polls the authority, which is what a client does too.
func waitForFinished(t *testing.T, s solving, id string) {
	t.Helper()
	for attempt := 0; attempt < 400; attempt++ {
		recorder := call(t, s.server, http.MethodGet, "/api/v1/jobs/"+id, asOwner, "")
		var job gen.Job
		if err := json.Unmarshal(recorder.Body.Bytes(), &job); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if job.FinishedAt != nil {
			return
		}
		waitABit()
	}
	t.Fatal("the job never finished")
}

// waitABit is one poll interval, short enough that a fast solve is not waited
// on and long enough that the loop is not a spin.
func waitABit() { time.Sleep(25 * time.Millisecond) }

// lessonModel is a school small enough to encode and big enough to dispatch.
// Stored without a timetable, an improve of it is refused unless a test gives
// it one.
func lessonModel(count int) *arrangov1.SchoolModel {
	model := &arrangov1.SchoolModel{Name: "test"}
	for index := range count {
		model.Lessons = append(model.Lessons, &arrangov1.LessonInstance{
			Id: uint32(index + 1), Duration: 1,
		})
	}
	return model
}

// A solve takes minutes and Server.WriteTimeout defaults to thirty seconds.
//
// net/http arms the write deadline when the request headers are read, so
// unless the handler clears it the stream dies mid-solve with nothing said.
// This runs over a real listener because an httptest recorder has no
// connection and therefore no deadline to expose.
func TestTheEventStreamOutlivesTheServersWriteTimeout(t *testing.T) {
	solving := newSolvingServer(t)
	planID, version := solving.ownedPlanWithVersion(t)
	// Released never, so the solve runs until it is cancelled.
	nodeID := solving.pair(t, "slow", &solveNode{release: make(chan struct{})})

	dispatched := solving.dispatch(t, planID, version, nodeID)
	if dispatched.Code != http.StatusAccepted {
		t.Fatalf("dispatch = %d: %s", dispatched.Code, dispatched.Body.String())
	}
	var job gen.Job
	if err := json.Unmarshal(dispatched.Body.Bytes(), &job); err != nil {
		t.Fatalf("decode: %v", err)
	}
	id, err := strconv.ParseInt(job.Id, 10, 64)
	if err != nil {
		t.Fatalf("job id: %v", err)
	}
	broker, live := solving.runner.Broker(id)
	if !live {
		t.Fatal("the solve is not running, so there is no live stream to test")
	}

	const writeTimeout = 600 * time.Millisecond
	server := httptest.NewUnstartedServer(solving.server.Handler())
	server.Config.WriteTimeout = writeTimeout
	server.Start()
	defer server.Close()

	request, err := http.NewRequestWithContext(t.Context(), http.MethodGet,
		server.URL+"/api/v1/jobs/"+job.Id+"/events", nil)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	request.Header.Set("Authorization", asOwner)
	response, err := server.Client().Do(request)
	if err != nil {
		t.Fatalf("watch: %v", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("watch = %d", response.StatusCode)
	}

	decoder := sse.NewDecoder(response.Body)
	read := func(what string) {
		t.Helper()
		done := make(chan error, 1)
		go func() {
			_, err := decoder.Decode()
			done <- err
		}()
		select {
		case err := <-done:
			if err != nil {
				t.Fatalf("%s: %v", what, err)
			}
		case <-time.After(5 * time.Second):
			t.Fatalf("%s: nothing arrived", what)
		}
	}

	publish := func() {
		// Handler subscribes after the response starts, so publish until one
		// frame lands rather than racing it.
		for range 50 {
			if broker.Count() > 0 {
				break
			}
			time.Sleep(10 * time.Millisecond)
		}
		broker.Publish(sse.Event{Type: "update", Data: arrango.Update{JobID: job.Id}})
	}

	publish()
	read("the first frame")

	// Past the deadline the server would have armed. A frame after this point
	// is the whole point of the test.
	time.Sleep(writeTimeout + 300*time.Millisecond)
	publish()
	read("a frame written after the write timeout would have fired")
}

// Deleting a project stops its running solve before it answers, so the node is
// not left carrying work for a project nobody can see.
func TestDeletingAPlanStopsItsSolveFirst(t *testing.T) {
	solving := newSolvingServer(t)
	planID, version := solving.ownedPlanWithVersion(t)
	// Released never, so the solve runs until it is cancelled.
	nodeID := solving.pair(t, "slow", &solveNode{release: make(chan struct{})})

	dispatched := solving.dispatch(t, planID, version, nodeID)
	if dispatched.Code != http.StatusAccepted {
		t.Fatalf("dispatch = %d: %s", dispatched.Code, dispatched.Body.String())
	}
	var job gen.Job
	if err := json.Unmarshal(dispatched.Body.Bytes(), &job); err != nil {
		t.Fatalf("decode: %v", err)
	}
	id, err := strconv.ParseInt(job.Id, 10, 64)
	if err != nil {
		t.Fatalf("job id: %v", err)
	}
	if solving.runner.Running() != 1 {
		t.Fatalf("%d solves running, want the one just dispatched", solving.runner.Running())
	}

	deleted := call(t, solving.server, http.MethodDelete, fmt.Sprintf("/api/v1/plans/%d", planID), asOwner, "")
	if deleted.Code != http.StatusNoContent {
		t.Fatalf("delete = %d: %s", deleted.Code, deleted.Body.String())
	}
	// No polling: the row is closed by the time delete answers.
	closed, err := solving.jobs.Get(t.Context(), id)
	if err != nil {
		t.Fatalf("get job: %v", err)
	}
	if closed.FinishedAt == nil {
		t.Errorf("the job is still open after delete answered: %+v", closed)
	}
	if solving.runner.Running() != 0 {
		t.Errorf("%d solves still running after delete", solving.runner.Running())
	}
	// The plan is in Trash, so its job is not found either.
	if read := call(t, solving.server, http.MethodGet, "/api/v1/jobs/"+job.Id, asOwner, ""); read.Code != http.StatusNotFound {
		t.Errorf("the job of a trashed plan answered %d", read.Code)
	}
}

// Without a node named, the server takes the first node that is free and
// answers, and the job says which.
func TestASolveWithoutANodeGoesToOneThatCanTakeIt(t *testing.T) {
	solving := newSolvingServer(t)
	planID, version := solving.ownedPlanWithVersion(t)
	if _, err := solving.registry.Record(t.Context(), "silent", paired(t, "127.0.0.1:1")); err != nil {
		t.Fatalf("record: %v", err)
	}
	time.Sleep(2 * time.Millisecond)
	chosen := solving.pair(t, "answering", &solveNode{release: make(chan struct{})})

	recorder := solving.dispatch(t, planID, version, 0)
	if recorder.Code != http.StatusAccepted {
		t.Fatalf("dispatch = %d: %s", recorder.Code, recorder.Body.String())
	}
	job := decodeJob(t, recorder)
	if job.NodeId != strconv.FormatInt(chosen, 10) || job.NodeLabel != "answering" {
		t.Errorf("the solve went to %s (%q), want the node that answers", job.NodeId, job.NodeLabel)
	}
}

// When no node can take a solve, the answer is 409 solve.nodeUnavailable and
// the cause says why, so the planner learns it at once.
func TestASolveNoNodeCanTakeIsAConflictThatSaysWhy(t *testing.T) {
	solving := newSolvingServer(t)
	planID, version := solving.ownedPlanWithVersion(t)
	expect := func(what string, nodeID int64, cause string) {
		t.Helper()
		recorder := solving.dispatch(t, planID, version, nodeID)
		if recorder.Code != http.StatusConflict {
			t.Fatalf("%s: dispatch = %d: %s", what, recorder.Code, recorder.Body.String())
		}
		body := envelope(t, recorder)
		if body.Message != messages.KeySolveNodeUnavailable || body.Details == nil || (*body.Details)["cause"] != cause {
			t.Errorf("%s: envelope = %+v, want cause %s", what, body, cause)
		}
	}

	expect("no node paired", 0, "none")
	silent, err := solving.registry.Record(t.Context(), "silent", paired(t, "127.0.0.1:1"))
	if err != nil {
		t.Fatalf("record: %v", err)
	}
	expect("the named node silent", silent.ID, "unreachable")
	expect("the only node silent", 0, "unreachable")
	if err := solving.registry.Revoke(t.Context(), silent.ID); err != nil {
		t.Fatalf("revoke: %v", err)
	}
	expect("the named node revoked", silent.ID, "none")

	busy := solving.pair(t, "busy", &solveNode{release: make(chan struct{})})
	for range 2 {
		if _, err := solving.jobs.Create(t.Context(), arrango.NewJob{
			PlanID: planID, Version: version, NodeID: busy, Workers: 4, StartedBy: arrango.Starter{ID: "owner"},
		}); err != nil {
			t.Fatalf("fill the node: %v", err)
		}
	}
	expect("the named node busy", busy, "busy")
	expect("the only node busy", 0, "busy")
}

// GET /jobs lists the school's solves across projects, newest first, each
// named by its project, with a status filter and pages. A trashed project's
// are not there, and only the caller's own say they can be cancelled.
func TestJobsAcrossProjectsAreEveryonesNewestFirst(t *testing.T) {
	solving := newSolvingServer(t)
	ctx := t.Context()
	nodeID := solving.node(t)
	september, _ := solving.planOf(t, "owner", "wrzesien")
	february, _ := solving.planOf(t, "owner", "luty")
	theirs, _ := solving.planOf(t, "stranger", "cudzy")
	trashed, _ := solving.planOf(t, "owner", "w koszu")

	open := func(planID int64, owner string, status string) arrango.Job {
		t.Helper()
		// Newest first is by creation time, to the microsecond.
		time.Sleep(2 * time.Millisecond)
		job, err := solving.jobs.Create(ctx, arrango.NewJob{
			PlanID: planID, Version: 1, NodeID: nodeID, Workers: 4, StartedBy: arrango.Starter{ID: owner},
		})
		if err != nil {
			t.Fatalf("create job: %v", err)
		}
		if status != arrango.StatusPending {
			if err := solving.jobs.Finish(ctx, job.ID, arrango.Outcome{Status: status}); err != nil {
				t.Fatalf("finish: %v", err)
			}
		}
		return job
	}
	done := open(september, "owner", arrango.StatusDone)
	failed := open(september, "owner", arrango.StatusFailed)
	pending := open(february, "owner", arrango.StatusPending)
	strangers := open(theirs, "stranger", arrango.StatusPending)
	open(trashed, "owner", arrango.StatusPending)
	if err := solving.plans.Trash(ctx, trashed); err != nil {
		t.Fatalf("trash: %v", err)
	}

	page := func(path, credential string) gen.JobPage {
		t.Helper()
		recorder := call(t, solving.server, http.MethodGet, path, credential, "")
		if recorder.Code != http.StatusOK {
			t.Fatalf("%s = %d: %s", path, recorder.Code, recorder.Body.String())
		}
		var listed gen.JobPage
		decoder := json.NewDecoder(strings.NewReader(recorder.Body.String()))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&listed); err != nil {
			t.Fatalf("decode %s: %v", recorder.Body.String(), err)
		}
		return listed
	}
	ids := func(listed gen.JobPage) []string {
		out := []string{}
		for _, job := range listed.Items {
			out = append(out, job.Id)
		}
		return out
	}
	id := func(job arrango.Job) string { return strconv.FormatInt(job.ID, 10) }

	all := page("/api/v1/jobs", asOwner)
	if got, want := ids(all), []string{id(strangers), id(pending), id(failed), id(done)}; strings.Join(got, ",") != strings.Join(want, ",") || all.Total != 4 {
		t.Errorf("jobs = %v of %d, want %v", got, all.Total, want)
	}
	if all.Items[0].PlanName != "cudzy" || all.Items[1].PlanName != "luty" || all.Items[3].PlanName != "wrzesien" || all.Items[1].NodeLabel != "node" {
		t.Errorf("names = %q, %q, %q on %q", all.Items[0].PlanName, all.Items[1].PlanName, all.Items[3].PlanName, all.Items[1].NodeLabel)
	}
	if all.Items[0].CanCancel || !all.Items[1].CanCancel || !all.Items[3].CanCancel || all.Items[0].StartedBy.Id != "stranger" {
		t.Errorf("the owner's view = %+v", all.Items)
	}
	active := page("/api/v1/jobs?status=pending&status=running", asOwner)
	if got := ids(active); len(got) != 2 || got[0] != id(strangers) || got[1] != id(pending) || active.Total != 2 {
		t.Errorf("active jobs = %v of %d, want the two pending ones", got, active.Total)
	}
	second := page("/api/v1/jobs?limit=1&page=2", asOwner)
	if got := ids(second); len(got) != 1 || got[0] != id(pending) || second.Total != 4 || second.Page != 2 || second.Limit != 1 {
		t.Errorf("page 2 = %v of %d (page %d, limit %d)", got, second.Total, second.Page, second.Limit)
	}
	seen := page("/api/v1/jobs", asStranger)
	if len(seen.Items) != 4 || !seen.Items[0].CanCancel || seen.Items[1].CanCancel {
		t.Errorf("the stranger sees %+v", seen.Items)
	}
	if recorder := call(t, solving.server, http.MethodGet, "/api/v1/jobs?status=nonsense", asOwner, ""); recorder.Code != http.StatusBadRequest {
		t.Errorf("an unknown status answered %d", recorder.Code)
	}
}

func strictPreview(t *testing.T, recorder *httptest.ResponseRecorder) gen.JobPreview {
	t.Helper()
	if recorder.Code != http.StatusOK {
		t.Fatalf("preview = %d: %s", recorder.Code, recorder.Body.String())
	}
	var preview gen.JobPreview
	decoder := json.NewDecoder(strings.NewReader(recorder.Body.String()))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&preview); err != nil {
		t.Fatalf("decode %s: %v", recorder.Body.String(), err)
	}
	return preview
}

// The preview is the best timetable a running solve sent, to everyone, and
// gone once the job ends.
func TestThePreviewShowsTheBestTimetableWhileTheSolveRuns(t *testing.T) {
	s := newSolvingServer(t)
	_, job, _ := solvingWithATimetable(t, s)
	path := fmt.Sprintf("/api/v1/jobs/%d/preview", job)

	preview := strictPreview(t, call(t, s.server, http.MethodGet, path, asOwner, ""))
	if preview.JobId != strconv.FormatInt(job, 10) || preview.PlanVersion != 1 || preview.Received != 1 ||
		preview.Quality == nil || *preview.Quality != 88 || preview.Verdict == nil || *preview.Verdict != "TIER_WARNINGS" {
		t.Errorf("preview = %+v", preview)
	}
	if preview.Lessons == nil || len(*preview.Lessons) != 1 {
		t.Fatalf("lessons = %v, want the one placed", preview.Lessons)
	}
	if lesson := (*preview.Lessons)[0]; lesson.LessonId != 1 || lesson.DayId != 1 || lesson.StartPeriod != 1 || lesson.RoomId != nil {
		t.Errorf("lesson = %+v", lesson)
	}
	if other := strictPreview(t, call(t, s.server, http.MethodGet, path, asStranger, "")); other.Received != preview.Received {
		t.Errorf("the stranger's preview = %+v, want the owner's", other)
	}

	if recorder := call(t, s.server, http.MethodDelete, fmt.Sprintf("/api/v1/jobs/%d", job), asOwner, ""); recorder.Code != http.StatusAccepted {
		t.Fatalf("cancel = %d", recorder.Code)
	}
	waitForFinished(t, s, strconv.FormatInt(job, 10))
	for attempt := 0; ; attempt++ {
		recorder := call(t, s.server, http.MethodGet, path, asOwner, "")
		if recorder.Code == http.StatusNotFound {
			break
		}
		if attempt == 200 {
			t.Fatalf("the preview of a finished job answered %d", recorder.Code)
		}
		waitABit()
	}
}

// Before the first timetable the preview answers with nothing received.
func TestThePreviewBeforeTheFirstTimetableIsEmpty(t *testing.T) {
	solving := newSolvingServer(t)
	planID, version := solving.ownedPlanWithVersion(t)
	nodeID := solving.pair(t, "slow", &solveNode{release: make(chan struct{})})
	job := decodeJob(t, solving.dispatch(t, planID, version, nodeID))

	preview := strictPreview(t, call(t, solving.server, http.MethodGet, "/api/v1/jobs/"+job.Id+"/preview", asOwner, ""))
	if preview.Received != 0 || preview.Lessons != nil || preview.Quality != nil {
		t.Errorf("preview = %+v, want nothing received", preview)
	}
}

// A client watching a running solve over a real connection sees it end.
func TestWatchingARunningSolveEndsWithATerminalFrame(t *testing.T) {
	solving := newSolvingServer(t)
	planID, version := solving.ownedPlanWithVersion(t)
	nodeID := solving.pair(t, "slow", &solveNode{release: make(chan struct{})})
	job := decodeJob(t, solving.dispatch(t, planID, version, nodeID))
	id, err := strconv.ParseInt(job.Id, 10, 64)
	if err != nil {
		t.Fatalf("job id: %v", err)
	}

	server := httptest.NewServer(solving.server.Handler())
	defer server.Close()
	request, err := http.NewRequestWithContext(t.Context(), http.MethodGet, server.URL+"/api/v1/jobs/"+job.Id+"/events", nil)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	request.Header.Set("Authorization", asOwner)
	response, err := server.Client().Do(request)
	if err != nil {
		t.Fatalf("watch: %v", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("watch = %d", response.StatusCode)
	}
	broker, live := solving.runner.Broker(id)
	if !live {
		t.Fatal("the solve is not running")
	}
	for attempt := 0; broker.Count() == 0; attempt++ {
		if attempt == 200 {
			t.Fatal("the stream never subscribed")
		}
		waitABit()
	}
	solving.runner.Cancel(id)

	decoder := sse.NewDecoder(response.Body)
	for {
		event, err := decoder.Decode()
		if err != nil {
			t.Fatalf("the stream ended without a terminal frame: %v", err)
		}
		var update arrango.Update
		if err := event.Unmarshal(&update); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if update.Terminal {
			if update.JobStatus != arrango.StatusCancelled {
				t.Errorf("terminal frame = %+v, want the job cancelled", update)
			}
			return
		}
	}
}

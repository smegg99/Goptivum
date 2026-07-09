// api/api_test.go

package api

import (
	"archive/zip"
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"mime/multipart"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"

	arrangov1 "github.com/smegg99/arrango/backend/gen/arrangov1"
	"github.com/smegg99/arrango/backend/jobs"
)

// stubSolver emits a fixed STARTED/SOLUTION/DONE sequence; Solve blocks
// before DONE when hold is set (for cancellation tests).
type stubSolver struct {
	arrangov1.UnimplementedSolverServiceServer
	hold chan struct{}
}

func (s *stubSolver) GetDemoSchool(_ context.Context,
	req *arrangov1.DemoRequest) (*arrangov1.SchoolModel, error) {
	return &arrangov1.SchoolModel{
		Name: "stub",
		Days: []*arrangov1.Day{{Id: 1, Name: "Pon", PeriodCount: 6}},
	}, nil
}

func (s *stubSolver) Validate(_ context.Context,
	req *arrangov1.ValidateRequest) (*arrangov1.ValidationReport, error) {
	return &arrangov1.ValidationReport{Valid: true}, nil
}

func (s *stubSolver) Solve(req *arrangov1.SolveRequest,
	stream grpc.ServerStreamingServer[arrangov1.SolveUpdate]) error {
	if err := stream.Send(&arrangov1.SolveUpdate{
		Phase: arrangov1.SolvePhase_SOLVE_PHASE_STARTED,
	}); err != nil {
		return err
	}
	if err := stream.Send(&arrangov1.SolveUpdate{
		Phase:     arrangov1.SolvePhase_SOLVE_PHASE_SOLUTION,
		Objective: 42,
	}); err != nil {
		return err
	}
	if s.hold != nil {
		select {
		case <-s.hold:
		case <-stream.Context().Done():
			return stream.Context().Err()
		}
	}
	return stream.Send(&arrangov1.SolveUpdate{
		Phase:  arrangov1.SolvePhase_SOLVE_PHASE_DONE,
		Status: arrangov1.SolveStatus_SOLVE_STATUS_OPTIMAL,
	})
}

func newTestServer(t *testing.T, stub *stubSolver) *httptest.Server {
	t.Helper()
	gin.SetMode(gin.TestMode)

	listener := bufconn.Listen(1 << 20)
	grpcServer := grpc.NewServer()
	arrangov1.RegisterSolverServiceServer(grpcServer, stub)
	go grpcServer.Serve(listener)
	t.Cleanup(grpcServer.Stop)

	conn, err := grpc.NewClient("passthrough:///bufconn",
		grpc.WithContextDialer(func(ctx context.Context,
			_ string) (net.Conn, error) {
			return listener.DialContext(ctx)
		}),
		grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })

	server := NewServer(arrangov1.NewSolverServiceClient(conn),
		jobs.NewRegistry())
	ts := httptest.NewServer(server.Router())
	t.Cleanup(ts.Close)
	return ts
}

func TestGetSchoolProxies(t *testing.T) {
	ts := newTestServer(t, &stubSolver{})
	resp, err := http.Get(ts.URL + "/api/school?preset=DEMO_PRESET_PRODUCTION")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var model map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&model); err != nil {
		t.Fatal(err)
	}
	if model["name"] != "stub" {
		t.Fatalf("name = %v", model["name"])
	}
}

func startJob(t *testing.T, ts *httptest.Server) string {
	t.Helper()
	resp, err := http.Post(ts.URL+"/api/jobs", "application/json",
		bytes.NewBufferString(`{"preset":"DEMO_PRESET_TINY","seed":1}`))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var body struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body.ID == "" {
		t.Fatal("no job id")
	}
	return body.ID
}

// Reads SSE events until `done` or timeout; returns event names seen.
func readEvents(t *testing.T, url string) []string {
	t.Helper()
	client := http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var events []string
	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "event: ") {
			events = append(events, strings.TrimPrefix(line, "event: "))
			if events[len(events)-1] == "done" {
				break
			}
		}
	}
	return events
}

func TestJobLifecycleStreamsUpdates(t *testing.T) {
	ts := newTestServer(t, &stubSolver{})
	id := startJob(t, ts)

	events := readEvents(t, ts.URL+"/api/jobs/"+id+"/events")
	if len(events) < 2 || events[len(events)-1] != "done" {
		t.Fatalf("events = %v", events)
	}

	resp, err := http.Get(ts.URL + "/api/jobs/" + id)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var status struct {
		State string `json:"state"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&status); err != nil {
		t.Fatal(err)
	}
	if status.State != string(jobs.StateDone) {
		t.Fatalf("state = %q", status.State)
	}
}

func TestCancelEndsRunningJob(t *testing.T) {
	stub := &stubSolver{hold: make(chan struct{})}
	ts := newTestServer(t, stub)
	id := startJob(t, ts)

	// Give the runner a moment to receive the first updates.
	time.Sleep(100 * time.Millisecond)
	resp, err := http.Post(ts.URL+"/api/jobs/"+id+"/cancel",
		"application/json", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var body struct {
		State string `json:"state"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body.State != string(jobs.StateCancelled) {
		t.Fatalf("state = %q", body.State)
	}
}

func TestImportEndpointParsesZip(t *testing.T) {
	ts := newTestServer(t, &stubSolver{})

	var zipBuf bytes.Buffer
	zw := zip.NewWriter(&zipBuf)
	f, err := zw.Create("plany/o1.html")
	if err != nil {
		t.Fatal(err)
	}
	f.Write([]byte(`<html><body>
<span class="tytulnapis">1a technik</span>
<table class="tabela">
<tr><th>Nr</th><th>Godz</th><th>Pon</th><th>Wt</th></tr>
<tr><td class="nr">1</td><td class="g">7:00- 7:45</td>
<td class="l"><span class="p">mat</span></td>
<td class="l">&nbsp;</td></tr>
</table></body></html>`))
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}

	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	fw, err := mw.CreateFormFile("archive", "plan.zip")
	if err != nil {
		t.Fatal(err)
	}
	fw.Write(zipBuf.Bytes())
	mw.Close()

	resp, err := http.Post(ts.URL+"/api/import", mw.FormDataContentType(), &body)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	var out struct {
		Summary struct {
			Divisions int `json:"divisions"`
			Lessons   int `json:"lessons"`
		} `json:"summary"`
		Validation struct {
			Valid bool `json:"valid"`
		} `json:"validation"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	if out.Summary.Divisions != 1 || out.Summary.Lessons != 1 {
		t.Fatalf("summary = %+v", out.Summary)
	}
	if !out.Validation.Valid {
		t.Fatal("stub validation should be valid")
	}
}

func TestExportEndpointReturnsZip(t *testing.T) {
	ts := newTestServer(t, &stubSolver{})
	body := `{"model":{"name":"x",
		"days":[{"id":1,"name":"Pon","periodCount":2}],
		"periods":[{"id":5,"name":"1"},{"id":6,"name":"2"}],
		"divisions":[{"id":2,"name":"1a","yearId":3}],
		"years":[{"id":3,"name":"Rok 1","level":1,"priority":300}],
		"subjects":[{"id":4,"name":"mat"}],
		"lessons":[{"id":7,"participants":[{"divisionId":2}],"subjectId":4,
			"duration":1,"requiresTeacher":false,"requiresRoom":false}]},
		"snapshot":{"lessons":[{"lessonId":7,
			"placement":{"dayId":1,"startPeriod":0,"roomId":0}}]}}`
	resp, err := http.Post(ts.URL+"/api/export", "application/json",
		bytes.NewBufferString(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "application/zip" {
		t.Fatalf("content type = %q", ct)
	}
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if len(data) < 4 || data[0] != 'P' || data[1] != 'K' {
		t.Fatal("not a zip payload")
	}
}

func TestSimpleSolveLifecycle(t *testing.T) {
	ts := newTestServer(t, &stubSolver{})
	body := `{"periodsPerDay":6,"dayCount":2,
		"divisions":[{"name":"1A","year":1}],
		"teachers":["T"],"rooms":["42"],
		"lessons":[{"division":"1A","subject":"mat","teacher":"T","room":"42","count":2}],
		"timeLimitSeconds":5}`
	resp, err := http.Post(ts.URL+"/api/simple/solve", "application/json",
		bytes.NewBufferString(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	var started struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&started); err != nil {
		t.Fatal(err)
	}

	// Stub finishes immediately; poll once after a beat.
	time.Sleep(200 * time.Millisecond)
	statusResp, err := http.Get(ts.URL + "/api/simple/jobs/" + started.ID)
	if err != nil {
		t.Fatal(err)
	}
	defer statusResp.Body.Close()
	var out struct {
		State  string `json:"state"`
		Result struct {
			Phase  string `json:"phase"`
			Status string `json:"status"`
		} `json:"result"`
	}
	if err := json.NewDecoder(statusResp.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	if out.State != "done" || out.Result.Phase != "DONE" ||
		out.Result.Status != "OPTIMAL" {
		t.Fatalf("out = %+v", out)
	}
}

func TestSimpleSolveRejectsUnknownNames(t *testing.T) {
	ts := newTestServer(t, &stubSolver{})
	body := `{"periodsPerDay":6,
		"divisions":[{"name":"1A"}],
		"lessons":[{"division":"1A","subject":"mat","teacher":"Ghost"}]}`
	resp, err := http.Post(ts.URL+"/api/simple/solve", "application/json",
		bytes.NewBufferString(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422", resp.StatusCode)
	}
}

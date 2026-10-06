// api/v1/improve_test.go

package v1_test

import (
	"encoding/json"
	"fmt"
	"maps"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"google.golang.org/protobuf/proto"

	arrangorpcv1 "github.com/smegg99/arrango/backend/gen/arrangorpcv1"
	arrangov1 "github.com/smegg99/arrango/backend/gen/arrangov1"

	"github.com/smegg99/goptivum/backend/api/v1/gen"
	"github.com/smegg99/goptivum/backend/core/plans"
	"github.com/smegg99/goptivum/document"
)

func cell(day, period, room uint32) *arrangov1.Placement {
	return &arrangov1.Placement{DayId: day, StartPeriod: period, RoomId: room}
}

// fullTimetable places both lessons of lessonModel(2).
func fullTimetable(first, second *arrangov1.Placement) *arrangov1.ScheduleSnapshot {
	return &arrangov1.ScheduleSnapshot{Lessons: []*arrangov1.ScheduledLesson{
		{LessonId: 1, Placement: first}, {LessonId: 2, Placement: second},
	}}
}

// fullFrame is a final frame whose timetable places both lessons.
func fullFrame(first, second *arrangov1.Placement) *arrangorpcv1.SolveUpdate {
	frame := timetableFrame(arrangorpcv1.SolvePhase_SOLVE_PHASE_DONE)
	frame.Snapshot = fullTimetable(first, second)
	return frame
}

// planWith stores a document as version 1 of a new plan of the owner.
func (s solving) planWith(t *testing.T, model *arrangov1.SchoolModel, schedule *arrangov1.ScheduleSnapshot) int64 {
	t.Helper()
	doc := document.New(model)
	doc.Schedule = schedule
	encoded, err := document.Encode(doc)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	key, err := s.store.Put(t.Context(), encoded)
	if err != nil {
		t.Fatalf("put: %v", err)
	}
	plan, _, err := s.plans.Create(t.Context(), plans.Plan{
		OwnerIdentityID: "owner", Name: "Plan 2027", SchoolYear: "2026/2027",
	}, plans.Upload{ObjectKey: key, SizeBytes: encoded.Size, CreatedBy: "owner"})
	if err != nil {
		t.Fatalf("create plan: %v", err)
	}
	return plan.ID
}

// solve posts a createJob body for a plan.
func (s solving) solve(t *testing.T, planID int64, body string) *httptest.ResponseRecorder {
	t.Helper()
	return call(t, s.server, http.MethodPost, fmt.Sprintf("/api/v1/plans/%d/jobs", planID), asOwner, body)
}

// startsFrom checks that a solve request carries each lesson's previous
// placement as want says, and asks for an improve.
func startsFrom(t *testing.T, request *arrangorpcv1.SolveRequest, want map[uint32]*arrangov1.Placement) {
	t.Helper()
	if request.GetConfig().GetIgnorePreviousPlacements() {
		t.Error("an improve was sent as a fresh solve")
	}
	for _, lesson := range request.GetModel().GetLessons() {
		if !lesson.GetHasPrevious() || !proto.Equal(lesson.GetPreviousPlacement(), want[lesson.GetId()]) {
			t.Errorf("lesson %d starts from %v, want %v", lesson.GetId(), lesson.GetPreviousPlacement(), want[lesson.GetId()])
		}
	}
}

// An improve of a version starts from that version's own timetable, not from
// what an importer once left on its lessons, and the stored school is left as
// it was.
func TestAnImproveStartsFromTheVersionsTimetable(t *testing.T) {
	s := newSolvingServer(t)
	model := lessonModel(2)
	model.Lessons[0].HasPrevious, model.Lessons[0].PreviousPlacement = true, cell(5, 6, 0)
	planID := s.planWith(t, model, fullTimetable(cell(1, 1, 0), cell(2, 3, 0)))
	node := &solveNode{frames: []*arrangorpcv1.SolveUpdate{fullFrame(cell(1, 2, 0), cell(2, 3, 0))}}
	nodeID := s.pair(t, "solving", node)

	dispatched := s.solve(t, planID, fmt.Sprintf(`{"node_id":"%d","plan_version":1,"ignore_previous_placements":false}`, nodeID))
	if dispatched.Code != http.StatusAccepted {
		t.Fatalf("dispatch = %d: %s", dispatched.Code, dispatched.Body.String())
	}
	job := decodeJob(t, dispatched)
	if job.SourceResultId != nil || job.PlanVersion != 1 {
		t.Errorf("job = %+v, want version 1 as its source", job)
	}
	waitForFinished(t, s, job.Id)

	solves := node.solves()
	if len(solves) != 1 {
		t.Fatalf("the node was asked %d solves, want 1", len(solves))
	}
	startsFrom(t, solves[0], map[uint32]*arrangov1.Placement{1: cell(1, 1, 0), 2: cell(2, 3, 0)})

	finished := decodeJob(t, call(t, s.server, http.MethodGet, "/api/v1/jobs/"+job.Id, asOwner, ""))
	if finished.ResultId == nil {
		t.Fatalf("job = %+v, want a result", finished)
	}
	resultID, _ := strconv.ParseInt(*finished.ResultId, 10, 64)
	kept, err := s.plans.Result(t.Context(), planID, resultID)
	if err != nil {
		t.Fatalf("result: %v", err)
	}
	doc, err := s.store.Load(t.Context(), kept.ObjectKey)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if !proto.Equal(doc.GetModel(), model) {
		t.Errorf("the result's school is %v, want the version's as stored", doc.GetModel())
	}
}

// A file whose timetable does not place every lesson cannot be improved on,
// and nothing reaches the node.
func TestAnImproveOfAFileWithoutAFullTimetableIsRefused(t *testing.T) {
	imported := lessonModel(2)
	for _, lesson := range imported.Lessons {
		lesson.HasPrevious, lesson.PreviousPlacement = true, cell(1, lesson.Id, 0)
	}
	cases := []struct {
		name     string
		model    *arrangov1.SchoolModel
		schedule *arrangov1.ScheduleSnapshot
		unplaced string
	}{
		{"one lesson unplaced", lessonModel(2), &arrangov1.ScheduleSnapshot{Lessons: []*arrangov1.ScheduledLesson{
			{LessonId: 1, Placement: cell(1, 1, 0)},
		}}, "1"},
		{"no timetable", lessonModel(2), nil, "2"},
		{"an importer's placements but no timetable", imported, nil, "2"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := newSolvingServer(t)
			planID := s.planWith(t, tc.model, tc.schedule)
			node := &solveNode{}
			nodeID := s.pair(t, "solving", node)

			refused := s.solve(t, planID, fmt.Sprintf(`{"node_id":"%d","plan_version":1,"ignore_previous_placements":false}`, nodeID))
			if refused.Code != http.StatusUnprocessableEntity {
				t.Fatalf("status = %d: %s", refused.Code, refused.Body.String())
			}
			var envelope gen.Error
			if err := json.Unmarshal(refused.Body.Bytes(), &envelope); err != nil {
				t.Fatalf("decode: %v", err)
			}
			if envelope.Message != "solve.incompleteBaseline" || envelope.Details == nil ||
				(*envelope.Details)["Unplaced"] != tc.unplaced || (*envelope.Details)["Lessons"] != "2" {
				t.Errorf("refusal = %+v, want %s of 2 unplaced", envelope, tc.unplaced)
			}
			if solves := node.solves(); len(solves) != 0 {
				t.Errorf("the node was asked %d solves", len(solves))
			}
		})
	}
}

// An improve of a timetable that breaks rules that must hold is refused with
// how many conflicts the node found and each kind once, and nothing is solved.
func TestAnImproveOfATimetableTheNodeFindsInvalidIsRefusedWithItsCountAndKinds(t *testing.T) {
	s := newSolvingServer(t)
	planID := s.planWith(t, lessonModel(2), fullTimetable(cell(1, 1, 0), cell(2, 3, 0)))
	node := &solveNode{validation: &arrangov1.ValidationReport{Conflicts: []*arrangov1.Conflict{
		{Kind: arrangov1.Conflict_KIND_EXTERNAL_BLOCK_OVERLAP, LessonIds: []uint32{1}},
		{Kind: arrangov1.Conflict_KIND_TEACHER_OVERLOADED, EntityId: 7},
		{Kind: arrangov1.Conflict_KIND_EXTERNAL_BLOCK_OVERLAP, LessonIds: []uint32{2}},
	}}}
	nodeID := s.pair(t, "solving", node)

	refused := s.solve(t, planID, fmt.Sprintf(`{"node_id":"%d","plan_version":1,"ignore_previous_placements":false}`, nodeID))
	if refused.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d: %s", refused.Code, refused.Body.String())
	}
	var envelope gen.Error
	if err := json.Unmarshal(refused.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode: %v", err)
	}
	want := map[string]string{"Count": "3", "conflicts": "KIND_EXTERNAL_BLOCK_OVERLAP,KIND_TEACHER_OVERLOADED"}
	if envelope.Code != "validation" || envelope.Message != "solve.modelInvalid" || envelope.Details == nil ||
		!maps.Equal(*envelope.Details, want) {
		t.Errorf("refusal = %+v (details %v), want solve.modelInvalid with %v", envelope, envelope.Details, want)
	}
	if solves := node.solves(); len(solves) != 0 {
		t.Errorf("the node was asked %d solves", len(solves))
	}
}

// An improve of a result reads the result's own document: its school, which
// the main file may since have changed, and its timetable. The job names the
// result, and so does its preview.
func TestAnImproveOfAResultStartsFromTheResultsTimetable(t *testing.T) {
	s := newSolvingServer(t)
	planID := s.planWith(t, lessonModel(2), nil)
	first := s.pair(t, "first", &solveNode{frames: []*arrangorpcv1.SolveUpdate{fullFrame(cell(1, 1, 0), cell(2, 3, 0))}})
	fresh := decodeJob(t, s.solve(t, planID, fmt.Sprintf(`{"node_id":"%d","plan_version":1,"ignore_previous_placements":true}`, first)))
	waitForFinished(t, s, fresh.Id)
	solved := decodeJob(t, call(t, s.server, http.MethodGet, "/api/v1/jobs/"+fresh.Id, asOwner, ""))
	if solved.ResultId == nil {
		t.Fatalf("job = %+v, want a result", solved)
	}
	source := *solved.ResultId

	// The main file moves on: the improve still reads the result's school.
	edited := lessonModel(2)
	edited.Name = "edited"
	encoded, err := document.Encode(document.New(edited))
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	key, err := s.store.Put(t.Context(), encoded)
	if err != nil {
		t.Fatalf("put: %v", err)
	}
	if _, err := s.plans.AddVersion(t.Context(), plans.Upload{
		PlanID: planID, Parent: 1, ObjectKey: key, SizeBytes: encoded.Size, CreatedBy: "owner",
	}); err != nil {
		t.Fatalf("edit: %v", err)
	}

	node := &solveNode{
		release: make(chan struct{}),
		frames:  []*arrangorpcv1.SolveUpdate{timetableFrame(arrangorpcv1.SolvePhase_SOLVE_PHASE_SOLUTION)},
		hold:    true,
	}
	nodeID := s.pair(t, "second", node)
	dispatched := s.solve(t, planID, fmt.Sprintf(`{"node_id":"%d","result_id":"%s","ignore_previous_placements":false}`, nodeID, source))
	if dispatched.Code != http.StatusAccepted {
		t.Fatalf("dispatch = %d: %s", dispatched.Code, dispatched.Body.String())
	}
	job := decodeJob(t, dispatched)
	if job.SourceResultId == nil || *job.SourceResultId != source || job.PlanVersion != 1 {
		t.Errorf("job = %+v, want result %s of version 1 as its source", job, source)
	}
	read := decodeJob(t, call(t, s.server, http.MethodGet, "/api/v1/jobs/"+job.Id, asOwner, ""))
	if read.SourceResultId == nil || *read.SourceResultId != source {
		t.Errorf("getJob = %+v, want result %s as its source", read, source)
	}
	preview := strictPreview(t, call(t, s.server, http.MethodGet, "/api/v1/jobs/"+job.Id+"/preview", asOwner, ""))
	if preview.SourceResultId == nil || *preview.SourceResultId != source || preview.PlanVersion != 1 {
		t.Errorf("preview = %+v, want result %s of version 1 as its source", preview, source)
	}

	// The solve reaches the node from its own goroutine.
	solves := node.solves()
	for attempt := 0; len(solves) == 0 && attempt < 400; attempt++ {
		waitABit()
		solves = node.solves()
	}
	if len(solves) != 1 {
		t.Fatalf("the node was asked %d solves, want 1", len(solves))
	}
	if name := solves[0].GetModel().GetName(); name != "test" {
		t.Errorf("the solve read school %q, want the result's", name)
	}
	startsFrom(t, solves[0], map[uint32]*arrangov1.Placement{1: cell(1, 1, 0), 2: cell(2, 3, 0)})

	close(node.release)
	if recorder := call(t, s.server, http.MethodDelete, "/api/v1/jobs/"+job.Id, asOwner, ""); recorder.Code != http.StatusAccepted {
		t.Fatalf("cancel = %d", recorder.Code)
	}
	waitForFinished(t, s, job.Id)
}

// A solve names exactly one file, and one the plan has out of Trash.
func TestASolveNamesExactlyOneFileThePlanHas(t *testing.T) {
	s := newSolvingServer(t)
	planID, job := s.solved(t)
	otherPlan, _ := s.ownedPlanWithVersion(t)
	nodeID := s.node(t)
	result := *job.ResultId

	for _, tc := range []struct {
		name, body, field, code string
	}{
		{"neither", `{"ignore_previous_placements":true}`, "plan_version", "required"},
		{"both", fmt.Sprintf(`{"plan_version":1,"result_id":"%s","ignore_previous_placements":true}`, result), "result_id", "exclusive"},
	} {
		recorder := s.solve(t, planID, fmt.Sprintf(`{"node_id":"%d",%s`, nodeID, tc.body[1:]))
		if recorder.Code != http.StatusUnprocessableEntity {
			t.Errorf("%s: status = %d: %s", tc.name, recorder.Code, recorder.Body.String())
			continue
		}
		var envelope gen.Error
		if err := json.Unmarshal(recorder.Body.Bytes(), &envelope); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if envelope.Details == nil || (*envelope.Details)[tc.field] != tc.code {
			t.Errorf("%s: details = %v, want %s: %s", tc.name, envelope.Details, tc.field, tc.code)
		}
	}

	notFound := func(what string, plan int64, id string) {
		t.Helper()
		body := fmt.Sprintf(`{"node_id":"%d","result_id":"%s","ignore_previous_placements":true}`, nodeID, id)
		if recorder := s.solve(t, plan, body); recorder.Code != http.StatusNotFound {
			t.Errorf("%s: status = %d, want 404: %s", what, recorder.Code, recorder.Body.String())
		}
	}
	notFound("an unknown result", planID, "99")
	notFound("another plan's result", otherPlan, result)
	if recorder := call(t, s.server, http.MethodDelete,
		fmt.Sprintf("/api/v1/plans/%d/results/%s", planID, result), asOwner, ""); recorder.Code != http.StatusNoContent {
		t.Fatalf("delete result = %d: %s", recorder.Code, recorder.Body.String())
	}
	notFound("a result in Trash", planID, result)
}

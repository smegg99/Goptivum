// api/v1/results_test.go

package v1_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	arrangorpcv1 "github.com/smegg99/arrango/backend/gen/arrangorpcv1"

	"github.com/smegg99/goptivum/backend/api/v1/gen"
	"github.com/smegg99/goptivum/document"
)

// solved runs one solve of a new plan to a timetable and returns the plan and
// the finished job.
func (s solving) solved(t *testing.T) (int64, gen.Job) {
	t.Helper()
	planID, version := s.ownedPlanWithVersion(t)
	return planID, s.solveAgain(t, planID, version)
}

// solveAgain runs one more solve of a plan's version to a timetable.
func (s solving) solveAgain(t *testing.T, planID, version int64) gen.Job {
	t.Helper()
	nodeID := s.pair(t, "solving", &solveNode{frames: []*arrangorpcv1.SolveUpdate{
		timetableFrame(arrangorpcv1.SolvePhase_SOLVE_PHASE_DONE),
	}})
	dispatched := s.dispatch(t, planID, version, nodeID)
	if dispatched.Code != http.StatusAccepted {
		t.Fatalf("dispatch = %d: %s", dispatched.Code, dispatched.Body.String())
	}
	job := decodeJob(t, dispatched)
	waitForFinished(t, s, job.Id)
	return decodeJob(t, call(t, s.server, http.MethodGet, "/api/v1/jobs/"+job.Id, asOwner, ""))
}

func strictResults(t *testing.T, recorder *httptest.ResponseRecorder) []gen.SolveResult {
	t.Helper()
	if recorder.Code != http.StatusOK {
		t.Fatalf("results = %d: %s", recorder.Code, recorder.Body.String())
	}
	var listed gen.SolveResultList
	decoder := json.NewDecoder(strings.NewReader(recorder.Body.String()))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&listed); err != nil {
		t.Fatalf("decode %s: %v", recorder.Body.String(), err)
	}
	return listed.Items
}

func strictResult(t *testing.T, recorder *httptest.ResponseRecorder) gen.SolveResult {
	t.Helper()
	if recorder.Code != http.StatusOK {
		t.Fatalf("result = %d: %s", recorder.Code, recorder.Body.String())
	}
	var result gen.SolveResult
	decoder := json.NewDecoder(strings.NewReader(recorder.Body.String()))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&result); err != nil {
		t.Fatalf("decode %s: %v", recorder.Body.String(), err)
	}
	return result
}

// A solve's timetable is a result of its plan: listed with where it came from,
// downloadable as a whole document, and never a new version of the main file.
func TestASolvedTimetableIsAResultOfItsPlan(t *testing.T) {
	s := newSolvingServer(t)
	planID, job := s.solved(t)
	if job.Status != gen.Done || job.ResultId == nil || job.Reason != nil {
		t.Fatalf("job = %+v, want done with a result", job)
	}

	items := strictResults(t, call(t, s.server, http.MethodGet, fmt.Sprintf("/api/v1/plans/%d/results", planID), asOwner, ""))
	if len(items) != 1 {
		t.Fatalf("results = %+v, want one", items)
	}
	result := items[0]
	if result.Id != *job.ResultId || result.JobId == nil || *result.JobId != job.Id || result.Device != nil || result.SourceVersion != 1 ||
		result.StartedBy.Id != "owner" || result.StartedBy.Name != "Anna Kowalska" ||
		result.Quality == nil || *result.Quality != 88 || result.Verdict == nil || *result.Verdict != "TIER_WARNINGS" ||
		result.Pinned || result.Name != nil || result.SizeBytes <= 0 {
		t.Errorf("result = %+v", result)
	}

	versions := call(t, s.server, http.MethodGet, fmt.Sprintf("/api/v1/plans/%d/versions", planID), asOwner, "")
	var timeline gen.VersionList
	if err := json.Unmarshal(versions.Body.Bytes(), &timeline); err != nil || len(timeline.Items) != 1 {
		t.Errorf("versions = %s, want only version 1", versions.Body.String())
	}

	downloaded := call(t, s.server, http.MethodGet, fmt.Sprintf("/api/v1/plans/%d/results/%s", planID, result.Id), asOwner, "")
	if downloaded.Code != http.StatusOK || downloaded.Header().Get("Content-Type") != "application/zstd" {
		t.Fatalf("download = %d %q", downloaded.Code, downloaded.Header().Get("Content-Type"))
	}
	encoded, err := document.FromBytes(downloaded.Body.Bytes())
	if err != nil {
		t.Fatalf("the download is not a document: %v", err)
	}
	doc, err := document.Decode(encoded.Bytes)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(doc.GetSchedule().GetLessons()) != 1 || len(doc.GetModel().GetLessons()) != 2 {
		t.Errorf("the result holds %d placements of %d lessons", len(doc.GetSchedule().GetLessons()), len(doc.GetModel().GetLessons()))
	}

	// Everyone reads a project's results; only its owner changes them.
	base := fmt.Sprintf("/api/v1/plans/%d/results/%s", planID, result.Id)
	for _, one := range []struct {
		method, path, body string
		want               int
	}{
		{http.MethodGet, fmt.Sprintf("/api/v1/plans/%d/results", planID), "", http.StatusOK},
		{http.MethodGet, base, "", http.StatusOK},
		{http.MethodPatch, base, `{"pinned":true}`, http.StatusForbidden},
		{http.MethodDelete, base, "", http.StatusForbidden},
		{http.MethodPost, base + "/restore", "", http.StatusForbidden},
	} {
		if recorder := call(t, s.server, one.method, one.path, asStranger, one.body); recorder.Code != one.want {
			t.Errorf("%s %s as a stranger = %d, want %d", one.method, one.path, recorder.Code, one.want)
		}
	}
	for _, one := range []struct{ method, path, body string }{
		{http.MethodGet, fmt.Sprintf("/api/v1/plans/%d/results", planID), ""},
		{http.MethodGet, base, ""},
		{http.MethodPatch, base, `{"pinned":true}`},
		{http.MethodDelete, base, ""},
		{http.MethodPost, base + "/restore", ""},
		{http.MethodGet, base + "/check", ""},
	} {
		if recorder := call(t, s.server, one.method, one.path, "", one.body); recorder.Code != http.StatusUnauthorized {
			t.Errorf("%s %s anonymous = %d, want 401", one.method, one.path, recorder.Code)
		}
	}
	if listed := call(t, s.server, http.MethodGet, fmt.Sprintf("/api/v1/plans/%d/results", planID), asOwner, ""); !strings.Contains(listed.Body.String(), `"pinned":false`) {
		t.Errorf("a stranger's refused pin changed the result: %s", listed.Body.String())
	}
}

// A result is named, pinned, deleted to Trash, listed there, and restored.
func TestAResultIsNamedPinnedDeletedAndRestored(t *testing.T) {
	s := newSolvingServer(t)
	planID, job := s.solved(t)
	base := fmt.Sprintf("/api/v1/plans/%d/results/%s", planID, *job.ResultId)

	named := strictResult(t, call(t, s.server, http.MethodPatch, base, asOwner, `{"name":"  Wersja z piątkiem  ","pinned":true}`))
	if named.Name == nil || *named.Name != "Wersja z piątkiem" || !named.Pinned {
		t.Errorf("named = %+v", named)
	}
	unnamed := strictResult(t, call(t, s.server, http.MethodPatch, base, asOwner, `{"name":""}`))
	if unnamed.Name != nil || !unnamed.Pinned {
		t.Errorf("unnamed = %+v, want no name and still pinned", unnamed)
	}
	long := call(t, s.server, http.MethodPatch, base, asOwner, fmt.Sprintf(`{"name":%q}`, strings.Repeat("ż", 201)))
	if long.Code != http.StatusUnprocessableEntity {
		t.Fatalf("a long name answered %d: %s", long.Code, long.Body.String())
	}
	if body := envelope(t, long); body.Details == nil || (*body.Details)["name"] != "too_long" {
		t.Errorf("envelope = %+v", body)
	}

	if recorder := call(t, s.server, http.MethodDelete, base, asOwner, ""); recorder.Code != http.StatusNoContent {
		t.Fatalf("delete = %d: %s", recorder.Code, recorder.Body.String())
	}
	if items := strictResults(t, call(t, s.server, http.MethodGet, fmt.Sprintf("/api/v1/plans/%d/results", planID), asOwner, "")); len(items) != 0 {
		t.Errorf("results after delete = %+v", items)
	}
	for _, one := range []struct{ method, path string }{
		{http.MethodDelete, base}, {http.MethodGet, base}, {http.MethodGet, base + "/check"},
	} {
		if recorder := call(t, s.server, one.method, one.path, asOwner, ""); recorder.Code != http.StatusNotFound {
			t.Errorf("%s %s of a result in Trash = %d, want 404", one.method, one.path, recorder.Code)
		}
	}

	trash := call(t, s.server, http.MethodGet, "/api/v1/trash", asOwner, "")
	var listed gen.TrashList
	decoder := json.NewDecoder(strings.NewReader(trash.Body.String()))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&listed); err != nil {
		t.Fatalf("trash %s: %v", trash.Body.String(), err)
	}
	if len(listed.Items) != 1 || listed.Items[0].Kind != gen.TrashItemKindResult || listed.Items[0].PlanId != strconv.FormatInt(planID, 10) ||
		listed.Items[0].Name != "wrzesien" || listed.Items[0].Result == nil || listed.Items[0].Result.Id != *job.ResultId {
		t.Fatalf("trash = %+v", listed.Items)
	}

	restored := strictResult(t, call(t, s.server, http.MethodPost, base+"/restore", asOwner, ""))
	if restored.Id != *job.ResultId || !restored.Pinned {
		t.Errorf("restored = %+v", restored)
	}
	strictResult(t, call(t, s.server, http.MethodPost, base+"/restore", asOwner, ""))
	if items := strictResults(t, call(t, s.server, http.MethodGet, fmt.Sprintf("/api/v1/plans/%d/results", planID), asOwner, "")); len(items) != 1 {
		t.Errorf("results after restore = %+v", items)
	}
}

// A result is checked from its own document, its timetable included, and the
// answer names the result.
func TestAResultIsCheckedFromItsOwnDocument(t *testing.T) {
	s := newSolvingServer(t)
	planID, version := s.ownedPlanWithVersion(t)
	node := &solveNode{frames: []*arrangorpcv1.SolveUpdate{timetableFrame(arrangorpcv1.SolvePhase_SOLVE_PHASE_DONE)}}
	nodeID := s.pair(t, "solving", node)
	job := decodeJob(t, s.dispatch(t, planID, version, nodeID))
	waitForFinished(t, s, job.Id)
	job = decodeJob(t, call(t, s.server, http.MethodGet, "/api/v1/jobs/"+job.Id, asOwner, ""))
	if job.ResultId == nil {
		t.Fatalf("job = %+v, want a result", job)
	}

	recorder := call(t, s.server, http.MethodGet, fmt.Sprintf("/api/v1/plans/%d/results/%s/check", planID, *job.ResultId), asOwner, "")
	if recorder.Code != http.StatusOK {
		t.Fatalf("check = %d: %s", recorder.Code, recorder.Body.String())
	}
	check := strictCheck(t, recorder.Body.Bytes())
	if check.ResultId == nil || *check.ResultId != *job.ResultId || check.Version != version {
		t.Errorf("check = %+v", check)
	}
	asked := node.checks()
	if len(asked) != 1 || len(asked[0].GetSnapshot().GetLessons()) != 1 {
		t.Errorf("the node checked %d requests, want one with the result's timetable", len(asked))
	}
}

// A plan's results cap is set through the plan and applied at the next result.
func TestAPlansResultsCapIsSetAndApplied(t *testing.T) {
	s := newSolvingServer(t)
	planID, version := s.ownedPlanWithVersion(t)
	path := fmt.Sprintf("/api/v1/plans/%d", planID)

	updated := call(t, s.server, http.MethodPatch, path, asOwner, `{"results_keep":1}`)
	var plan gen.Plan
	if err := json.Unmarshal(updated.Body.Bytes(), &plan); err != nil || updated.Code != http.StatusOK ||
		plan.ResultsKeep != 1 || plan.ResultsKeepDefault != 20 {
		t.Fatalf("update = %d %s", updated.Code, updated.Body.String())
	}
	refused := call(t, s.server, http.MethodPatch, path, asOwner, `{"results_keep":-1}`)
	if refused.Code != http.StatusUnprocessableEntity {
		t.Fatalf("a negative cap answered %d", refused.Code)
	}

	s.solveAgain(t, planID, version)
	newest := s.solveAgain(t, planID, version)
	items := strictResults(t, call(t, s.server, http.MethodGet, fmt.Sprintf("/api/v1/plans/%d/results", planID), asOwner, ""))
	if len(items) != 1 || items[0].Id != *newest.ResultId {
		t.Errorf("results = %+v, want only the newest", items)
	}
}

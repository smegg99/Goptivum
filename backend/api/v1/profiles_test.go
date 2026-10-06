// api/v1/profiles_test.go

package v1_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"google.golang.org/protobuf/proto"

	arrangorpcv1 "github.com/smegg99/arrango/backend/gen/arrangorpcv1"
	arrangov1 "github.com/smegg99/arrango/backend/gen/arrangov1"

	"github.com/smegg99/goptivum/backend/api/v1/gen"
)

const profilesPath = "/api/v1/rules/profiles"

// strictDecode reads a body into out and refuses a field the contract does not have.
func strictDecode(t *testing.T, recorder *httptest.ResponseRecorder, status int, out any) {
	t.Helper()
	if recorder.Code != status {
		t.Fatalf("status = %d, want %d: %s", recorder.Code, status, recorder.Body.String())
	}
	decoder := json.NewDecoder(strings.NewReader(recorder.Body.String()))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(out); err != nil {
		t.Fatalf("decode %s: %v", recorder.Body.String(), err)
	}
}

func listProfiles(t *testing.T, s solving, credential string) []gen.SchoolRuleProfile {
	t.Helper()
	var listed gen.SchoolRuleProfileList
	strictDecode(t, call(t, s.server, http.MethodGet, profilesPath, credential, ""), http.StatusOK, &listed)
	return listed.Items
}

// createProfile stores "Nasza szkoła" with teacher gaps off, as the owner.
func createProfile(t *testing.T, s solving) gen.SchoolRuleProfile {
	t.Helper()
	var created gen.SchoolRuleProfile
	strictDecode(t, call(t, s.server, http.MethodPost, profilesPath, asOwner,
		`{"name":" Nasza szkoła ","overrides":[{"rule":"teacher_gaps","mode":"off"}]}`), http.StatusCreated, &created)
	return created
}

// A school profile is everybody's to see and use, and only its creator's and
// an administrator's to change or delete.
func TestSchoolRuleProfilesAreSharedAndChangedOnlyByTheirCreatorOrAnAdmin(t *testing.T) {
	s := newSolvingServer(t)
	created := createProfile(t, s)
	if created.Name != "Nasza szkoła" || !created.Editable || created.CreatedBy.Id != "owner" ||
		created.CreatedBy.Name != "Anna Kowalska" || len(created.Overrides) != 1 ||
		created.Overrides[0].Rule != "teacher_gaps" || created.Overrides[0].Mode == nil || *created.Overrides[0].Mode != gen.Off ||
		created.Overrides[0].Weight != nil || created.Overrides[0].Param != nil {
		t.Fatalf("created = %+v", created)
	}

	seen := listProfiles(t, s, asStranger)
	if len(seen) != 1 || seen[0].Id != created.Id || seen[0].Editable {
		t.Fatalf("a stranger sees %+v, want the profile, not editable", seen)
	}
	path := profilesPath + "/" + created.Id
	for _, one := range []struct{ method, body string }{
		{http.MethodPatch, `{"name":"Cudza"}`},
		{http.MethodDelete, ""},
	} {
		recorder := call(t, s.server, one.method, path, asStranger, one.body)
		if recorder.Code != http.StatusForbidden || errorKey(t, recorder.Body.Bytes()) != "rules.profileNotYours" {
			t.Errorf("a stranger's %s = %d %s, want 403 rules.profileNotYours", one.method, recorder.Code, recorder.Body.String())
		}
	}

	if listed := listProfiles(t, s, asAdmin); len(listed) != 1 || !listed[0].Editable {
		t.Fatalf("an administrator sees %+v, want it editable", listed)
	}
	var renamed gen.SchoolRuleProfile
	strictDecode(t, call(t, s.server, http.MethodPatch, path, asAdmin, `{"name":"Luźny"}`), http.StatusOK, &renamed)
	if renamed.Name != "Luźny" || len(renamed.Overrides) != 1 || renamed.CreatedBy.Id != "owner" {
		t.Errorf("renamed = %+v", renamed)
	}
	var changed gen.SchoolRuleProfile
	strictDecode(t, call(t, s.server, http.MethodPatch, path, asOwner,
		`{"overrides":[{"rule":"student_gaps","mode":"soft","weight":5000},{"rule":"anti_split_shift","param":2}]}`),
		http.StatusOK, &changed)
	if changed.Name != "Luźny" || len(changed.Overrides) != 2 || *changed.Overrides[0].Weight != 5000 ||
		changed.Overrides[1].Mode != nil || *changed.Overrides[1].Param != 2 {
		t.Errorf("changed = %+v", changed)
	}

	if recorder := call(t, s.server, http.MethodDelete, path, asOwner, ""); recorder.Code != http.StatusNoContent {
		t.Fatalf("delete = %d %s", recorder.Code, recorder.Body.String())
	}
	if listed := listProfiles(t, s, asOwner); len(listed) != 0 {
		t.Errorf("after delete: %+v", listed)
	}
	for _, method := range []string{http.MethodPatch, http.MethodDelete} {
		if recorder := call(t, s.server, method, path, asOwner, `{"name":"x"}`); recorder.Code != http.StatusNotFound {
			t.Errorf("%s of a deleted profile = %d, want 404", method, recorder.Code)
		}
	}
}

// What a profile cannot hold is refused with the field and why.
func TestARuleProfileIsRefusedWhatItCannotHold(t *testing.T) {
	s := newSolvingServer(t)
	created := createProfile(t, s)
	cases := []struct {
		name, body, field, code string
	}{
		{"no name", `{"name":"  ","overrides":[]}`, "name", "required"},
		{"a taken name", `{"name":"Nasza szkoła","overrides":[]}`, "name", "taken"},
		{"a long name", `{"name":"` + strings.Repeat("ż", 201) + `","overrides":[]}`, "name", "too_long"},
		{"an unknown mode", `{"name":"a","overrides":[{"rule":"teacher_gaps","mode":"sometimes"}]}`, "overrides", "invalid"},
		{"a zero weight", `{"name":"a","overrides":[{"rule":"teacher_gaps","weight":0}]}`, "overrides", "invalid"},
		{"a zero param", `{"name":"a","overrides":[{"rule":"anti_split_shift","param":0}]}`, "overrides", "invalid"},
		{"a malformed key", `{"name":"a","overrides":[{"rule":"Teacher gaps"}]}`, "overrides", "invalid"},
		{"one rule twice", `{"name":"a","overrides":[{"rule":"teacher_gaps","mode":"off"},{"rule":"teacher_gaps","weight":3}]}`, "overrides", "duplicate"},
		{"no overrides", `{"name":"a"}`, "overrides", "required"},
	}
	for _, one := range cases {
		t.Run(one.name, func(t *testing.T) {
			recorder := call(t, s.server, http.MethodPost, profilesPath, asOwner, one.body)
			if recorder.Code != http.StatusUnprocessableEntity {
				t.Fatalf("create = %d %s, want 422", recorder.Code, recorder.Body.String())
			}
			refusal := envelope(t, recorder)
			if refusal.Message != "error.validation" || refusal.Details == nil || (*refusal.Details)[one.field] != one.code {
				t.Errorf("refusal = %+v, want %s: %s", refusal, one.field, one.code)
			}
		})
	}
	recorder := call(t, s.server, http.MethodPatch, profilesPath+"/"+created.Id, asOwner, `{"name":""}`)
	if refusal := envelope(t, recorder); recorder.Code != http.StatusUnprocessableEntity || (*refusal.Details)["name"] != "required" {
		t.Errorf("an empty rename = %d %+v", recorder.Code, refusal)
	}
	if listed := listProfiles(t, s, asOwner); len(listed) != 1 {
		t.Errorf("stored after refusals: %+v", listed)
	}
}

func TestRuleProfileRoutesNeedASession(t *testing.T) {
	s := newSolvingServer(t)
	for _, one := range []struct{ method, path string }{
		{http.MethodGet, profilesPath},
		{http.MethodPost, profilesPath},
		{http.MethodPatch, profilesPath + "/1"},
		{http.MethodDelete, profilesPath + "/1"},
	} {
		if recorder := call(t, s.server, one.method, one.path, "", `{}`); recorder.Code != http.StatusUnauthorized {
			t.Errorf("%s %s = %d, want 401", one.method, one.path, recorder.Code)
		}
	}
}

// dispatchWith asks for a from-scratch solve of a version with extra fields.
func dispatchWith(t *testing.T, s solving, planID, version int64, extra string) *httptest.ResponseRecorder {
	t.Helper()
	body := fmt.Sprintf(`{"plan_version":%d,"ignore_previous_placements":true%s}`, version, extra)
	return call(t, s.server, http.MethodPost, fmt.Sprintf("/api/v1/plans/%d/jobs", planID), asOwner, body)
}

// A solve with a profile sends the school without its own rules and the
// profile as the request's, and its job and result name the profile; the
// result's check sends the same rules.
func TestASolveWithARuleProfileReplacesThePlansRules(t *testing.T) {
	s := newSolvingServer(t)
	planID, version := s.ownedPlanWithVersion(t)
	node := &solveNode{frames: []*arrangorpcv1.SolveUpdate{timetableFrame(arrangorpcv1.SolvePhase_SOLVE_PHASE_DONE)}}
	s.pair(t, "solving", node)
	profile := createProfile(t, s)
	gapsOff := &arrangov1.RuleConfig{Overrides: []*arrangov1.RuleOverride{
		{Rule: "teacher_gaps", Mode: arrangov1.RuleMode_RULE_MODE_OFF},
	}}

	cases := []struct {
		name  string
		extra string
		rules *arrangov1.RuleConfig
		want  gen.SolveRuleProfile
	}{
		{"a built-in", `,"builtin_rule_profile":"relaxed"`, &arrangov1.RuleConfig{Profile: "relaxed"}, gen.SolveRuleProfile{Name: "relaxed"}},
		{"the school's", `,"school_rule_profile_id":"` + profile.Id + `"`, gapsOff, gen.SolveRuleProfile{Name: "Nasza szkoła", Id: &profile.Id}},
	}
	for i, one := range cases {
		t.Run(one.name, func(t *testing.T) {
			dispatched := dispatchWith(t, s, planID, version, one.extra)
			if dispatched.Code != http.StatusAccepted {
				t.Fatalf("dispatch = %d: %s", dispatched.Code, dispatched.Body.String())
			}
			job := decodeJob(t, dispatched)
			if job.RuleProfile == nil || job.RuleProfile.Name != one.want.Name || !samePointer(job.RuleProfile.Id, one.want.Id) {
				t.Errorf("the job names %+v, want %+v", job.RuleProfile, one.want)
			}
			waitForFinished(t, s, job.Id)

			sent := node.solves()
			if len(sent) != i+1 {
				t.Fatalf("the node was asked %d solves, want %d", len(sent), i+1)
			}
			if sent[i].GetModel().GetRuleConfig() != nil || !proto.Equal(sent[i].GetRuleConfig(), one.rules) {
				t.Errorf("sent model rules %v and request rules %v, want none and %v",
					sent[i].GetModel().GetRuleConfig(), sent[i].GetRuleConfig(), one.rules)
			}

			finished := decodeJob(t, call(t, s.server, http.MethodGet, "/api/v1/jobs/"+job.Id, asOwner, ""))
			if finished.ResultId == nil {
				t.Fatalf("job = %+v, want a result", finished)
			}
			var result gen.SolveResult
			for _, item := range strictResults(t, call(t, s.server, http.MethodGet,
				fmt.Sprintf("/api/v1/plans/%d/results", planID), asOwner, "")) {
				if item.Id == *finished.ResultId {
					result = item
				}
			}
			if result.Id == "" {
				t.Fatalf("result %s is not listed", *finished.ResultId)
			}
			if result.RuleProfile == nil || result.RuleProfile.Name != one.want.Name || !samePointer(result.RuleProfile.Id, one.want.Id) {
				t.Errorf("the result names %+v, want %+v", result.RuleProfile, one.want)
			}

			checked := call(t, s.server, http.MethodGet,
				fmt.Sprintf("/api/v1/plans/%d/results/%s/check", planID, result.Id), asOwner, "")
			if checked.Code != http.StatusOK {
				t.Fatalf("check = %d %s", checked.Code, checked.Body.String())
			}
			asked := node.checks()
			last := asked[len(asked)-1]
			if last.GetModel().GetRuleConfig() != nil || !proto.Equal(last.GetRuleConfig(), one.rules) {
				t.Errorf("the check sent model rules %v and request rules %v, want none and %v",
					last.GetModel().GetRuleConfig(), last.GetRuleConfig(), one.rules)
			}
		})
	}

	// A later edit of the profile changes neither what the job recorded nor
	// what its result's check sends.
	strictDecode(t, call(t, s.server, http.MethodPatch, profilesPath+"/"+profile.Id, asOwner,
		`{"name":"Inna","overrides":[]}`), http.StatusOK, &profile)
	jobs := decodeJobs(t, call(t, s.server, http.MethodGet, fmt.Sprintf("/api/v1/plans/%d/jobs", planID), asOwner, ""))
	if len(jobs) != 2 || jobs[0].RuleProfile == nil || jobs[0].RuleProfile.Name != "Nasza szkoła" {
		t.Errorf("after the edit the jobs name %+v", jobs)
	}
}

// A solve names at most one profile, and a school profile it names must exist.
func TestASolveNamesOneRuleProfileThatExists(t *testing.T) {
	s := newSolvingServer(t)
	planID, version := s.ownedPlanWithVersion(t)
	s.node(t)
	profile := createProfile(t, s)

	both := dispatchWith(t, s, planID, version, `,"builtin_rule_profile":"relaxed","school_rule_profile_id":"`+profile.Id+`"`)
	if refusal := envelope(t, both); both.Code != http.StatusUnprocessableEntity || (*refusal.Details)["school_rule_profile_id"] != "exclusive" {
		t.Errorf("both = %d %s", both.Code, both.Body.String())
	}
	if unknown := dispatchWith(t, s, planID, version, `,"school_rule_profile_id":"42"`); unknown.Code != http.StatusNotFound {
		t.Errorf("an unknown profile = %d %s, want 404", unknown.Code, unknown.Body.String())
	}
	malformed := dispatchWith(t, s, planID, version, `,"builtin_rule_profile":"Dobry plan"`)
	if refusal := envelope(t, malformed); malformed.Code != http.StatusUnprocessableEntity || (*refusal.Details)["builtin_rule_profile"] != "invalid" {
		t.Errorf("a malformed built-in = %d %s", malformed.Code, malformed.Body.String())
	}
	jobs := decodeJobs(t, call(t, s.server, http.MethodGet, fmt.Sprintf("/api/v1/plans/%d/jobs", planID), asOwner, ""))
	if len(jobs) != 0 {
		t.Errorf("refused solves left jobs: %+v", jobs)
	}
}

func samePointer(got, want *string) bool {
	if got == nil || want == nil {
		return got == nil && want == nil
	}
	return *got == *want
}

func decodeJobs(t *testing.T, recorder *httptest.ResponseRecorder) []gen.Job {
	t.Helper()
	var listed gen.JobList
	strictDecode(t, recorder, http.StatusOK, &listed)
	return listed.Items
}

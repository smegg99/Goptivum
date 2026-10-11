// api/v1/uploadresult_test.go

package v1_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	arrangov1 "github.com/smegg99/arrango/backend/gen/arrangov1"

	"github.com/smegg99/goptivum/backend/api/v1/gen"
	"github.com/smegg99/goptivum/backend/arrango"
	"github.com/smegg99/goptivum/backend/common/messages"
	"github.com/smegg99/goptivum/document"
)

// deviceResult is one upload of a result solved on a device: every form part,
// each of which a case can drop or replace.
type deviceResult struct {
	document []byte
	fields   map[string]string
	// profile is the rule_profile part, sent as JSON; empty sends none.
	profile string
}

// solvedOnDevice is what a desktop sends after solving version 1 of a plan on
// its own node: the version's school with a timetable, and what the node said.
func solvedOnDevice(t *testing.T, office *backOffice, plan gen.Plan) deviceResult {
	t.Helper()
	downloaded := call(t, office.server, http.MethodGet, "/api/v1/plans/"+plan.Id+"/versions/1", asAnna, "")
	if downloaded.Code != http.StatusOK {
		t.Fatalf("download = %d: %s", downloaded.Code, downloaded.Body.String())
	}
	doc, err := document.Decode(downloaded.Body.Bytes())
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	doc.Schedule = &arrangov1.ScheduleSnapshot{Lessons: []*arrangov1.ScheduledLesson{
		{LessonId: 1, Placement: &arrangov1.Placement{DayId: 1, StartPeriod: 2, RoomId: 1}},
	}}
	encoded, err := document.Encode(doc)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	return deviceResult{
		document: encoded.Bytes,
		fields: map[string]string{
			"source_version": "1",
			"upload_key":     "device-result-000001",
			"device":         "  Laptop Anny  ",
			"solved_at":      "2026-09-20T10:15:00Z",
			"quality":        "91.5",
			"verdict":        "TIER_PRISTINE",
		},
		profile: `{"name":"Nasza szkoła","id":"4242","overrides":[{"rule":"teacher_gaps","mode":"off"}]}`,
	}
}

// with is the same upload with one field replaced; an empty value drops it.
func (d deviceResult) with(field, value string) deviceResult {
	fields := make(map[string]string, len(d.fields))
	for name, sent := range d.fields {
		fields[name] = sent
	}
	if value == "" {
		delete(fields, field)
	} else {
		fields[field] = value
	}
	d.fields = fields
	return d
}

// uploadDeviceResult sends one upload as one account.
func uploadDeviceResult(t *testing.T, office *backOffice, planID, credential string, sent deviceResult) *httptest.ResponseRecorder {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	for name, value := range sent.fields {
		_ = writer.WriteField(name, value)
	}
	if sent.profile != "" {
		header := textproto.MIMEHeader{}
		header.Set("Content-Disposition", `form-data; name="rule_profile"`)
		header.Set("Content-Type", "application/json")
		part, _ := writer.CreatePart(header)
		_, _ = part.Write([]byte(sent.profile))
	}
	if sent.document != nil {
		part, _ := writer.CreateFormFile("document", "result.gpf")
		_, _ = part.Write(sent.document)
	}
	_ = writer.Close()
	request := httptest.NewRequest(http.MethodPost, "/api/v1/plans/"+planID+"/results", &body)
	request.Header.Set("Content-Type", writer.FormDataContentType())
	if credential != "" {
		request.Header.Set("Authorization", credential)
	}
	recorder := httptest.NewRecorder()
	office.server.Handler().ServeHTTP(recorder, request)
	return recorder
}

// keptResult decodes a 201 strictly against the contract.
func keptResult(t *testing.T, recorder *httptest.ResponseRecorder) gen.SolveResult {
	t.Helper()
	if recorder.Code != http.StatusCreated {
		t.Fatalf("upload = %d: %s", recorder.Code, recorder.Body.String())
	}
	var result gen.SolveResult
	decoder := json.NewDecoder(strings.NewReader(recorder.Body.String()))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&result); err != nil {
		t.Fatalf("decode %s: %v", recorder.Body.String(), err)
	}
	return result
}

func listedResults(t *testing.T, office *backOffice, planID string) []gen.SolveResult {
	t.Helper()
	return answer[gen.SolveResultList](t, office, http.MethodGet, "/api/v1/plans/"+planID+"/results", asPiotr, "").Items
}

// Karol's slice 17 test 4: a result solved on this computer uploads as a
// result of the plan, marked with the device, carrying what the node said and
// the profile the solve used, and opens as the bytes the device sent.
func TestAResultSolvedOnADeviceIsKeptAsAResult(t *testing.T) {
	office := newBackOffice(t)
	plan := newProject(t, office, asAnna, "Plan 2027")
	sent := solvedOnDevice(t, office, plan)

	result := keptResult(t, uploadDeviceResult(t, office, plan.Id, asAnna, sent))
	solvedAt := time.Date(2026, 9, 20, 10, 15, 0, 0, time.UTC)
	if result.JobId != nil || result.Device == nil || *result.Device != "Laptop Anny" ||
		result.PlanId != plan.Id || result.SourceVersion != 1 || result.Pinned || result.Name != nil ||
		!result.CreatedAt.Equal(solvedAt) || result.SizeBytes != int64(len(sent.document)) {
		t.Errorf("result = %+v", result)
	}
	if result.StartedBy.Id != "identity-anna" || result.StartedBy.Name != "Anna Kowalska" {
		t.Errorf("started by = %+v, want the account that uploaded it", result.StartedBy)
	}
	if result.Quality == nil || *result.Quality != 91.5 || result.Verdict == nil || *result.Verdict != "TIER_PRISTINE" {
		t.Errorf("rating = %v, verdict = %v", result.Quality, result.Verdict)
	}
	if result.RuleProfile == nil || result.RuleProfile.Name != "Nasza szkoła" ||
		result.RuleProfile.Id == nil || *result.RuleProfile.Id != "4242" {
		t.Errorf("profile = %+v", result.RuleProfile)
	}

	// The result's check sends the rules the solve was sent.
	id, _ := strconv.ParseInt(result.Id, 10, 64)
	planID, _ := strconv.ParseInt(plan.Id, 10, 64)
	stored, err := office.index.Result(t.Context(), planID, id)
	if err != nil {
		t.Fatalf("stored: %v", err)
	}
	rules, err := arrango.DecodeRules(stored.RuleProfile.Rules)
	if err != nil {
		t.Fatalf("rules: %v", err)
	}
	if rules.GetProfile() != "" || len(rules.GetOverrides()) != 1 || rules.GetOverrides()[0].GetRule() != "teacher_gaps" ||
		rules.GetOverrides()[0].GetMode() != arrangov1.RuleMode_RULE_MODE_OFF {
		t.Errorf("stored rules = %v, want the one override the solve was sent", rules)
	}

	// Everyone sees it, and it opens as what the device sent.
	listed := listedResults(t, office, plan.Id)
	if len(listed) != 1 || listed[0].Id != result.Id || listed[0].Device == nil {
		t.Errorf("listed = %+v", listed)
	}
	downloaded := call(t, office.server, http.MethodGet, "/api/v1/plans/"+plan.Id+"/results/"+result.Id, asPiotr, "")
	if downloaded.Code != http.StatusOK || !bytes.Equal(downloaded.Body.Bytes(), sent.document) {
		t.Errorf("download = %d, %d bytes, want the %d bytes sent", downloaded.Code, downloaded.Body.Len(), len(sent.document))
	}
}

// A built-in profile is named alone and sent as the solver's own profile;
// without a profile the result used the plan's rules.
func TestAnUploadedResultNamesItsBuiltInProfileOrNone(t *testing.T) {
	office := newBackOffice(t)
	plan := newProject(t, office, asAnna, "Plan 2027")
	planID, _ := strconv.ParseInt(plan.Id, 10, 64)

	builtIn := solvedOnDevice(t, office, plan)
	builtIn.profile = `{"name":"dobry_plan"}`
	result := keptResult(t, uploadDeviceResult(t, office, plan.Id, asAnna, builtIn))
	if result.RuleProfile == nil || result.RuleProfile.Name != "dobry_plan" || result.RuleProfile.Id != nil {
		t.Errorf("profile = %+v, want the built-in by name", result.RuleProfile)
	}
	id, _ := strconv.ParseInt(result.Id, 10, 64)
	stored, err := office.index.Result(t.Context(), planID, id)
	if err != nil {
		t.Fatalf("stored: %v", err)
	}
	if rules, err := arrango.DecodeRules(stored.RuleProfile.Rules); err != nil || rules.GetProfile() != "dobry_plan" {
		t.Errorf("stored rules = %v, %v, want the built-in by name", rules, err)
	}

	plain := solvedOnDevice(t, office, plan).with("upload_key", "device-result-000002")
	plain.profile = ""
	if result := keptResult(t, uploadDeviceResult(t, office, plan.Id, asAnna, plain)); result.RuleProfile != nil {
		t.Errorf("profile = %+v, want none for the plan's own rules", result.RuleProfile)
	}
}

// A retry after a lost answer keeps one result, and the same key with other
// bytes is a client bug, refused rather than answered with another result.
func TestAnUploadSentTwiceKeepsOneResult(t *testing.T) {
	office := newBackOffice(t)
	plan := newProject(t, office, asAnna, "Plan 2027")
	sent := solvedOnDevice(t, office, plan)

	first := keptResult(t, uploadDeviceResult(t, office, plan.Id, asAnna, sent))
	again := keptResult(t, uploadDeviceResult(t, office, plan.Id, asAnna, sent))
	if again.Id != first.Id {
		t.Errorf("the retry kept %s, want %s", again.Id, first.Id)
	}
	if listed := listedResults(t, office, plan.Id); len(listed) != 1 {
		t.Errorf("listed %d results, want 1", len(listed))
	}

	other := sent
	other.document = append([]byte(nil), sent.document...)
	doc, _ := document.Decode(other.document)
	doc.Schedule.Lessons[0].Placement.StartPeriod = 3
	encoded, _ := document.Encode(doc)
	other.document = encoded.Bytes
	refusedUpload(t, uploadDeviceResult(t, office, plan.Id, asAnna, other), "upload_key", "reused")

	// The same bytes said to come from another version are not the same upload.
	if recorder := uploadAs(t, office, "/api/v1/plans/"+plan.Id+"/versions", asAnna, sent.document); recorder.Code != http.StatusCreated {
		t.Fatalf("version 2 = %d: %s", recorder.Code, recorder.Body.String())
	}
	refusedUpload(t, uploadDeviceResult(t, office, plan.Id, asAnna, sent.with("source_version", "2")), "upload_key", "reused")
	if listed := listedResults(t, office, plan.Id); len(listed) != 1 {
		t.Errorf("listed %d results after refused reuses, want 1", len(listed))
	}
}

// refusedUpload asserts one answer is 422 with one field's code.
func refusedUpload(t *testing.T, recorder *httptest.ResponseRecorder, field, code string) {
	t.Helper()
	var envelope gen.Error
	if recorder.Code != http.StatusUnprocessableEntity || json.Unmarshal(recorder.Body.Bytes(), &envelope) != nil ||
		envelope.Message != messages.KeyErrorValidation || envelope.Details == nil || (*envelope.Details)[field] != code {
		t.Errorf("answer = %d %s, want 422 %s: %s", recorder.Code, recorder.Body.String(), field, code)
	}
}

// What is not a result solved on a device is refused, field by field, and
// nothing is kept.
func TestAnUploadThatIsNotAResultIsRefused(t *testing.T) {
	office := newBackOffice(t)
	plan := newProject(t, office, asAnna, "Plan 2027")
	sent := solvedOnDevice(t, office, plan)

	noTimetable := sent
	doc, _ := document.Decode(sent.document)
	doc.Schedule = nil
	encoded, _ := document.Encode(doc)
	noTimetable.document = encoded.Bytes
	noDocument := sent
	noDocument.document = nil
	garbage := sent
	garbage.document = []byte("not a document")
	profile := func(json string) deviceResult {
		with := sent
		with.profile = json
		return with
	}

	for _, one := range []struct {
		name        string
		sent        deviceResult
		field, code string
	}{
		{"no document", noDocument, "document", "required"},
		{"bytes that are not a document", garbage, "document", "unreadable"},
		{"a document without a timetable", noTimetable, "document", "no_timetable"},
		{"no source version", sent.with("source_version", ""), "source_version", "required"},
		{"a source version that is not a number", sent.with("source_version", "one"), "source_version", "invalid"},
		{"source version 0", sent.with("source_version", "0"), "source_version", "invalid"},
		{"a version the plan never had", sent.with("source_version", "7"), "source_version", "unknown"},
		{"no upload key", sent.with("upload_key", ""), "upload_key", "required"},
		{"a short upload key", sent.with("upload_key", "short"), "upload_key", "invalid"},
		{"no device", sent.with("device", ""), "device", "required"},
		{"a blank device", sent.with("device", "   "), "device", "required"},
		{"a device name over 100 letters", sent.with("device", strings.Repeat("ł", 101)), "device", "too_long"},
		{"a solve time that is not one", sent.with("solved_at", "wczoraj"), "solved_at", "invalid"},
		{"a rating that is not a number", sent.with("quality", "good"), "quality", "invalid"},
		{"a rating below 0", sent.with("quality", "-0.5"), "quality", "invalid"},
		{"a rating above 100", sent.with("quality", "100.1"), "quality", "invalid"},
		{"a rating that is NaN", sent.with("quality", "NaN"), "quality", "invalid"},
		{"a verdict the node never sends", sent.with("verdict", "TIER_UNSPECIFIED"), "verdict", "invalid"},
		{"a profile that is not JSON", profile(`{"name":`), "rule_profile", "invalid"},
		{"a profile with a field it does not have", profile(`{"name":"default","scope":1}`), "rule_profile", "invalid"},
		{"a built-in name that is not one", profile(`{"name":"Nasza szkoła"}`), "rule_profile", "invalid"},
		{"a built-in with overrides", profile(`{"name":"default","overrides":[]}`), "rule_profile", "invalid"},
		{"a school profile without its overrides", profile(`{"name":"Nasza","id":"42"}`), "rule_profile", "invalid"},
		{"a school profile id that is not one", profile(`{"name":"Nasza","id":"x42","overrides":[]}`), "rule_profile", "invalid"},
		{"a blank school profile name", profile(`{"name":" ","id":"42","overrides":[]}`), "rule_profile", "invalid"},
		{"an override that is not one", profile(`{"name":"Nasza","id":"42","overrides":[{"rule":"Teacher Gaps"}]}`), "rule_profile", "invalid"},
		{"one rule twice", profile(`{"name":"Nasza","id":"42","overrides":[{"rule":"teacher_gaps"},{"rule":"teacher_gaps","mode":"off"}]}`), "rule_profile", "invalid"},
	} {
		t.Run(one.name, func(t *testing.T) {
			refusedUpload(t, uploadDeviceResult(t, office, plan.Id, asAnna, one.sent), one.field, one.code)
		})
	}
	if listed := listedResults(t, office, plan.Id); len(listed) != 0 {
		t.Errorf("refused uploads kept %d results", len(listed))
	}
}

// A solve time the server's clock has not reached yet is now: a device clock
// ahead of the server must not put a result above every later one for ever.
func TestAResultFromTheFutureIsKeptNow(t *testing.T) {
	office := newBackOffice(t)
	plan := newProject(t, office, asAnna, "Plan 2027")
	before := time.Now().UTC()
	result := keptResult(t, uploadDeviceResult(t, office, plan.Id, asAnna,
		solvedOnDevice(t, office, plan).with("solved_at", before.Add(48*time.Hour).Format(time.RFC3339))))
	if result.CreatedAt.Before(before.Add(-time.Second)) || result.CreatedAt.After(time.Now().UTC().Add(time.Second)) {
		t.Errorf("created at %s, want now", result.CreatedAt)
	}
	absent := keptResult(t, uploadDeviceResult(t, office, plan.Id, asAnna,
		solvedOnDevice(t, office, plan).with("solved_at", "").with("upload_key", "device-result-000002")))
	if absent.CreatedAt.Before(before.Add(-time.Second)) {
		t.Errorf("created at %s without a solve time, want now", absent.CreatedAt)
	}
}

// Only the plan's owner uploads a result; another account may fork the plan.
// A plan in Trash or one that does not exist takes none.
func TestOnlyTheOwnerUploadsAResult(t *testing.T) {
	office := newBackOffice(t)
	plan := newProject(t, office, asAnna, "Plan 2027")
	sent := solvedOnDevice(t, office, plan)

	recorder := uploadDeviceResult(t, office, plan.Id, asPiotr, sent)
	var envelope gen.Error
	if recorder.Code != http.StatusForbidden || json.Unmarshal(recorder.Body.Bytes(), &envelope) != nil ||
		envelope.Message != messages.KeyPlansNotYours {
		t.Errorf("another account = %d %s, want 403 %s", recorder.Code, recorder.Body.String(), messages.KeyPlansNotYours)
	}
	if recorder := uploadDeviceResult(t, office, plan.Id, "", sent); recorder.Code != http.StatusUnauthorized {
		t.Errorf("no session = %d, want 401", recorder.Code)
	}
	if recorder := uploadDeviceResult(t, office, "999999", asAnna, sent); recorder.Code != http.StatusNotFound {
		t.Errorf("no such plan = %d, want 404", recorder.Code)
	}
	deleteProject(t, office, asAnna, plan)
	if recorder := uploadDeviceResult(t, office, plan.Id, asAnna, sent); recorder.Code != http.StatusNotFound {
		t.Errorf("a plan in Trash = %d, want 404", recorder.Code)
	}
}

// An uploaded result counts in the plan's results cap like any other: the
// oldest unpinned ones beyond it go, by when each was solved.
func TestAnUploadedResultCountsInTheResultsCap(t *testing.T) {
	office := newBackOffice(t)
	plan := newProject(t, office, asAnna, "Plan 2027")
	if recorder := call(t, office.server, http.MethodPatch, "/api/v1/plans/"+plan.Id, asAnna, `{"results_keep":2}`); recorder.Code != http.StatusOK {
		t.Fatalf("cap = %d: %s", recorder.Code, recorder.Body.String())
	}
	sent := solvedOnDevice(t, office, plan)
	var kept []string
	for day := 1; day <= 3; day++ {
		result := keptResult(t, uploadDeviceResult(t, office, plan.Id, asAnna, sent.
			with("upload_key", fmt.Sprintf("device-result-%06d", day)).
			with("solved_at", fmt.Sprintf("2026-09-%02dT08:00:00Z", day))))
		kept = append(kept, result.Id)
	}
	var listed []string
	for _, result := range listedResults(t, office, plan.Id) {
		listed = append(listed, result.Id)
	}
	if want := []string{kept[2], kept[1]}; !slices.Equal(listed, want) {
		t.Errorf("listed %v, want the 2 newest %v", listed, want)
	}
	refused := uploadDeviceResult(t, office, plan.Id, asAnna, sent.
		with("upload_key", "device-result-000004").
		with("solved_at", "2026-08-01T08:00:00Z"))
	var envelope gen.Error
	if refused.Code != http.StatusUnprocessableEntity || json.Unmarshal(refused.Body.Bytes(), &envelope) != nil ||
		envelope.Details == nil || (*envelope.Details)["solved_at"] != "invalid" {
		t.Fatalf("older than retention = %d %s", refused.Code, refused.Body.String())
	}
	listed = nil
	for _, result := range listedResults(t, office, plan.Id) {
		listed = append(listed, result.Id)
	}
	if !slices.Equal(listed, []string{kept[2], kept[1]}) {
		t.Fatalf("refused upload changed the retained results: %v", listed)
	}
}

// The change listing names the plan's results, so another device reads them
// again; an upload is not written to the audit, as a server solve's result is
// not.
func TestAnUploadedResultIsAChangeAndNotAnAuditEntry(t *testing.T) {
	office := newBackOffice(t)
	plan := newProject(t, office, asAnna, "Plan 2027")
	sent := solvedOnDevice(t, office, plan)
	point := changesSince(t, office, asPiotr, "")
	audited := auditPage(t, office, "").Total

	keptResult(t, uploadDeviceResult(t, office, plan.Id, asAnna, sent))
	changed := changesSince(t, office, asPiotr, point.Cursor)
	if len(changed.Plans) != 1 || changed.Plans[0].PlanId != plan.Id ||
		!slices.Contains(changed.Plans[0].Parts, gen.ChangedPartResults) {
		t.Errorf("changes = %+v, want the plan's results", changed.Plans)
	}
	if after := auditPage(t, office, "").Total; after != audited {
		t.Errorf("audit grew from %d to %d entries", audited, after)
	}

	// A retry changes nothing.
	keptResult(t, uploadDeviceResult(t, office, plan.Id, asAnna, sent))
	if again := changesSince(t, office, asPiotr, changed.Cursor); len(again.Plans) != 0 {
		t.Errorf("a retry is listed as a change: %+v", again.Plans)
	}
}

// A document over the upload limit is refused as too large, never cut short.
func TestAnUploadedResultOverTheLimitIsTooLarge(t *testing.T) {
	api := planAPIWithLimit(t, 4096)
	ctx := signedIn("identity-1")
	plan := createPlan(t, api, ctx, "Plan 2027")
	response, err := api.UploadResult(ctx, gen.UploadResultRequestObject{
		PlanId: plan.Id,
		Body: form(t, "document", "result.gpf", bytes.Repeat([]byte("x"), 5000), map[string]string{
			"source_version": "1", "upload_key": "device-result-000001", "device": "Laptop Anny",
		}),
	})
	if err != nil {
		t.Fatalf("upload: %v", err)
	}
	refused, ok := response.(gen.UploadResult413JSONResponse)
	if !ok || refused.Message != messages.KeyErrorRequestTooLarge {
		t.Fatalf("an oversized document returned %#v, want 413", response)
	}
}

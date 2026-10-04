// api/v1/errors_test.go

package v1_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/smegg99/goptivum/backend/api/v1/gen"
)

// envelope decodes an error body and fails the test when it is anything else,
// such as the generated boundary's own {"msg": ...} or an empty body.
func envelope(t *testing.T, recorder *httptest.ResponseRecorder) gen.Error {
	t.Helper()
	var body gen.Error
	decoder := json.NewDecoder(bytes.NewReader(recorder.Body.Bytes()))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&body); err != nil || body.Code == "" || body.Message == "" {
		t.Fatalf("status %d answered %q, not the error envelope", recorder.Code, recorder.Body.String())
	}
	return body
}

// A value the generated binding cannot read is a 400 envelope. It was an empty
// 500, because the wrapper's error handler was nil.
func TestAnUnreadableParameterIsABadRequestEnvelope(t *testing.T) {
	solving := newSolvingServer(t)
	planID, _ := solving.ownedPlanWithVersion(t)

	for _, path := range []string{
		"/api/v1/plans?page=abc",
		"/api/v1/plans?limit=abc",
		fmt.Sprintf("/api/v1/plans/%d/versions/abc", planID),
	} {
		t.Run(path, func(t *testing.T) {
			recorder := call(t, solving.server, http.MethodGet, path, asOwner, "")
			if recorder.Code != http.StatusBadRequest {
				t.Fatalf("status = %d: %s", recorder.Code, recorder.Body.String())
			}
			body := envelope(t, recorder)
			if body.Code != "bad_request" || body.Message != "error.badRequest" {
				t.Errorf("envelope = %+v", body)
			}
		})
	}

	// A value that parses and is out of range is clamped, never refused.
	if recorder := call(t, solving.server, http.MethodGet, "/api/v1/plans?page=0&limit=10000", asOwner, ""); recorder.Code != http.StatusOK {
		t.Errorf("an out-of-range page answered %d", recorder.Code)
	}
}

// A body the boundary cannot decode is the same envelope, not {"msg"}.
func TestAnUnreadableBodyIsABadRequestEnvelope(t *testing.T) {
	solving := newSolvingServer(t)
	planID, _ := solving.ownedPlanWithVersion(t)
	id := strconv.FormatInt(planID, 10)

	cases := []struct {
		name, method, path, body string
	}{
		{"broken JSON", http.MethodPatch, "/api/v1/plans/" + id, `{"name":`},
		// createPlan is multipart; a JSON body has no boundary to read.
		{"JSON where multipart belongs", http.MethodPost, "/api/v1/plans", `{"name":"Plan 2027"}`},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			recorder := call(t, solving.server, test.method, test.path, asOwner, test.body)
			if recorder.Code != http.StatusBadRequest {
				t.Fatalf("status = %d: %s", recorder.Code, recorder.Body.String())
			}
			if body := envelope(t, recorder); body.Message != "error.badRequest" {
				t.Errorf("envelope = %+v", body)
			}
		})
	}
}

// createPlan sits on the upload group and speaks multipart end to end.
func TestCreatePlanOverHTTP(t *testing.T) {
	solving := newSolvingServer(t)

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	_ = writer.WriteField("name", "Plan 2027")
	_ = writer.WriteField("school_year", "2026/2027")
	if err := writer.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	request := httptest.NewRequest(http.MethodPost, "/api/v1/plans", &body)
	request.Header.Set("Content-Type", writer.FormDataContentType())
	request.Header.Set("Authorization", asOwner)
	recorder := httptest.NewRecorder()
	solving.server.Handler().ServeHTTP(recorder, request)
	if recorder.Code != http.StatusCreated {
		t.Fatalf("create = %d: %s", recorder.Code, recorder.Body.String())
	}
	var plan gen.Plan
	if err := json.Unmarshal(recorder.Body.Bytes(), &plan); err != nil {
		t.Fatalf("decode: %v", err)
	}

	listed := call(t, solving.server, http.MethodGet, "/api/v1/plans/"+plan.Id+"/versions", asOwner, "")
	var timeline gen.VersionList
	if err := json.Unmarshal(listed.Body.Bytes(), &timeline); err != nil || len(timeline.Items) != 1 {
		t.Fatalf("versions = %s, %v", listed.Body.String(), err)
	}

	// A blank rename over HTTP is the validation envelope.
	refused := call(t, solving.server, http.MethodPatch, "/api/v1/plans/"+plan.Id, asOwner, `{"name":"  "}`)
	if refused.Code != http.StatusUnprocessableEntity {
		t.Fatalf("blank rename = %d: %s", refused.Code, refused.Body.String())
	}
	if body := envelope(t, refused); body.Message != "error.validation" || body.Details == nil || (*body.Details)["name"] != "required" {
		t.Errorf("envelope = %+v", body)
	}
}

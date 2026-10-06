package v1_test

import (
	"encoding/json"
	"net/http"

	"github.com/smegg99/goptivum/backend/api/v1/gen"
	"testing"

	arrangorpcv1 "github.com/smegg99/arrango/backend/gen/arrangorpcv1"
)

func TestCreateJobRefusesExcessiveTimeWithThePeerCeiling(t *testing.T) {
	s := newSolvingServer(t)
	s.pair(t, "short", &solveNode{status: policed(1, 4)})
	plan := s.planWith(t, lessonModel(2), nil)
	refused := s.solve(t, plan, `{"plan_version":1,"ignore_previous_placements":true,"total_time_seconds":3600}`)
	if refused.Code != http.StatusConflict {
		t.Fatalf("status = %d, body = %s", refused.Code, refused.Body.String())
	}
	envelope := decodeError(t, refused)
	if envelope.Message != "solve.limitTooLong" || envelope.Details == nil || (*envelope.Details)["max_seconds"] != "900" {
		t.Fatalf("envelope = %+v", envelope)
	}
}

func TestCreateJobRejectsOutOfRangeTimeBeforeDispatch(t *testing.T) {
	s := newSolvingServer(t)
	s.pair(t, "day", &solveNode{status: &arrangorpcv1.NodeStatusResponse{Cores: 8, AvailableBytes: 16 << 30, Policy: &arrangorpcv1.PeerPolicy{MaxSolveSeconds: 86400}}})
	plan := s.planWith(t, lessonModel(2), nil)
	for _, body := range []string{
		`{"plan_version":1,"ignore_previous_placements":true,"total_time_seconds":0}`,
		`{"plan_version":1,"ignore_previous_placements":true,"total_time_seconds":86401}`,
		`{"plan_version":1,"ignore_previous_placements":true,"stall_stop_seconds":-1}`,
		`{"plan_version":1,"ignore_previous_placements":true,"stall_stop_seconds":86401}`,
	} {
		refused := s.solve(t, plan, body)
		if refused.Code != http.StatusUnprocessableEntity {
			t.Fatalf("status = %d, body = %s", refused.Code, refused.Body.String())
		}
	}
}

func TestCreateJobCarriesCustomDurationAndStallWindow(t *testing.T) {
	s := newSolvingServer(t)
	s.pair(t, "day", &solveNode{})
	plan := s.planWith(t, lessonModel(2), nil)
	accepted := s.solve(t, plan, `{"plan_version":1,"ignore_previous_placements":true,"total_time_seconds":4200,"stall_stop_seconds":600}`)
	if accepted.Code != http.StatusAccepted {
		t.Fatalf("status = %d, body = %s", accepted.Code, accepted.Body.String())
	}
	var job gen.Job
	if err := json.Unmarshal(accepted.Body.Bytes(), &job); err != nil {
		t.Fatal(err)
	}
	if job.TimeLimitSeconds != 4200 || job.StallStopSeconds != 600 || job.Improve {
		t.Fatalf("job = %+v", job)
	}
}

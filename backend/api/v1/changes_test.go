// api/v1/changes_test.go

package v1_test

import (
	"encoding/json"
	"net/http"
	"net/url"
	"reflect"
	"testing"

	"github.com/smegg99/goptivum/backend/api/v1/gen"
)

func changesSince(t *testing.T, office *backOffice, credential, since string) gen.ChangeList {
	t.Helper()
	path := "/api/v1/changes"
	if since != "" {
		path += "?since=" + url.QueryEscape(since)
	}
	return answer[gen.ChangeList](t, office, http.MethodGet, path, credential, "")
}

// The desktop takes a point, reads everything, then polls from the point:
// each answer names the plans that changed, once, with the parts to read
// again and where each plan is now. Another account's plans are listed too,
// because every account reads every plan.
func TestChangesTellTheDesktopWhatToReadAgain(t *testing.T) {
	office := newBackOffice(t)

	start := changesSince(t, office, asPiotr, "")
	if !start.Reset || start.More || len(start.Plans) != 0 || start.Cursor == "" {
		t.Fatalf("the first answer = %+v, want a reset with a cursor and no plans", start)
	}

	plan := newProject(t, office, asAnna, "Plan 2027")
	created := changesSince(t, office, asPiotr, start.Cursor)
	want := []gen.PlanChange{{
		PlanId: plan.Id, State: gen.Live,
		Parts: []gen.ChangedPart{gen.ChangedPartProject, gen.ChangedPartVersions},
	}}
	if created.Reset || created.More || !reflect.DeepEqual(created.Plans, want) {
		t.Fatalf("after a create = %+v, want %+v", created, want)
	}

	quiet := changesSince(t, office, asPiotr, created.Cursor)
	if quiet.Reset || quiet.More || len(quiet.Plans) != 0 || quiet.Cursor != created.Cursor {
		t.Errorf("a poll with nothing new = %+v", quiet)
	}

	answer[gen.Plan](t, office, http.MethodPatch, "/api/v1/plans/"+plan.Id, asAnna, `{"name":"Plan 2027 test"}`)
	if deleted := call(t, office.server, http.MethodDelete, "/api/v1/plans/"+plan.Id, asAnna, ""); deleted.Code != http.StatusNoContent {
		t.Fatalf("delete = %d: %s", deleted.Code, deleted.Body.String())
	}
	trashed := changesSince(t, office, asPiotr, quiet.Cursor)
	want = []gen.PlanChange{{
		PlanId: plan.Id, State: gen.Trash, Parts: []gen.ChangedPart{gen.ChangedPartProject},
	}}
	if trashed.Reset || !reflect.DeepEqual(trashed.Plans, want) {
		t.Errorf("after a rename and a delete = %+v, want %+v", trashed, want)
	}
}

// A point from another database, which the desktop holds after the server's
// database was made again, starts the desktop over rather than hiding what
// changed.
func TestChangesFromAnotherDatabaseStartOver(t *testing.T) {
	office := newBackOffice(t)
	newProject(t, office, asAnna, "Plan 2027")

	const elsewhere = "0123456789abcdef.1"
	changes := changesSince(t, office, asAnna, elsewhere)
	if !changes.Reset || len(changes.Plans) != 0 || changes.Cursor == elsewhere || changes.Cursor == "" {
		t.Errorf("since another database = %+v, want a reset at this one's point", changes)
	}
}

// A since the route never hands out is the caller's mistake, named.
func TestChangesRefuseWhatIsNoCursor(t *testing.T) {
	office := newBackOffice(t)
	for _, since := range []string{"", "42", "0123456789ABCDEF.1", "0123456789abcdef.-1"} {
		recorder := call(t, office.server, http.MethodGet, "/api/v1/changes?since="+url.QueryEscape(since), asPiotr, "")
		var envelope gen.Error
		if recorder.Code != http.StatusUnprocessableEntity ||
			json.Unmarshal(recorder.Body.Bytes(), &envelope) != nil ||
			envelope.Message != "error.validation" || envelope.Details == nil || (*envelope.Details)["since"] != "invalid" {
			t.Errorf("since %q = %d %s, want 422 since: invalid", since, recorder.Code, recorder.Body.String())
		}
	}
}

// Anonymous learns nothing, not even which ids changed.
func TestChangesNeedASession(t *testing.T) {
	office := newBackOffice(t)
	if recorder := call(t, office.server, http.MethodGet, "/api/v1/changes", "", ""); recorder.Code != http.StatusUnauthorized {
		t.Errorf("anonymous = %d, want 401", recorder.Code)
	}
}

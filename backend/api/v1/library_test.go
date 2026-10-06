// api/v1/library_test.go

package v1_test

import (
	"context"
	"fmt"
	"net/http"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/smegg99/s99ab/data/db"

	apiv1 "github.com/smegg99/goptivum/backend/api/v1"
	"github.com/smegg99/goptivum/backend/api/v1/gen"
	"github.com/smegg99/goptivum/backend/common/messages"
	"github.com/smegg99/goptivum/backend/core/library"
	"github.com/smegg99/goptivum/backend/core/plans"
	"github.com/smegg99/goptivum/backend/migrations"
)

const libraryPath = "/api/v1/account/library"

func readLibrary(t *testing.T, s solving, credential string) gen.AccountLibrary {
	t.Helper()
	var read gen.AccountLibrary
	strictDecode(t, call(t, s.server, http.MethodGet, libraryPath, credential, ""), http.StatusOK, &read)
	return read
}

// Karol's slice 20 test 3 at the HTTP layer: a second window of the same
// account reads what the first saved; a window that saves from an old version
// is refused with what is stored, and another account never sees any of it.
func TestTheLibraryIsTheAccountsOwnAndSavedWithItsVersion(t *testing.T) {
	s := newSolvingServer(t)
	first, _ := s.planOf(t, "owner", "Plan 2027")
	second, _ := s.planOf(t, "owner", "Plan 2028")
	a, b := strconv.FormatInt(first, 10), strconv.FormatInt(second, 10)

	recorder := call(t, s.server, http.MethodGet, libraryPath, asOwner, "")
	if body := strings.TrimSpace(recorder.Body.String()); recorder.Code != http.StatusOK ||
		body != `{"folders":[],"pinned":[],"projects":[],"recent":[],"version":0}` {
		t.Fatalf("a first read = %d %s, want version 0 with empty lists", recorder.Code, body)
	}

	body := fmt.Sprintf(`{"version":0,"folders":[
		{"id":"f-2027","name":" Rok 2027 ","accent":"violet","pinned":true,"projects":[%q]},
		{"id":"f-stare","name":"Stare","parent_id":"f-2027","accent":"","pinned":false,"projects":[]}],
		"projects":[%q],"pinned":[%q],"recent":[%q,%q]}`, a, b, a, b, a)
	var saved gen.AccountLibrary
	strictDecode(t, call(t, s.server, http.MethodPut, libraryPath, asOwner, body), http.StatusOK, &saved)
	parent := "f-2027"
	want := gen.AccountLibrary{
		Version: 1,
		Folders: []gen.LibraryFolder{
			{Id: "f-2027", Name: "Rok 2027", Accent: "violet", Pinned: true, Projects: []string{a}},
			{Id: "f-stare", Name: "Stare", ParentId: &parent, Accent: "", Projects: []string{}},
		},
		Projects: []string{b}, Pinned: []string{a}, Recent: []string{b, a},
	}
	if !reflect.DeepEqual(saved, want) {
		t.Fatalf("saved = %+v\nwant    %+v", saved, want)
	}
	if read := readLibrary(t, s, asOwner); !reflect.DeepEqual(read, want) {
		t.Fatalf("the second window reads %+v, want %+v", read, want)
	}
	if read := readLibrary(t, s, asStranger); read.Version != 0 || len(read.Folders)+len(read.Projects)+len(read.Pinned)+len(read.Recent) != 0 {
		t.Fatalf("another account reads %+v, want its own empty arrangement", read)
	}

	var conflict gen.AccountLibraryConflict
	strictDecode(t, call(t, s.server, http.MethodPut, libraryPath, asOwner,
		`{"version":0,"folders":[],"projects":[],"pinned":[],"recent":[]}`), http.StatusConflict, &conflict)
	if conflict.Code != "conflict" || conflict.Message != messages.KeyAccountLibraryConflict || !reflect.DeepEqual(conflict.Current, want) {
		t.Fatalf("conflict = %+v, want the stored arrangement at version 1", conflict)
	}

	var next gen.AccountLibrary
	strictDecode(t, call(t, s.server, http.MethodPut, libraryPath, asOwner,
		`{"version":1,"folders":[],"projects":[],"pinned":[],"recent":[]}`), http.StatusOK, &next)
	if next.Version != 2 || len(next.Folders) != 0 {
		t.Fatalf("a save at the stored version = %+v", next)
	}
}

// A project removed for good is left out; one in Trash keeps its place.
func TestTheLibraryLeavesOutAProjectRemovedForGood(t *testing.T) {
	s := newSolvingServer(t)
	purged, _ := s.planOf(t, "owner", "Usunięty")
	trashed, _ := s.planOf(t, "owner", "Kosz")
	gone, kept := strconv.FormatInt(purged, 10), strconv.FormatInt(trashed, 10)

	body := fmt.Sprintf(`{"version":0,"folders":[{"id":"rok","name":"Rok","accent":"","pinned":false,"projects":[%q,%q]}],
		"projects":[],"pinned":[%q],"recent":[%q]}`, gone, kept, gone, kept)
	if recorder := call(t, s.server, http.MethodPut, libraryPath, asOwner, body); recorder.Code != http.StatusOK {
		t.Fatalf("save = %d %s", recorder.Code, recorder.Body.String())
	}
	if err := s.plans.Trash(t.Context(), purged); err != nil {
		t.Fatalf("trash: %v", err)
	}
	if _, err := s.plans.Purge(t.Context(), time.Now().Add(48*time.Hour), 1); err != nil {
		t.Fatalf("purge: %v", err)
	}
	if err := s.plans.Trash(t.Context(), trashed); err != nil {
		t.Fatalf("trash: %v", err)
	}
	read := readLibrary(t, s, asOwner)
	if read.Version != 1 || !reflect.DeepEqual(read.Folders[0].Projects, []string{kept}) ||
		len(read.Pinned) != 0 || !reflect.DeepEqual(read.Recent, []string{kept}) {
		t.Fatalf("read = %+v, want only the project in Trash, still at version 1", read)
	}
}

func TestALibrarySaveIsRefusedWhatItCannotHold(t *testing.T) {
	s := newSolvingServer(t)
	cases := []struct {
		name, body, field, code string
	}{
		{"a project id that is not one", `{"version":0,"folders":[],"projects":["abc"],"pinned":[],"recent":[]}`, "projects", "invalid"},
		{"a project id past 64 bits", `{"version":0,"folders":[],"projects":["9999999999999999999"],"pinned":[],"recent":[]}`, "projects", "invalid"},
		{"a folder's project id that is not one", `{"version":0,"folders":[{"id":"a","name":"A","accent":"","pinned":false,"projects":["-1"]}],"projects":[],"pinned":[],"recent":[]}`, "folders.projects", "invalid"},
		{"a pinned id that is not one", `{"version":0,"folders":[],"projects":[],"pinned":["x"],"recent":[]}`, "pinned", "invalid"},
		{"a Recent id that is not one", `{"version":0,"folders":[],"projects":[],"pinned":[],"recent":[""]}`, "recent", "invalid"},
		{"a parent that is not there", `{"version":0,"folders":[{"id":"a","name":"A","parent_id":"b","accent":"","pinned":false,"projects":[]}],"projects":[],"pinned":[],"recent":[]}`, "folders.parent_id", "unknown"},
		{"a blank folder name", `{"version":0,"folders":[{"id":"a","name":" ","accent":"","pinned":false,"projects":[]}],"projects":[],"pinned":[],"recent":[]}`, "folders.name", "required"},
		{"21 in Recent", `{"version":0,"folders":[],"projects":[],"pinned":[],"recent":[` + idList(21) + `]}`, "recent", "too_many"},
		{"a version below 0", `{"version":-1,"folders":[],"projects":[],"pinned":[],"recent":[]}`, "version", "invalid"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var refused gen.Error
			strictDecode(t, call(t, s.server, http.MethodPut, libraryPath, asOwner, tc.body), http.StatusUnprocessableEntity, &refused)
			if refused.Message != messages.KeyErrorValidation || refused.Details == nil || (*refused.Details)[tc.field] != tc.code {
				t.Fatalf("refused = %+v, want %s: %s", refused, tc.field, tc.code)
			}
		})
	}
	if read := readLibrary(t, s, asOwner); read.Version != 0 {
		t.Fatalf("after the refusals the account reads version %d, want nothing stored", read.Version)
	}

	if recorder := call(t, s.server, http.MethodPut, libraryPath, asOwner, `{"version":`); recorder.Code != http.StatusBadRequest {
		t.Errorf("a body that does not parse = %d %s, want 400", recorder.Code, recorder.Body.String())
	}
}

func idList(count int) string {
	ids := make([]string, count)
	for i := range ids {
		ids[i] = strconv.Quote(strconv.Itoa(i + 1))
	}
	return strings.Join(ids, ",")
}

func TestLibraryRoutesNeedASession(t *testing.T) {
	s := newSolvingServer(t)
	for _, method := range []string{http.MethodGet, http.MethodPut} {
		recorder := call(t, s.server, method, libraryPath, "",
			`{"version":0,"folders":[],"projects":[],"pinned":[],"recent":[]}`)
		if recorder.Code != http.StatusUnauthorized {
			t.Errorf("%s without a session = %d, want 401", method, recorder.Code)
		}
	}
}

// Past the session middleware a caller always has an identity; without one the
// handlers refuse rather than read or save an arrangement nobody owns.
func TestTheLibraryHandlersRefuseACallerWithoutAnIdentity(t *testing.T) {
	conn := db.TestDB(t, migrations.FS)
	index := plans.NewRepository(conn, db.SQLite, plans.Caps{BackupsKeep: 100, ResultsKeep: 20})
	libraries := apiv1.NewLibraries(library.NewRepository(conn, db.SQLite, index))

	read, err := libraries.GetAccountLibrary(context.Background(), gen.GetAccountLibraryRequestObject{})
	if _, ok := read.(gen.GetAccountLibrary401JSONResponse); err != nil || !ok {
		t.Errorf("read = %T, %v; want 401", read, err)
	}
	body := gen.AccountLibrary{Folders: []gen.LibraryFolder{}, Projects: []string{}, Pinned: []string{}, Recent: []string{}}
	saved, err := libraries.SaveAccountLibrary(context.Background(), gen.SaveAccountLibraryRequestObject{Body: &body})
	if _, ok := saved.(gen.SaveAccountLibrary401JSONResponse); err != nil || !ok {
		t.Errorf("save = %T, %v; want 401", saved, err)
	}
}

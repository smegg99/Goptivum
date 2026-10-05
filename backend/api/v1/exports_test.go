// api/v1/exports_test.go

package v1_test

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strconv"
	"testing"

	"github.com/smegg99/s99ab/auth/kratosauth"
	"github.com/smegg99/s99ab/auth/kratosauth/kratosauthtest"
	"github.com/smegg99/s99ab/data/db"
	"github.com/smegg99/s99ab/data/storage"
	"google.golang.org/protobuf/proto"

	arrangov1 "github.com/smegg99/arrango/backend/gen/arrangov1"

	apiv1 "github.com/smegg99/goptivum/backend/api/v1"
	"github.com/smegg99/goptivum/backend/api/v1/gen"
	"github.com/smegg99/goptivum/backend/common/messages"
	"github.com/smegg99/goptivum/backend/core/documents"
	"github.com/smegg99/goptivum/backend/core/escrow"
	"github.com/smegg99/goptivum/backend/core/exports"
	"github.com/smegg99/goptivum/backend/core/plans"
	"github.com/smegg99/goptivum/backend/formats"
	"github.com/smegg99/goptivum/backend/formats/goptivum"
	optivumhtml "github.com/smegg99/goptivum/backend/formats/optivum/html"
	planxml "github.com/smegg99/goptivum/backend/formats/optivum/xml"
	"github.com/smegg99/goptivum/backend/migrations"
	"github.com/smegg99/goptivum/document"
	goptivumv1 "github.com/smegg99/goptivum/document/gen/goptivumv1"
	"github.com/smegg99/goptivum/document/gpf"
)

const exportRealm = "szkola.example.invalid"

// exporting is the plan routes and the export routes over one index, one
// store and one school key, which is either held or not.
type exporting struct {
	server  *apiv1.Server
	api     *apiv1.API
	exports *apiv1.Exports
	index   *plans.Repository
	store   *documents.Store
	objects storage.Storage
}

func newExportServer(t *testing.T, passphrase string) exporting {
	t.Helper()
	conn := db.TestDB(t, migrations.FS)
	index := plans.NewRepository(conn, db.SQLite, plans.Caps{BackupsKeep: 100, ResultsKeep: 20})
	objects := storage.NewLocalDir(t.TempDir(), "/objects")
	store := documents.NewStore(objects).WithGarbage(index)
	keys := escrow.NewStore(conn, db.SQLite, passphrase).WithWorkFactor(10)
	if keys.Enabled() {
		if _, err := keys.Ensure(t.Context()); err != nil {
			t.Fatalf("ensure: %v", err)
		}
	}
	api, err := apiv1.NewAPI(index, store,
		formats.NewRegistry(planxml.New(), optivumhtml.NewImporter(),
			goptivum.NewImporter(exportRealm).WithKeys(keys.ForFiles())),
		nil, apiv1.Settings{TrashDays: fixedTrashDays})
	if err != nil {
		t.Fatalf("new api: %v", err)
	}
	service, err := exports.New(index, store, objects,
		exports.Format{Exporter: goptivum.NewExporter(exportRealm, "goptivum-server 1.2.3").WithKeys(keys.ForFiles()),
			ContentType: "application/octet-stream"},
		exports.Format{Exporter: optivumhtml.NewExporter(), ContentType: "application/zip"})
	if err != nil {
		t.Fatalf("exports: %v", err)
	}
	handler := apiv1.NewExports(api, service)
	server, err := apiv1.New(apiv1.Options{
		Build:            apiv1.BuildInfo{Version: "1.2.3"},
		MinClientVersion: "0.1.0",
		Handlers:         &apiv1.Handlers{API: api, Exports: handler},
		Kratos: kratosauthtest.Fake(t, map[string]kratosauth.Identity{
			asOwner:    {ID: "owner", Traits: json.RawMessage(`{"username":"owner"}`)},
			asStranger: {ID: "stranger", Traits: json.RawMessage(`{"username":"stranger"}`)},
		}),
	})
	if err != nil {
		t.Fatalf("new server: %v", err)
	}
	return exporting{server: server, api: api, exports: handler, index: index, store: store, objects: objects}
}

// timetabledSchool is one class with one placed lesson, so every HTML page
// has something on it.
func timetabledSchool() *goptivumv1.Document {
	doc := document.New(&arrangov1.SchoolModel{
		Name:      "Szkoła Eksportowa",
		Days:      []*arrangov1.Day{{Id: 1, Name: "Poniedziałek", PeriodCount: 2}},
		Periods:   []*arrangov1.Period{{Id: 2, Name: "08:00-08:45"}, {Id: 3, Name: "08:55-09:40"}},
		Divisions: []*arrangov1.Division{{Id: 4, Name: "1a"}},
		Teachers:  []*arrangov1.Teacher{{Id: 5, Name: "Anna Kowalska"}},
		Subjects:  []*arrangov1.Subject{{Id: 6, Name: "Matematyka"}},
		Rooms:     []*arrangov1.Room{{Id: 7, Name: "101", Designator: "101"}},
		Lessons: []*arrangov1.LessonInstance{{
			Id: 8, Duration: 1, SubjectId: 6, TeacherId: 5, RequiresTeacher: true, RequiresRoom: true,
			Participants: []*arrangov1.Participant{{DivisionId: 4}},
		}},
	})
	doc.Schedule = &arrangov1.ScheduleSnapshot{Lessons: []*arrangov1.ScheduledLesson{
		{LessonId: 8, Placement: &arrangov1.Placement{DayId: 1, StartPeriod: 1, RoomId: 7}},
	}}
	return doc
}

// plan keeps doc as version 1 of an owner's plan.
func (e exporting) plan(t *testing.T, doc *goptivumv1.Document) plans.Plan {
	t.Helper()
	key, size := e.put(t, doc)
	plan, _, err := e.index.Create(t.Context(), plans.Plan{OwnerIdentityID: "owner", Name: "Plan 2027"},
		plans.Upload{ObjectKey: key, SizeBytes: size, CreatedBy: "owner"})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	return plan
}

// result keeps doc as a result of the plan, solved on a device.
func (e exporting) result(t *testing.T, planID int64, doc *goptivumv1.Document) plans.Result {
	t.Helper()
	key, size := e.put(t, doc)
	result, err := e.index.AddResult(t.Context(), plans.NewResult{
		PlanID: planID, SourceVersion: 1, ObjectKey: key, SizeBytes: size,
		StartedBy: "owner", Device: "laptop", UploadKey: "export-test-upload-key",
	})
	if err != nil {
		t.Fatalf("add result: %v", err)
	}
	return result
}

func (e exporting) put(t *testing.T, doc *goptivumv1.Document) (string, int64) {
	t.Helper()
	encoded, err := document.Encode(doc)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	key, err := e.store.Put(t.Context(), encoded)
	if err != nil {
		t.Fatalf("put: %v", err)
	}
	return key, encoded.Size
}

func versionExport(planID int64, version int, format string) string {
	return fmt.Sprintf("/api/v1/plans/%d/versions/%d/export/%s", planID, version, format)
}

func resultExport(planID, resultID int64, format string) string {
	return fmt.Sprintf("/api/v1/plans/%d/results/%d/export/%s", planID, resultID, format)
}

// zipNames lists the files in a zip.
func zipNames(t *testing.T, data []byte) []string {
	t.Helper()
	archive, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("the export is not a zip: %v", err)
	}
	var names []string
	for _, file := range archive.File {
		names = append(names, file.Name)
	}
	return names
}

// Exporting is reading: every signed-in account exports every live plan, the
// file is kept, and the same request again answers from what was kept.
func TestEveryAccountExportsAndTheFileIsKept(t *testing.T) {
	e := newExportServer(t, "the school's escrow passphrase")
	plan := e.plan(t, timetabledSchool())

	first := call(t, e.server, http.MethodGet, versionExport(plan.ID, 1, "goptivum.gpf"), asStranger, "")
	if first.Code != http.StatusOK || first.Header().Get("Content-Type") != "application/octet-stream" {
		t.Fatalf("gpf = %d %q: %s", first.Code, first.Header().Get("Content-Type"), first.Body.String())
	}
	key := fmt.Sprintf("exports/%d/versions/1/goptivum.gpf", plan.ID)
	if ok, err := e.objects.Exists(t.Context(), key); err != nil || !ok {
		t.Errorf("the file is not kept at %s: %v", key, err)
	}
	// An Identity-mode file is sealed afresh on every build, so the same bytes
	// twice can only come from what was kept.
	second := call(t, e.server, http.MethodGet, versionExport(plan.ID, 1, "goptivum.gpf"), asOwner, "")
	if second.Code != http.StatusOK || !bytes.Equal(second.Body.Bytes(), first.Body.Bytes()) {
		t.Errorf("second = %d, same bytes %v", second.Code, bytes.Equal(second.Body.Bytes(), first.Body.Bytes()))
	}

	html := call(t, e.server, http.MethodGet, versionExport(plan.ID, 1, "optivum.html"), asStranger, "")
	if html.Code != http.StatusOK || html.Header().Get("Content-Type") != "application/zip" {
		t.Fatalf("html = %d %q: %s", html.Code, html.Header().Get("Content-Type"), html.Body.String())
	}
	names := zipNames(t, html.Body.Bytes())
	for _, want := range []string{"index.html", "o1.html", "n1.html", "s1.html"} {
		if !slices.Contains(names, want) {
			t.Errorf("the HTML export holds %v, want %s", names, want)
		}
	}

	for _, one := range []struct {
		path, credential string
		want             int
	}{
		{versionExport(plan.ID, 1, "goptivum.gpf"), "", http.StatusUnauthorized},
		{versionExport(plan.ID, 1, "optivum.pdf"), asOwner, http.StatusBadRequest},
		{versionExport(plan.ID, 9, "goptivum.gpf"), asOwner, http.StatusNotFound},
		{versionExport(plan.ID+1, 1, "goptivum.gpf"), asOwner, http.StatusNotFound},
		{resultExport(plan.ID, 12345, "goptivum.gpf"), asOwner, http.StatusNotFound},
	} {
		if recorder := call(t, e.server, http.MethodGet, one.path, one.credential, ""); recorder.Code != one.want {
			t.Errorf("GET %s = %d %s, want %d", one.path, recorder.Code, recorder.Body.String(), one.want)
		}
	}
}

// Karol's first test: a result exported as .gpf and imported again is the
// same school and the same timetable, and the file is sealed to the school's
// own key, so nobody outside the school opens it.
func TestAnExportedResultImportsAsTheSameSchoolAndTimetable(t *testing.T) {
	e := newExportServer(t, "the school's escrow passphrase")
	plan := e.plan(t, document.New(timetabledSchool().GetModel()))
	solved := timetabledSchool()
	result := e.result(t, plan.ID, solved)

	exported := call(t, e.server, http.MethodGet, resultExport(plan.ID, result.ID, "goptivum.gpf"), asStranger, "")
	if exported.Code != http.StatusOK {
		t.Fatalf("export = %d: %s", exported.Code, exported.Body.String())
	}
	file := exported.Body.Bytes()
	if _, err := gpf.Decode(file, gpf.ReadOptions{Realm: exportRealm}); !errors.Is(err, gpf.ErrIdentityRequired) {
		t.Errorf("opened without the school's key: err = %v, want ErrIdentityRequired", err)
	}
	if bytes.Contains(file, []byte("Anna Kowalska")) {
		t.Error("the exported file carries the school in the clear")
	}

	ctx := signedIn("stranger")
	created := imported(t, importFile(t, &apiv1.Handlers{API: e.api}, ctx, "wynik.gpf", file, nil))
	if created.Importer != goptivum.ID {
		t.Errorf("importer = %s, want %s", created.Importer, goptivum.ID)
	}
	downloaded, err := e.api.DownloadVersion(ctx, gen.DownloadVersionRequestObject{PlanId: created.PlanId, Version: 1})
	if err != nil {
		t.Fatalf("download: %v", err)
	}
	body, ok := downloaded.(gen.DownloadVersion200ApplicationzstdResponse)
	if !ok {
		t.Fatalf("download = %T", downloaded)
	}
	data, err := io.ReadAll(body.Body)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	read, err := document.Decode(data)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !proto.Equal(read.GetModel(), solved.GetModel()) {
		t.Error("the imported school differs from the exported one")
	}
	if !proto.Equal(read.GetSchedule(), solved.GetSchedule()) {
		t.Errorf("the imported timetable = %v, want %v", read.GetSchedule(), solved.GetSchedule())
	}
}

// A server that holds no school key writes plain files: that is a choice a
// school makes by not configuring one.
func TestWithoutASchoolKeyTheExportIsPlain(t *testing.T) {
	e := newExportServer(t, "")
	plan := e.plan(t, timetabledSchool())
	exported := call(t, e.server, http.MethodGet, versionExport(plan.ID, 1, "goptivum.gpf"), asOwner, "")
	if exported.Code != http.StatusOK {
		t.Fatalf("export = %d: %s", exported.Code, exported.Body.String())
	}
	read, err := gpf.Decode(exported.Body.Bytes(), gpf.ReadOptions{Realm: exportRealm})
	if err != nil || read.Mode != gpf.ModePlain {
		t.Fatalf("read = %+v, %v; want a plain file", read, err)
	}
	if read.Header.Get(gpf.FieldRealm) != exportRealm || read.Header.Get(gpf.FieldSchool) != "Szkoła Eksportowa" {
		t.Errorf("header realm %q school %q", read.Header.Get(gpf.FieldRealm), read.Header.Get(gpf.FieldSchool))
	}
}

// A main file without a timetable has no HTML export, and says why; its .gpf
// still exports.
func TestAVersionWithoutATimetableHasNoHTMLExport(t *testing.T) {
	e := newExportServer(t, "")
	plan := e.plan(t, document.New(timetabledSchool().GetModel()))
	ctx := signedIn("stranger")

	response, err := e.exports.ExportVersion(ctx, gen.ExportVersionRequestObject{
		PlanId: strconv.FormatInt(plan.ID, 10), Version: 1, Format: gen.OptivumHtml,
	})
	if err != nil {
		t.Fatalf("export: %v", err)
	}
	refused, ok := response.(gen.ExportVersion422JSONResponse)
	if !ok || refused.Code != "validation" || refused.Message != messages.KeyExportNoTimetable {
		t.Fatalf("html = %T %+v, want 422 %s", response, response, messages.KeyExportNoTimetable)
	}
	if recorder := call(t, e.server, http.MethodGet, versionExport(plan.ID, 1, "goptivum.gpf"), asOwner, ""); recorder.Code != http.StatusOK {
		t.Errorf("gpf = %d: %s", recorder.Code, recorder.Body.String())
	}
}

// A result in Trash and a plan in Trash answer as absent, like every other
// read of them.
func TestWhatIsInTrashIsNotExported(t *testing.T) {
	e := newExportServer(t, "")
	plan := e.plan(t, timetabledSchool())
	result := e.result(t, plan.ID, timetabledSchool())
	if err := e.index.TrashResult(t.Context(), plan.ID, result.ID); err != nil {
		t.Fatalf("trash result: %v", err)
	}
	if recorder := call(t, e.server, http.MethodGet, resultExport(plan.ID, result.ID, "optivum.html"), asOwner, ""); recorder.Code != http.StatusNotFound {
		t.Errorf("a result in Trash = %d, want 404", recorder.Code)
	}
	if err := e.index.Trash(t.Context(), plan.ID); err != nil {
		t.Fatalf("trash plan: %v", err)
	}
	if recorder := call(t, e.server, http.MethodGet, versionExport(plan.ID, 1, "goptivum.gpf"), asOwner, ""); recorder.Code != http.StatusNotFound {
		t.Errorf("a plan in Trash = %d, want 404", recorder.Code)
	}
}

// A request that reached the handler without a session is refused, whatever
// the route in front of it did.
func TestAnExportNeedsASession(t *testing.T) {
	e := newExportServer(t, "")
	plan := e.plan(t, timetabledSchool())
	response, err := e.exports.ExportVersion(t.Context(), gen.ExportVersionRequestObject{
		PlanId: strconv.FormatInt(plan.ID, 10), Version: 1, Format: gen.GoptivumGpf,
	})
	if _, ok := response.(gen.ExportVersion401JSONResponse); err != nil || !ok {
		t.Errorf("version = %T, %v; want 401", response, err)
	}
	result, err := e.exports.ExportResult(t.Context(), gen.ExportResultRequestObject{
		PlanId: strconv.FormatInt(plan.ID, 10), ResultId: "1", Format: gen.GoptivumGpf,
	})
	if _, ok := result.(gen.ExportResult401JSONResponse); err != nil || !ok {
		t.Errorf("result = %T, %v; want 401", result, err)
	}
}

// A server wired without exports answers 404 rather than failing.
func TestWithoutExportsTheRoutesAnswerNotFound(t *testing.T) {
	var none *apiv1.Exports
	response, err := none.ExportVersion(signedIn("owner"), gen.ExportVersionRequestObject{PlanId: "1", Version: 1, Format: gen.GoptivumGpf})
	if _, ok := response.(gen.ExportVersion404JSONResponse); err != nil || !ok {
		t.Errorf("version = %T, %v; want 404", response, err)
	}
	result, err := none.ExportResult(signedIn("owner"), gen.ExportResultRequestObject{PlanId: "1", ResultId: "1", Format: gen.GoptivumGpf})
	if _, ok := result.(gen.ExportResult404JSONResponse); err != nil || !ok {
		t.Errorf("result = %T, %v; want 404", result, err)
	}
}

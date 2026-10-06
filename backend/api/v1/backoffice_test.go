// api/v1/backoffice_test.go

package v1_test

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/smegg99/s99ab/auth/kratosauth"
	"github.com/smegg99/s99ab/auth/kratosauth/kratosauthtest"
	"github.com/smegg99/s99ab/data/db"
	"github.com/smegg99/s99ab/data/storage"
	"github.com/smegg99/s99logger"

	apiv1 "github.com/smegg99/goptivum/backend/api/v1"
	"github.com/smegg99/goptivum/backend/api/v1/gen"
	"github.com/smegg99/goptivum/backend/common/config"
	"github.com/smegg99/goptivum/backend/common/logger"
	"github.com/smegg99/goptivum/backend/core/audit"
	"github.com/smegg99/goptivum/backend/core/documents"
	"github.com/smegg99/goptivum/backend/core/lockout"
	"github.com/smegg99/goptivum/backend/core/plans"
	"github.com/smegg99/goptivum/backend/formats"
	"github.com/smegg99/goptivum/backend/formats/goptivum"
	"github.com/smegg99/goptivum/backend/identity"
	"github.com/smegg99/goptivum/backend/migrations"
)

// Two accounts: Anna administers the school and has a name; Piotr plans and
// has only a username.
const (
	asAnna  = "Bearer anna-token"
	asPiotr = "Bearer piotr-token"
)

// backOffice is a server with Backend > Server, Storage, Logs and the audit
// trail over one database, one settings file and one ring of log lines.
type backOffice struct {
	server   *apiv1.Server
	settings *config.SettingsStore
	index    *plans.Repository
	conn     *sql.DB
	ring     *logger.Ring
}

func newBackOffice(t *testing.T) *backOffice {
	t.Helper()
	conn := db.TestDB(t, migrations.FS)
	settings, err := config.OpenSettings(filepath.Join(t.TempDir(), "config.yaml"))
	if err != nil {
		t.Fatalf("settings: %v", err)
	}
	started := settings.Current()
	index := plans.NewRepository(conn, db.SQLite, plans.Caps{
		BackupsKeep: started.Projects.BackupsKeep, ResultsKeep: started.Projects.ResultsKeep,
	})
	store := documents.NewStore(storage.NewLocalDir(t.TempDir(), "/objects")).WithGarbage(index)
	trail := audit.New(conn, db.SQLite)
	api, err := apiv1.NewAPI(index, store, formats.NewRegistry(goptivum.NewImporter("")), nil,
		apiv1.Settings{TrashDays: func() int { return settings.Current().Projects.TrashDays }})
	if err != nil {
		t.Fatalf("new api: %v", err)
	}

	fake, kratosAdmin := newKratosAdmin(t)
	fake.Identities["identity-anna"] = map[string]any{
		"id": "identity-anna", "state": "active", "metadata_public": map[string]any{"role": "admin"},
		"traits": map[string]any{"username": "anna", "email": "anna@example.invalid", "name": "Anna Kowalska"},
	}
	fake.Identities["identity-piotr"] = map[string]any{
		"id": "identity-piotr", "state": "active", "metadata_public": map[string]any{"role": "planner"},
		"traits": map[string]any{"username": "piotr", "email": "piotr@example.invalid"},
	}
	// Ewa's account is disabled: no plan can be given to her.
	fake.Identities["identity-ewa"] = map[string]any{
		"id": "identity-ewa", "state": "inactive", "metadata_public": map[string]any{"role": "planner"},
		"traits": map[string]any{"username": "ewa", "email": "ewa@example.invalid"},
	}
	accounts, err := identity.NewAdmin(kratosAdmin, 2*time.Second)
	if err != nil {
		t.Fatalf("admin client: %v", err)
	}

	ring := logger.NewRing(50)
	server, err := apiv1.New(apiv1.Options{
		Build:            apiv1.BuildInfo{Version: "1.2.3"},
		MinClientVersion: "0.1.0",
		Addresses:        func() []string { return settings.Current().Server.PublicAddresses },
		Handlers: &apiv1.Handlers{
			API:   api.WithAudit(trail).WithAccounts(accounts),
			Admin: apiv1.NewAdmin(nil, apiv1.Nodes{}).WithAudit(trail),
			Jobs:  apiv1.NewJobs(nil, nil, nil, nil, nil),
			ServerSettings: apiv1.NewServerSettings(settings, index, apiv1.Running{
				MaximumUploadBytes: started.Server.MaximumUploadBytes,
				RateLimitPerMinute: started.Server.RateLimitPerMinute,
			}, trail),
			Security: apiv1.NewSecurity(settings, lockout.New(apiv1.LimitsOf(started.Lockout), nil), "").
				WithAudit(trail),
			Storage:    apiv1.NewStorage(index, store, accounts, trail),
			Logs:       apiv1.NewLogs(ring, logger.Translator()),
			AuditTrail: apiv1.NewAuditTrail(trail),
		},
		Kratos: kratosauthtest.Fake(t, map[string]kratosauth.Identity{
			asAnna: {
				ID:             "identity-anna",
				MetadataPublic: json.RawMessage(`{"role":"admin"}`),
				Traits:         json.RawMessage(`{"username":"anna","name":"Anna Kowalska"}`),
			},
			asPiotr: {
				ID:             "identity-piotr",
				MetadataPublic: json.RawMessage(`{"role":"planner"}`),
				Traits:         json.RawMessage(`{"username":"piotr"}`),
			},
		}),
	})
	if err != nil {
		t.Fatalf("new server: %v", err)
	}
	return &backOffice{server: server, settings: settings, index: index, conn: conn, ring: ring}
}

// answer calls a route and decodes a 200 or 201 body into T.
func answer[T any](t *testing.T, office *backOffice, method, path, credential, body string) T {
	t.Helper()
	recorder := call(t, office.server, method, path, credential, body)
	if recorder.Code != http.StatusOK && recorder.Code != http.StatusCreated {
		t.Fatalf("%s %s = %d: %s", method, path, recorder.Code, recorder.Body.String())
	}
	var out T
	if err := json.Unmarshal(recorder.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode %s: %v", recorder.Body.String(), err)
	}
	return out
}

// newProject creates an empty project over HTTP, the way the desktop does.
func newProject(t *testing.T, office *backOffice, credential, name string) gen.Plan {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	_ = writer.WriteField("name", name)
	_ = writer.WriteField("school_year", "2026/2027")
	_ = writer.Close()
	request := httptest.NewRequest(http.MethodPost, "/api/v1/plans", &body)
	request.Header.Set("Content-Type", writer.FormDataContentType())
	request.Header.Set("Authorization", credential)
	recorder := httptest.NewRecorder()
	office.server.Handler().ServeHTTP(recorder, request)
	if recorder.Code != http.StatusCreated {
		t.Fatalf("create %s = %d: %s", name, recorder.Code, recorder.Body.String())
	}
	var plan gen.Plan
	if err := json.Unmarshal(recorder.Body.Bytes(), &plan); err != nil {
		t.Fatalf("decode: %v", err)
	}
	return plan
}

func deleteProject(t *testing.T, office *backOffice, credential string, plan gen.Plan) {
	t.Helper()
	if recorder := call(t, office.server, http.MethodDelete, "/api/v1/plans/"+plan.Id, credential, ""); recorder.Code != http.StatusNoContent {
		t.Fatalf("delete %s = %d: %s", plan.Name, recorder.Code, recorder.Body.String())
	}
}

func auditPage(t *testing.T, office *backOffice, query string) gen.AuditPage {
	t.Helper()
	return answer[gen.AuditPage](t, office, http.MethodGet, "/api/v1/audit"+query, asAnna, "")
}

// Karol's tests 1-3: a new default cap reads on a new project, Trash dates
// follow the new days, and a public address is published at once. The
// upload cap and the limiter are stored and wait for a restart.
func TestServerSettingsApplyAsTheySay(t *testing.T) {
	office := newBackOffice(t)

	read := answer[gen.ServerSettings](t, office, http.MethodGet, "/api/v1/server/settings", asAnna, "")
	if read.BackupsKeep != 100 || read.ResultsKeep != 20 || read.TrashDays != 30 ||
		read.MaximumUploadBytes != 16<<20 || read.RateLimitPerMinute != 600 ||
		read.PublicAddresses == nil || len(read.PublicAddresses) != 0 || len(read.RestartPending) != 0 {
		t.Errorf("defaults = %+v", read)
	}
	want := gen.ServerSettingsApplies{
		PublicAddresses: gen.Now, TrashDays: gen.Now, BackupsKeep: gen.Now, ResultsKeep: gen.Now,
		MaximumUploadBytes: gen.Restart, RateLimitPerMinute: gen.Restart,
	}
	if read.Applies != want {
		t.Errorf("applies = %+v, want %+v", read.Applies, want)
	}

	changed := answer[gen.ServerSettings](t, office, http.MethodPatch, "/api/v1/server/settings", asAnna,
		`{"backups_keep":50,"trash_days":7,"public_addresses":["  http://192.0.2.10:8833 "]}`)
	if changed.BackupsKeep != 50 || changed.TrashDays != 7 ||
		!slices.Equal(changed.PublicAddresses, []string{"http://192.0.2.10:8833"}) || len(changed.RestartPending) != 0 {
		t.Errorf("changed = %+v", changed)
	}

	plan := newProject(t, office, asAnna, "Plan 2027")
	if plan.BackupsKeep != 0 || plan.BackupsKeepDefault != 50 {
		t.Errorf("new project caps = %d / %d, want its own none and the default 50", plan.BackupsKeep, plan.BackupsKeepDefault)
	}

	throwaway := newProject(t, office, asAnna, "Do usunięcia")
	deleteProject(t, office, asAnna, throwaway)
	trash := answer[gen.TrashList](t, office, http.MethodGet, "/api/v1/trash", asAnna, "")
	if len(trash.Items) != 1 || !trash.Items[0].RemovesAt.Equal(trash.Items[0].DeletedAt.AddDate(0, 0, 7)) {
		t.Errorf("trash = %+v, want the removal date 7 days out", trash.Items)
	}

	info := answer[gen.Info](t, office, http.MethodGet, "/api/info", "", "")
	if info.Addresses == nil || !slices.Equal(*info.Addresses, []string{"http://192.0.2.10:8833"}) {
		t.Errorf("published = %v, want the new address", info.Addresses)
	}
	answer[gen.ServerSettings](t, office, http.MethodPatch, "/api/v1/server/settings", asAnna, `{"public_addresses":[]}`)
	if info := answer[gen.Info](t, office, http.MethodGet, "/api/info", "", ""); info.Addresses != nil {
		t.Errorf("published after removing = %v, want none", *info.Addresses)
	}

	restart := answer[gen.ServerSettings](t, office, http.MethodPatch, "/api/v1/server/settings", asAnna,
		`{"maximum_upload_bytes":33554432,"rate_limit_per_minute":0}`)
	if restart.MaximumUploadBytes != 32<<20 || restart.RateLimitPerMinute != 0 ||
		!slices.Equal(restart.RestartPending, []gen.ServerSettingName{gen.MaximumUploadBytes, gen.RateLimitPerMinute}) {
		t.Errorf("restart settings = %+v, want both stored and pending", restart)
	}
	if stored := office.settings.Current().Server; stored.MaximumUploadBytes != 32<<20 {
		t.Errorf("stored = %+v", stored)
	}
	back := answer[gen.ServerSettings](t, office, http.MethodPatch, "/api/v1/server/settings", asAnna,
		`{"maximum_upload_bytes":16777216,"rate_limit_per_minute":600}`)
	if len(back.RestartPending) != 0 {
		t.Errorf("pending after going back = %v, want none", back.RestartPending)
	}

	entries := auditPage(t, office, "?kind=setting")
	if entries.Total != 4 {
		t.Fatalf("setting entries = %+v, want one per change", entries)
	}
	first := entries.Items[len(entries.Items)-1]
	if first.Action != gen.AuditActionSettingChanged || first.Target.Id != "server" ||
		first.Details["backups_keep"] != "50" || first.Details["trash_days"] != "7" ||
		first.Details["public_addresses"] != "http://192.0.2.10:8833" || len(first.Details) != 3 {
		t.Errorf("first change = %+v", first)
	}
}

// A refused value names its field and changes nothing, in the file or in
// force.
func TestServerSettingsRefuseWhatTheSchemaRefuses(t *testing.T) {
	office := newBackOffice(t)
	before, err := os.ReadFile(office.settings.Path())
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	seventeen := make([]string, 17)
	for i := range seventeen {
		seventeen[i] = fmt.Sprintf("%q", fmt.Sprintf("http://192.0.2.%d", i+1))
	}
	for body, want := range map[string]map[string]string{
		`{}`: {"body": "empty"},
		`{"public_addresses":["ftp://192.0.2.10"]}`:                       {"public_addresses[0]": "invalid"},
		`{"public_addresses":["http://192.0.2.10","192.0.2.11:8833"]}`:    {"public_addresses[1]": "invalid"},
		`{"public_addresses":["http://me@192.0.2.10"]}`:                   {"public_addresses[0]": "invalid"},
		`{"public_addresses":["http://192.0.2.10?x=1"]}`:                  {"public_addresses[0]": "invalid"},
		`{"public_addresses":["HTTP://192.0.2.10"]}`:                      {"public_addresses[0]": "invalid"},
		`{"public_addresses":["http://192.0.2.10","http://192.0.2.10 "]}`: {"public_addresses[1]": "duplicate"},
		`{"public_addresses":[` + strings.Join(seventeen, ",") + `]}`:     {"public_addresses": "too_many"},
		`{"trash_days":0}`:                   {"trash_days": "invalid"},
		`{"trash_days":3651}`:                {"trash_days": "invalid"},
		`{"backups_keep":10001}`:             {"backups_keep": "invalid"},
		`{"results_keep":0}`:                 {"results_keep": "invalid"},
		`{"rate_limit_per_minute":-1}`:       {"rate_limit_per_minute": "invalid"},
		`{"maximum_upload_bytes":1023}`:      {"maximum_upload_bytes": "invalid"},
		`{"maximum_upload_bytes":268435457}`: {"maximum_upload_bytes": "invalid"},
		`{"backups_keep":5,"trash_days":0}`:  {"trash_days": "invalid"},
	} {
		recorder := call(t, office.server, http.MethodPatch, "/api/v1/server/settings", asAnna, body)
		if recorder.Code != http.StatusUnprocessableEntity {
			t.Errorf("%s = %d, want 422: %s", body, recorder.Code, recorder.Body.String())
			continue
		}
		envelope := decodeError(t, recorder)
		if envelope.Details == nil || fmt.Sprint(*envelope.Details) != fmt.Sprint(want) {
			t.Errorf("%s: details = %v, want %v", body, envelope.Details, want)
		}
	}
	after, err := os.ReadFile(office.settings.Path())
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(after) != string(before) || office.index.DefaultBackupsKeep() != 100 {
		t.Errorf("a refused change was written or applied:\n%s", after)
	}
	if entries := auditPage(t, office, ""); entries.Total != 0 {
		t.Errorf("a refused change was audited: %+v", entries)
	}
}

// Karol's test 4 and the storage actions: every owner's projects with their
// sizes and owners, and an empty Trash afterwards.
func TestStorageSeesEveryOwnerAndEmptiesEveryTrash(t *testing.T) {
	office := newBackOffice(t)
	mine := newProject(t, office, asAnna, "Plan 2027")
	theirs := newProject(t, office, asPiotr, "M0 weryfikacja")
	gone := newProject(t, office, asPiotr, "Do usunięcia")
	deleteProject(t, office, asPiotr, gone)

	usage := answer[gen.StorageUsage](t, office, http.MethodGet, "/api/v1/storage", asAnna, "")
	if len(usage.Projects) != 3 {
		t.Fatalf("projects = %+v, want every owner's three", usage.Projects)
	}
	byName := map[string]gen.ProjectStorage{}
	for _, project := range usage.Projects {
		byName[project.Name] = project
	}
	if project := byName["Plan 2027"]; project.PlanId != mine.Id || project.Owner.Id != "identity-anna" ||
		project.Owner.Name == nil || *project.Owner.Name != "Anna Kowalska" ||
		project.MainFile.Count != 1 || project.MainFile.Bytes <= 0 || project.DeletedAt != nil {
		t.Errorf("Plan 2027 = %+v", project)
	}
	if project := byName["M0 weryfikacja"]; project.PlanId != theirs.Id || project.Owner.Name == nil || *project.Owner.Name != "piotr" {
		t.Errorf("M0 weryfikacja = %+v, want Piotr's username as the owner", project)
	}
	if project := byName["Do usunięcia"]; project.DeletedAt == nil {
		t.Errorf("the deleted project = %+v, want its deletion time", project)
	}
	if usage.StoredObjects != 3 || usage.StoredBytes <= 0 {
		t.Errorf("stored = %d objects, %d bytes", usage.StoredObjects, usage.StoredBytes)
	}

	emptied := answer[gen.TrashEmptied](t, office, http.MethodPost, "/api/v1/storage/trash/empty", asAnna, "")
	if emptied != (gen.TrashEmptied{Projects: 1}) {
		t.Errorf("emptied = %+v, want Piotr's one project", emptied)
	}
	if trash := answer[gen.TrashList](t, office, http.MethodGet, "/api/v1/trash", asPiotr, ""); len(trash.Items) != 0 {
		t.Errorf("Piotr's Trash = %+v, want empty", trash.Items)
	}
	after := answer[gen.StorageUsage](t, office, http.MethodGet, "/api/v1/storage", asAnna, "")
	if len(after.Projects) != 2 || after.WaitingObjects != 1 {
		t.Errorf("after = %d projects, %d waiting; want 2 and the purged object waiting", len(after.Projects), after.WaitingObjects)
	}

	// The purged object waits an hour; the sweep takes it once it has.
	swept := answer[gen.ObjectsSwept](t, office, http.MethodPost, "/api/v1/storage/sweep", asAnna, "")
	if swept != (gen.ObjectsSwept{Deleted: 0, Waiting: 1}) {
		t.Errorf("swept too early = %+v", swept)
	}
	if _, err := office.conn.Exec(`update object_garbage set since = ?`, time.Now().UTC().Add(-2*time.Hour)); err != nil {
		t.Fatalf("age the garbage: %v", err)
	}
	if swept := answer[gen.ObjectsSwept](t, office, http.MethodPost, "/api/v1/storage/sweep", asAnna, ""); swept != (gen.ObjectsSwept{Deleted: 1}) {
		t.Errorf("swept = %+v, want the purged object deleted", swept)
	}

	entries := auditPage(t, office, "?kind=storage")
	actions := []gen.AuditAction{}
	for _, entry := range entries.Items {
		actions = append(actions, entry.Action)
	}
	// The sweep that deleted nothing changed nothing, so it is not there.
	if !slices.Equal(actions, []gen.AuditAction{gen.AuditActionStorageSwept, gen.AuditActionStorageTrashEmptied}) {
		t.Fatalf("storage entries = %v", actions)
	}
	if swept := entries.Items[0]; swept.Details["deleted"] != "1" || swept.Details["waiting"] != "0" {
		t.Errorf("swept entry = %+v", swept)
	}
	if emptiedEntry := entries.Items[1]; emptiedEntry.Details["projects"] != "1" || emptiedEntry.Actor.Name == nil || *emptiedEntry.Actor.Name != "Anna Kowalska" {
		t.Errorf("emptied entry = %+v", emptiedEntry)
	}
}

// Trimming applies the cap in force to every owner's projects now.
func TestTrimBackupsAppliesTheNewDefaultNow(t *testing.T) {
	office := newBackOffice(t)
	plan := newProject(t, office, asPiotr, "Plan 2027")
	id := mustID(t, plan.Id)
	for parent := int64(1); parent < 4; parent++ {
		if _, err := office.index.AddVersion(t.Context(), plans.Upload{
			PlanID: id, Parent: parent, CreatedBy: "identity-piotr",
			ObjectKey: fmt.Sprintf("plans/synthetic/v%d", parent+1), SizeBytes: 10,
		}); err != nil {
			t.Fatalf("upload: %v", err)
		}
	}
	answer[gen.ServerSettings](t, office, http.MethodPatch, "/api/v1/server/settings", asAnna, `{"backups_keep":1}`)

	trimmed := answer[gen.BackupsTrimmed](t, office, http.MethodPost, "/api/v1/storage/backups/trim", asAnna, "")
	if trimmed.Versions != 2 {
		t.Errorf("trimmed = %d, want 2 of the 3 backups", trimmed.Versions)
	}
	versions := answer[gen.VersionList](t, office, http.MethodGet, "/api/v1/plans/"+plan.Id+"/versions", asPiotr, "")
	if len(versions.Items) != 2 {
		t.Errorf("versions = %d, want the main file and one backup", len(versions.Items))
	}
}

func mustID(t *testing.T, raw string) int64 {
	t.Helper()
	var id int64
	if _, err := fmt.Sscan(raw, &id); err != nil {
		t.Fatalf("plan id %q: %v", raw, err)
	}
	return id
}

// Karol's test 6: recent lines, newest first, narrowed by level, worded in
// the language asked for, with secrets redacted.
func TestLogsListRecentLinesByLevel(t *testing.T) {
	office := newBackOffice(t)
	log := s99logger.New(office.ring, s99logger.Options{
		Service: "goptivum-server", Language: "en", Translator: logger.Translator(),
	})
	log.Info(s99logger.NewEvent(logger.EventServerStarted, s99logger.String("listen", "127.0.0.1:8833")))
	log.Warn(s99logger.NewEvent(logger.EventBootstrapAdminCreated,
		s99logger.String("username", "admin"), s99logger.String("password", "Tajne-Haslo-123")))
	log.Error(s99logger.NewEvent(logger.EventObjectSweepFailed, s99logger.String("error", "synthetic")))

	all := answer[gen.LogLines](t, office, http.MethodGet, "/api/v1/logs", asAnna, "")
	events := []string{}
	for _, line := range all.Items {
		events = append(events, line.Event)
	}
	if !slices.Equal(events, []string{string(logger.EventObjectSweepFailed), string(logger.EventBootstrapAdminCreated), string(logger.EventServerStarted)}) {
		t.Errorf("events = %v, want newest first", events)
	}
	if all.Capacity != 50 {
		t.Errorf("capacity = %d", all.Capacity)
	}
	created := all.Items[1]
	if created.Level != gen.LogLevelWarn || created.Fields == nil ||
		(*created.Fields)["password"] != logger.Redacted || (*created.Fields)["username"] != "admin" {
		t.Errorf("bootstrap line = %+v", created)
	}
	if strings.Contains(fmt.Sprint(all), "Tajne-Haslo-123") {
		t.Error("the password reached the answer")
	}

	errorsOnly := answer[gen.LogLines](t, office, http.MethodGet, "/api/v1/logs?level=error", asAnna, "")
	if len(errorsOnly.Items) != 1 || errorsOnly.Items[0].Level != gen.LogLevelError {
		t.Errorf("errors = %+v", errorsOnly.Items)
	}
	limited := answer[gen.LogLines](t, office, http.MethodGet, "/api/v1/logs?limit=1", asAnna, "")
	if len(limited.Items) != 1 {
		t.Errorf("limit 1 = %d lines", len(limited.Items))
	}

	english := all.Items[2].Message
	polish := answer[gen.LogLines](t, office, http.MethodGet, "/api/v1/logs?lang=pl", asAnna, "").Items[2].Message
	if english == "" || polish == "" || english == polish || english == string(logger.EventServerStarted) {
		t.Errorf("messages = %q / %q, want a sentence in each language", english, polish)
	}

	for _, query := range []string{"?level=loud", "?lang=de"} {
		if recorder := call(t, office.server, http.MethodGet, "/api/v1/logs"+query, asAnna, ""); recorder.Code != http.StatusUnprocessableEntity {
			t.Errorf("%s = %d, want 422", query, recorder.Code)
		}
	}
}

// Karol's test 5: a rename and its reverse are two entries, newest first,
// with the old and new names and who did it.
func TestTheAuditTrailRecordsProjectChanges(t *testing.T) {
	office := newBackOffice(t)
	plan := newProject(t, office, asAnna, "Plan 2027")
	for _, name := range []string{"Plan 2027 test", "Plan 2027"} {
		answer[gen.Plan](t, office, http.MethodPatch, "/api/v1/plans/"+plan.Id, asAnna, `{"name":"`+name+`"}`)
	}
	answer[gen.Plan](t, office, http.MethodPatch, "/api/v1/plans/"+plan.Id, asAnna, `{"school_year":"2027/2028","backups_keep":5}`)
	deleteProject(t, office, asAnna, plan)
	answer[gen.Plan](t, office, http.MethodPost, "/api/v1/plans/"+plan.Id+"/restore", asAnna, "")

	page := auditPage(t, office, "?kind=project")
	actions := []gen.AuditAction{}
	for _, entry := range page.Items {
		actions = append(actions, entry.Action)
	}
	want := []gen.AuditAction{
		gen.AuditActionProjectRestored, gen.AuditActionProjectDeleted, gen.AuditActionProjectChanged,
		gen.AuditActionProjectRenamed, gen.AuditActionProjectRenamed, gen.AuditActionProjectCreated,
	}
	if !slices.Equal(actions, want) {
		t.Fatalf("actions = %v, want %v", actions, want)
	}
	back, there := page.Items[3], page.Items[4]
	if there.Details["from"] != "Plan 2027" || there.Details["to"] != "Plan 2027 test" ||
		back.Details["from"] != "Plan 2027 test" || back.Details["to"] != "Plan 2027" {
		t.Errorf("renames = %v then %v", there.Details, back.Details)
	}
	for _, entry := range page.Items {
		if entry.Actor.Id != "identity-anna" || entry.Actor.Name == nil || *entry.Actor.Name != "Anna Kowalska" ||
			entry.Target.Kind != gen.AuditKindProject || entry.Target.Id != plan.Id {
			t.Errorf("entry = %+v", entry)
		}
	}
	if changed := page.Items[2].Details; changed["school_year"] != "2027/2028" || changed["backups_keep"] != "5" || len(changed) != 2 {
		t.Errorf("changed = %v", changed)
	}
	if created := page.Items[5].Details; created["source"] != "new" {
		t.Errorf("created = %v", created)
	}

	paged := auditPage(t, office, "?page=2&limit=4")
	if paged.Total != 6 || len(paged.Items) != 2 || paged.Page != 2 || paged.Limit != 4 {
		t.Errorf("page 2 = %+v", paged)
	}
	if recorder := call(t, office.server, http.MethodGet, "/api/v1/audit?kind=everything", asAnna, ""); recorder.Code != http.StatusUnprocessableEntity {
		t.Errorf("an unknown kind = %d, want 422", recorder.Code)
	}
}

// A planner reaches none of it, not even to learn it exists.
func TestTheBackOfficeIsClosedToPlanners(t *testing.T) {
	office := newBackOffice(t)
	for _, route := range []struct{ method, path string }{
		{http.MethodGet, "/api/v1/server/settings"},
		{http.MethodPatch, "/api/v1/server/settings"},
		{http.MethodGet, "/api/v1/storage"},
		{http.MethodPost, "/api/v1/storage/trash/empty"},
		{http.MethodPost, "/api/v1/storage/backups/trim"},
		{http.MethodPost, "/api/v1/storage/sweep"},
		{http.MethodGet, "/api/v1/logs"},
		{http.MethodGet, "/api/v1/audit"},
	} {
		if recorder := call(t, office.server, route.method, route.path, asPiotr, `{"trash_days":1}`); recorder.Code != http.StatusNotFound {
			t.Errorf("%s %s as a planner = %d, want 404", route.method, route.path, recorder.Code)
		}
	}
	if days := office.settings.Current().Projects.TrashDays; days != 30 {
		t.Errorf("a planner changed Trash days to %d", days)
	}
}

// A lockout change is a setting change like the server's, with only what
// changed.
func TestALockoutChangeIsAudited(t *testing.T) {
	office := newBackOffice(t)
	answer[gen.Lockout](t, office, http.MethodPatch, "/api/v1/security/lockout", asAnna,
		`{"account_failures":3,"block_minutes":15}`)
	page := auditPage(t, office, "")
	if page.Total != 1 {
		t.Fatalf("trail = %+v", page)
	}
	entry := page.Items[0]
	if entry.Action != gen.AuditActionSettingChanged || entry.Target.Kind != gen.AuditKindSetting ||
		entry.Target.Id != "lockout" || fmt.Sprint(entry.Details) != "map[account_failures:3]" {
		t.Errorf("entry = %+v", entry)
	}
}

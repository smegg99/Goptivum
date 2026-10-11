// api/v1/handlers.go

package v1

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"strconv"
	"strings"
	"time"

	openapi_types "github.com/oapi-codegen/runtime/types"
	"github.com/smegg99/s99ab/auth/kratosauth"
	"github.com/smegg99/s99ab/net/httpmw"

	arrangov1 "github.com/smegg99/arrango/backend/gen/arrangov1"

	"github.com/smegg99/goptivum/backend/api/v1/gen"
	"github.com/smegg99/goptivum/backend/arrango"
	"github.com/smegg99/goptivum/backend/common/messages"
	"github.com/smegg99/goptivum/backend/core/audit"
	"github.com/smegg99/goptivum/backend/core/documents"
	"github.com/smegg99/goptivum/backend/core/plans"
	"github.com/smegg99/goptivum/backend/formats"
	"github.com/smegg99/goptivum/backend/identity"
	"github.com/smegg99/goptivum/document"
)

// defaultLimit and maxLimit mirror apipage, which the contract's pagination
// parameters describe.
const (
	defaultLimit = 25
	maxLimit     = 200
)

// API implements the generated contract over the plan index, the document
// store and the importers. It holds no transport concerns: every decision here
// is about plans, not about HTTP.
type API struct {
	plans     *plans.Repository
	documents *documents.Store
	importers *formats.Registry
	// runner is nil when no node can be reached, and then no solve runs that
	// a delete would have to stop.
	runner *arrango.Runner
	// trashDays reads how long a deleted plan, checkpoint or result stays in
	// Trash, at each use.
	trashDays func() int
	// uploadLimit bounds each part of a multipart body.
	uploadLimit int64
	// trail records who created, changed, deleted, restored, forked and
	// transferred which plan; nil records nothing.
	trail *audit.Log
	// accounts names the plans' owners and checks the account a plan is
	// transferred to; nil leaves owners other than the caller unnamed and
	// answers a transfer with 404.
	accounts *identity.Admin
	// language is what the server renders in: a fork with no name is named
	// in it.
	language string
}

// Settings are the parts of the server configuration the handlers act on.
type Settings struct {
	// TrashDays reads how long a deleted plan, checkpoint or result stays in
	// Trash. It is read at each use, so a change applies at once. Required.
	TrashDays func() int
	// MaxUploadSize bounds each part of a multipart body, and should be the
	// cap the upload routes carry. Zero is this package's MaxUploadSize.
	MaxUploadSize int64
	// Language is the server's own, application.language; empty is the
	// catalogue's default.
	Language string
}

// NewAPI wires the handlers. The index, the store, the importers and
// TrashDays are required, and runner may be nil.
func NewAPI(index *plans.Repository, store *documents.Store, importers *formats.Registry,
	runner *arrango.Runner, settings Settings) (*API, error) {
	if index == nil || store == nil || importers == nil {
		return nil, fmt.Errorf("api: the plan index, the document store and the importers are all required")
	}
	if settings.TrashDays == nil {
		return nil, fmt.Errorf("api: no Trash days")
	}
	return &API{plans: index, documents: store, importers: importers,
		runner: runner, trashDays: settings.TrashDays,
		uploadLimit: orDefault(settings.MaxUploadSize, MaxUploadSize), language: settings.Language}, nil
}

// WithAudit records plan changes in trail.
func (a *API) WithAudit(trail *audit.Log) *API {
	a.trail = trail
	return a
}

// WithAccounts names the plans' owners from accounts, and lets an
// administrator transfer a plan to one of them.
func (a *API) WithAccounts(accounts *identity.Admin) *API {
	a.accounts = accounts
	return a
}

// planTarget is a plan as the audit trail names it.
func planTarget(plan plans.Plan) audit.Target {
	return audit.Target{Kind: audit.Project, ID: strconv.FormatInt(plan.ID, 10), Name: plan.Name}
}

// callerID is the signed-in identity. Every route that reaches a handler has
// passed kratosauth.Require, so an absent identity is a wiring mistake rather
// than an anonymous caller.
func callerID(ctx context.Context) (string, bool) {
	identity, ok := kratosauth.FromContext(ctx)
	if !ok || identity.ID == "" {
		return "", false
	}
	return identity.ID, true
}

func unauthorized() gen.Error {
	return gen.Error{Code: "unauthorized", Message: messages.KeyErrorUnauthorized}
}

func notFound() gen.Error {
	return gen.Error{Code: "not_found", Message: messages.KeyErrorNotFound}
}

// refused turns a domain refusal into the envelope.
//
// A keyed error already carries the key the desktop resolves and the
// parameters that key needs, so a refusal the domain worded carefully reaches
// the person who caused it instead of collapsing into one generic reason.
func refused(err error) (gen.Error, bool) {
	var keyed messages.Keyed
	if !errors.As(err, &keyed) {
		return gen.Error{}, false
	}
	out := gen.Error{Code: "validation", Message: keyed.Key()}
	if data := keyed.Data(); len(data) > 0 {
		details := make(map[string]string, len(data))
		for name, value := range data {
			details[name] = fmt.Sprint(value)
		}
		out.Details = &details
	}
	return out, true
}

func validation(field, code string) gen.Error {
	return gen.Error{
		Code: "validation", Message: messages.KeyErrorValidation,
		Details: &map[string]string{field: code},
	}
}

func unavailable() gen.Error {
	return gen.Error{Code: "unavailable", Message: messages.KeyErrorUnavailable}
}

// tooLarge refuses an upload over limit, naming the limit in bytes.
func tooLarge(limit int64) gen.Error {
	return gen.Error{
		Code: "too_large", Message: messages.KeyErrorRequestTooLarge,
		Details: &map[string]string{"Limit": strconv.FormatInt(limit, 10)},
	}
}

// invalid turns a field the plan index refused into the envelope. The second
// value is false when err is not such a refusal.
func invalid(err error) (gen.Error, bool) {
	var refusal *plans.Invalid
	if !errors.As(err, &refusal) {
		return gen.Error{}, false
	}
	return validation(refusal.Field, refusal.Code), true
}

// planID reads the decimal string a path carries. The id is 64-bit, and a JSON
// number is a double in every browser.
func planID(raw string) (int64, bool) {
	id, err := strconv.ParseInt(strings.TrimSpace(raw), 10, 64)
	if err != nil || id <= 0 {
		return 0, false
	}
	return id, true
}

func versionBody(version plans.Version) gen.Version {
	body := gen.Version{
		PlanId:    strconv.FormatInt(version.PlanID, 10),
		Version:   version.Version,
		ObjectKey: version.ObjectKey,
		SizeBytes: version.SizeBytes,
		CreatedAt: version.CreatedAt,
		CreatedBy: version.CreatedBy,
	}
	if version.Parent != 0 {
		parent := version.Parent
		body.Parent = &parent
	}
	if !version.ValidFrom.IsZero() {
		body.ValidFrom = &openapi_types.Date{Time: version.ValidFrom}
	}
	if !version.ValidTo.IsZero() {
		body.ValidTo = &openapi_types.Date{Time: version.ValidTo}
	}
	if len(version.Summary) > 0 {
		summary := gen.VersionSummary(version.Summary)
		body.Summary = &summary
	}
	// A checkpoint in Trash is listed as the plain version it now is.
	if version.Checkpoint != "" && version.CheckpointDeletedAt.IsZero() {
		body.Checkpoint = pointerTo(version.Checkpoint)
	}
	return body
}

// ListPlans returns one page of the school's live plans.
func (a *API) ListPlans(ctx context.Context, request gen.ListPlansRequestObject) (gen.ListPlansResponseObject, error) {
	if _, ok := callerID(ctx); !ok {
		return gen.ListPlans401JSONResponse{UnauthorizedJSONResponse: gen.UnauthorizedJSONResponse(unauthorized())}, nil
	}

	page, limit := pageOf(request.Params.Page, request.Params.Limit)
	query := ""
	if request.Params.Q != nil {
		query = *request.Params.Q
	}
	shown, total, err := a.plans.ListPage(ctx, query, pageOffset(page, limit), limit)
	if err != nil {
		return gen.ListPlans503JSONResponse{UnavailableJSONResponse: gen.UnavailableJSONResponse(unavailable())}, nil
	}
	view, err := a.viewOf(ctx, shown...)
	if err != nil {
		return gen.ListPlans503JSONResponse{UnavailableJSONResponse: gen.UnavailableJSONResponse(unavailable())}, nil
	}
	items := make([]gen.Plan, 0, len(shown))
	for _, plan := range shown {
		items = append(items, a.planBody(plan, view))
	}
	return gen.ListPlans200JSONResponse{
		Items: items, Page: page, Limit: limit, Total: total,
	}, nil
}

// pageOf clamps rather than rejects: a page of 0 or a limit of 10000 is
// answerable, and a 422 on a stale bookmark turns a table nobody can load into
// a bug report.
func pageOf(page, limit *int) (int, int) {
	resolvedPage := 1
	if page != nil && *page > 1 {
		resolvedPage = *page
	}
	resolvedLimit := defaultLimit
	if limit != nil && *limit > 0 {
		resolvedLimit = *limit
	}
	if resolvedLimit > maxLimit {
		resolvedLimit = maxLimit
	}
	return resolvedPage, resolvedLimit
}

func pageOffset(page, limit int) int {
	const maxInt = int(^uint(0) >> 1)
	if page-1 > maxInt/limit {
		return maxInt
	}
	return (page - 1) * limit
}

// CreatePlan makes a plan together with its version 1: the document sent, or
// an empty school named after the plan.
func (a *API) CreatePlan(ctx context.Context, request gen.CreatePlanRequestObject) (gen.CreatePlanResponseObject, error) {
	identity, ok := callerID(ctx)
	if !ok {
		return gen.CreatePlan401JSONResponse{UnauthorizedJSONResponse: gen.UnauthorizedJSONResponse(unauthorized())}, nil
	}
	form, err := readForm(request.Body, "document", a.uploadLimit)
	if errors.Is(err, errTooLarge) {
		return gen.CreatePlan413JSONResponse{TooLargeJSONResponse: gen.TooLargeJSONResponse(tooLarge(a.uploadLimit))}, nil
	}
	if err != nil {
		return gen.CreatePlan422JSONResponse{ValidationJSONResponse: gen.ValidationJSONResponse(validation("document", "unreadable"))}, nil
	}
	// Checked before anything is stored, so a refused name leaves no object.
	name, year, err := plans.Clean(form.fields["name"], form.fields["school_year"])
	if refusal, ok := invalid(err); ok {
		return gen.CreatePlan422JSONResponse{ValidationJSONResponse: gen.ValidationJSONResponse(refusal)}, nil
	}

	var encoded document.Encoded
	if form.hasFile {
		encoded, _, err = document.FromBytes(form.file)
		if err != nil {
			return gen.CreatePlan422JSONResponse{ValidationJSONResponse: gen.ValidationJSONResponse(validation("document", "unreadable"))}, nil
		}
	} else {
		// No week yet, so it cannot be checked or solved until one is added.
		encoded, err = document.Encode(document.New(&arrangov1.SchoolModel{Name: name}))
		if err != nil {
			return nil, err
		}
	}

	plan, _, err := a.createWith(ctx, identity, name, year, encoded, sourceNew)
	if refusal, ok := invalid(err); ok {
		return gen.CreatePlan422JSONResponse{ValidationJSONResponse: gen.ValidationJSONResponse(refusal)}, nil
	}
	if err != nil {
		return gen.CreatePlan503JSONResponse{UnavailableJSONResponse: gen.UnavailableJSONResponse(unavailable())}, nil
	}
	body, err := a.planAnswer(ctx, plan)
	if err != nil {
		return gen.CreatePlan503JSONResponse{UnavailableJSONResponse: gen.UnavailableJSONResponse(unavailable())}, nil
	}
	return gen.CreatePlan201JSONResponse(body), nil
}

// Where a new plan's first document came from, as the audit trail says it.
const (
	sourceNew    = "new"
	sourceImport = "import"
	sourceDemo   = "demo"
	sourceFork   = "fork"
)

// createWith stores a document and makes it version 1 of a new plan, in one
// transaction with the plan row. A document stored for a plan that then fails
// goes to object_garbage for the sweep; a plan without a version would be a
// bug.
func (a *API) createWith(ctx context.Context, identity, name, schoolYear string,
	encoded document.Encoded, source string) (plans.Plan, plans.Version, error) {
	key, err := a.documents.Put(ctx, encoded)
	if err != nil {
		return plans.Plan{}, plans.Version{}, err
	}
	plan, version, err := a.plans.Create(ctx, plans.Plan{
		OwnerIdentityID: identity, Name: name, SchoolYear: schoolYear,
	}, plans.Upload{ObjectKey: key, SizeBytes: encoded.Size, CreatedBy: identity})
	if err != nil {
		_ = a.plans.Discard(ctx, key)
		return plan, version, err
	}
	audited(ctx, a.trail, audit.ProjectCreated, planTarget(plan), map[string]string{"source": source})
	return plan, version, nil
}

func schoolYear(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

// GetPlan reads one live plan, whoever owns it.
func (a *API) GetPlan(ctx context.Context, request gen.GetPlanRequestObject) (gen.GetPlanResponseObject, error) {
	if _, ok := callerID(ctx); !ok {
		return gen.GetPlan401JSONResponse{UnauthorizedJSONResponse: gen.UnauthorizedJSONResponse(unauthorized())}, nil
	}
	plan, err := a.readable(ctx, request.PlanId)
	if err != nil {
		if errors.Is(err, plans.ErrNotFound) {
			return gen.GetPlan404JSONResponse{NotFoundJSONResponse: gen.NotFoundJSONResponse(notFound())}, nil
		}
		return gen.GetPlan503JSONResponse{UnavailableJSONResponse: gen.UnavailableJSONResponse(unavailable())}, nil
	}
	body, err := a.planAnswer(ctx, plan)
	if err != nil {
		return gen.GetPlan503JSONResponse{UnavailableJSONResponse: gen.UnavailableJSONResponse(unavailable())}, nil
	}
	return gen.GetPlan200JSONResponse(body), nil
}

// UpdatePlan renames a plan, changes its school year or sets one of its caps.
func (a *API) UpdatePlan(ctx context.Context, request gen.UpdatePlanRequestObject) (gen.UpdatePlanResponseObject, error) {
	identity, ok := callerID(ctx)
	if !ok {
		return gen.UpdatePlan401JSONResponse{UnauthorizedJSONResponse: gen.UnauthorizedJSONResponse(unauthorized())}, nil
	}
	if request.Body == nil {
		return gen.UpdatePlan422JSONResponse{ValidationJSONResponse: gen.ValidationJSONResponse(validation("body", "required"))}, nil
	}
	before, err := a.editable(ctx, request.PlanId, identity)
	var plan plans.Plan
	if err == nil {
		plan, err = a.plans.Update(ctx, before.ID, plans.Change{
			Name: request.Body.Name, SchoolYear: request.Body.SchoolYear,
			BackupsKeep: request.Body.BackupsKeep, ResultsKeep: request.Body.ResultsKeep,
		})
	}
	if refusal, ok := invalid(err); ok {
		return gen.UpdatePlan422JSONResponse{ValidationJSONResponse: gen.ValidationJSONResponse(refusal)}, nil
	}
	switch {
	case errors.Is(err, errNotYours):
		return gen.UpdatePlan403JSONResponse{ForbiddenJSONResponse: gen.ForbiddenJSONResponse(planNotYours())}, nil
	case errors.Is(err, plans.ErrNotFound):
		return gen.UpdatePlan404JSONResponse{NotFoundJSONResponse: gen.NotFoundJSONResponse(notFound())}, nil
	case err != nil:
		return gen.UpdatePlan503JSONResponse{UnavailableJSONResponse: gen.UnavailableJSONResponse(unavailable())}, nil
	}
	a.auditUpdate(ctx, before, plan)
	body, err := a.planAnswer(ctx, plan)
	if err != nil {
		return gen.UpdatePlan503JSONResponse{UnavailableJSONResponse: gen.UnavailableJSONResponse(unavailable())}, nil
	}
	return gen.UpdatePlan200JSONResponse(body), nil
}

// auditUpdate records a rename, and any other change of the same request as
// a second entry.
func (a *API) auditUpdate(ctx context.Context, before, after plans.Plan) {
	if before.Name != after.Name {
		audited(ctx, a.trail, audit.ProjectRenamed, planTarget(after),
			map[string]string{"from": before.Name, "to": after.Name})
	}
	changed := map[string]string{}
	if before.SchoolYear != after.SchoolYear {
		changed["school_year"] = after.SchoolYear
	}
	if before.BackupsKeep != after.BackupsKeep {
		changed["backups_keep"] = strconv.Itoa(after.BackupsKeep)
	}
	if before.ResultsKeep != after.ResultsKeep {
		changed["results_keep"] = strconv.Itoa(after.ResultsKeep)
	}
	if len(changed) > 0 {
		audited(ctx, a.trail, audit.ProjectChanged, planTarget(after), changed)
	}
}

// DeletePlan stops the plan's solves, waits for their jobs to close, and then
// moves the plan to Trash.
//
// The order matters. A cancelled solve that had found a timetable keeps it as
// one of the plan's results before the plan leaves the listings, so its job
// answers with the result it kept. A solve that started while the first cancel
// waited is stopped after the move; what it found is kept with the plan in
// Trash. One the runner takes only after the second cancel never reaches its
// node: the dispatcher checks the plan is live before it dials.
func (a *API) DeletePlan(ctx context.Context, request gen.DeletePlanRequestObject) (gen.DeletePlanResponseObject, error) {
	identity, ok := callerID(ctx)
	if !ok {
		return gen.DeletePlan401JSONResponse{UnauthorizedJSONResponse: gen.UnauthorizedJSONResponse(unauthorized())}, nil
	}
	plan, err := a.editable(ctx, request.PlanId, identity)
	switch {
	case errors.Is(err, errNotYours):
		return gen.DeletePlan403JSONResponse{ForbiddenJSONResponse: gen.ForbiddenJSONResponse(planNotYours())}, nil
	case errors.Is(err, plans.ErrNotFound):
		return gen.DeletePlan404JSONResponse{NotFoundJSONResponse: gen.NotFoundJSONResponse(notFound())}, nil
	case err != nil:
		return gen.DeletePlan503JSONResponse{UnavailableJSONResponse: gen.UnavailableJSONResponse(unavailable())}, nil
	}
	if a.runner != nil {
		if err := a.runner.CancelPlan(ctx, plan.ID); err != nil {
			return gen.DeletePlan503JSONResponse{UnavailableJSONResponse: gen.UnavailableJSONResponse(unavailable())}, nil
		}
	}
	if err := a.plans.Trash(ctx, plan.ID); err != nil {
		if errors.Is(err, plans.ErrNotFound) {
			return gen.DeletePlan404JSONResponse{NotFoundJSONResponse: gen.NotFoundJSONResponse(notFound())}, nil
		}
		return gen.DeletePlan503JSONResponse{UnavailableJSONResponse: gen.UnavailableJSONResponse(unavailable())}, nil
	}
	audited(ctx, a.trail, audit.ProjectDeleted, planTarget(plan), nil)
	if a.runner != nil {
		// The plan is in Trash whatever this wait says: the cancel went out.
		_ = a.runner.CancelPlan(ctx, plan.ID)
	}
	return gen.DeletePlan204Response{}, nil
}

// ListVersions returns the plan's timeline.
func (a *API) ListVersions(ctx context.Context, request gen.ListVersionsRequestObject) (gen.ListVersionsResponseObject, error) {
	if _, ok := callerID(ctx); !ok {
		return gen.ListVersions401JSONResponse{UnauthorizedJSONResponse: gen.UnauthorizedJSONResponse(unauthorized())}, nil
	}
	plan, err := a.readable(ctx, request.PlanId)
	if err != nil {
		if errors.Is(err, plans.ErrNotFound) {
			return gen.ListVersions404JSONResponse{NotFoundJSONResponse: gen.NotFoundJSONResponse(notFound())}, nil
		}
		return gen.ListVersions503JSONResponse{UnavailableJSONResponse: gen.UnavailableJSONResponse(unavailable())}, nil
	}

	timeline, err := a.plans.Versions(ctx, plan.ID)
	if err != nil {
		return gen.ListVersions503JSONResponse{UnavailableJSONResponse: gen.UnavailableJSONResponse(unavailable())}, nil
	}
	items := make([]gen.Version, 0, len(timeline))
	for _, version := range timeline {
		items = append(items, versionBody(version))
	}
	return gen.ListVersions200JSONResponse{Items: items}, nil
}

// DownloadVersion returns the stored document as it was written.
func (a *API) DownloadVersion(ctx context.Context, request gen.DownloadVersionRequestObject) (gen.DownloadVersionResponseObject, error) {
	if _, ok := callerID(ctx); !ok {
		return gen.DownloadVersion401JSONResponse{UnauthorizedJSONResponse: gen.UnauthorizedJSONResponse(unauthorized())}, nil
	}
	plan, err := a.readable(ctx, request.PlanId)
	if err != nil {
		if errors.Is(err, plans.ErrNotFound) {
			return gen.DownloadVersion404JSONResponse{NotFoundJSONResponse: gen.NotFoundJSONResponse(notFound())}, nil
		}
		return gen.DownloadVersion503JSONResponse{UnavailableJSONResponse: gen.UnavailableJSONResponse(unavailable())}, nil
	}

	version, err := a.plans.Version(ctx, plan.ID, request.Version)
	if err != nil {
		if errors.Is(err, plans.ErrNoVersion) || errors.Is(err, plans.ErrNotFound) {
			return gen.DownloadVersion404JSONResponse{NotFoundJSONResponse: gen.NotFoundJSONResponse(notFound())}, nil
		}
		return gen.DownloadVersion503JSONResponse{UnavailableJSONResponse: gen.UnavailableJSONResponse(unavailable())}, nil
	}

	data, err := a.documents.Bytes(ctx, version.ObjectKey)
	if err != nil {
		// A row pointing at nothing is a repairable inconsistency, not a
		// missing plan: say the store is unavailable rather than that the
		// version does not exist.
		return gen.DownloadVersion503JSONResponse{UnavailableJSONResponse: gen.UnavailableJSONResponse(unavailable())}, nil
	}
	return gen.DownloadVersion200ApplicationzstdResponse{
		Body: bytes.NewReader(data), ContentLength: int64(len(data)),
	}, nil
}

// UploadVersion stores a document the client encoded, if the parent it names
// is still the newest version. A retry with an upload key the plan already
// stored is answered with that version, 201 like the first time.
func (a *API) UploadVersion(ctx context.Context, request gen.UploadVersionRequestObject) (gen.UploadVersionResponseObject, error) {
	identity, ok := callerID(ctx)
	if !ok {
		return gen.UploadVersion401JSONResponse{UnauthorizedJSONResponse: gen.UnauthorizedJSONResponse(unauthorized())}, nil
	}
	plan, err := a.editable(ctx, request.PlanId, identity)
	switch {
	case errors.Is(err, errNotYours):
		return gen.UploadVersion403JSONResponse{ForbiddenJSONResponse: gen.ForbiddenJSONResponse(planNotYours())}, nil
	case errors.Is(err, plans.ErrNotFound):
		return gen.UploadVersion404JSONResponse{NotFoundJSONResponse: gen.NotFoundJSONResponse(notFound())}, nil
	case err != nil:
		return gen.UploadVersion503JSONResponse{UnavailableJSONResponse: gen.UnavailableJSONResponse(unavailable())}, nil
	}

	form, err := readForm(request.Body, "document", a.uploadLimit)
	if errors.Is(err, errTooLarge) {
		return gen.UploadVersion413JSONResponse{TooLargeJSONResponse: gen.TooLargeJSONResponse(tooLarge(a.uploadLimit))}, nil
	}
	if err != nil {
		return gen.UploadVersion422JSONResponse{ValidationJSONResponse: gen.ValidationJSONResponse(validation("document", "unreadable"))}, nil
	}
	if len(form.file) == 0 {
		return gen.UploadVersion422JSONResponse{ValidationJSONResponse: gen.ValidationJSONResponse(validation("document", "required"))}, nil
	}
	upload, err := uploadOf(form)
	if err != nil {
		if refusal, ok := invalid(err); ok {
			return gen.UploadVersion422JSONResponse{ValidationJSONResponse: gen.ValidationJSONResponse(refusal)}, nil
		}
		return nil, err
	}

	// Everything the form carries is checked before anything is stored, so a
	// refused upload leaves no object behind.
	encoded, _, err := document.FromBytes(form.file)
	if err != nil {
		return gen.UploadVersion422JSONResponse{ValidationJSONResponse: gen.ValidationJSONResponse(validation("document", "unreadable"))}, nil
	}
	key, err := a.documents.Put(ctx, encoded)
	if err != nil {
		return gen.UploadVersion503JSONResponse{UnavailableJSONResponse: gen.UnavailableJSONResponse(unavailable())}, nil
	}

	upload.PlanID, upload.ObjectKey, upload.SizeBytes, upload.CreatedBy = plan.ID, key, encoded.Size, identity
	version, err := a.plans.AddVersion(ctx, upload)
	if err != nil {
		// Best effort: a key that stays unlisted is only an object kept.
		_ = a.plans.Discard(ctx, key)
		var conflict *plans.Conflict
		if errors.As(err, &conflict) {
			return gen.UploadVersion409JSONResponse{
				Code:           gen.ConflictCodeConflict,
				Message:        messages.KeyPlansVersionConflict,
				CurrentVersion: conflict.Current,
			}, nil
		}
		if refusal, ok := invalid(err); ok {
			return gen.UploadVersion422JSONResponse{ValidationJSONResponse: gen.ValidationJSONResponse(refusal)}, nil
		}
		if errors.Is(err, plans.ErrNotFound) {
			return gen.UploadVersion404JSONResponse{NotFoundJSONResponse: gen.NotFoundJSONResponse(notFound())}, nil
		}
		return gen.UploadVersion503JSONResponse{UnavailableJSONResponse: gen.UnavailableJSONResponse(unavailable())}, nil
	}
	return gen.UploadVersion201JSONResponse(versionBody(version)), nil
}

func pointerTo[T any](value T) *T { return &value }

// uploadForm is one multipart body: the file part and every plain field.
// hasFile tells an absent file part from an empty one.
type uploadForm struct {
	file     []byte
	filename string
	hasFile  bool
	fields   map[string]string
}

// errTooLarge is a multipart body or part over the upload limit.
var errTooLarge = errors.New("api: upload over the limit")

// readForm reads a multipart body, taking the named part as the file. The
// group this runs on caps the whole body, and each part is held to limit as
// well; either one tripping is errTooLarge, never a silently cut part.
func readForm(reader *multipart.Reader, fileField string, limit int64) (uploadForm, error) {
	form := uploadForm{fields: map[string]string{}}
	if reader == nil {
		return form, fmt.Errorf("api: no body")
	}
	for {
		part, err := reader.NextPart()
		if errors.Is(err, io.EOF) {
			return form, nil
		}
		if httpmw.TooLarge(err) {
			return form, errTooLarge
		}
		if err != nil {
			return form, err
		}
		value, err := io.ReadAll(io.LimitReader(part, limit+1))
		part.Close()
		if httpmw.TooLarge(err) || int64(len(value)) > limit {
			return form, errTooLarge
		}
		if err != nil {
			return form, err
		}
		if part.FormName() == fileField {
			form.file = value
			form.filename = part.FileName()
			form.hasFile = true
			continue
		}
		form.fields[part.FormName()] = string(value)
	}
}

// uploadOf reads what an upload form says besides the document, refusing a
// field with an *plans.Invalid.
func uploadOf(form uploadForm) (plans.Upload, error) {
	parent, code := parentOf(form.fields)
	if code != "" {
		return plans.Upload{}, &plans.Invalid{Field: "parent", Code: code}
	}
	key := form.fields["upload_key"]
	if err := plans.CheckUploadKey(key); err != nil {
		return plans.Upload{}, err
	}
	summary, err := plans.ParseSummary([]byte(form.fields["summary"]))
	if err != nil {
		return plans.Upload{}, err
	}
	validFrom, err := dateOf(form.fields, "valid_from")
	if err != nil {
		return plans.Upload{}, err
	}
	validTo, err := dateOf(form.fields, "valid_to")
	if err != nil {
		return plans.Upload{}, err
	}
	return plans.Upload{Parent: parent, UploadKey: key, Summary: summary,
		ValidFrom: validFrom, ValidTo: validTo}, nil
}

// parentOf reads the version an upload was derived from, or the code that
// refuses it. Every plan is created with version 1, so an upload always names
// one.
func parentOf(fields map[string]string) (int64, string) {
	return positiveOf(fields, "parent")
}

// dateOf reads a schedule validity date: absent is nil, which keeps the
// parent's; empty is a zero date, which clears it; anything that is not a date
// is refused, because a mistyped date must not vanish silently.
func dateOf(fields map[string]string, field string) (*time.Time, error) {
	raw, sent := fields[field]
	if !sent {
		return nil, nil
	}
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return &time.Time{}, nil
	}
	date, err := time.Parse(time.DateOnly, raw)
	if err != nil {
		return nil, &plans.Invalid{Field: field, Code: "invalid"}
	}
	date = date.UTC()
	return &date, nil
}

// The generated contract is the boundary: this assertion is what makes a
// contract change a compile error here rather than a runtime surprise. It is
// on Handlers, because neither half implements the whole contract alone.
var _ gen.StrictServerInterface = (*Handlers)(nil)

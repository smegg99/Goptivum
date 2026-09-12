// api/v1/handlers.go

package v1

import (
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

	"github.com/smegg99/goptivum/backend/api/v1/gen"
	"github.com/smegg99/goptivum/backend/core/documents"
	"github.com/smegg99/goptivum/backend/core/plans"
	"github.com/smegg99/goptivum/backend/formats"
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
}

// NewAPI wires the handlers. Every argument is required.
func NewAPI(index *plans.Repository, store *documents.Store, importers *formats.Registry) (*API, error) {
	if index == nil || store == nil || importers == nil {
		return nil, fmt.Errorf("api: the plan index, the document store and the importers are all required")
	}
	return &API{plans: index, documents: store, importers: importers}, nil
}

// owner is the signed-in identity. Every route that reaches a handler has
// passed kratosauth.Require, so an absent identity is a wiring mistake rather
// than an anonymous caller.
func owner(ctx context.Context) (string, bool) {
	identity, ok := kratosauth.FromContext(ctx)
	if !ok || identity.ID == "" {
		return "", false
	}
	return identity.ID, true
}

func unauthorized() gen.Error {
	return gen.Error{Code: "unauthorized", Message: "error.unauthorized"}
}

func notFound() gen.Error {
	return gen.Error{Code: "not_found", Message: "error.notFound"}
}

func validation(field, code string) gen.Error {
	return gen.Error{
		Code: "validation", Message: "error.validation",
		Details: &map[string]string{field: code},
	}
}

func unavailable() gen.Error {
	return gen.Error{Code: "unavailable", Message: "error.unavailable"}
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

func planBody(plan plans.Plan) gen.Plan {
	return gen.Plan{
		Id:         strconv.FormatInt(plan.ID, 10),
		Name:       plan.Name,
		SchoolYear: plan.SchoolYear,
		CreatedAt:  plan.CreatedAt,
		UpdatedAt:  plan.UpdatedAt,
	}
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
	return body
}

// ListPlans returns one page of the caller's plans.
func (a *API) ListPlans(ctx context.Context, request gen.ListPlansRequestObject) (gen.ListPlansResponseObject, error) {
	identity, ok := owner(ctx)
	if !ok {
		return gen.ListPlans401JSONResponse{UnauthorizedJSONResponse: gen.UnauthorizedJSONResponse(unauthorized())}, nil
	}

	all, err := a.plans.List(ctx, identity)
	if err != nil {
		return gen.ListPlans503JSONResponse{UnavailableJSONResponse: gen.UnavailableJSONResponse(unavailable())}, nil
	}

	matched := search(all, request.Params.Q)
	page, limit := pageOf(request.Params.Page, request.Params.Limit)
	items := make([]gen.Plan, 0, limit)
	for _, plan := range window(matched, page, limit) {
		items = append(items, planBody(plan))
	}
	return gen.ListPlans200JSONResponse{
		Items: items, Page: page, Limit: limit, Total: len(matched),
	}, nil
}

// search filters on the plan name and the school year, which is what the
// contract says q covers.
func search(all []plans.Plan, query *string) []plans.Plan {
	if query == nil || strings.TrimSpace(*query) == "" {
		return all
	}
	needle := strings.ToLower(strings.TrimSpace(*query))
	var out []plans.Plan
	for _, plan := range all {
		if strings.Contains(strings.ToLower(plan.Name), needle) ||
			strings.Contains(strings.ToLower(plan.SchoolYear), needle) {
			out = append(out, plan)
		}
	}
	return out
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

func window(all []plans.Plan, page, limit int) []plans.Plan {
	start := (page - 1) * limit
	if start < 0 || start >= len(all) {
		return nil
	}
	end := start + limit
	if end > len(all) {
		end = len(all)
	}
	return all[start:end]
}

// CreatePlan makes an empty plan. Its first document arrives as an upload or
// as an import.
func (a *API) CreatePlan(ctx context.Context, request gen.CreatePlanRequestObject) (gen.CreatePlanResponseObject, error) {
	identity, ok := owner(ctx)
	if !ok {
		return gen.CreatePlan401JSONResponse{UnauthorizedJSONResponse: gen.UnauthorizedJSONResponse(unauthorized())}, nil
	}
	if request.Body == nil || strings.TrimSpace(request.Body.Name) == "" {
		return gen.CreatePlan422JSONResponse{ValidationJSONResponse: gen.ValidationJSONResponse(validation("name", "required"))}, nil
	}

	plan, err := a.plans.Create(ctx, plans.Plan{
		OwnerIdentityID: identity,
		Name:            strings.TrimSpace(request.Body.Name),
		SchoolYear:      schoolYear(request.Body.SchoolYear),
	})
	if err != nil {
		return gen.CreatePlan503JSONResponse{UnavailableJSONResponse: gen.UnavailableJSONResponse(unavailable())}, nil
	}
	return gen.CreatePlan201JSONResponse(planBody(plan)), nil
}

func schoolYear(value *string) string {
	if value == nil {
		return ""
	}
	return strings.TrimSpace(*value)
}

// GetPlan reads one plan the caller owns.
func (a *API) GetPlan(ctx context.Context, request gen.GetPlanRequestObject) (gen.GetPlanResponseObject, error) {
	identity, ok := owner(ctx)
	if !ok {
		return gen.GetPlan401JSONResponse{UnauthorizedJSONResponse: gen.UnauthorizedJSONResponse(unauthorized())}, nil
	}
	plan, err := a.owned(ctx, request.PlanId, identity)
	if err != nil {
		if errors.Is(err, plans.ErrNotFound) {
			return gen.GetPlan404JSONResponse{NotFoundJSONResponse: gen.NotFoundJSONResponse(notFound())}, nil
		}
		return gen.GetPlan503JSONResponse{UnavailableJSONResponse: gen.UnavailableJSONResponse(unavailable())}, nil
	}
	return gen.GetPlan200JSONResponse(planBody(plan)), nil
}

// DeletePlan removes a plan and its whole timeline.
func (a *API) DeletePlan(ctx context.Context, request gen.DeletePlanRequestObject) (gen.DeletePlanResponseObject, error) {
	identity, ok := owner(ctx)
	if !ok {
		return gen.DeletePlan401JSONResponse{UnauthorizedJSONResponse: gen.UnauthorizedJSONResponse(unauthorized())}, nil
	}
	plan, err := a.owned(ctx, request.PlanId, identity)
	if err != nil {
		if errors.Is(err, plans.ErrNotFound) {
			return gen.DeletePlan404JSONResponse{NotFoundJSONResponse: gen.NotFoundJSONResponse(notFound())}, nil
		}
		return gen.DeletePlan503JSONResponse{UnavailableJSONResponse: gen.UnavailableJSONResponse(unavailable())}, nil
	}
	if err := a.plans.Delete(ctx, plan.ID); err != nil {
		if errors.Is(err, plans.ErrNotFound) {
			return gen.DeletePlan404JSONResponse{NotFoundJSONResponse: gen.NotFoundJSONResponse(notFound())}, nil
		}
		return gen.DeletePlan503JSONResponse{UnavailableJSONResponse: gen.UnavailableJSONResponse(unavailable())}, nil
	}
	return gen.DeletePlan204Response{}, nil
}

// owned resolves a plan id from a path and refuses one the caller does not
// own. Another owner's plan is not found rather than forbidden: whether it
// exists is not this caller's business.
func (a *API) owned(ctx context.Context, raw string, identity string) (plans.Plan, error) {
	id, ok := planID(raw)
	if !ok {
		return plans.Plan{}, fmt.Errorf("%w: %q", plans.ErrNotFound, raw)
	}
	plan, err := a.plans.Get(ctx, id)
	if err != nil {
		return plans.Plan{}, err
	}
	if plan.OwnerIdentityID != identity {
		return plans.Plan{}, fmt.Errorf("%w: %d", plans.ErrNotFound, id)
	}
	return plan, nil
}

// ListVersions returns the plan's timeline.
func (a *API) ListVersions(ctx context.Context, request gen.ListVersionsRequestObject) (gen.ListVersionsResponseObject, error) {
	identity, ok := owner(ctx)
	if !ok {
		return gen.ListVersions401JSONResponse{UnauthorizedJSONResponse: gen.UnauthorizedJSONResponse(unauthorized())}, nil
	}
	plan, err := a.owned(ctx, request.PlanId, identity)
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
	identity, ok := owner(ctx)
	if !ok {
		return gen.DownloadVersion401JSONResponse{UnauthorizedJSONResponse: gen.UnauthorizedJSONResponse(unauthorized())}, nil
	}
	plan, err := a.owned(ctx, request.PlanId, identity)
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
		Body: strings.NewReader(string(data)), ContentLength: int64(len(data)),
	}, nil
}

// UploadVersion stores a document the client encoded, if the parent it names
// is still the newest version.
func (a *API) UploadVersion(ctx context.Context, request gen.UploadVersionRequestObject) (gen.UploadVersionResponseObject, error) {
	identity, ok := owner(ctx)
	if !ok {
		return gen.UploadVersion401JSONResponse{UnauthorizedJSONResponse: gen.UnauthorizedJSONResponse(unauthorized())}, nil
	}
	plan, err := a.owned(ctx, request.PlanId, identity)
	if err != nil {
		if errors.Is(err, plans.ErrNotFound) {
			return gen.UploadVersion404JSONResponse{NotFoundJSONResponse: gen.NotFoundJSONResponse(notFound())}, nil
		}
		return gen.UploadVersion503JSONResponse{UnavailableJSONResponse: gen.UnavailableJSONResponse(unavailable())}, nil
	}

	form, err := readForm(request.Body, "document")
	if err != nil {
		return gen.UploadVersion422JSONResponse{ValidationJSONResponse: gen.ValidationJSONResponse(validation("document", "unreadable"))}, nil
	}
	if len(form.file) == 0 {
		return gen.UploadVersion422JSONResponse{ValidationJSONResponse: gen.ValidationJSONResponse(validation("document", "required"))}, nil
	}
	parent, ok := parentOf(form.fields["parent"])
	if !ok {
		return gen.UploadVersion422JSONResponse{ValidationJSONResponse: gen.ValidationJSONResponse(validation("parent", "invalid"))}, nil
	}

	doc, err := documents.FromBytes(form.file)
	if err != nil {
		return gen.UploadVersion422JSONResponse{ValidationJSONResponse: gen.ValidationJSONResponse(validation("document", "unreadable"))}, nil
	}
	if err := a.documents.Put(ctx, doc); err != nil {
		return gen.UploadVersion503JSONResponse{UnavailableJSONResponse: gen.UnavailableJSONResponse(unavailable())}, nil
	}

	version, err := a.plans.AddVersion(ctx, plans.Upload{
		PlanID: plan.ID, Parent: parent, ObjectKey: doc.Key, SizeBytes: doc.Size,
		CreatedBy: identity,
		ValidFrom: dateOf(form.fields["valid_from"]),
		ValidTo:   dateOf(form.fields["valid_to"]),
	})
	if err != nil {
		var conflict *plans.Conflict
		if errors.As(err, &conflict) {
			return gen.UploadVersion409JSONResponse{
				Code:           gen.ConflictCodeConflict,
				Message:        "plan.staleParent",
				CurrentVersion: conflict.Current,
			}, nil
		}
		if errors.Is(err, plans.ErrNotFound) {
			return gen.UploadVersion404JSONResponse{NotFoundJSONResponse: gen.NotFoundJSONResponse(notFound())}, nil
		}
		return gen.UploadVersion503JSONResponse{UnavailableJSONResponse: gen.UnavailableJSONResponse(unavailable())}, nil
	}
	return gen.UploadVersion201JSONResponse(versionBody(version)), nil
}

// ImportPlan turns a school's own file into a plan and its first version.
func (a *API) ImportPlan(ctx context.Context, request gen.ImportPlanRequestObject) (gen.ImportPlanResponseObject, error) {
	identity, ok := owner(ctx)
	if !ok {
		return gen.ImportPlan401JSONResponse{UnauthorizedJSONResponse: gen.UnauthorizedJSONResponse(unauthorized())}, nil
	}

	form, err := readForm(request.Body, "file")
	if err != nil {
		return gen.ImportPlan422JSONResponse{ValidationJSONResponse: gen.ValidationJSONResponse(validation("file", "unreadable"))}, nil
	}
	if len(form.file) == 0 {
		return gen.ImportPlan422JSONResponse{ValidationJSONResponse: gen.ValidationJSONResponse(validation("file", "required"))}, nil
	}

	// Detection is by content: the uploaded name only names the plan.
	source := formats.Open(form.filename, form.file)
	importer, err := a.importers.Best(source)
	if err != nil {
		return gen.ImportPlan415JSONResponse{Code: "unsupported_format", Message: "import.unsupportedFormat"}, nil
	}
	result, err := importer.Import(ctx, source)
	if err != nil {
		return gen.ImportPlan422JSONResponse{ValidationJSONResponse: gen.ValidationJSONResponse(validation("file", "unreadable"))}, nil
	}

	doc, err := documents.Encode(result.Model)
	if err != nil {
		return gen.ImportPlan422JSONResponse{ValidationJSONResponse: gen.ValidationJSONResponse(validation("file", "unreadable"))}, nil
	}
	if err := a.documents.Put(ctx, doc); err != nil {
		return gen.ImportPlan503JSONResponse{UnavailableJSONResponse: gen.UnavailableJSONResponse(unavailable())}, nil
	}

	plan, err := a.plans.Create(ctx, plans.Plan{
		OwnerIdentityID: identity,
		Name:            planName(form, result.Model.GetName()),
		SchoolYear:      strings.TrimSpace(form.fields["school_year"]),
	})
	if err != nil {
		return gen.ImportPlan503JSONResponse{UnavailableJSONResponse: gen.UnavailableJSONResponse(unavailable())}, nil
	}
	version, err := a.plans.AddVersion(ctx, plans.Upload{
		PlanID: plan.ID, ObjectKey: doc.Key, SizeBytes: doc.Size, CreatedBy: identity,
	})
	if err != nil {
		return gen.ImportPlan503JSONResponse{UnavailableJSONResponse: gen.UnavailableJSONResponse(unavailable())}, nil
	}

	return gen.ImportPlan201JSONResponse{
		PlanId:   strconv.FormatInt(plan.ID, 10),
		Version:  version.Version,
		Importer: pointerTo(importer.ID()),
		Findings: findingBodies(result.Findings),
	}, nil
}

// planName prefers what the user typed, then what the file says the school is,
// then the file name without its extension.
func planName(form uploadForm, school string) string {
	if name := strings.TrimSpace(form.fields["name"]); name != "" {
		return name
	}
	if school = strings.TrimSpace(school); school != "" {
		return school
	}
	name := form.filename
	if dot := strings.LastIndex(name, "."); dot > 0 {
		name = name[:dot]
	}
	if name = strings.TrimSpace(name); name != "" {
		return name
	}
	return "Import"
}

func findingBodies(findings []formats.Finding) []gen.Finding {
	out := make([]gen.Finding, 0, len(findings))
	for _, finding := range findings {
		body := gen.Finding{
			Kind:     string(finding.Kind),
			Severity: gen.FindingSeverity(finding.Severity.String()),
		}
		if len(finding.Entities) > 0 {
			refs := make([]gen.EntityRef, 0, len(finding.Entities))
			for _, ref := range finding.Entities {
				refs = append(refs, gen.EntityRef{
					Kind: gen.EntityRefKind(entityKindName(ref.GetKind().String())),
					Id:   int(ref.GetId()),
				})
			}
			body.Entities = &refs
		}
		out = append(out, body)
	}
	return out
}

// entityKindName turns ENTITY_KIND_DIVISION into division, which is what the
// contract carries.
func entityKindName(enum string) string {
	return strings.ToLower(strings.TrimPrefix(enum, "ENTITY_KIND_"))
}

func pointerTo[T any](value T) *T { return &value }

// uploadForm is one multipart body: the file part and every plain field.
type uploadForm struct {
	file     []byte
	filename string
	fields   map[string]string
}

// readForm reads a multipart body, taking the named part as the file. The
// group this runs on caps the body, so the read is already bounded.
func readForm(reader *multipart.Reader, fileField string) (uploadForm, error) {
	form := uploadForm{fields: map[string]string{}}
	if reader == nil {
		return form, fmt.Errorf("api: no body")
	}
	for {
		part, err := reader.NextPart()
		if errors.Is(err, io.EOF) {
			return form, nil
		}
		if err != nil {
			return form, err
		}
		value, err := io.ReadAll(io.LimitReader(part, MaxUploadSize))
		part.Close()
		if err != nil {
			return form, err
		}
		if part.FormName() == fileField {
			form.file = value
			form.filename = part.FileName()
			continue
		}
		form.fields[part.FormName()] = string(value)
	}
}

// parentOf reads the version an upload was derived from. Absent means the
// first version, which is 0.
func parentOf(raw string) (int64, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0, true
	}
	parent, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || parent < 0 {
		return 0, false
	}
	return parent, true
}

// dateOf reads a schedule validity date. An unreadable one is no date rather
// than an error: validity is optional and a wrong value must not lose an
// upload.
func dateOf(raw string) time.Time {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return time.Time{}
	}
	date, err := time.Parse(time.DateOnly, raw)
	if err != nil {
		return time.Time{}
	}
	return date.UTC()
}

// The generated contract is the boundary: this assertion is what makes a
// contract change a compile error here rather than a runtime surprise.
var _ gen.StrictServerInterface = (*API)(nil)

// api/v1/imports.go

package v1

import (
	"context"
	"errors"
	"strconv"
	"strings"

	"github.com/smegg99/goptivum/backend/api/v1/gen"
	"github.com/smegg99/goptivum/backend/common/messages"
	"github.com/smegg99/goptivum/backend/core/plans"
	"github.com/smegg99/goptivum/backend/formats"
	"github.com/smegg99/goptivum/document"
	goptivumv1 "github.com/smegg99/goptivum/document/gen/goptivumv1"
)

// maxPassphrase bounds the passphrase an import takes, in letters.
const maxPassphrase = 1024

// fileRead is a school file read the way an import reads it, before anything
// is stored: the preview answers from it and the import stores it.
type fileRead struct {
	importer string
	document *goptivumv1.Document
	encoded  document.Encoded
	findings []formats.Finding
	// school is the name the file gives the school, and name what the plan is
	// called when the request names none.
	school string
	name   string
}

// importRefusal is why a file cannot be imported: no importer knows it (415)
// or the envelope of a 422.
type importRefusal struct {
	unsupported bool
	body        gen.Error
}

func unsupportedFormat() gen.Error {
	return gen.Error{Code: "unsupported_format", Message: messages.KeyImportUnsupportedFormat}
}

// readImport detects, imports and encodes the uploaded file, storing nothing.
// A passphrase reaches only the importer that won detection.
func (a *API) readImport(ctx context.Context, form uploadForm) (fileRead, *importRefusal) {
	if len(form.file) == 0 {
		return fileRead{}, &importRefusal{body: validation("file", "required")}
	}
	passphrase := form.fields["passphrase"]
	if len([]rune(passphrase)) > maxPassphrase {
		return fileRead{}, &importRefusal{body: validation("passphrase", "too_long")}
	}

	// Detection is by content: the uploaded name only names the plan.
	source := formats.Open(form.filename, form.file)
	importer, err := a.importers.Best(source)
	if err != nil {
		return fileRead{}, &importRefusal{unsupported: true, body: unsupportedFormat()}
	}
	var result *formats.Result
	if unlocker, ok := importer.(formats.Unlocker); ok {
		result, err = unlocker.ImportUnlocked(ctx, source, passphrase)
	} else {
		result, err = importer.Import(ctx, source)
	}
	if err != nil {
		// The importer already worded why. Collapsing every refusal into one
		// reason is what makes a person retry the same file three times.
		if envelope, ok := refused(err); ok {
			return fileRead{}, &importRefusal{body: envelope}
		}
		return fileRead{}, &importRefusal{body: validation("file", "unreadable")}
	}

	// Goptivum's own format arrives as a whole envelope and is stored as one.
	// Rebuilding it would recompute next_arrango_id from the model, and a file
	// whose sender deleted their highest-numbered entity carries a counter
	// above what the model implies: recomputing would hand out an id that
	// sender already spent, which retargets locked placements silently.
	doc := result.Document
	if doc == nil {
		// Every other importer assembles a model, so the envelope is built
		// here. The schedule travels with it: an Optivum export IS a published
		// timetable and dropping it loses the point of the import.
		doc = document.New(result.Model)
		doc.Schedule = result.Schedule
	}
	encoded, err := document.Encode(doc)
	if err != nil {
		return fileRead{}, &importRefusal{body: validation("file", "unreadable")}
	}
	school := strings.TrimSpace(doc.GetModel().GetName())
	return fileRead{
		importer: importer.ID(),
		document: doc,
		encoded:  encoded,
		findings: formats.Merge(result.Findings),
		school:   school,
		name:     derivedName(school, form.filename),
	}, nil
}

// ImportPlan turns a school's own file into a plan and its first version.
func (a *API) ImportPlan(ctx context.Context, request gen.ImportPlanRequestObject) (gen.ImportPlanResponseObject, error) {
	identity, ok := callerID(ctx)
	if !ok {
		return gen.ImportPlan401JSONResponse{UnauthorizedJSONResponse: gen.UnauthorizedJSONResponse(unauthorized())}, nil
	}
	form, err := readForm(request.Body, "file", a.uploadLimit)
	if errors.Is(err, errTooLarge) {
		return gen.ImportPlan413JSONResponse{TooLargeJSONResponse: gen.TooLargeJSONResponse(tooLarge(a.uploadLimit))}, nil
	}
	if err != nil {
		return gen.ImportPlan422JSONResponse{ValidationJSONResponse: gen.ValidationJSONResponse(validation("file", "unreadable"))}, nil
	}
	read, refusal := a.readImport(ctx, form)
	if refusal != nil {
		if refusal.unsupported {
			return gen.ImportPlan415JSONResponse{UnsupportedFormatJSONResponse: gen.UnsupportedFormatJSONResponse(refusal.body)}, nil
		}
		return gen.ImportPlan422JSONResponse{ValidationJSONResponse: gen.ValidationJSONResponse(refusal.body)}, nil
	}

	name := read.name
	if typed := strings.TrimSpace(form.fields["name"]); typed != "" {
		name = typed
	}
	// The plan and its first version land together, so a failed import leaves
	// no plan with nothing to open.
	plan, version, err := a.createWith(ctx, identity, name, form.fields["school_year"], read.encoded, sourceImport)
	if refusal, ok := invalid(err); ok {
		return gen.ImportPlan422JSONResponse{ValidationJSONResponse: gen.ValidationJSONResponse(refusal)}, nil
	}
	if err != nil {
		return gen.ImportPlan503JSONResponse{UnavailableJSONResponse: gen.UnavailableJSONResponse(unavailable())}, nil
	}
	return gen.ImportPlan201JSONResponse{
		PlanId:   strconv.FormatInt(plan.ID, 10),
		Version:  version.Version,
		Importer: read.importer,
		Findings: findingBodies(read.findings),
	}, nil
}

// PreviewImport reads a school's file as ImportPlan would and stores nothing.
func (a *API) PreviewImport(ctx context.Context, request gen.PreviewImportRequestObject) (gen.PreviewImportResponseObject, error) {
	if _, ok := callerID(ctx); !ok {
		return gen.PreviewImport401JSONResponse{UnauthorizedJSONResponse: gen.UnauthorizedJSONResponse(unauthorized())}, nil
	}
	form, err := readForm(request.Body, "file", a.uploadLimit)
	if errors.Is(err, errTooLarge) {
		return gen.PreviewImport413JSONResponse{TooLargeJSONResponse: gen.TooLargeJSONResponse(tooLarge(a.uploadLimit))}, nil
	}
	if err != nil {
		return gen.PreviewImport422JSONResponse{ValidationJSONResponse: gen.ValidationJSONResponse(validation("file", "unreadable"))}, nil
	}
	read, refusal := a.readImport(ctx, form)
	if refusal != nil {
		if refusal.unsupported {
			return gen.PreviewImport415JSONResponse{UnsupportedFormatJSONResponse: gen.UnsupportedFormatJSONResponse(refusal.body)}, nil
		}
		return gen.PreviewImport422JSONResponse{ValidationJSONResponse: gen.ValidationJSONResponse(refusal.body)}, nil
	}
	body := gen.PreviewImport200JSONResponse{
		Importer: read.importer,
		Name:     read.name,
		Counts:   importCounts(read.document),
		Findings: findingBodies(read.findings),
	}
	if read.school != "" {
		body.School = pointerTo(read.school)
	}
	return body, nil
}

// derivedName is the school the file names, then the file name without its
// extension, cut to what a plan name may hold rather than refused: nobody
// typed it.
func derivedName(school, filename string) string {
	name := school
	if name == "" {
		name = strings.TrimSpace(filename)
		if dot := strings.LastIndex(name, "."); dot > 0 {
			name = strings.TrimSpace(name[:dot])
		}
	}
	if name == "" {
		return "Import"
	}
	if letters := []rune(name); len(letters) > plans.MaxNameLength {
		name = strings.TrimSpace(string(letters[:plans.MaxNameLength]))
	}
	return name
}

// importCounts is how much school a document holds.
func importCounts(doc *goptivumv1.Document) gen.ImportCounts {
	model := doc.GetModel()
	placed := map[uint32]bool{}
	for _, lesson := range doc.GetSchedule().GetLessons() {
		if lesson.GetPlacement() != nil {
			placed[lesson.GetLessonId()] = true
		}
	}
	return gen.ImportCounts{
		Days:     len(model.GetDays()),
		Periods:  len(model.GetPeriods()),
		Classes:  len(model.GetDivisions()),
		Groups:   len(model.GetGroups()),
		Teachers: len(model.GetTeachers()),
		Subjects: len(model.GetSubjects()),
		Rooms:    len(model.GetRooms()),
		Lessons:  len(model.GetLessons()),
		Placed:   len(placed),
	}
}

// findingBodies puts merged findings on the wire; Merge set every count.
func findingBodies(findings []formats.Finding) []gen.Finding {
	out := make([]gen.Finding, 0, len(findings))
	for _, finding := range findings {
		body := gen.Finding{
			Kind:     string(finding.Kind),
			Severity: gen.FindingSeverity(finding.Severity.String()),
			Count:    finding.Count,
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

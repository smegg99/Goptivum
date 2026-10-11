// api/v1/uploadresult.go

package v1

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"math"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/smegg99/s99ab/auth/kratosauth"

	arrangov1 "github.com/smegg99/arrango/backend/gen/arrangov1"

	"github.com/smegg99/goptivum/backend/api/v1/gen"
	"github.com/smegg99/goptivum/backend/arrango"
	"github.com/smegg99/goptivum/backend/core/plans"
	"github.com/smegg99/goptivum/backend/core/profiles"
	"github.com/smegg99/goptivum/backend/core/results"
	"github.com/smegg99/goptivum/document"
)

// maxDeviceLength is the contract's bound on a device's name, in letters.
const maxDeviceLength = 100

// decimalID is the contract's shape of an id carried in a body.
var decimalID = regexp.MustCompile(`^[0-9]{1,19}$`)

// UploadResult keeps a timetable solved on a device's own node as a result of
// the caller's own plan, with the caller as who started it.
func (a *API) UploadResult(ctx context.Context, request gen.UploadResultRequestObject) (gen.UploadResultResponseObject, error) {
	identity, ok := kratosauth.FromContext(ctx)
	if !ok || identity.ID == "" {
		return gen.UploadResult401JSONResponse{UnauthorizedJSONResponse: gen.UnauthorizedJSONResponse(unauthorized())}, nil
	}
	plan, err := a.editable(ctx, request.PlanId, identity.ID)
	switch {
	case errors.Is(err, errNotYours):
		return gen.UploadResult403JSONResponse{ForbiddenJSONResponse: gen.ForbiddenJSONResponse(planNotYours())}, nil
	case errors.Is(err, plans.ErrNotFound):
		return gen.UploadResult404JSONResponse{NotFoundJSONResponse: gen.NotFoundJSONResponse(notFound())}, nil
	case err != nil:
		return gen.UploadResult503JSONResponse{UnavailableJSONResponse: gen.UnavailableJSONResponse(unavailable())}, nil
	}

	form, err := readForm(request.Body, "document", a.uploadLimit)
	if errors.Is(err, errTooLarge) {
		return gen.UploadResult413JSONResponse{TooLargeJSONResponse: gen.TooLargeJSONResponse(tooLarge(a.uploadLimit))}, nil
	}
	if err != nil {
		return uploadRefused(validation("document", "unreadable")), nil
	}
	if len(form.file) == 0 {
		return uploadRefused(validation("document", "required")), nil
	}
	add, err := deviceResultOf(form, time.Now())
	if refusal, ok := invalid(err); ok {
		return uploadRefused(refusal), nil
	}
	if err != nil {
		return nil, err
	}

	// Everything the form carries is checked before anything is stored, so a
	// refused upload leaves no object behind.
	encoded, doc, err := document.FromBytes(form.file)
	if err != nil {
		return uploadRefused(validation("document", "unreadable")), nil
	}
	if len(doc.GetSchedule().GetLessons()) == 0 {
		return uploadRefused(validation("document", "no_timetable")), nil
	}
	current, err := a.plans.Current(ctx, plan.ID)
	switch {
	case errors.Is(err, plans.ErrNotFound):
		return gen.UploadResult404JSONResponse{NotFoundJSONResponse: gen.NotFoundJSONResponse(notFound())}, nil
	case err != nil:
		return gen.UploadResult503JSONResponse{UnavailableJSONResponse: gen.UnavailableJSONResponse(unavailable())}, nil
	case add.SourceVersion > current:
		return uploadRefused(validation("source_version", "unknown")), nil
	}

	add.PlanID, add.StartedBy, add.StartedByName = plan.ID, identity.ID, displayName(identity)
	kept, err := results.Add(ctx, a.plans, a.documents, encoded, add)
	if refusal, ok := invalid(err); ok {
		return uploadRefused(refusal), nil
	}
	switch {
	case errors.Is(err, plans.ErrNotFound):
		return gen.UploadResult404JSONResponse{NotFoundJSONResponse: gen.NotFoundJSONResponse(notFound())}, nil
	case err != nil:
		return gen.UploadResult503JSONResponse{UnavailableJSONResponse: gen.UnavailableJSONResponse(unavailable())}, nil
	}
	return gen.UploadResult201JSONResponse(resultBody(kept)), nil
}

func uploadRefused(envelope gen.Error) gen.UploadResultResponseObject {
	return gen.UploadResult422JSONResponse{ValidationJSONResponse: gen.ValidationJSONResponse(envelope)}
}

// deviceResultOf reads what an upload form says besides the document, refusing
// a field with a *plans.Invalid. A solve time later than now is now.
func deviceResultOf(form uploadForm, now time.Time) (plans.NewResult, error) {
	version, code := positiveOf(form.fields, "source_version")
	if code != "" {
		return plans.NewResult{}, &plans.Invalid{Field: "source_version", Code: code}
	}
	key := form.fields["upload_key"]
	if err := plans.CheckUploadKey(key); err != nil {
		return plans.NewResult{}, err
	}
	device := strings.TrimSpace(form.fields["device"])
	switch {
	case device == "":
		return plans.NewResult{}, &plans.Invalid{Field: "device", Code: "required"}
	case utf8.RuneCountInString(device) > maxDeviceLength:
		return plans.NewResult{}, &plans.Invalid{Field: "device", Code: "too_long"}
	}
	add := plans.NewResult{SourceVersion: version, UploadKey: key, Device: device}

	if raw := strings.TrimSpace(form.fields["solved_at"]); raw != "" {
		solved, err := time.Parse(time.RFC3339Nano, raw)
		if err != nil {
			return plans.NewResult{}, &plans.Invalid{Field: "solved_at", Code: "invalid"}
		}
		if solved.Before(now) {
			add.CreatedAt = solved
		}
	}
	if raw, sent := form.fields["quality"]; sent {
		quality, err := strconv.ParseFloat(strings.TrimSpace(raw), 64)
		if err != nil || math.IsNaN(quality) || quality < 0 || quality > 100 {
			return plans.NewResult{}, &plans.Invalid{Field: "quality", Code: "invalid"}
		}
		add.Quality = &quality
	}
	if raw, sent := form.fields["verdict"]; sent {
		if !gen.SolveResultUploadVerdict(raw).Valid() {
			return plans.NewResult{}, &plans.Invalid{Field: "verdict", Code: "invalid"}
		}
		add.Verdict = raw
	}
	if raw, sent := form.fields["rule_profile"]; sent {
		profile, ok := ruleProfileOf(raw)
		if !ok {
			return plans.NewResult{}, &plans.Invalid{Field: "rule_profile", Code: "invalid"}
		}
		add.RuleProfile = profile
	}
	return add, nil
}

// positiveOf reads a required whole number of at least 1, or the code that refuses it.
func positiveOf(fields map[string]string, field string) (int64, string) {
	raw, ok := fields[field]
	if !ok || strings.TrimSpace(raw) == "" {
		return 0, "required"
	}
	value, valid := planID(raw)
	if !valid {
		return 0, "invalid"
	}
	return value, ""
}

// ruleProfileOf reads the profile a device's solve used, as the rules a check
// of the result sends: a built-in by name, or a school profile's overrides as
// the solve was sent them. False is anything that is not one of the two.
func ruleProfileOf(raw string) (*plans.RuleProfile, bool) {
	decoder := json.NewDecoder(bytes.NewReader([]byte(raw)))
	decoder.DisallowUnknownFields()
	var body gen.SolveResultUploadProfile
	if err := decoder.Decode(&body); err != nil {
		return nil, false
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return nil, false
	}
	name := strings.TrimSpace(body.Name)
	rules := &arrangov1.RuleConfig{}
	var id int64
	if body.Id == nil {
		if body.Overrides != nil || !builtinProfilePattern.MatchString(name) {
			return nil, false
		}
		rules.Profile = name
	} else {
		parsed, ok := profileID(*body.Id)
		if !ok || !decimalID.MatchString(*body.Id) || body.Overrides == nil || name == "" || utf8.RuneCountInString(name) > profiles.MaxNameLength {
			return nil, false
		}
		overrides, ok := overridesOf(*body.Overrides)
		if !ok || profiles.CheckOverrides(overrides) != nil {
			return nil, false
		}
		id, rules.Overrides = parsed, overrides
	}
	encoded, err := arrango.EncodeRules(rules)
	if err != nil {
		return nil, false
	}
	return &plans.RuleProfile{Name: name, ID: id, Rules: encoded}, true
}

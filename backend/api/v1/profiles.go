// api/v1/profiles.go

package v1

import (
	"context"
	"errors"
	"math"
	"strconv"

	"github.com/smegg99/s99ab/auth/kratosauth"

	arrangov1 "github.com/smegg99/arrango/backend/gen/arrangov1"

	"github.com/smegg99/goptivum/backend/api/v1/gen"
	"github.com/smegg99/goptivum/backend/common/messages"
	"github.com/smegg99/goptivum/backend/core/profiles"
)

// Profiles serves the school's own rule profiles: everybody's to read and use, their creator's and an administrator's to change.
type Profiles struct {
	profiles *profiles.Repository
}

// NewProfiles wires the profile routes; with a nil repository they answer that the store is unavailable.
func NewProfiles(repository *profiles.Repository) *Profiles {
	return &Profiles{profiles: repository}
}

// ListRuleProfiles is every school profile, oldest first.
func (p *Profiles) ListRuleProfiles(ctx context.Context, _ gen.ListRuleProfilesRequestObject) (gen.ListRuleProfilesResponseObject, error) {
	caller, ok := editorOf(ctx)
	if !ok {
		return gen.ListRuleProfiles401JSONResponse{UnauthorizedJSONResponse: gen.UnauthorizedJSONResponse(unauthorized())}, nil
	}
	if p == nil || p.profiles == nil {
		return gen.ListRuleProfiles503JSONResponse{UnavailableJSONResponse: gen.UnavailableJSONResponse(unavailable())}, nil
	}
	listed, err := p.profiles.List(ctx)
	if err != nil {
		return gen.ListRuleProfiles503JSONResponse{UnavailableJSONResponse: gen.UnavailableJSONResponse(unavailable())}, nil
	}
	items := make([]gen.SchoolRuleProfile, 0, len(listed))
	for _, profile := range listed {
		body, err := profileBody(profile, caller)
		if err != nil {
			return nil, err
		}
		items = append(items, body)
	}
	return gen.ListRuleProfiles200JSONResponse{Items: items}, nil
}

// CreateRuleProfile stores a profile the whole school shares, created by the caller.
func (p *Profiles) CreateRuleProfile(ctx context.Context, request gen.CreateRuleProfileRequestObject) (gen.CreateRuleProfileResponseObject, error) {
	caller, ok := editorOf(ctx)
	if !ok {
		return gen.CreateRuleProfile401JSONResponse{UnauthorizedJSONResponse: gen.UnauthorizedJSONResponse(unauthorized())}, nil
	}
	if p == nil || p.profiles == nil {
		return gen.CreateRuleProfile503JSONResponse{UnavailableJSONResponse: gen.UnavailableJSONResponse(unavailable())}, nil
	}
	body := request.Body
	switch {
	case body == nil:
		return gen.CreateRuleProfile422JSONResponse{ValidationJSONResponse: gen.ValidationJSONResponse(validation("body", "required"))}, nil
	case body.Overrides == nil:
		return gen.CreateRuleProfile422JSONResponse{ValidationJSONResponse: gen.ValidationJSONResponse(validation("overrides", "required"))}, nil
	}
	overrides, ok := overridesOf(body.Overrides)
	if !ok {
		return gen.CreateRuleProfile422JSONResponse{ValidationJSONResponse: gen.ValidationJSONResponse(validation("overrides", "invalid"))}, nil
	}
	author := starter(ctx)
	created, err := p.profiles.Create(ctx, profiles.NewProfile{
		Name: body.Name, Overrides: overrides, CreatedBy: author.ID, CreatedByName: author.Name,
	})
	var invalid *profiles.Invalid
	switch {
	case errors.As(err, &invalid):
		return gen.CreateRuleProfile422JSONResponse{ValidationJSONResponse: gen.ValidationJSONResponse(validation(invalid.Field, invalid.Code))}, nil
	case err != nil:
		return gen.CreateRuleProfile503JSONResponse{UnavailableJSONResponse: gen.UnavailableJSONResponse(unavailable())}, nil
	}
	out, err := profileBody(created, caller)
	if err != nil {
		return nil, err
	}
	return gen.CreateRuleProfile201JSONResponse(out), nil
}

// UpdateRuleProfile renames a profile or replaces its overrides, for its creator or an administrator.
func (p *Profiles) UpdateRuleProfile(ctx context.Context, request gen.UpdateRuleProfileRequestObject) (gen.UpdateRuleProfileResponseObject, error) {
	caller, ok := editorOf(ctx)
	if !ok {
		return gen.UpdateRuleProfile401JSONResponse{UnauthorizedJSONResponse: gen.UnauthorizedJSONResponse(unauthorized())}, nil
	}
	id, ok := profileID(request.ProfileId)
	if !ok || p == nil || p.profiles == nil {
		return gen.UpdateRuleProfile404JSONResponse{NotFoundJSONResponse: gen.NotFoundJSONResponse(notFound())}, nil
	}
	body := request.Body
	if body == nil {
		return gen.UpdateRuleProfile422JSONResponse{ValidationJSONResponse: gen.ValidationJSONResponse(validation("body", "required"))}, nil
	}
	change := profiles.Change{Name: body.Name}
	if body.Overrides != nil {
		overrides, ok := overridesOf(*body.Overrides)
		if !ok {
			return gen.UpdateRuleProfile422JSONResponse{ValidationJSONResponse: gen.ValidationJSONResponse(validation("overrides", "invalid"))}, nil
		}
		change.Overrides = &overrides
	}
	changed, err := p.profiles.Update(ctx, id, caller, change)
	var invalid *profiles.Invalid
	switch {
	case errors.Is(err, profiles.ErrNotFound):
		return gen.UpdateRuleProfile404JSONResponse{NotFoundJSONResponse: gen.NotFoundJSONResponse(notFound())}, nil
	case errors.Is(err, profiles.ErrNotYours):
		return gen.UpdateRuleProfile403JSONResponse{ForbiddenJSONResponse: gen.ForbiddenJSONResponse(notYours())}, nil
	case errors.As(err, &invalid):
		return gen.UpdateRuleProfile422JSONResponse{ValidationJSONResponse: gen.ValidationJSONResponse(validation(invalid.Field, invalid.Code))}, nil
	case err != nil:
		return gen.UpdateRuleProfile503JSONResponse{UnavailableJSONResponse: gen.UnavailableJSONResponse(unavailable())}, nil
	}
	out, err := profileBody(changed, caller)
	if err != nil {
		return nil, err
	}
	return gen.UpdateRuleProfile200JSONResponse(out), nil
}

// DeleteRuleProfile removes a profile for good, for its creator or an administrator.
func (p *Profiles) DeleteRuleProfile(ctx context.Context, request gen.DeleteRuleProfileRequestObject) (gen.DeleteRuleProfileResponseObject, error) {
	caller, ok := editorOf(ctx)
	if !ok {
		return gen.DeleteRuleProfile401JSONResponse{UnauthorizedJSONResponse: gen.UnauthorizedJSONResponse(unauthorized())}, nil
	}
	id, ok := profileID(request.ProfileId)
	if !ok || p == nil || p.profiles == nil {
		return gen.DeleteRuleProfile404JSONResponse{NotFoundJSONResponse: gen.NotFoundJSONResponse(notFound())}, nil
	}
	err := p.profiles.Delete(ctx, id, caller)
	switch {
	case errors.Is(err, profiles.ErrNotFound):
		return gen.DeleteRuleProfile404JSONResponse{NotFoundJSONResponse: gen.NotFoundJSONResponse(notFound())}, nil
	case errors.Is(err, profiles.ErrNotYours):
		return gen.DeleteRuleProfile403JSONResponse{ForbiddenJSONResponse: gen.ForbiddenJSONResponse(notYours())}, nil
	case err != nil:
		return gen.DeleteRuleProfile503JSONResponse{UnavailableJSONResponse: gen.UnavailableJSONResponse(unavailable())}, nil
	}
	return gen.DeleteRuleProfile204Response{}, nil
}

// editorOf is the signed-in account as a profile's permissions see it; the role is read from metadata_public, never traits.
func editorOf(ctx context.Context) (profiles.Editor, bool) {
	identity, ok := kratosauth.FromContext(ctx)
	if !ok || identity.ID == "" {
		return profiles.Editor{}, false
	}
	return profiles.Editor{ID: identity.ID, Admin: kratosauth.HasMeta(identity, "role", roleAdmin)}, true
}

func notYours() gen.Error {
	return gen.Error{Code: "forbidden", Message: messages.KeyRulesProfileNotYours}
}

// profileID reads a path id; one that cannot be an id is not found.
func profileID(raw string) (int64, bool) {
	id, err := strconv.ParseInt(raw, 10, 64)
	return id, err == nil && id > 0
}

func profileBody(profile profiles.Profile, caller profiles.Editor) (gen.SchoolRuleProfile, error) {
	overrides := make([]gen.RuleOverride, 0, len(profile.Overrides))
	for _, override := range profile.Overrides {
		out, err := profileOverrideBody(profile.Name, override)
		if err != nil {
			return gen.SchoolRuleProfile{}, err
		}
		overrides = append(overrides, out)
	}
	return gen.SchoolRuleProfile{
		Id: strconv.FormatInt(profile.ID, 10), Name: profile.Name, Overrides: overrides,
		CreatedBy: gen.Starter{Id: profile.CreatedBy, Name: profile.CreatedByName},
		CreatedAt: profile.CreatedAt, UpdatedAt: profile.UpdatedAt, Editable: profile.EditableBy(caller),
	}, nil
}

// overridesOf maps the contract's overrides to the node's; false is a mode, weight or param outside the contract's schema.
func overridesOf(body []gen.RuleOverride) ([]*arrangov1.RuleOverride, bool) {
	out := make([]*arrangov1.RuleOverride, 0, len(body))
	for _, one := range body {
		override := &arrangov1.RuleOverride{Rule: one.Rule}
		if one.Mode != nil {
			mode, ok := nodeRuleMode(*one.Mode)
			if !ok {
				return nil, false
			}
			override.Mode = mode
		}
		if one.Weight != nil {
			// Zero is the node's "keep the built-in weight", which an absent weight already says.
			if *one.Weight < 1 {
				return nil, false
			}
			override.Weight = *one.Weight
		}
		if one.Param != nil {
			if *one.Param < 1 || int64(*one.Param) > math.MaxUint32 {
				return nil, false
			}
			override.Param = uint32(*one.Param)
		}
		out = append(out, override)
	}
	return out, true
}

func nodeRuleMode(mode gen.RuleMode) (arrangov1.RuleMode, bool) {
	switch mode {
	case gen.Hard:
		return arrangov1.RuleMode_RULE_MODE_HARD, true
	case gen.Soft:
		return arrangov1.RuleMode_RULE_MODE_SOFT, true
	case gen.Off:
		return arrangov1.RuleMode_RULE_MODE_OFF, true
	}
	return arrangov1.RuleMode_RULE_MODE_DEFAULT, false
}

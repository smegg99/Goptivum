// api/v1/checkpoints.go

package v1

import (
	"context"
	"errors"

	"github.com/smegg99/goptivum/backend/api/v1/gen"
	"github.com/smegg99/goptivum/backend/core/plans"
)

// NameCheckpoint names one version of the caller's own plan as a checkpoint,
// or renames one.
func (a *API) NameCheckpoint(ctx context.Context, request gen.NameCheckpointRequestObject) (gen.NameCheckpointResponseObject, error) {
	identity, ok := callerID(ctx)
	if !ok {
		return gen.NameCheckpoint401JSONResponse{UnauthorizedJSONResponse: gen.UnauthorizedJSONResponse(unauthorized())}, nil
	}
	if request.Body == nil {
		return gen.NameCheckpoint422JSONResponse{ValidationJSONResponse: gen.ValidationJSONResponse(validation("body", "required"))}, nil
	}
	plan, err := a.editable(ctx, request.PlanId, identity)
	if err == nil {
		var version plans.Version
		version, err = a.plans.NameCheckpoint(ctx, plan.ID, request.Version, request.Body.Name)
		if err == nil {
			return gen.NameCheckpoint200JSONResponse(versionBody(version)), nil
		}
	}
	if refusal, ok := invalid(err); ok {
		return gen.NameCheckpoint422JSONResponse{ValidationJSONResponse: gen.ValidationJSONResponse(refusal)}, nil
	}
	if errors.Is(err, errNotYours) {
		return gen.NameCheckpoint403JSONResponse{ForbiddenJSONResponse: gen.ForbiddenJSONResponse(planNotYours())}, nil
	}
	if missing(err) {
		return gen.NameCheckpoint404JSONResponse{NotFoundJSONResponse: gen.NotFoundJSONResponse(notFound())}, nil
	}
	return gen.NameCheckpoint503JSONResponse{UnavailableJSONResponse: gen.UnavailableJSONResponse(unavailable())}, nil
}

// DeleteCheckpoint moves a checkpoint of the caller's own plan to Trash.
func (a *API) DeleteCheckpoint(ctx context.Context, request gen.DeleteCheckpointRequestObject) (gen.DeleteCheckpointResponseObject, error) {
	identity, ok := callerID(ctx)
	if !ok {
		return gen.DeleteCheckpoint401JSONResponse{UnauthorizedJSONResponse: gen.UnauthorizedJSONResponse(unauthorized())}, nil
	}
	plan, err := a.editable(ctx, request.PlanId, identity)
	if err == nil {
		if err = a.plans.DeleteCheckpoint(ctx, plan.ID, request.Version); err == nil {
			return gen.DeleteCheckpoint204Response{}, nil
		}
	}
	if errors.Is(err, errNotYours) {
		return gen.DeleteCheckpoint403JSONResponse{ForbiddenJSONResponse: gen.ForbiddenJSONResponse(planNotYours())}, nil
	}
	if missing(err) {
		return gen.DeleteCheckpoint404JSONResponse{NotFoundJSONResponse: gen.NotFoundJSONResponse(notFound())}, nil
	}
	return gen.DeleteCheckpoint503JSONResponse{UnavailableJSONResponse: gen.UnavailableJSONResponse(unavailable())}, nil
}

// RestoreCheckpoint takes a checkpoint of the caller's own live plan out of
// Trash.
func (a *API) RestoreCheckpoint(ctx context.Context, request gen.RestoreCheckpointRequestObject) (gen.RestoreCheckpointResponseObject, error) {
	identity, ok := callerID(ctx)
	if !ok {
		return gen.RestoreCheckpoint401JSONResponse{UnauthorizedJSONResponse: gen.UnauthorizedJSONResponse(unauthorized())}, nil
	}
	plan, err := a.editable(ctx, request.PlanId, identity)
	if err == nil {
		var version plans.Version
		if version, err = a.plans.RestoreCheckpoint(ctx, plan.ID, request.Version); err == nil {
			return gen.RestoreCheckpoint200JSONResponse(versionBody(version)), nil
		}
	}
	if errors.Is(err, errNotYours) {
		return gen.RestoreCheckpoint403JSONResponse{ForbiddenJSONResponse: gen.ForbiddenJSONResponse(planNotYours())}, nil
	}
	if missing(err) {
		return gen.RestoreCheckpoint404JSONResponse{NotFoundJSONResponse: gen.NotFoundJSONResponse(notFound())}, nil
	}
	return gen.RestoreCheckpoint503JSONResponse{UnavailableJSONResponse: gen.UnavailableJSONResponse(unavailable())}, nil
}

// missing is a plan, a version or a result that is not there, or a plan in
// Trash.
func missing(err error) bool {
	return errors.Is(err, plans.ErrNotFound) || errors.Is(err, plans.ErrNoVersion) ||
		errors.Is(err, plans.ErrNoResult)
}

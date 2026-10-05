// api/v1/results.go

package v1

import (
	"bytes"
	"context"
	"errors"
	"strconv"

	"github.com/smegg99/goptivum/backend/api/v1/gen"
	"github.com/smegg99/goptivum/backend/core/plans"
)

// resultID reads the decimal string a path carries, as planID does.
func resultID(raw string) (int64, bool) { return planID(raw) }

func resultBody(result plans.Result) gen.SolveResult {
	body := gen.SolveResult{
		Id:            strconv.FormatInt(result.ID, 10),
		PlanId:        strconv.FormatInt(result.PlanID, 10),
		SourceVersion: result.SourceVersion,
		ObjectKey:     result.ObjectKey,
		SizeBytes:     result.SizeBytes,
		CreatedAt:     result.CreatedAt,
		StartedBy:     gen.Starter{Id: result.StartedBy, Name: result.StartedByName},
		Quality:       result.Quality,
		Pinned:        result.Pinned,
	}
	if result.JobID != 0 {
		body.JobId = pointerTo(strconv.FormatInt(result.JobID, 10))
	}
	if result.Device != "" {
		body.Device = pointerTo(result.Device)
	}
	if result.Verdict != "" {
		body.Verdict = pointerTo(result.Verdict)
	}
	if result.Name != "" {
		body.Name = pointerTo(result.Name)
	}
	if profile := result.RuleProfile; profile != nil {
		body.RuleProfile = solveRuleProfile(profile.Name, profile.ID)
	}
	return body
}

// resultOf resolves a result out of Trash of a plan already resolved; anything
// else is plans.ErrNoResult.
func (a *API) resultOf(ctx context.Context, plan plans.Plan, rawResult string) (plans.Result, error) {
	id, ok := resultID(rawResult)
	if !ok {
		return plans.Result{}, plans.ErrNoResult
	}
	return a.plans.Result(ctx, plan.ID, id)
}

// readableResult resolves a result out of Trash of any live plan; anything
// else is plans.ErrNotFound or plans.ErrNoResult.
func (a *API) readableResult(ctx context.Context, rawPlan, rawResult string) (plans.Plan, plans.Result, error) {
	plan, err := a.readable(ctx, rawPlan)
	if err != nil {
		return plans.Plan{}, plans.Result{}, err
	}
	result, err := a.resultOf(ctx, plan, rawResult)
	return plan, result, err
}

// editableResult resolves a result out of Trash of a live plan the caller
// owns; another owner's plan is errNotYours.
func (a *API) editableResult(ctx context.Context, caller, rawPlan, rawResult string) (plans.Plan, plans.Result, error) {
	plan, err := a.editable(ctx, rawPlan, caller)
	if err != nil {
		return plans.Plan{}, plans.Result{}, err
	}
	result, err := a.resultOf(ctx, plan, rawResult)
	return plan, result, err
}

// ListResults is the plan's results folder.
func (a *API) ListResults(ctx context.Context, request gen.ListResultsRequestObject) (gen.ListResultsResponseObject, error) {
	if _, ok := callerID(ctx); !ok {
		return gen.ListResults401JSONResponse{UnauthorizedJSONResponse: gen.UnauthorizedJSONResponse(unauthorized())}, nil
	}
	plan, err := a.readable(ctx, request.PlanId)
	if err != nil {
		if errors.Is(err, plans.ErrNotFound) {
			return gen.ListResults404JSONResponse{NotFoundJSONResponse: gen.NotFoundJSONResponse(notFound())}, nil
		}
		return gen.ListResults503JSONResponse{UnavailableJSONResponse: gen.UnavailableJSONResponse(unavailable())}, nil
	}
	listed, err := a.plans.Results(ctx, plan.ID)
	if err != nil {
		return gen.ListResults503JSONResponse{UnavailableJSONResponse: gen.UnavailableJSONResponse(unavailable())}, nil
	}
	items := make([]gen.SolveResult, 0, len(listed))
	for _, result := range listed {
		items = append(items, resultBody(result))
	}
	return gen.ListResults200JSONResponse{Items: items}, nil
}

// DownloadResult returns a result's stored document as it was written.
func (a *API) DownloadResult(ctx context.Context, request gen.DownloadResultRequestObject) (gen.DownloadResultResponseObject, error) {
	if _, ok := callerID(ctx); !ok {
		return gen.DownloadResult401JSONResponse{UnauthorizedJSONResponse: gen.UnauthorizedJSONResponse(unauthorized())}, nil
	}
	_, result, err := a.readableResult(ctx, request.PlanId, request.ResultId)
	if err != nil {
		if missing(err) {
			return gen.DownloadResult404JSONResponse{NotFoundJSONResponse: gen.NotFoundJSONResponse(notFound())}, nil
		}
		return gen.DownloadResult503JSONResponse{UnavailableJSONResponse: gen.UnavailableJSONResponse(unavailable())}, nil
	}
	data, err := a.documents.Bytes(ctx, result.ObjectKey)
	if err != nil {
		// A row pointing at nothing is a repairable inconsistency, as in DownloadVersion.
		return gen.DownloadResult503JSONResponse{UnavailableJSONResponse: gen.UnavailableJSONResponse(unavailable())}, nil
	}
	return gen.DownloadResult200ApplicationzstdResponse{
		Body: bytes.NewReader(data), ContentLength: int64(len(data)),
	}, nil
}

// UpdateResult names, renames or pins a result of the caller's own plan.
func (a *API) UpdateResult(ctx context.Context, request gen.UpdateResultRequestObject) (gen.UpdateResultResponseObject, error) {
	identity, ok := callerID(ctx)
	if !ok {
		return gen.UpdateResult401JSONResponse{UnauthorizedJSONResponse: gen.UnauthorizedJSONResponse(unauthorized())}, nil
	}
	if request.Body == nil {
		return gen.UpdateResult422JSONResponse{ValidationJSONResponse: gen.ValidationJSONResponse(validation("body", "required"))}, nil
	}
	plan, result, err := a.editableResult(ctx, identity, request.PlanId, request.ResultId)
	if err == nil {
		result, err = a.plans.UpdateResult(ctx, plan.ID, result.ID, plans.ResultChange{
			Name: request.Body.Name, Pinned: request.Body.Pinned,
		})
	}
	if refusal, ok := invalid(err); ok {
		return gen.UpdateResult422JSONResponse{ValidationJSONResponse: gen.ValidationJSONResponse(refusal)}, nil
	}
	switch {
	case errors.Is(err, errNotYours):
		return gen.UpdateResult403JSONResponse{ForbiddenJSONResponse: gen.ForbiddenJSONResponse(planNotYours())}, nil
	case missing(err):
		return gen.UpdateResult404JSONResponse{NotFoundJSONResponse: gen.NotFoundJSONResponse(notFound())}, nil
	case err != nil:
		return gen.UpdateResult503JSONResponse{UnavailableJSONResponse: gen.UnavailableJSONResponse(unavailable())}, nil
	}
	return gen.UpdateResult200JSONResponse(resultBody(result)), nil
}

// DeleteResult moves a result of the caller's own plan to Trash.
func (a *API) DeleteResult(ctx context.Context, request gen.DeleteResultRequestObject) (gen.DeleteResultResponseObject, error) {
	identity, ok := callerID(ctx)
	if !ok {
		return gen.DeleteResult401JSONResponse{UnauthorizedJSONResponse: gen.UnauthorizedJSONResponse(unauthorized())}, nil
	}
	plan, result, err := a.editableResult(ctx, identity, request.PlanId, request.ResultId)
	if err == nil {
		err = a.plans.TrashResult(ctx, plan.ID, result.ID)
	}
	switch {
	case errors.Is(err, errNotYours):
		return gen.DeleteResult403JSONResponse{ForbiddenJSONResponse: gen.ForbiddenJSONResponse(planNotYours())}, nil
	case missing(err):
		return gen.DeleteResult404JSONResponse{NotFoundJSONResponse: gen.NotFoundJSONResponse(notFound())}, nil
	case err != nil:
		return gen.DeleteResult503JSONResponse{UnavailableJSONResponse: gen.UnavailableJSONResponse(unavailable())}, nil
	}
	return gen.DeleteResult204Response{}, nil
}

// RestoreResult takes a result of the caller's own live plan out of Trash. It
// does not go through editableResult, which sees results out of Trash only.
func (a *API) RestoreResult(ctx context.Context, request gen.RestoreResultRequestObject) (gen.RestoreResultResponseObject, error) {
	identity, ok := callerID(ctx)
	if !ok {
		return gen.RestoreResult401JSONResponse{UnauthorizedJSONResponse: gen.UnauthorizedJSONResponse(unauthorized())}, nil
	}
	plan, err := a.editable(ctx, request.PlanId, identity)
	var result plans.Result
	if err == nil {
		id, ok := resultID(request.ResultId)
		if !ok {
			return gen.RestoreResult404JSONResponse{NotFoundJSONResponse: gen.NotFoundJSONResponse(notFound())}, nil
		}
		result, err = a.plans.RestoreResult(ctx, plan.ID, id)
	}
	switch {
	case errors.Is(err, errNotYours):
		return gen.RestoreResult403JSONResponse{ForbiddenJSONResponse: gen.ForbiddenJSONResponse(planNotYours())}, nil
	case missing(err):
		return gen.RestoreResult404JSONResponse{NotFoundJSONResponse: gen.NotFoundJSONResponse(notFound())}, nil
	case err != nil:
		return gen.RestoreResult503JSONResponse{UnavailableJSONResponse: gen.UnavailableJSONResponse(unavailable())}, nil
	}
	return gen.RestoreResult200JSONResponse(resultBody(result)), nil
}

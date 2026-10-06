// api/v1/trash.go

package v1

import (
	"context"
	"errors"
	"sort"
	"strconv"

	"github.com/smegg99/goptivum/backend/api/v1/gen"
	"github.com/smegg99/goptivum/backend/core/audit"
	"github.com/smegg99/goptivum/backend/core/plans"
)

// ListTrash is what the caller owns in Trash and can still restore: its
// projects, and the checkpoints and results of its live projects, most
// recently deleted first. An administrator sees every owner's in Storage.
func (a *API) ListTrash(ctx context.Context, _ gen.ListTrashRequestObject) (gen.ListTrashResponseObject, error) {
	identity, ok := callerID(ctx)
	if !ok {
		return gen.ListTrash401JSONResponse{UnauthorizedJSONResponse: gen.UnauthorizedJSONResponse(unauthorized())}, nil
	}
	trashed, err := a.plans.Trashed(ctx, identity)
	if err != nil {
		return gen.ListTrash503JSONResponse{UnavailableJSONResponse: gen.UnavailableJSONResponse(unavailable())}, nil
	}
	checkpoints, err := a.plans.TrashedCheckpoints(ctx, identity)
	if err != nil {
		return gen.ListTrash503JSONResponse{UnavailableJSONResponse: gen.UnavailableJSONResponse(unavailable())}, nil
	}
	results, err := a.plans.TrashedResults(ctx, identity)
	if err != nil {
		return gen.ListTrash503JSONResponse{UnavailableJSONResponse: gen.UnavailableJSONResponse(unavailable())}, nil
	}
	days := a.trashDays()
	items := make([]gen.TrashItem, 0, len(trashed)+len(checkpoints)+len(results))
	for _, plan := range trashed {
		items = append(items, gen.TrashItem{
			Kind:       gen.TrashItemKindProject,
			PlanId:     strconv.FormatInt(plan.ID, 10),
			Name:       plan.Name,
			SchoolYear: plan.SchoolYear,
			DeletedAt:  plan.DeletedAt,
			RemovesAt:  plans.RemovesAt(plan.DeletedAt, days),
		})
	}
	for _, checkpoint := range checkpoints {
		items = append(items, gen.TrashItem{
			Kind:       gen.TrashItemKindCheckpoint,
			PlanId:     strconv.FormatInt(checkpoint.Plan.ID, 10),
			Name:       checkpoint.Plan.Name,
			SchoolYear: checkpoint.Plan.SchoolYear,
			Version:    pointerTo(checkpoint.Version),
			Checkpoint: pointerTo(checkpoint.Name),
			DeletedAt:  checkpoint.DeletedAt,
			RemovesAt:  plans.RemovesAt(checkpoint.DeletedAt, days),
		})
	}
	for _, trashedResult := range results {
		result := resultBody(trashedResult.Result)
		items = append(items, gen.TrashItem{
			Kind:       gen.TrashItemKindResult,
			PlanId:     strconv.FormatInt(trashedResult.Plan.ID, 10),
			Name:       trashedResult.Plan.Name,
			SchoolYear: trashedResult.Plan.SchoolYear,
			Result:     &result,
			DeletedAt:  trashedResult.Result.DeletedAt,
			RemovesAt:  plans.RemovesAt(trashedResult.Result.DeletedAt, days),
		})
	}
	// Every list is newest first; the merge keeps that across kinds.
	sort.SliceStable(items, func(i, j int) bool { return items[i].DeletedAt.After(items[j].DeletedAt) })
	return gen.ListTrash200JSONResponse{Items: items}, nil
}

// RestorePlan takes one of the caller's plans out of Trash. It does not go
// through editable, which sees live plans only; another owner's plan, in
// Trash or not, is not the caller's to restore.
func (a *API) RestorePlan(ctx context.Context, request gen.RestorePlanRequestObject) (gen.RestorePlanResponseObject, error) {
	identity, ok := callerID(ctx)
	if !ok {
		return gen.RestorePlan401JSONResponse{UnauthorizedJSONResponse: gen.UnauthorizedJSONResponse(unauthorized())}, nil
	}
	id, ok := planID(request.PlanId)
	if !ok {
		return gen.RestorePlan404JSONResponse{NotFoundJSONResponse: gen.NotFoundJSONResponse(notFound())}, nil
	}
	found, err := a.plans.Lookup(ctx, []int64{id})
	if err != nil {
		return gen.RestorePlan503JSONResponse{UnavailableJSONResponse: gen.UnavailableJSONResponse(unavailable())}, nil
	}
	if stored, exists := found[id]; !exists {
		return gen.RestorePlan404JSONResponse{NotFoundJSONResponse: gen.NotFoundJSONResponse(notFound())}, nil
	} else if stored.OwnerIdentityID != identity {
		return gen.RestorePlan403JSONResponse{ForbiddenJSONResponse: gen.ForbiddenJSONResponse(planNotYours())}, nil
	}
	plan, err := a.plans.Restore(ctx, id, identity)
	switch {
	case errors.Is(err, plans.ErrNotFound):
		return gen.RestorePlan404JSONResponse{NotFoundJSONResponse: gen.NotFoundJSONResponse(notFound())}, nil
	case err != nil:
		return gen.RestorePlan503JSONResponse{UnavailableJSONResponse: gen.UnavailableJSONResponse(unavailable())}, nil
	}
	audited(ctx, a.trail, audit.ProjectRestored, planTarget(plan), nil)
	body, err := a.planAnswer(ctx, plan)
	if err != nil {
		return gen.RestorePlan503JSONResponse{UnavailableJSONResponse: gen.UnavailableJSONResponse(unavailable())}, nil
	}
	return gen.RestorePlan200JSONResponse(body), nil
}

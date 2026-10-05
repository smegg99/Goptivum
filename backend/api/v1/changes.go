// api/v1/changes.go

package v1

import (
	"context"
	"strconv"

	"github.com/smegg99/goptivum/backend/api/v1/gen"
	"github.com/smegg99/goptivum/backend/core/plans"
)

// ListChanges tells a desktop which plans changed after a point, so it reads
// again only those.
func (a *API) ListChanges(ctx context.Context, request gen.ListChangesRequestObject) (gen.ListChangesResponseObject, error) {
	if _, ok := callerID(ctx); !ok {
		return gen.ListChanges401JSONResponse{UnauthorizedJSONResponse: gen.UnauthorizedJSONResponse(unauthorized())}, nil
	}
	var since *plans.Cursor
	if request.Params.Since != nil {
		cursor, err := plans.ParseCursor(*request.Params.Since)
		if refusal, ok := invalid(err); ok {
			return gen.ListChanges422JSONResponse{ValidationJSONResponse: gen.ValidationJSONResponse(refusal)}, nil
		}
		since = &cursor
	}
	changes, err := a.plans.Changes(ctx, since, plans.ChangesPage)
	if err != nil {
		return gen.ListChanges503JSONResponse{UnavailableJSONResponse: gen.UnavailableJSONResponse(unavailable())}, nil
	}

	body := gen.ListChanges200JSONResponse{
		Cursor: changes.Cursor.String(), Reset: changes.Reset, More: changes.More,
		Plans: make([]gen.PlanChange, 0, len(changes.Plans)),
	}
	for _, change := range changes.Plans {
		parts := make([]gen.ChangedPart, 0, len(change.Parts))
		for _, part := range change.Parts {
			parts = append(parts, gen.ChangedPart(part))
		}
		body.Plans = append(body.Plans, gen.PlanChange{
			PlanId: strconv.FormatInt(change.PlanID, 10), State: gen.PlanState(change.State), Parts: parts,
		})
	}
	return body, nil
}

// api/v1/sharing.go

package v1

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/smegg99/goptivum/backend/api/v1/gen"
	"github.com/smegg99/goptivum/backend/common/messages"
	"github.com/smegg99/goptivum/backend/core/audit"
	"github.com/smegg99/goptivum/backend/core/plans"
	"github.com/smegg99/goptivum/backend/identity"
)

// ForkPlan makes the caller's own copy of any live plan: a fork of another
// owner's plan, or a duplicate of its own.
func (a *API) ForkPlan(ctx context.Context, request gen.ForkPlanRequestObject) (gen.ForkPlanResponseObject, error) {
	identity, ok := callerID(ctx)
	if !ok {
		return gen.ForkPlan401JSONResponse{UnauthorizedJSONResponse: gen.UnauthorizedJSONResponse(unauthorized())}, nil
	}
	body := request.Body
	if body == nil {
		return gen.ForkPlan422JSONResponse{ValidationJSONResponse: gen.ValidationJSONResponse(validation("body", "required"))}, nil
	}
	var version int64
	if body.Version != nil {
		if *body.Version < 1 {
			return gen.ForkPlan422JSONResponse{ValidationJSONResponse: gen.ValidationJSONResponse(validation("version", "invalid"))}, nil
		}
		version = *body.Version
	}
	origin, err := a.readable(ctx, request.PlanId)
	switch {
	case errors.Is(err, plans.ErrNotFound):
		return gen.ForkPlan404JSONResponse{NotFoundJSONResponse: gen.NotFoundJSONResponse(notFound())}, nil
	case err != nil:
		return gen.ForkPlan503JSONResponse{UnavailableJSONResponse: gen.UnavailableJSONResponse(unavailable())}, nil
	}
	name := a.forkName(origin.Name)
	if body.Name != nil {
		name = *body.Name
	}

	fork, _, err := a.plans.Fork(ctx, plans.ForkOf{Origin: origin.ID, Version: version, Owner: identity, Name: name})
	if refusal, ok := invalid(err); ok {
		return gen.ForkPlan422JSONResponse{ValidationJSONResponse: gen.ValidationJSONResponse(refusal)}, nil
	}
	switch {
	case missing(err):
		return gen.ForkPlan404JSONResponse{NotFoundJSONResponse: gen.NotFoundJSONResponse(notFound())}, nil
	case err != nil:
		return gen.ForkPlan503JSONResponse{UnavailableJSONResponse: gen.UnavailableJSONResponse(unavailable())}, nil
	}
	audited(ctx, a.trail, audit.ProjectCreated, planTarget(fork), map[string]string{
		"source": sourceFork, "origin": strconv.FormatInt(origin.ID, 10), "origin_name": origin.Name,
		"version": strconv.FormatInt(fork.Origin.Version, 10),
	})
	answer, err := a.planAnswer(ctx, fork)
	if err != nil {
		return gen.ForkPlan503JSONResponse{UnavailableJSONResponse: gen.UnavailableJSONResponse(unavailable())}, nil
	}
	return gen.ForkPlan201JSONResponse(answer), nil
}

// forkName is the name a copy gets when the request names none, in the
// server's language, with the origin's name shortened until it fits.
func (a *API) forkName(origin string) string {
	words := messages.Localizer(a.language)
	name := messages.PlansForkName(words, messages.PlansForkNameParams{Name: origin})
	if over := utf8.RuneCountInString(name) - plans.MaxNameLength; over > 0 {
		kept := []rune(origin)
		kept = kept[:max(len(kept)-over, 0)]
		name = messages.PlansForkName(words, messages.PlansForkNameParams{Name: strings.TrimSpace(string(kept))})
	}
	return name
}

// TransferPlan gives a live plan to another account. Its route is
// administrator-only, so the handler does not check the role again.
func (a *API) TransferPlan(ctx context.Context, request gen.TransferPlanRequestObject) (gen.TransferPlanResponseObject, error) {
	if _, ok := callerID(ctx); !ok {
		return gen.TransferPlan401JSONResponse{UnauthorizedJSONResponse: gen.UnauthorizedJSONResponse(unauthorized())}, nil
	}
	if a.accounts == nil {
		return gen.TransferPlan404JSONResponse{NotFoundJSONResponse: gen.NotFoundJSONResponse(notFound())}, nil
	}
	if request.Body == nil {
		return gen.TransferPlan422JSONResponse{ValidationJSONResponse: gen.ValidationJSONResponse(validation("body", "required"))}, nil
	}
	ownerID := strings.TrimSpace(request.Body.OwnerId)
	if ownerID == "" {
		return gen.TransferPlan422JSONResponse{ValidationJSONResponse: gen.ValidationJSONResponse(validation("owner_id", "required"))}, nil
	}
	plan, err := a.readable(ctx, request.PlanId)
	switch {
	case errors.Is(err, plans.ErrNotFound):
		return gen.TransferPlan404JSONResponse{NotFoundJSONResponse: gen.NotFoundJSONResponse(notFound())}, nil
	case err != nil:
		return gen.TransferPlan503JSONResponse{UnavailableJSONResponse: gen.UnavailableJSONResponse(unavailable())}, nil
	}
	account, err := a.accounts.Get(ctx, ownerID)
	switch {
	case errors.Is(err, identity.ErrNotFound):
		return gen.TransferPlan422JSONResponse{ValidationJSONResponse: gen.ValidationJSONResponse(validation("owner_id", "unknown"))}, nil
	case err != nil:
		return gen.TransferPlan503JSONResponse{UnavailableJSONResponse: gen.UnavailableJSONResponse(unavailable())}, nil
	case !account.Active:
		return gen.TransferPlan422JSONResponse{ValidationJSONResponse: gen.ValidationJSONResponse(validation("owner_id", "disabled"))}, nil
	}

	moved := plan
	if plan.OwnerIdentityID != account.ID {
		moved, err = a.plans.Transfer(ctx, plan.ID, account.ID)
		switch {
		case errors.Is(err, plans.ErrNotFound):
			return gen.TransferPlan404JSONResponse{NotFoundJSONResponse: gen.NotFoundJSONResponse(notFound())}, nil
		case err != nil:
			return gen.TransferPlan503JSONResponse{UnavailableJSONResponse: gen.UnavailableJSONResponse(unavailable())}, nil
		}
		// The previous owner is named as Kratos knows it; an account that is
		// gone leaves the name empty.
		previous := ""
		if before, err := a.accounts.Get(ctx, plan.OwnerIdentityID); err == nil {
			previous = accountName(*before)
		}
		audited(ctx, a.trail, audit.ProjectTransferred, planTarget(moved), map[string]string{
			"from_id": plan.OwnerIdentityID, "from": previous, "to_id": account.ID, "to": accountName(*account),
		})
	}
	answer, err := a.planAnswer(ctx, moved)
	if err != nil {
		return gen.TransferPlan503JSONResponse{UnavailableJSONResponse: gen.UnavailableJSONResponse(unavailable())}, nil
	}
	return gen.TransferPlan200JSONResponse(answer), nil
}

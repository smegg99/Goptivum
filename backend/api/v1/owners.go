// api/v1/owners.go

package v1

import (
	"context"
	"errors"
	"fmt"
	"strconv"

	"github.com/smegg99/s99ab/auth/kratosauth"
	"github.com/smegg99/s99logger"

	"github.com/smegg99/goptivum/backend/api/v1/gen"
	"github.com/smegg99/goptivum/backend/common/logger"
	"github.com/smegg99/goptivum/backend/common/messages"
	"github.com/smegg99/goptivum/backend/core/plans"
	"github.com/smegg99/goptivum/backend/identity"
)

// errNotYours is a change to a plan the caller does not own. Everyone reads
// every live plan; only its owner changes it.
var errNotYours = errors.New("api: the plan is another account's")

func planNotYours() gen.Error {
	return gen.Error{Code: "forbidden", Message: messages.KeyPlansNotYours}
}

// readable resolves a live plan from a path. Every signed-in account reads
// every one.
func (a *API) readable(ctx context.Context, raw string) (plans.Plan, error) {
	id, ok := planID(raw)
	if !ok {
		return plans.Plan{}, fmt.Errorf("%w: %q", plans.ErrNotFound, raw)
	}
	return a.plans.Get(ctx, id)
}

// editable resolves a live plan from a path that the caller owns; another
// owner's is errNotYours.
func (a *API) editable(ctx context.Context, raw string, caller string) (plans.Plan, error) {
	plan, err := a.readable(ctx, raw)
	if err != nil {
		return plans.Plan{}, err
	}
	if plan.OwnerIdentityID != caller {
		return plans.Plan{}, fmt.Errorf("%w: %d", errNotYours, plan.ID)
	}
	return plan, nil
}

// accountNames is every account's display name by identity id, read from
// Kratos. No Kratos, or one that does not answer, leaves every account
// unnamed rather than the answer empty, and says so in the log.
func accountNames(ctx context.Context, accounts *identity.Admin) map[string]string {
	names := map[string]string{}
	if accounts == nil {
		return names
	}
	listed, err := accounts.List(ctx)
	if err != nil {
		logger.Log.Warn(s99logger.NewEvent(logger.EventOwnersUnread, s99logger.Err(err)))
		return names
	}
	for _, account := range listed {
		names[account.ID] = accountName(account)
	}
	return names
}

// accountName is an account's name, or its username when it has none.
func accountName(account identity.Account) string {
	if account.Name != "" {
		return account.Name
	}
	return account.Username
}

// planView is what a plan's answer needs beyond its row: who asks, the
// owners' names and the names of the live plans they were forked from.
type planView struct {
	caller  string
	owners  map[string]string
	origins map[int64]string
}

// viewOf reads the names the answers about these plans carry. The caller's
// own name comes from its session; Kratos is asked only when someone else
// owns one of them, and an origin in Trash or gone is left unnamed.
func (a *API) viewOf(ctx context.Context, listed ...plans.Plan) (planView, error) {
	view := planView{owners: map[string]string{}, origins: map[int64]string{}}
	session, signed := kratosauth.FromContext(ctx)
	if signed {
		view.caller = session.ID
	}
	var origins []int64
	foreign := false
	for _, plan := range listed {
		if plan.OwnerIdentityID != view.caller {
			foreign = true
		}
		if plan.Origin.PlanID != 0 {
			origins = append(origins, plan.Origin.PlanID)
		}
	}
	if foreign {
		view.owners = accountNames(ctx, a.accounts)
	}
	if signed {
		if name := displayName(session); name != "" {
			view.owners[view.caller] = name
		}
	}
	found, err := a.plans.Lookup(ctx, origins)
	if err != nil {
		return planView{}, err
	}
	for id, origin := range found {
		if origin.DeletedAt.IsZero() {
			view.origins[id] = origin.Name
		}
	}
	return view, nil
}

func (a *API) planBody(plan plans.Plan, view planView) gen.Plan {
	owns := plan.OwnerIdentityID == view.caller
	body := gen.Plan{
		Id:                 strconv.FormatInt(plan.ID, 10),
		Name:               plan.Name,
		SchoolYear:         plan.SchoolYear,
		CreatedAt:          plan.CreatedAt,
		UpdatedAt:          plan.UpdatedAt,
		BackupsKeep:        plan.BackupsKeep,
		BackupsKeepDefault: a.plans.DefaultBackupsKeep(),
		ResultsKeep:        plan.ResultsKeep,
		ResultsKeepDefault: a.plans.DefaultResultsKeep(),
		Owner:              gen.Owner{Id: plan.OwnerIdentityID},
		CanEdit:            owns,
		CanDelete:          owns,
	}
	if name := view.owners[plan.OwnerIdentityID]; name != "" {
		body.Owner.Name = pointerTo(name)
	}
	if origin := plan.Origin; origin.PlanID != 0 {
		body.Origin = &gen.PlanOrigin{PlanId: strconv.FormatInt(origin.PlanID, 10), Version: origin.Version}
		if name, live := view.origins[origin.PlanID]; live {
			body.Origin.Name = pointerTo(name)
		}
	}
	return body
}

// planAnswer is one plan's body as its caller sees it.
func (a *API) planAnswer(ctx context.Context, plan plans.Plan) (gen.Plan, error) {
	view, err := a.viewOf(ctx, plan)
	if err != nil {
		return gen.Plan{}, err
	}
	return a.planBody(plan, view), nil
}

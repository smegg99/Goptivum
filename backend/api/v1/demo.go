// api/v1/demo.go

package v1

import (
	"context"
	"errors"

	"github.com/smegg99/s99logger"

	"github.com/smegg99/goptivum/backend/api/v1/gen"
	"github.com/smegg99/goptivum/backend/arrango"
	"github.com/smegg99/goptivum/backend/common/logger"
	"github.com/smegg99/goptivum/backend/common/messages"
	"github.com/smegg99/goptivum/backend/core/plans"
	"github.com/smegg99/goptivum/document"
	"github.com/smegg99/goptivum/solveclient"
)

// Demo serves the development-only route that stores Arrango's demo school as
// a new plan. The route is registered only when Options.DemoSchool is set.
type Demo struct {
	api    *API
	nodes  *arrango.Registry
	dialer *solveclient.Dialer
}

// NewDemo wires the demo route over the plan handlers and the paired nodes.
func NewDemo(api *API, nodes *arrango.Registry, dialer *solveclient.Dialer) *Demo {
	return &Demo{api: api, nodes: nodes, dialer: dialer}
}

// CreateDemoPlan asks the first paired, unrevoked node for the demo school and
// stores it as version 1 of a new plan.
func (d *Demo) CreateDemoPlan(ctx context.Context, request gen.CreateDemoPlanRequestObject) (gen.CreateDemoPlanResponseObject, error) {
	if d == nil || d.api == nil {
		return gen.CreateDemoPlan404JSONResponse{NotFoundJSONResponse: gen.NotFoundJSONResponse(notFound())}, nil
	}
	identity, ok := callerID(ctx)
	if !ok {
		return gen.CreateDemoPlan401JSONResponse{UnauthorizedJSONResponse: gen.UnauthorizedJSONResponse(unauthorized())}, nil
	}
	if request.Body == nil {
		return gen.CreateDemoPlan422JSONResponse{ValidationJSONResponse: gen.ValidationJSONResponse(validation("body", "required"))}, nil
	}
	name, year, err := plans.Clean(request.Body.Name, schoolYear(request.Body.SchoolYear))
	if refusal, ok := invalid(err); ok {
		return gen.CreateDemoPlan422JSONResponse{ValidationJSONResponse: gen.ValidationJSONResponse(refusal)}, nil
	}

	model, err := arrango.DemoSchool(ctx, d.nodes, d.dialer)
	if err != nil {
		// Said in the log, because the answer carries only the key: a node that
		// is not paired and one that did not answer need different fixes.
		logger.Log.Warn(s99logger.NewEvent(logger.EventDemoSchoolFailed,
			s99logger.String("error", err.Error()),
			s99logger.Bool("no_node", errors.Is(err, arrango.ErrNoNode))))
		return gen.CreateDemoPlan409JSONResponse(gen.Error{
			Code: "conflict", Message: messages.KeySolveNodeUnavailable,
		}), nil
	}
	encoded, err := document.Encode(document.New(model))
	if err != nil {
		return nil, err
	}

	plan, _, err := d.api.createWith(ctx, identity, name, year, encoded, sourceDemo)
	if refusal, ok := invalid(err); ok {
		return gen.CreateDemoPlan422JSONResponse{ValidationJSONResponse: gen.ValidationJSONResponse(refusal)}, nil
	}
	if err != nil {
		return gen.CreateDemoPlan503JSONResponse{UnavailableJSONResponse: gen.UnavailableJSONResponse(unavailable())}, nil
	}
	body, err := d.api.planAnswer(ctx, plan)
	if err != nil {
		return gen.CreateDemoPlan503JSONResponse{UnavailableJSONResponse: gen.UnavailableJSONResponse(unavailable())}, nil
	}
	return gen.CreateDemoPlan201JSONResponse(body), nil
}

// api/v1/rules.go

package v1

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/smegg99/s99logger"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	arrangorpcv1 "github.com/smegg99/arrango/backend/gen/arrangorpcv1"
	arrangov1 "github.com/smegg99/arrango/backend/gen/arrangov1"

	"github.com/smegg99/goptivum/backend/api/v1/gen"
	"github.com/smegg99/goptivum/backend/arrango"
	"github.com/smegg99/goptivum/backend/common/logger"
	"github.com/smegg99/goptivum/backend/common/messages"
	"github.com/smegg99/goptivum/solveclient"
)

// RulesDeadline bounds one catalogue call, dialling included: the node's child waits for memory without saying so.
const RulesDeadline = 20 * time.Second

// errRulesWaiting is the deadline's cause, which tells a slow node from one that failed.
var errRulesWaiting = errors.New("v1: the node did not list its rules in time")

// RuleNode asks a node for the solver's rule catalogue.
type RuleNode func(ctx context.Context) (*arrangorpcv1.RuleCatalogue, error)

// Rules serves the rule catalogue route.
type Rules struct {
	node     RuleNode
	deadline time.Duration
}

// NewRules wires the catalogue route; with a nil node every call answers that no node is available.
func NewRules(node RuleNode, deadline time.Duration) *Rules {
	return &Rules{node: node, deadline: deadline}
}

// GetRuleCatalogue lists the solver's rules and built-in profiles as the first paired node reports them.
func (r *Rules) GetRuleCatalogue(ctx context.Context, _ gen.GetRuleCatalogueRequestObject) (gen.GetRuleCatalogueResponseObject, error) {
	if r == nil || r.node == nil {
		return gen.GetRuleCatalogue409JSONResponse(gen.Error{Code: "conflict", Message: messages.KeySolveNodeUnavailable}), nil
	}
	askCtx, cancel := context.WithTimeoutCause(ctx, r.deadline, errRulesWaiting)
	defer cancel()
	catalogue, err := r.node(askCtx)

	var outdated *solveclient.ErrOutdated
	switch {
	case err == nil:
		body, err := ruleCatalogueBody(catalogue)
		if err != nil {
			return nil, err
		}
		return gen.GetRuleCatalogue200JSONResponse(body), nil
	case errors.Is(context.Cause(askCtx), errRulesWaiting), errors.Is(err, context.DeadlineExceeded), status.Code(err) == codes.DeadlineExceeded:
		logRuleCatalogueFailed(err)
		return gen.GetRuleCatalogue504JSONResponse(gen.Error{Code: "timeout", Message: messages.KeyRulesWaitingForSolver}), nil
	case errors.Is(err, arrango.ErrNoNode), errors.Is(err, solveclient.ErrUnreachable), errors.Is(err, solveclient.ErrRefused):
		logRuleCatalogueFailed(err)
		return gen.GetRuleCatalogue409JSONResponse(gen.Error{Code: "conflict", Message: messages.KeySolveNodeUnavailable}), nil
	case errors.As(err, &outdated):
		logRuleCatalogueFailed(err)
		return gen.GetRuleCatalogue409JSONResponse(gen.Error{Code: "conflict", Message: messages.KeyNodeOutdated,
			Details: &map[string]string{"Capability": outdated.Capability}}), nil
	default:
		return nil, err
	}
}

// logRuleCatalogueFailed says why, because the answer carries only the key.
func logRuleCatalogueFailed(err error) {
	logger.Log.Warn(s99logger.NewEvent(logger.EventRuleCatalogueFailed,
		s99logger.Err(err), s99logger.Bool("no_node", errors.Is(err, arrango.ErrNoNode))))
}

func ruleCatalogueBody(catalogue *arrangorpcv1.RuleCatalogue) (gen.RuleCatalogue, error) {
	body := gen.RuleCatalogue{
		Rules:    make([]gen.RuleDefinition, 0, len(catalogue.GetRules())),
		Profiles: make([]gen.RuleProfile, 0, len(catalogue.GetProfiles())),
	}
	for _, rule := range catalogue.GetRules() {
		mode, ok := ruleMode(rule.GetDefaultMode())
		if !ok {
			return gen.RuleCatalogue{}, fmt.Errorf("v1: the node's rule %s has default mode %v", rule.GetKey(), rule.GetDefaultMode())
		}
		kind := gen.RuleDefinitionParamKind(rule.GetParamKind())
		if !kind.Valid() || rule.GetDefaultWeight() <= 0 || (kind == gen.None && rule.GetDefaultParam() != 0) {
			return gen.RuleCatalogue{}, fmt.Errorf("v1: the node's rule %s has invalid defaults", rule.GetKey())
		}
		body.Rules = append(body.Rules, gen.RuleDefinition{
			Key: rule.GetKey(), DefaultMode: mode, DefaultWeight: rule.GetDefaultWeight(),
			DefaultParam: int(rule.GetDefaultParam()), ParamKind: kind, Hardable: rule.GetHardable(),
			ByYear: rule.GetByYear(), ByDivision: rule.GetByDivision(),
			BySubject: rule.GetBySubject(), ByTeacher: rule.GetByTeacher(),
		})
	}
	for _, profile := range catalogue.GetProfiles() {
		overrides := make([]gen.RuleOverride, 0, len(profile.GetOverrides()))
		for _, override := range profile.GetOverrides() {
			out, err := profileOverrideBody(profile.GetName(), override)
			if err != nil {
				return gen.RuleCatalogue{}, err
			}
			overrides = append(overrides, out)
		}
		body.Profiles = append(body.Profiles, gen.RuleProfile{Name: profile.GetName(), Overrides: overrides})
	}
	return body, nil
}

// profileOverrideBody leaves out what the override keeps: a default mode, a zero weight or param.
func profileOverrideBody(profile string, override *arrangov1.RuleOverride) (gen.RuleOverride, error) {
	if override.GetYearId() != 0 || override.GetDivisionId() != 0 || override.GetSubjectId() != 0 || override.GetTeacherId() != 0 {
		return gen.RuleOverride{}, fmt.Errorf("v1: the node's profile %s scopes its %s override", profile, override.GetRule())
	}
	out := gen.RuleOverride{Rule: override.GetRule()}
	if override.GetMode() != arrangov1.RuleMode_RULE_MODE_DEFAULT {
		mode, ok := ruleMode(override.GetMode())
		if !ok {
			return gen.RuleOverride{}, fmt.Errorf("v1: the node's profile %s sets %s to mode %v", profile, override.GetRule(), override.GetMode())
		}
		out.Mode = &mode
	}
	if weight := override.GetWeight(); weight != 0 {
		out.Weight = &weight
	}
	if param := override.GetParam(); param != 0 {
		out.Param = pointerTo(int(param))
	}
	return out, nil
}

func ruleMode(mode arrangov1.RuleMode) (gen.RuleMode, bool) {
	switch mode {
	case arrangov1.RuleMode_RULE_MODE_HARD:
		return gen.Hard, true
	case arrangov1.RuleMode_RULE_MODE_SOFT:
		return gen.Soft, true
	case arrangov1.RuleMode_RULE_MODE_OFF:
		return gen.Off, true
	}
	return "", false
}

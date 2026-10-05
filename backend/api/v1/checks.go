// api/v1/checks.go

package v1

import (
	"context"
	"errors"
	arrangov1 "github.com/smegg99/arrango/backend/gen/arrangov1"
	"net/http"
	"strconv"

	"github.com/smegg99/goptivum/backend/api/v1/gen"
	"github.com/smegg99/goptivum/backend/arrango"
	"github.com/smegg99/goptivum/backend/common/messages"
	"github.com/smegg99/goptivum/backend/core/checks"
	"github.com/smegg99/goptivum/backend/core/plans"
	goptivumv1 "github.com/smegg99/goptivum/document/gen/goptivumv1"
	"github.com/smegg99/goptivum/solveclient"
)

// Checks serves the check route over the plan handlers and the check service.
type Checks struct {
	api     *API
	service *checks.Service
}

// NewChecks wires the check route; with a nil service it answers 404.
func NewChecks(api *API, service *checks.Service) *Checks {
	return &Checks{api: api, service: service}
}

// CheckVersion asks the node what a solve's input checks say about one version of any live plan.
func (c *Checks) CheckVersion(ctx context.Context, request gen.CheckVersionRequestObject) (gen.CheckVersionResponseObject, error) {
	if c == nil || c.api == nil || c.service == nil {
		return gen.CheckVersion404JSONResponse{NotFoundJSONResponse: gen.NotFoundJSONResponse(notFound())}, nil
	}
	if _, ok := callerID(ctx); !ok {
		return gen.CheckVersion401JSONResponse{UnauthorizedJSONResponse: gen.UnauthorizedJSONResponse(unauthorized())}, nil
	}
	if request.Params.RulesVersion != nil && *request.Params.RulesVersion < 1 {
		return gen.CheckVersion400JSONResponse{BadRequestJSONResponse: gen.BadRequestJSONResponse(validation("rules_version", "invalid"))}, nil
	}
	plan, err := c.api.readable(ctx, request.PlanId)
	if err != nil {
		if errors.Is(err, plans.ErrNotFound) {
			return gen.CheckVersion404JSONResponse{NotFoundJSONResponse: gen.NotFoundJSONResponse(notFound())}, nil
		}
		return gen.CheckVersion503JSONResponse{UnavailableJSONResponse: gen.UnavailableJSONResponse(unavailable())}, nil
	}
	version, err := c.api.plans.Version(ctx, plan.ID, request.Version)
	if err != nil {
		if errors.Is(err, plans.ErrNoVersion) || errors.Is(err, plans.ErrNotFound) {
			return gen.CheckVersion404JSONResponse{NotFoundJSONResponse: gen.NotFoundJSONResponse(notFound())}, nil
		}
		return gen.CheckVersion503JSONResponse{UnavailableJSONResponse: gen.UnavailableJSONResponse(unavailable())}, nil
	}

	subject := checks.Subject{PlanID: plan.ID, Version: version.Version, ObjectKey: version.ObjectKey}
	if request.Params.RulesVersion != nil {
		subject.Rules, err = c.comparisonRules(ctx, plan.ID, int64(*request.Params.RulesVersion))
		if errors.Is(err, plans.ErrNoVersion) || errors.Is(err, plans.ErrNotFound) {
			return gen.CheckVersion404JSONResponse{NotFoundJSONResponse: gen.NotFoundJSONResponse(notFound())}, nil
		}
		if err != nil {
			return gen.CheckVersion503JSONResponse{UnavailableJSONResponse: gen.UnavailableJSONResponse(unavailable())}, nil
		}
	}
	report, err := c.run(ctx, subject)
	if err == nil {
		return gen.CheckVersion200JSONResponse(checkBody(subject, report)), nil
	}
	refusal, status, err := checkRefusal(err)
	switch status {
	case http.StatusConflict:
		return gen.CheckVersion409JSONResponse(refusal), nil
	case http.StatusGatewayTimeout:
		return gen.CheckVersion504JSONResponse(refusal), nil
	case http.StatusServiceUnavailable:
		return gen.CheckVersion503JSONResponse{UnavailableJSONResponse: gen.UnavailableJSONResponse(refusal)}, nil
	}
	return nil, err
}

// CheckResult asks the node what a solve's input checks say about one result's own document.
func (c *Checks) CheckResult(ctx context.Context, request gen.CheckResultRequestObject) (gen.CheckResultResponseObject, error) {
	if c == nil || c.api == nil || c.service == nil {
		return gen.CheckResult404JSONResponse{NotFoundJSONResponse: gen.NotFoundJSONResponse(notFound())}, nil
	}
	if _, ok := callerID(ctx); !ok {
		return gen.CheckResult401JSONResponse{UnauthorizedJSONResponse: gen.UnauthorizedJSONResponse(unauthorized())}, nil
	}
	if request.Params.RulesVersion != nil && *request.Params.RulesVersion < 1 {
		return gen.CheckResult400JSONResponse{BadRequestJSONResponse: gen.BadRequestJSONResponse(validation("rules_version", "invalid"))}, nil
	}
	plan, result, err := c.api.readableResult(ctx, request.PlanId, request.ResultId)
	if err != nil {
		if missing(err) {
			return gen.CheckResult404JSONResponse{NotFoundJSONResponse: gen.NotFoundJSONResponse(notFound())}, nil
		}
		return gen.CheckResult503JSONResponse{UnavailableJSONResponse: gen.UnavailableJSONResponse(unavailable())}, nil
	}
	subject := checks.Subject{PlanID: plan.ID, Version: result.SourceVersion, ResultID: result.ID, ObjectKey: result.ObjectKey}
	if request.Params.RulesVersion != nil {
		subject.Rules, err = c.comparisonRules(ctx, plan.ID, int64(*request.Params.RulesVersion))
		if errors.Is(err, plans.ErrNoVersion) || errors.Is(err, plans.ErrNotFound) {
			return gen.CheckResult404JSONResponse{NotFoundJSONResponse: gen.NotFoundJSONResponse(notFound())}, nil
		}
		if err != nil {
			return gen.CheckResult503JSONResponse{UnavailableJSONResponse: gen.UnavailableJSONResponse(unavailable())}, nil
		}
	} else if profile := result.RuleProfile; profile != nil {
		// Judged the way its solve was: by the rules the solve request carried.
		if subject.Rules, err = arrango.DecodeRules(profile.Rules); err != nil {
			return nil, err
		}
	}
	report, err := c.run(ctx, subject)
	if err == nil {
		return gen.CheckResult200JSONResponse(checkBody(subject, report)), nil
	}
	refusal, status, err := checkRefusal(err)
	switch status {
	case http.StatusConflict:
		return gen.CheckResult409JSONResponse(refusal), nil
	case http.StatusGatewayTimeout:
		return gen.CheckResult504JSONResponse(refusal), nil
	case http.StatusServiceUnavailable:
		return gen.CheckResult503JSONResponse{UnavailableJSONResponse: gen.UnavailableJSONResponse(refusal)}, nil
	}
	return nil, err
}

// comparisonRules loads one captured main-file version's rules for both comparison sides.
func (c *Checks) comparisonRules(ctx context.Context, planID, versionNumber int64) (*arrangov1.RuleConfig, error) {
	version, err := c.api.plans.Version(ctx, planID, versionNumber)
	if err != nil {
		return nil, err
	}
	doc, err := c.api.documents.Load(ctx, version.ObjectKey)
	if err != nil {
		return nil, err
	}
	rules := doc.GetModel().GetRuleConfig()
	if rules == nil {
		rules = &arrangov1.RuleConfig{}
	}
	return rules, nil
}

// run checks the stored document a subject names.
func (c *Checks) run(ctx context.Context, subject checks.Subject) (checks.Report, error) {
	return c.service.Check(ctx, subject, func(ctx context.Context) (*goptivumv1.Document, error) {
		return c.api.documents.Load(ctx, subject.ObjectKey)
	})
}

// checkRefusal turns a check that did not run into its envelope and status;
// status 0 means err is not a refusal and is returned as it is.
func checkRefusal(err error) (gen.Error, int, error) {
	var outdated *solveclient.ErrOutdated
	switch {
	case errors.Is(err, arrango.ErrNoNode), errors.Is(err, solveclient.ErrUnreachable), errors.Is(err, solveclient.ErrRefused):
		return gen.Error{Code: "conflict", Message: messages.KeySolveNodeUnavailable}, http.StatusConflict, nil
	case errors.As(err, &outdated):
		return gen.Error{Code: "conflict", Message: messages.KeyNodeOutdated,
			Details: &map[string]string{"Capability": outdated.Capability}}, http.StatusConflict, nil
	case errors.Is(err, checks.ErrSuperseded):
		return gen.Error{Code: "conflict", Message: messages.KeyCheckSuperseded}, http.StatusConflict, nil
	case errors.Is(err, checks.ErrWaiting):
		return gen.Error{Code: "timeout", Message: messages.KeyCheckWaitingForSolver}, http.StatusGatewayTimeout, nil
	case errors.Is(err, checks.ErrDocument):
		// A row pointing at nothing is a repairable inconsistency, as in DownloadVersion.
		return unavailable(), http.StatusServiceUnavailable, nil
	}
	return gen.Error{}, 0, err
}

func checkBody(subject checks.Subject, report checks.Report) gen.Check {
	body := gen.Check{
		PlanId: strconv.FormatInt(subject.PlanID, 10), Version: subject.Version,
		Errors: report.Errors, Warnings: report.Warnings, Notes: report.Notes, Ignored: report.Ignored,
		Findings: make([]gen.CheckFinding, 0, len(report.Findings)),
	}
	if subject.ResultID != 0 {
		body.ResultId = pointerTo(strconv.FormatInt(subject.ResultID, 10))
	}
	for _, finding := range report.Findings {
		body.Findings = append(body.Findings, findingBody(finding))
	}
	if report.Score != nil {
		body.Score = scoreBody(*report.Score)
	}
	return body
}

func findingBody(finding checks.Finding) gen.CheckFinding {
	out := gen.CheckFinding{
		Part: gen.CheckFindingPart(finding.Part), Kind: finding.Kind, Severity: gen.CheckFindingSeverity(finding.Severity), Ignored: finding.Ignored,
		Needed: intOf(finding.Needed), Available: intOf(finding.Available), Count: intOf(finding.Count),
	}
	if len(finding.Entities) > 0 {
		entities := make([]gen.EntityRef, 0, len(finding.Entities))
		for _, entity := range finding.Entities {
			entities = append(entities, gen.EntityRef{Kind: gen.EntityRefKind(entity.Kind), Id: int(entity.ID)})
		}
		out.Entities = &entities
	}
	if len(finding.Lessons) > 0 {
		lessons := make([]gen.CheckLesson, 0, len(finding.Lessons))
		for _, lesson := range finding.Lessons {
			body := gen.CheckLesson{LessonId: int(lesson.ID)}
			if lesson.Placed {
				body.DayId, body.StartPeriod = pointerTo(int(lesson.DayID)), pointerTo(int(lesson.StartPeriod))
				if lesson.RoomID != 0 {
					body.RoomId = pointerTo(int(lesson.RoomID))
				}
			}
			if lesson.Duration != 0 {
				body.Duration = pointerTo(int(lesson.Duration))
			}
			lessons = append(lessons, body)
		}
		out.Lessons = &lessons
	}
	if len(finding.Spans) > 0 {
		spans := make([]gen.CheckSpan, 0, len(finding.Spans))
		for _, span := range finding.Spans {
			spans = append(spans, gen.CheckSpan{
				DayId: int(span.DayID), StartPeriod: int(span.StartPeriod), PeriodSpan: int(span.PeriodSpan),
			})
		}
		out.Spans = &spans
	}
	return out
}

func scoreBody(score checks.Score) *gen.CheckScore {
	out := &gen.CheckScore{
		Rating: score.Rating, Quality: score.Quality, StudentsQuality: score.StudentsQuality, TeachersQuality: score.TeachersQuality,
		Metrics:  make([]gen.CheckMetric, 0, len(score.Metrics)),
		Classes:  entityScoreBodies(score.Classes),
		Teachers: entityScoreBodies(score.Teachers),
	}
	for _, metric := range score.Metrics {
		group := gen.Students
		if metric.Teachers {
			group = gen.Teachers
		}
		out.Metrics = append(out.Metrics, gen.CheckMetric{
			Key: metric.Key, Group: group, Applicable: metric.Applicable,
			Rate: metric.Rate, Subscore: metric.Subscore, Count: metric.Count,
		})
	}
	return out
}

func entityScoreBodies(scores []checks.EntityScore) []gen.CheckEntityScore {
	out := make([]gen.CheckEntityScore, 0, len(scores))
	for _, score := range scores {
		items := make([]gen.CheckScoreItem, 0, len(score.Items))
		for _, item := range score.Items {
			items = append(items, gen.CheckScoreItem{Category: item.Category, Count: int(item.Count)})
		}
		out = append(out, gen.CheckEntityScore{Id: int(score.ID), Rating: score.Rating, Quality: score.Quality, Items: items})
	}
	return out
}

func intOf(value *uint32) *int {
	if value == nil {
		return nil
	}
	return pointerTo(int(*value))
}

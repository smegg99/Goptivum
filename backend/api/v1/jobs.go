// api/v1/jobs.go

package v1

import (
	"context"
	"errors"
	"io"
	"math"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/smegg99/s99ab/api/apierr"
	"github.com/smegg99/s99ab/auth/kratosauth"
	"github.com/smegg99/s99ab/net/sse"

	arrangorpcv1 "github.com/smegg99/arrango/backend/gen/arrangorpcv1"
	arrangov1 "github.com/smegg99/arrango/backend/gen/arrangov1"

	"github.com/smegg99/goptivum/backend/api/v1/gen"
	"github.com/smegg99/goptivum/backend/arrango"
	"github.com/smegg99/goptivum/backend/common/messages"
	"github.com/smegg99/goptivum/backend/core/documents"
	"github.com/smegg99/goptivum/backend/core/plans"
	"github.com/smegg99/goptivum/backend/core/profiles"
	goptivumv1 "github.com/smegg99/goptivum/document/gen/goptivumv1"
	"github.com/smegg99/goptivum/solveclient"
)

// Jobs implements the solving half of the contract.
//
// Every signed-in account reads every live plan's jobs, previews and events;
// starting and cancelling a solve is the plan owner's, as every other change
// to a plan is.
type Jobs struct {
	plans     *plans.Repository
	documents *documents.Store
	runner    *arrango.Runner
	jobs      *arrango.Jobs
	profiles  *profiles.Repository
}

// NewJobs wires the solving handlers. A nil runner means no node can be
// reached, and every route here answers 404 rather than pretending; with nil
// profiles a solve that names a school profile finds none.
func NewJobs(index *plans.Repository, store *documents.Store,
	runner *arrango.Runner, jobs *arrango.Jobs, ruleProfiles *profiles.Repository) *Jobs {
	return &Jobs{plans: index, documents: store, runner: runner, jobs: jobs, profiles: ruleProfiles}
}

// ListJobs is one plan's solves.
func (j *Jobs) ListJobs(ctx context.Context, request gen.ListJobsRequestObject) (gen.ListJobsResponseObject, error) {
	if j.jobs == nil {
		return gen.ListJobs404JSONResponse{NotFoundJSONResponse: gen.NotFoundJSONResponse(notFound())}, nil
	}
	plan, ok := j.readablePlan(ctx, request.PlanId)
	if !ok {
		return gen.ListJobs404JSONResponse{NotFoundJSONResponse: gen.NotFoundJSONResponse(notFound())}, nil
	}
	rows, err := j.jobs.ForPlan(ctx, plan.ID)
	if err != nil {
		return nil, err
	}
	caller, _ := callerID(ctx)
	items := []gen.Job{}
	for _, row := range rows {
		items = append(items, toJob(row, plan, caller))
	}
	return gen.ListJobs200JSONResponse{Items: items}, nil
}

// ListAllJobs is one page of the solves of every live plan of the school.
func (j *Jobs) ListAllJobs(ctx context.Context, request gen.ListAllJobsRequestObject) (gen.ListAllJobsResponseObject, error) {
	caller, ok := callerID(ctx)
	if !ok {
		return gen.ListAllJobs401JSONResponse{UnauthorizedJSONResponse: gen.UnauthorizedJSONResponse(unauthorized())}, nil
	}
	var statuses []string
	if request.Params.Status != nil {
		for _, status := range *request.Params.Status {
			// The binding does not check enums, so a value no job can have is
			// refused rather than answered with an empty page.
			if !status.Valid() {
				return gen.ListAllJobs400JSONResponse{BadRequestJSONResponse: gen.BadRequestJSONResponse(
					gen.Error{Code: "bad_request", Message: messages.KeyErrorBadRequest})}, nil
			}
			statuses = append(statuses, string(status))
		}
	}
	page, limit := pageOf(request.Params.Page, request.Params.Limit)
	if j.plans == nil || j.jobs == nil {
		return gen.ListAllJobs200JSONResponse{Items: []gen.Job{}, Page: page, Limit: limit}, nil
	}
	rows, total, err := j.jobs.List(ctx, arrango.Filter{
		Statuses: statuses, Offset: (page - 1) * limit, Limit: limit,
	})
	if err != nil {
		return gen.ListAllJobs503JSONResponse{UnavailableJSONResponse: gen.UnavailableJSONResponse(unavailable())}, nil
	}
	found, err := j.plans.Lookup(ctx, planIDsOf(rows))
	if err != nil {
		return gen.ListAllJobs503JSONResponse{UnavailableJSONResponse: gen.UnavailableJSONResponse(unavailable())}, nil
	}
	items := make([]gen.Job, 0, len(rows))
	for _, row := range rows {
		items = append(items, toJob(row, found[row.PlanID], caller))
	}
	return gen.ListAllJobs200JSONResponse{Items: items, Page: page, Limit: limit, Total: total}, nil
}

// CreateJob dispatches a solve and answers at once.
func (j *Jobs) CreateJob(ctx context.Context, request gen.CreateJobRequestObject) (gen.CreateJobResponseObject, error) {
	if j.runner == nil {
		return gen.CreateJob404JSONResponse{NotFoundJSONResponse: gen.NotFoundJSONResponse(notFound())}, nil
	}
	plan, ok := j.readablePlan(ctx, request.PlanId)
	if !ok {
		return gen.CreateJob404JSONResponse{NotFoundJSONResponse: gen.NotFoundJSONResponse(notFound())}, nil
	}
	caller, _ := callerID(ctx)
	if plan.OwnerIdentityID != caller {
		return gen.CreateJob403JSONResponse{ForbiddenJSONResponse: gen.ForbiddenJSONResponse(planNotYours())}, nil
	}
	body := request.Body
	if body == nil {
		return gen.CreateJob422JSONResponse(validation("body", "required")), nil
	}
	var nodeID int64
	if body.NodeId != nil {
		id, err := strconv.ParseInt(*body.NodeId, 10, 64)
		if err != nil || id <= 0 {
			return gen.CreateJob404JSONResponse{NotFoundJSONResponse: gen.NotFoundJSONResponse(notFound())}, nil
		}
		nodeID = id
	}

	switch {
	case body.TotalTimeSeconds != nil && (math.IsNaN(float64(*body.TotalTimeSeconds)) || math.IsInf(float64(*body.TotalTimeSeconds), 0) || *body.TotalTimeSeconds < 1 || *body.TotalTimeSeconds > 86400):
		return gen.CreateJob422JSONResponse(validation("total_time_seconds", "invalid")), nil
	case body.StallStopSeconds != nil && (math.IsNaN(*body.StallStopSeconds) || math.IsInf(*body.StallStopSeconds, 0) || *body.StallStopSeconds < 0 || *body.StallStopSeconds > 86400):
		return gen.CreateJob422JSONResponse(validation("stall_stop_seconds", "invalid")), nil
	case body.PlanVersion == nil && body.ResultId == nil:
		return gen.CreateJob422JSONResponse(validation("plan_version", "required")), nil
	case body.PlanVersion != nil && body.ResultId != nil:
		return gen.CreateJob422JSONResponse(validation("result_id", "exclusive")), nil
	case body.BuiltinRuleProfile != nil && body.SchoolRuleProfileId != nil:
		return gen.CreateJob422JSONResponse(validation("school_rule_profile_id", "exclusive")), nil
	case body.BuiltinRuleProfile != nil && !builtinProfilePattern.MatchString(*body.BuiltinRuleProfile):
		return gen.CreateJob422JSONResponse(validation("builtin_rule_profile", "invalid")), nil
	}
	profile, err := j.ruleProfile(ctx, body)
	switch {
	case errors.Is(err, profiles.ErrNotFound):
		return gen.CreateJob404JSONResponse{NotFoundJSONResponse: gen.NotFoundJSONResponse(notFound())}, nil
	case err != nil:
		return gen.CreateJob503JSONResponse{UnavailableJSONResponse: gen.UnavailableJSONResponse(unavailable())}, nil
	}

	file, err := j.file(ctx, plan.ID, body)
	if missing(err) {
		return gen.CreateJob404JSONResponse{NotFoundJSONResponse: gen.NotFoundJSONResponse(notFound())}, nil
	}
	if err != nil {
		return nil, err
	}
	config := solverConfig(body)
	model := file.doc.GetModel()
	if !config.GetIgnorePreviousPlacements() {
		solveclient.SetBaseline(model, file.doc.GetSchedule())
	}

	job, err := j.runner.Start(ctx, arrango.Order{
		PlanID: plan.ID, Version: file.version, SourceResultID: file.result, NodeID: nodeID,
		StartedBy: starter(ctx), Profile: profile,
		Request: &arrangorpcv1.SolveRequest{Model: model, Config: config},
	})
	switch {
	case err == nil:
	case errors.Is(err, arrango.ErrNoNode) && nodeID != 0:
		return gen.CreateJob404JSONResponse{NotFoundJSONResponse: gen.NotFoundJSONResponse(notFound())}, nil
	case errors.Is(err, arrango.ErrNoNode), errors.Is(err, arrango.ErrRevoked):
		return nodeUnavailable("none"), nil
	case isTooLong(err):
		envelope, _ := refused(err)
		return gen.CreateJob409JSONResponse(envelope), nil
	case errors.Is(err, arrango.ErrNoCapacity):
		return nodeUnavailable("busy"), nil
	case errors.Is(err, solveclient.ErrUnreachable):
		return nodeUnavailable("unreachable"), nil
	default:
		// The node checked the timetable before solving and refused it. The
		// kinds travel beside the count, so a client can say which problems as
		// well as how many.
		var invalid *solveclient.ErrModelInvalid
		if errors.As(err, &invalid) {
			envelope, _ := refused(err)
			details := map[string]string{}
			if envelope.Details != nil {
				details = *envelope.Details
			}
			details["conflicts"] = strings.Join(invalid.Kinds(), ",")
			envelope.Details = &details
			return gen.CreateJob422JSONResponse(envelope), nil
		}
		var baseline *solveclient.ErrIncompleteBaseline
		if errors.As(err, &baseline) {
			return gen.CreateJob422JSONResponse(
				refusedOr(err, validation("ignore_previous_placements", "incomplete_baseline"))), nil
		}
		if envelope, ok := refused(err); ok {
			return gen.CreateJob409JSONResponse(envelope), nil
		}
		return nil, err
	}
	return gen.CreateJob202JSONResponse(toJob(job, plan, caller)), nil
}

// builtinProfilePattern is the contract's shape of a built-in profile name; whether the solver has it is the node's answer.
var builtinProfilePattern = regexp.MustCompile(`^[a-z][a-z0-9_]{0,63}$`)

// ruleProfile is the profile a createJob body names, nil for the plan's own
// rules: a built-in by name, or a school profile's overrides as they are now.
// An unknown school profile is profiles.ErrNotFound.
func (j *Jobs) ruleProfile(ctx context.Context, body *gen.JobCreate) (*solveclient.Profile, error) {
	switch {
	case body.BuiltinRuleProfile != nil:
		name := *body.BuiltinRuleProfile
		return &solveclient.Profile{Name: name, Rules: &arrangov1.RuleConfig{Profile: name}}, nil
	case body.SchoolRuleProfileId != nil:
		id, ok := profileID(*body.SchoolRuleProfileId)
		if !ok || j.profiles == nil {
			return nil, profiles.ErrNotFound
		}
		stored, err := j.profiles.Get(ctx, id)
		if err != nil {
			return nil, err
		}
		return &solveclient.Profile{Name: stored.Name, ID: stored.ID, Rules: stored.Rules()}, nil
	}
	return nil, nil
}

// nodeUnavailable is the request that is fine and that no node can take, with
// why: none, busy or unreachable.
func nodeUnavailable(cause string) gen.CreateJob409JSONResponse {
	return gen.CreateJob409JSONResponse(gen.Error{
		Code: "conflict", Message: messages.KeySolveNodeUnavailable,
		Details: &map[string]string{"cause": cause},
	})
}

// starter is the signed-in account as a job records it: its name, or its
// username when it has none.
func starter(ctx context.Context) arrango.Starter {
	identity, ok := kratosauth.FromContext(ctx)
	if !ok {
		return arrango.Starter{}
	}
	return arrango.Starter{ID: identity.ID, Name: displayName(identity)}
}

// GetJob is the authority on what a solve did.
func (j *Jobs) GetJob(ctx context.Context, request gen.GetJobRequestObject) (gen.GetJobResponseObject, error) {
	job, plan, ok, err := j.readableJob(ctx, request.JobId)
	if err != nil {
		return nil, err
	}
	if !ok {
		return gen.GetJob404JSONResponse{NotFoundJSONResponse: gen.NotFoundJSONResponse(notFound())}, nil
	}
	caller, _ := callerID(ctx)
	return gen.GetJob200JSONResponse(toJob(job, plan, caller)), nil
}

// GetJobPreview is the best timetable a running solve sent.
func (j *Jobs) GetJobPreview(ctx context.Context, request gen.GetJobPreviewRequestObject) (gen.GetJobPreviewResponseObject, error) {
	job, _, ok, err := j.readableJob(ctx, request.JobId)
	if err != nil {
		return nil, err
	}
	if !ok || j.runner == nil {
		return gen.GetJobPreview404JSONResponse{NotFoundJSONResponse: gen.NotFoundJSONResponse(notFound())}, nil
	}
	preview, live := j.runner.Preview(job.ID)
	if !live {
		// Finished: the timetable, when one was kept, is the job's result.
		return gen.GetJobPreview404JSONResponse{NotFoundJSONResponse: gen.NotFoundJSONResponse(notFound())}, nil
	}
	body := gen.JobPreview{
		JobId: strconv.FormatInt(job.ID, 10), PlanVersion: preview.PlanVersion,
		Received: int(preview.Received), Quality: preview.Quality,
	}
	if job.SourceResultID != nil {
		body.SourceResultId = pointerTo(strconv.FormatInt(*job.SourceResultID, 10))
	}
	if preview.Verdict != "" {
		body.Verdict = pointerTo(preview.Verdict)
	}
	if preview.Snapshot != nil {
		lessons := make([]gen.PreviewLesson, 0, len(preview.Snapshot.GetLessons()))
		for _, lesson := range preview.Snapshot.GetLessons() {
			placement := lesson.GetPlacement()
			item := gen.PreviewLesson{
				LessonId: int64(lesson.GetLessonId()), DayId: int64(placement.GetDayId()),
				StartPeriod: int64(placement.GetStartPeriod()),
			}
			if room := placement.GetRoomId(); room != 0 {
				item.RoomId = pointerTo(int64(room))
			}
			lessons = append(lessons, item)
		}
		body.Lessons = &lessons
	}
	return gen.GetJobPreview200JSONResponse(body), nil
}

// CancelJob stops a running solve of the caller's own plan.
func (j *Jobs) CancelJob(ctx context.Context, request gen.CancelJobRequestObject) (gen.CancelJobResponseObject, error) {
	job, plan, ok, err := j.readableJob(ctx, request.JobId)
	if err != nil {
		return nil, err
	}
	if !ok {
		return gen.CancelJob404JSONResponse{NotFoundJSONResponse: gen.NotFoundJSONResponse(notFound())}, nil
	}
	if caller, _ := callerID(ctx); plan.OwnerIdentityID != caller {
		return gen.CancelJob403JSONResponse{ForbiddenJSONResponse: gen.ForbiddenJSONResponse(planNotYours())}, nil
	}
	if j.runner != nil {
		// A job that already finished is not an error: the caller wanted it
		// stopped and it is stopped.
		j.runner.Cancel(job.ID)
	}
	return gen.CancelJob202Response{}, nil
}

// sseHeartbeat keeps an idle stream open through proxies that cut a silent
// connection, as S99AB's handler does.
const sseHeartbeat = 20 * time.Second

// watch is the event stream, hand-wired because it writes text/event-stream
// rather than a JSON body.
//
// It is a nudge and not the truth: a slow reader is dropped a frame rather
// than allowed to stall the solve. It always ends with a terminal frame: when
// the job has ended, or ends while the client subscribes, or its terminal frame
// was dropped, the frame is written from the job row.
func (j *Jobs) watch(c *gin.Context) {
	ctx := c.Request.Context()
	job, _, ok, err := j.readableJob(ctx, c.Param("jobId"))
	if err != nil {
		apierr.Respond(c, err)
		return
	}
	if !ok {
		apierr.Respond(c, apierr.New(apierr.NotFound, "error.notFound"))
		return
	}

	// Server.WriteTimeout arms a write deadline when the request headers are
	// read, so without this every stream dies at write_timeout_seconds, which
	// defaults to 30 and a solve takes minutes. Clearing it is safe here
	// because the stream ends on its own terminal frame.
	if err := http.NewResponseController(c.Writer).SetWriteDeadline(time.Time{}); err != nil {
		// The writer beneath does not carry a deadline, so there is none to
		// clear and the stream is not on a clock.
		_ = err
	}

	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	c.Header("X-Accel-Buffering", "no")
	c.Status(http.StatusOK)
	if _, err := io.WriteString(c.Writer, ":ok\n\n"); err != nil {
		return
	}
	c.Writer.Flush()

	var events <-chan sse.Event
	if j.runner != nil {
		if broker, live := j.runner.Broker(job.ID); live {
			subscribed, cancel := broker.Subscribe()
			defer cancel()
			events = subscribed
		}
	}
	if events != nil {
		// Read after subscribing: a job that ended in between has published
		// its terminal frame to nobody.
		if current, err := j.jobs.Get(ctx, job.ID); err == nil && terminal(current.Status) {
			events = nil
		}
	}
	if events != nil && !j.relay(c, events) {
		return
	}

	// Already finished, or the stream closed before its terminal frame reached
	// this client. One terminal frame from the row, so the client settles.
	final, err := j.jobs.Get(context.WithoutCancel(ctx), job.ID)
	if err != nil {
		final = job
	}
	_, _ = sse.WriteEvent(c.Writer, sse.Event{Type: "update", Data: arrango.Update{
		JobID: strconv.FormatInt(final.ID, 10), JobStatus: final.Status, Message: final.Detail, Terminal: true,
	}})
	c.Writer.Flush()
}

// relay writes the broker's events until the terminal one. It reports whether
// the stream still needs a terminal frame: false once one was written or the
// client went away.
func (j *Jobs) relay(c *gin.Context, events <-chan sse.Event) bool {
	beat := time.NewTicker(sseHeartbeat)
	defer beat.Stop()
	for {
		select {
		case <-c.Request.Context().Done():
			return false
		case <-beat.C:
			if _, err := io.WriteString(c.Writer, ":\n\n"); err != nil {
				return false
			}
			c.Writer.Flush()
		case event, open := <-events:
			if !open {
				return true
			}
			if _, err := sse.WriteEvent(c.Writer, event); err != nil {
				return false
			}
			c.Writer.Flush()
			if update, ok := event.Data.(arrango.Update); ok && update.Terminal {
				return false
			}
		}
	}
}

// terminal reports whether a job status is one of the three final ones.
func terminal(status string) bool {
	switch status {
	case arrango.StatusDone, arrango.StatusFailed, arrango.StatusCancelled:
		return true
	}
	return false
}

// readablePlan resolves a live plan for a signed-in caller, whoever owns it.
func (j *Jobs) readablePlan(ctx context.Context, raw string) (plans.Plan, bool) {
	if j.plans == nil {
		return plans.Plan{}, false
	}
	if _, ok := callerID(ctx); !ok {
		return plans.Plan{}, false
	}
	id, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return plans.Plan{}, false
	}
	plan, err := j.plans.Get(ctx, id)
	if err != nil {
		return plans.Plan{}, false
	}
	return plan, true
}

// readableJob resolves a job of a live plan for a signed-in caller.
func (j *Jobs) readableJob(ctx context.Context, raw string) (arrango.Job, plans.Plan, bool, error) {
	if j.jobs == nil {
		return arrango.Job{}, plans.Plan{}, false, nil
	}
	id, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return arrango.Job{}, plans.Plan{}, false, nil
	}
	job, err := j.jobs.Get(ctx, id)
	if errors.Is(err, arrango.ErrNoJob) {
		return arrango.Job{}, plans.Plan{}, false, nil
	}
	if err != nil {
		return arrango.Job{}, plans.Plan{}, false, err
	}
	plan, ok := j.readablePlan(ctx, strconv.FormatInt(job.PlanID, 10))
	if !ok {
		return arrango.Job{}, plans.Plan{}, false, nil
	}
	return job, plan, true, nil
}

// solveFile is the file a solve reads: a version, or a result (0 for none)
// with the version whose school it holds.
type solveFile struct {
	version int64
	result  int64
	doc     *goptivumv1.Document
}

// file loads the version or the live result a createJob body names; exactly
// one is set. An id that cannot be one is plans.ErrNoResult.
func (j *Jobs) file(ctx context.Context, planID int64, body *gen.JobCreate) (solveFile, error) {
	var file solveFile
	key := ""
	if body.ResultId != nil {
		id, err := strconv.ParseInt(*body.ResultId, 10, 64)
		if err != nil || id <= 0 {
			return solveFile{}, plans.ErrNoResult
		}
		result, err := j.plans.Result(ctx, planID, id)
		if err != nil {
			return solveFile{}, err
		}
		file.version, file.result, key = result.SourceVersion, result.ID, result.ObjectKey
	} else {
		stored, err := j.plans.Version(ctx, planID, *body.PlanVersion)
		if err != nil {
			return solveFile{}, err
		}
		file.version, key = stored.Version, stored.ObjectKey
	}
	doc, err := j.documents.Load(ctx, key)
	if err != nil {
		return solveFile{}, err
	}
	file.doc = doc
	return file, nil
}

// solverConfig maps the handful of knobs the contract exposes.
//
// Deliberately a handful: SolverConfig has twenty-one fields and a school does
// not have an opinion about most of them. num_workers is a hint here and is
// clamped before dispatch whatever arrives.
func solverConfig(body *gen.JobCreate) *arrangorpcv1.SolverConfig {
	config := &arrangorpcv1.SolverConfig{}
	if body.TotalTimeSeconds != nil {
		config.TotalTimeSeconds = float64(*body.TotalTimeSeconds)
	}
	if body.StallStopSeconds != nil {
		config.StallStopSeconds = *body.StallStopSeconds
	}
	if body.NumWorkers != nil && *body.NumWorkers > 0 {
		config.NumWorkers = uint32(*body.NumWorkers)
	}
	if body.IgnorePreviousPlacements != nil {
		config.IgnorePreviousPlacements = *body.IgnorePreviousPlacements
	}
	return config
}

// refusedOr uses the domain's own key when it has one, and the given envelope
// otherwise.
func refusedOr(err error, fallback gen.Error) gen.Error {
	if envelope, ok := refused(err); ok {
		return envelope
	}
	return fallback
}

// planIDsOf is the plan of each job, as Lookup takes them.
func planIDsOf(rows []arrango.Job) []int64 {
	ids := make([]int64, 0, len(rows))
	for _, row := range rows {
		ids = append(ids, row.PlanID)
	}
	return ids
}

// toJob is a job as caller sees it. plan is the job's plan, zero when it is
// gone; only its owner cancels the solve, and only while the plan is live.
func toJob(job arrango.Job, plan plans.Plan, caller string) gen.Job {
	out := gen.Job{
		Id:               strconv.FormatInt(job.ID, 10),
		PlanId:           strconv.FormatInt(job.PlanID, 10),
		PlanName:         plan.Name,
		PlanVersion:      job.PlanVersion,
		NodeId:           strconv.FormatInt(job.NodeID, 10),
		NodeLabel:        job.NodeLabel,
		Status:           gen.JobStatus(job.Status),
		NumWorkers:       int(job.NumWorkers),
		TimeLimitSeconds: job.TimeLimitSeconds, Improve: job.Improve, StallStopSeconds: job.StallStopSeconds,
		StartedBy: gen.Starter{Id: job.StartedBy.ID, Name: job.StartedBy.Name},
		CreatedAt: job.CreatedAt,
		Quality:   job.Quality,
		CanCancel: plan.ID != 0 && plan.DeletedAt.IsZero() && caller != "" &&
			plan.OwnerIdentityID == caller,
	}
	if profile := job.Profile; profile != nil {
		out.RuleProfile = solveRuleProfile(profile.Name, profile.ID)
	}
	if job.SourceResultID != nil {
		out.SourceResultId = pointerTo(strconv.FormatInt(*job.SourceResultID, 10))
	}
	if job.ResultID != nil {
		out.ResultId = pointerTo(strconv.FormatInt(*job.ResultID, 10))
	}
	if job.Reason != "" {
		out.Reason = pointerTo(gen.JobReason(job.Reason))
	}
	if job.Verdict != "" {
		out.Verdict = pointerTo(job.Verdict)
	}
	// The node worked this out and it is the most useful thing a failed solve
	// can carry, so it travels whole rather than folded into detail.
	if job.Explanation != "" {
		out.Explanation = pointerTo(job.Explanation)
	}
	if job.Detail != "" {
		out.Detail = pointerTo(job.Detail)
	}
	out.PeakRssBytes, out.CpuSeconds = job.PeakRSSBytes, job.CPUSeconds
	out.StartedAt, out.FinishedAt = job.StartedAt, job.FinishedAt
	return out
}

// solveRuleProfile is the profile a job or a result records; id 0 is a built-in.
func solveRuleProfile(name string, id int64) *gen.SolveRuleProfile {
	out := &gen.SolveRuleProfile{Name: name}
	if id != 0 {
		out.Id = pointerTo(strconv.FormatInt(id, 10))
	}
	return out
}

func isTooLong(err error) bool {
	var limit *solveclient.ErrLimitTooLong
	return errors.As(err, &limit)
}

// arrango/dispatch.go

package arrango

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"google.golang.org/protobuf/proto"

	"github.com/smegg99/goptivum/solveclient"

	arrangorpcv1 "github.com/smegg99/arrango/backend/gen/arrangorpcv1"

	"github.com/smegg99/goptivum/backend/common/messages"
)

// finishTimeout bounds each storage/finalization phase independently. These
// phases outlive caller cancellation, but neither may consume the other's bound.
const finishTimeout = 10 * time.Second

// Persist keeps a solve's timetable (result.Snapshot, never nil) as a result of the job's plan and reports its id.
//
// It runs before the job's row closes, so a timetable that cannot be stored
// fails the job instead of passing it with nothing to open. The caller that
// owns plans and documents supplies it; this package knows neither.
type Persist func(ctx context.Context, job Job, result Result) (int64, error)

// Order is one solve a caller asks for.
type Order struct {
	PlanID int64
	// Version is the version whose school the solve reads: the one named, or
	// the source of SourceResultID.
	Version int64
	// SourceResultID is the result the solve reads, 0 when it reads Version.
	SourceResultID int64
	// NodeID is the node to solve on; 0 lets the dispatcher pick one.
	NodeID    int64
	StartedBy Starter
	// Profile replaces the model's own rules for this solve; nil keeps them.
	Profile *solveclient.Profile
	Request *arrangorpcv1.SolveRequest
}

// PlanLive reports whether a plan is still out of Trash, supplied by the caller that owns plans.
type PlanLive func(ctx context.Context, planID int64) (bool, error)

// Dispatcher sends solves to nodes and keeps the job table honest about them.
type Dispatcher struct {
	dialer  *solveclient.Dialer
	nodes   *Registry
	jobs    *Jobs
	persist Persist
	live    PlanLive

	// admitting makes counting a node's solves and opening the job one step,
	// so two requests at once cannot both take its last slot.
	admitting sync.Mutex
}

// NewDispatcher wires the registry, the jobs and the dialer together. The
// limits on each node are the registry's; a nil live check sends every solve.
func NewDispatcher(dialer *solveclient.Dialer, nodes *Registry, jobs *Jobs, persist Persist, live PlanLive) (*Dispatcher, error) {
	if dialer == nil || nodes == nil || jobs == nil {
		return nil, fmt.Errorf("arrango: the dialer, the node registry and the job table are all required")
	}
	return &Dispatcher{dialer: dialer, nodes: nodes, jobs: jobs, persist: persist, live: live}, nil
}

// Dialer is the pinned dialer this dispatcher uses, for a caller that needs a
// connection of its own without building a second identity.
func (d *Dispatcher) Dialer() *solveclient.Dialer { return d.dialer }

// Result is what one solve produced, for the job that ran it.
type Result struct {
	solveclient.Result
	Job Job
	// ResultID is the result the schedule was kept as, zero when nothing was
	// stored.
	ResultID int64
}

// Solve dispatches one solve and consumes the stream to its end.
//
// Admission runs first and always. The job row exists before anything is
// dialled, so an attempt that dies still leaves a record of itself.
func (d *Dispatcher) Solve(ctx context.Context, order Order, observe solveclient.Observer) (Result, error) {
	admission, job, err := d.Begin(ctx, order)
	if err != nil {
		return Result{}, err
	}
	return d.Finish(ctx, admission, job, order.Request, observe)
}

// Begin admits the request and opens the job, without running it.
//
// Split from the run so a caller that answers an HTTP request can report a
// refusal synchronously and then let the solve, which takes minutes, outlive
// the request that asked for it.
func (d *Dispatcher) Begin(ctx context.Context, order Order) (Admission, Job, error) {
	request := order.Request
	if request == nil || request.GetModel() == nil {
		return Admission{}, Job{}, fmt.Errorf("arrango: a solve needs a model")
	}
	config := request.GetConfig()
	if config == nil {
		config = &arrangorpcv1.SolverConfig{}
		request.Config = config
	}

	if config.TotalTimeSeconds <= 0 {
		config.TotalTimeSeconds = 180
	}
	judged := request.GetModel()
	if order.Profile != nil {
		order.Profile.Apply(request)
		if !config.GetIgnorePreviousPlacements() {
			// Validate takes no request rules, so the baseline is judged by a
			// copy carrying the profile as its own; unscoped rules resolve the
			// same from either side.
			judged = proto.CloneOf(request.Model)
			judged.RuleConfig = order.Profile.Rules
		}
	}

	d.admitting.Lock()
	defer d.admitting.Unlock()
	admission, err := d.Admit(ctx, order.NodeID, judged, config)
	if err != nil {
		return Admission{}, Job{}, err
	}
	// The clamp is applied to the outgoing message, not merely checked. A zero
	// that only failed a validation would still be a zero on the wire.
	config.NumWorkers = admission.Workers

	job, err := d.jobs.Create(ctx, NewJob{
		PlanID: order.PlanID, Version: order.Version, SourceResultID: order.SourceResultID,
		NodeID: admission.Node.ID, Workers: admission.Workers, StartedBy: order.StartedBy,
		Profile: order.Profile, TimeLimitSeconds: config.GetTotalTimeSeconds(),
		Improve: !config.GetIgnorePreviousPlacements(), StallStopSeconds: config.GetStallStopSeconds(),
	})
	if err != nil {
		return Admission{}, Job{}, err
	}
	job.NodeLabel = admission.Node.Label
	return admission, job, nil
}

// Finish runs the stream for a job Begin opened and records the outcome
// whatever it turns out to be.
func (d *Dispatcher) Finish(ctx context.Context, admission Admission, job Job,
	request *arrangorpcv1.SolveRequest, observe solveclient.Observer) (Result, error) {

	result, runErr := d.run(ctx, admission, job, request, observe)
	result.Job = job
	status, reason, detail := StatusDone, "", result.Message
	switch {
	case runErr != nil:
		status, detail = StatusFailed, runErr.Error()
		if solveclient.Cancelled(runErr) {
			status = StatusCancelled
		} else {
			reason = messages.KeySolveNodeError
		}
	case result.Status == arrangorpcv1.SolveStatus_SOLVE_STATUS_CANCELLED:
		status = StatusCancelled
	case !result.HasSchedule:
		status, reason = StatusFailed, failureReason(result.Status)
		if result.Status == arrangorpcv1.SolveStatus_SOLVE_STATUS_ERROR && result.PreflightRefused {
			reason = messages.KeySolvePreflightFailed
		}
	}
	// One owner persists the best schedule and closes the row, even on cancellation.
	var stored *int64
	if result.HasSchedule && d.persist != nil {
		storeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), finishTimeout)
		id, persistErr := d.persist(storeCtx, job, result)
		cancel()
		if persistErr == nil {
			stored, result.ResultID = &id, id
		} else {
			if status == StatusDone {
				status = StatusFailed
			}
			reason = messages.KeySolveResultNotStored
			detail = fmt.Sprintf("%s; the schedule could not be stored: %v", detail, persistErr)
		}
	}
	finishCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), finishTimeout)
	defer cancel()
	if finishErr := d.jobs.Finish(finishCtx, job.ID, outcomeOf(result, status, reason, detail, stored)); finishErr != nil {
		return result, errors.Join(runErr, finishErr)
	}
	closed, readErr := d.jobs.Get(finishCtx, job.ID)
	if readErr == nil {
		result.Job = closed
	}
	return result, errors.Join(runErr, readErr)
}

// outcomeOf is what the job row records about a finished solve.
//
// The quality, the verdict and the explanation all come from the node and are
// what a school actually reads: whether the timetable is any good, whether it
// is clean, and when it could not be built, which inputs cannot all hold.
func outcomeOf(result Result, status, reason, detail string, resultID *int64) Outcome {
	outcome := Outcome{
		Status: status, Reason: reason, Detail: detail, ResultID: resultID,
		Workers: result.Config.GetNumWorkers(), TimeLimitSeconds: result.Config.GetTotalTimeSeconds(), PeakRSSBytes: result.PeakRSSBytes, CPUSeconds: result.CPUSeconds,
	}
	if result.Verdict != nil {
		outcome.Verdict = result.Verdict.GetTier().String()
	}
	if result.HasSchedule && result.Score != nil {
		quality := result.Quality
		outcome.Quality = &quality
	}
	if core := result.Explanation; core != nil {
		outcome.Explanation = solveclient.Explain(core)
	}
	return outcome
}

// failureReason is why a stream that closed cleanly kept nothing, as the
// node's final status says.
func failureReason(status arrangorpcv1.SolveStatus) string {
	switch status {
	case arrangorpcv1.SolveStatus_SOLVE_STATUS_INFEASIBLE:
		return messages.KeySolveInfeasible
	case arrangorpcv1.SolveStatus_SOLVE_STATUS_TIMEOUT:
		return messages.KeySolveNoScheduleInTime
	default:
		return messages.KeySolveNodeError
	}
}

// run does the dialling and the stream. Its caller owns the failure path, so
// every exit here is either a clean result or an error to record.
func (d *Dispatcher) run(ctx context.Context, admission Admission, job Job,
	request *arrangorpcv1.SolveRequest, observe solveclient.Observer) (Result, error) {

	// A delete cancels the solves the runner holds, and the runner holds this
	// one before it gets here, so a delete either stops it or is seen here.
	// Without this, a solve admitted just before a delete ran for its whole
	// budget on a plan in Trash, holding a node slot for a result nobody keeps.
	if d.live != nil {
		live, err := d.live(ctx, job.PlanID)
		if err != nil {
			return Result{}, err
		}
		if !live {
			return Result{}, fmt.Errorf("arrango: plan %d is in Trash: %w", job.PlanID, context.Canceled)
		}
	}
	// The same for a revoke, which is written before it cancels the node's
	// solves: one admitted just before it is seen here.
	current, err := d.nodes.Get(ctx, admission.Node.ID)
	if err != nil {
		return Result{}, err
	}
	if current.Revoked() {
		return Result{}, fmt.Errorf("arrango: node %s is revoked: %w", current.Label, context.Canceled)
	}

	conn, err := d.dialer.Reach(ctx, admission.Node.Label, admission.Node.Address, admission.Node.Fingerprint)
	if err != nil {
		return Result{}, err
	}
	defer conn.Close()

	solving, err := conn.Solve(ctx, request)
	if err != nil {
		return Result{}, err
	}
	if err := d.jobs.Start(ctx, job.ID); err != nil {
		solving.Close()
		return Result{}, err
	}
	var grantErr error
	recorded := false
	found, err := solving.Wait(func(update *arrangorpcv1.SolveUpdate) {
		if config := update.GetConfig(); !recorded && config != nil {
			recorded = true
			grantErr = d.jobs.Grant(ctx, job.ID, config.GetTotalTimeSeconds(), config.GetNumWorkers(), config.GetStallStopSeconds())
		}
		if observe != nil {
			observe(update)
		}
	})
	return Result{Result: found, Job: job}, errors.Join(err, grantErr)
}

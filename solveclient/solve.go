// solveclient/solve.go

package solveclient

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	arrangorpcv1 "github.com/smegg99/arrango/backend/gen/arrangorpcv1"
	arrangov1 "github.com/smegg99/arrango/backend/gen/arrangov1"
)

// DefaultWorkers is what a request for zero workers gets.
//
// Conservative on purpose: about 0.9 GB per worker plus 1 GB of base, so four
// is roughly 4.6 GB and fits a small box. The node knows its own host far
// better than the caller does and clamps again on arrival.
const DefaultWorkers uint32 = 4

// MaxWorkers is a sanity ceiling, not a capacity answer. A caller asking for
// thousands is a bug or an attack, never a plan.
const MaxWorkers uint32 = 64

// explainerFraction and its bounds are the infeasibility explainer's own budget
// inside the solver. It only runs on an infeasible result, but the caller does
// not know the outcome in advance, so the deadline has to allow for it.
const (
	explainerFraction = 0.25
	explainerMinimum  = 5 * time.Second
	explainerMaximum  = 30 * time.Second
)

// nodeOverhead mirrors the node's own overheadSeconds: validate, score and the
// audit's rescore run after the search and are inside the budget the node
// checks the deadline against.
const nodeOverhead = 60 * time.Second

// deadlineMargin sits on top of the node's whole budget: dialling, the model on
// the wire, and a short wait for a slot.
const deadlineMargin = 30 * time.Second

// ClampWorkers is the one rule a caller enforces whatever a node reports.
//
// Zero means "all cores" to the solver, which on a 24 core box is about 22 GB
// of workers and the documented path into the OOM killer. A caller-supplied
// zero is never passed through unexamined.
func ClampWorkers(requested, ceiling uint32) uint32 {
	if ceiling == 0 || ceiling > MaxWorkers {
		ceiling = MaxWorkers
	}
	if requested == 0 {
		requested = DefaultWorkers
	}
	if requested > ceiling {
		return ceiling
	}
	return requested
}

// Deadline is how long to wait on one solve.
//
// It must sit above the search budget with real margin. A deadline tighter than
// the budget kills a legitimate long solve through its own transport, and from
// the caller that is indistinguishable from a cancel: the DONE frame is simply
// lost, and a failure is recorded that never happened.
func Deadline(config *arrangorpcv1.SolverConfig) time.Duration {
	return NodeBudget(config) + deadlineMargin
}

// NodeBudget is what the node computes this request will take, mirroring its
// own Budget. Kept here rather than imported: importing it would pull the whole
// node service into a client.
func NodeBudget(config *arrangorpcv1.SolverConfig) time.Duration {
	search := time.Duration(config.GetTotalTimeSeconds() * float64(time.Second))
	if search <= 0 {
		// The solver's own default when the field is unset.
		search = 180 * time.Second
	}
	explainer := time.Duration(config.GetExplainBudgetSeconds() * float64(time.Second))
	if explainer <= 0 {
		explainer = time.Duration(float64(search) * explainerFraction)
		if explainer < explainerMinimum {
			explainer = explainerMinimum
		}
		if explainer > explainerMaximum {
			explainer = explainerMaximum
		}
	}
	return search + explainer + nodeOverhead
}

// Result is what one solve's stream produced.
//
// HasSchedule is the only honest answer to "did this work", and it is true only
// because a solution frame arrived and its snapshot was kept.
type Result struct {
	HasSchedule bool
	// PreflightRefused is the typed input refusal on the terminal frame.
	PreflightRefused bool
	Snapshot         *arrangov1.ScheduleSnapshot
	Status           arrangorpcv1.SolveStatus
	Objective        int64
	Message          string
	// Quality is the node's own 0-100 rating of the schedule, where 100 is
	// pristine. Objective above is the raw search objective, which the proto
	// calls "a different animal": it moves with the weights, so two runs of
	// the same school cannot be compared by it and a person will try anyway.
	Quality float64
	// Verdict is the one-glance tier over errors and warnings.
	Verdict *arrangorpcv1.ScheduleVerdict
	// Score is the full report: per-division, per-year and per-teacher
	// qualities, and located soft issues.
	Score *arrangov1.ScoreReport
	// Validation is what is actually broken, when anything is.
	Validation *arrangov1.ValidationReport
	// Explanation names the inputs that cannot all hold, after an infeasible
	// solve. It is the difference between "no feasible solution" and "teacher
	// Kowalski's availability and these four lessons".
	Explanation *arrangorpcv1.InfeasibleCore
	// Config is what the node actually ran, which it echoes on every frame so
	// a stored result records the configuration that produced it.
	Config *arrangorpcv1.SolverConfig
	// PeakRSSBytes and CPUSeconds are what the solve cost, from the node's
	// final frame; zero when it did not say.
	PeakRSSBytes uint64
	CPUSeconds   float64
}

// Observer is told about each update as it arrives. It must not block: the
// stream is read on the waiting goroutine and a slow observer stalls the solve.
type Observer func(*arrangorpcv1.SolveUpdate)

// Solving is one solve open on a node.
type Solving struct {
	label  string
	stream grpc.ServerStreamingClient[arrangorpcv1.SolveUpdate]
	cancel context.CancelFunc
}

// Solve opens one solve, bounded by Deadline(request.Config). Cancelling ctx
// stops it; Wait or Close must follow.
//
// A clean close of the stream is not success: Solve on the node returns OK
// unconditionally, even for a rejected request and for a proven-infeasible
// model, and the outcome lives only in the terminal update, in band.
func (c *Conn) Solve(ctx context.Context, request *arrangorpcv1.SolveRequest) (*Solving, error) {
	// Above the search budget with real margin. A deadline inside the budget
	// kills a legitimate solve through its own transport, and that is
	// indistinguishable from the caller cancelling.
	solveCtx, cancel := context.WithTimeout(ctx, Deadline(request.GetConfig()))
	stream, err := c.Solver.Solve(solveCtx, request)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("solveclient: dispatch to %s: %w", c.label, err)
	}
	return &Solving{label: c.label, stream: stream, cancel: cancel}, nil
}

// Wait reads the stream to its end, telling observe about each update.
//
// A broken stream returns the partial result with the error, not an empty one:
// a cancel, a node restart and the deadline all arrive there, and by then the
// result may hold fifteen improving schedules, of which it keeps the best.
func (s *Solving) Wait(observe Observer) (Result, error) {
	defer s.cancel()
	var result Result
	for {
		update, err := s.stream.Recv()
		if errors.Is(err, io.EOF) {
			return result, nil
		}
		if err != nil {
			return result, fmt.Errorf("solveclient: solve stream from %s: %w", s.label, err)
		}
		if observe != nil {
			observe(update)
		}
		absorb(&result, update)
	}
}

// Close stops a solve that will not be waited for.
func (s *Solving) Close() { s.cancel() }

// Cancelled reports whether a solve's error is the caller stopping it.
//
// Both checks are needed and the second is the one that fires. A cancelled
// stream returns a *status.Error with code Canceled, which does not wrap
// context.Canceled, so errors.Is alone answers false and every cancel would
// read as a failure. Measured against the pinned grpc.
func Cancelled(err error) bool {
	return errors.Is(err, context.Canceled) || status.Code(err) == codes.Canceled
}

// absorb folds one update into the result.
//
// Only a solution frame carrying a snapshot is evidence a schedule exists.
// Every callback in the solver sets FEASIBLE unconditionally, including the
// heartbeat, and solutions_found counts what the search found rather than what
// arrived here: a solution that fails validation is dropped before it is ever
// written to the stream.
func absorb(result *Result, update *arrangorpcv1.SolveUpdate) {
	if message := update.GetMessage(); message != "" {
		result.Message = message
	}
	// Echoed on every frame so a stored result records what produced it.
	if config := update.GetConfig(); config != nil {
		result.Config = config
	}

	switch update.GetPhase() {
	case arrangorpcv1.SolvePhase_SOLVE_PHASE_SOLUTION:
		if !Worse(result.HasSchedule, result.Objective, update) {
			keep(result, update)
		}
	case arrangorpcv1.SolvePhase_SOLVE_PHASE_DONE:
		result.Status = update.GetStatus()
		result.PreflightRefused = update.GetPreflightRefused()
		result.PeakRSSBytes = update.GetPeakRssBytes()
		result.CPUSeconds = update.GetCpuSeconds()
		// A node that could not finish attaches the newest timetable it
		// forwarded, which is not always the best one received.
		if !Worse(result.HasSchedule, result.Objective, update) {
			keep(result, update)
		}
		// Only on DONE, and only when the model could not be satisfied. This
		// is the node's answer to "why", and it took it up to thirty seconds
		// of extra work to produce.
		if core := update.GetInfeasibleCore(); core != nil {
			result.Explanation = core
		}
	}
}

// Worse reports whether a frame's timetable scores above the best one already
// received. An improve streams its baseline first, and its search can then
// send a worse timetable before a better one. Every streamed objective is the
// scorer's full penalty, so lower is better.
func Worse(received bool, best int64, update *arrangorpcv1.SolveUpdate) bool {
	return received && update.GetSnapshot() != nil && update.GetObjective() > best
}

// keep takes the schedule and everything the node said about it.
//
// The analysis rides on the same frame as the snapshot, so a caller that keeps
// one and drops the other hands a school a timetable with no idea whether it is
// any good.
func keep(result *Result, update *arrangorpcv1.SolveUpdate) {
	if verdict := update.GetVerdict(); verdict != nil {
		result.Verdict = verdict
	}
	if validation := update.GetValidation(); validation != nil {
		result.Validation = validation
	}
	if score := update.GetScore(); score != nil {
		result.Score = score
		result.Quality = score.GetOverallQuality()
	}
	if snapshot := update.GetSnapshot(); snapshot != nil {
		result.Snapshot = snapshot
		result.Objective = update.GetObjective()
		result.HasSchedule = true
	}
}

// Explain renders the node's infeasible core as one readable line.
//
// The structured items carry entity names and lesson ids, and a client that
// wants to highlight them reads the core itself. This is the sentence a person
// sees first, and it is the node's own words: it spent up to thirty seconds of
// extra search working them out.
func Explain(core *arrangorpcv1.InfeasibleCore) string {
	if message := strings.TrimSpace(core.GetMessage()); message != "" {
		return message
	}
	var named []string
	for _, item := range core.GetItems() {
		if text := strings.TrimSpace(item.GetMessage()); text != "" {
			named = append(named, text)
		}
	}
	if len(named) == 0 {
		for _, hint := range core.GetHints() {
			if text := strings.TrimSpace(hint.GetMessage()); text != "" {
				named = append(named, text)
			}
		}
	}
	return strings.Join(named, "; ")
}

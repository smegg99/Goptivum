// arrango/runner.go

package arrango

import (
	"context"
	"strconv"
	"sync"

	"github.com/smegg99/goptivum/solveclient"
	"github.com/smegg99/s99ab/net/sse"

	arrangorpcv1 "github.com/smegg99/arrango/backend/gen/arrangorpcv1"
	arrangov1 "github.com/smegg99/arrango/backend/gen/arrangov1"
)

// eventBuffer is how many updates a subscriber may fall behind by.
//
// The broker drops a frame rather than stall the goroutine reading the solve,
// which is exactly why the stream is a nudge and the job row is the truth.
const eventBuffer = 32

// Update is what a subscriber is told. It is deliberately small: everything
// here is a hint for an indicator, and a client reads the job itself for
// anything it will act on. A field with nothing to say is left out, so the
// terminal frame carries only what it knows.
type Update struct {
	JobID string `json:"job_id"`
	Phase string `json:"phase,omitempty"`
	// Status is the solver's enum, never the job's: they are two different
	// vocabularies and a client that confused them would read FEASIBLE as done.
	Status         string  `json:"status,omitempty"`
	Stage          string  `json:"stage,omitempty"`
	WallSeconds    float64 `json:"wall_seconds,omitempty"`
	Objective      int64   `json:"objective,omitempty"`
	SolutionsFound uint32  `json:"solutions_found,omitempty"`
	// Received is how many timetables arrived here so far, the count a person
	// is shown; SolutionsFound also counts ones that never arrived.
	Received uint32 `json:"received,omitempty"`
	Message  string `json:"message,omitempty"`
	// JobStatus is the row's status, and only on the terminal frame.
	JobStatus string `json:"job_status,omitempty"`
	// Terminal says the job reached a terminal state and no further update is
	// coming, so a client can stop waiting without a timeout.
	Terminal bool `json:"terminal"`
}

// live is one running solve: whose it is, how to stop it, who is watching, the
// best timetable it sent, and done, which closes once its row is closed and
// its terminal frame is out. preview is guarded by the runner's mutex.
type live struct {
	planID  int64
	nodeID  int64
	cancel  context.CancelFunc
	broker  *sse.Broker
	done    chan struct{}
	preview Preview
	// objective is the preview timetable's, which a worse one does not replace.
	objective int64
}

// Preview is the best timetable a running solve sent, and how many arrived.
// Snapshot is nil until the first one; nothing here is ever written again, so
// a reader may keep it after the runner moves on.
type Preview struct {
	PlanVersion int64
	Received    uint32
	Snapshot    *arrangov1.ScheduleSnapshot
	// Quality is the node's 0-100 rating of it, nil when it sent none.
	Quality *float64
	Verdict string
}

// Runner owns the solves that are in flight.
//
// A solve takes minutes and an HTTP request does not, so the work runs on a
// context of its own and the request only starts it. Cancelling a job cancels
// that context, which is what the node reads as a cancel.
type Runner struct {
	dispatcher *Dispatcher
	jobs       *Jobs

	mu      sync.Mutex
	running map[int64]*live
	closed  bool
	done    sync.WaitGroup
}

// NewRunner binds a dispatcher and its job table.
func NewRunner(dispatcher *Dispatcher, jobs *Jobs) *Runner {
	return &Runner{dispatcher: dispatcher, jobs: jobs, running: map[int64]*live{}}
}

// Start admits the request, opens the job, and runs it in the background.
//
// Admission is synchronous, so a revoked, full or silent node or an incomplete
// baseline is an error the caller can answer with. Everything after that
// outlives the request.
func (r *Runner) Start(ctx context.Context, order Order) (Job, error) {
	admission, job, err := r.dispatcher.Begin(ctx, order)
	if err != nil {
		return Job{}, err
	}

	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		// The server is shutting down. The row stays pending and the next boot
		// sees a job that never ran, which is the truth.
		return job, context.Canceled
	}
	// A fresh context, not one derived from the request's. The solve outlives
	// the request, nothing in it needs a request value, and its lifetime comes
	// from Close.
	solveCtx, cancel := context.WithCancel(context.Background())
	broker := sse.NewBroker(sse.WithBuffer(eventBuffer))
	running := &live{
		planID: job.PlanID, nodeID: job.NodeID,
		cancel: cancel, broker: broker, done: make(chan struct{}),
		preview: Preview{PlanVersion: job.PlanVersion},
	}
	r.running[job.ID] = running
	r.done.Add(1)
	r.mu.Unlock()

	go r.run(solveCtx, running, admission, job, order.Request)
	return job, nil
}

// run consumes the stream and publishes as it goes.
func (r *Runner) run(ctx context.Context, running *live,
	admission Admission, job Job, request *arrangorpcv1.SolveRequest) {

	broker := running.broker
	defer r.done.Done()
	defer running.cancel()
	defer func() {
		r.mu.Lock()
		delete(r.running, job.ID)
		r.mu.Unlock()
		// Closed after the terminal update is published, so a subscriber sees
		// the end rather than a connection that simply stopped.
		broker.Close()
		close(running.done)
	}()

	_, err := r.dispatcher.Finish(ctx, admission, job, request, func(update *arrangorpcv1.SolveUpdate) {
		out := toUpdate(job.ID, update)
		out.Received = r.observe(running, update)
		broker.Publish(sse.Event{Type: "update", Data: out})
	})

	// One last event carrying the row as it was written, so a client that only
	// ever reads the stream still learns the job is over.
	r.mu.Lock()
	final := Update{JobID: idString(job.ID), Terminal: true, Received: running.preview.Received}
	r.mu.Unlock()
	stored, readErr := r.jobs.Get(context.WithoutCancel(ctx), job.ID)
	if readErr == nil {
		final.JobStatus = stored.Status
		final.Message = stored.Detail
	} else if err != nil {
		final.JobStatus = StatusFailed
		final.Message = err.Error()
	}
	broker.Publish(sse.Event{Type: "update", Data: final})
}

// observe keeps the best timetable the solution frames carry and reports how
// many have arrived.
func (r *Runner) observe(running *live, update *arrangorpcv1.SolveUpdate) uint32 {
	r.mu.Lock()
	defer r.mu.Unlock()
	if update.GetPhase() != arrangorpcv1.SolvePhase_SOLVE_PHASE_SOLUTION || update.GetSnapshot() == nil {
		return running.preview.Received
	}
	if solveclient.Worse(running.preview.Snapshot != nil, running.objective, update) {
		running.preview.Received++
		return running.preview.Received
	}
	running.objective = update.GetObjective()
	preview := Preview{
		PlanVersion: running.preview.PlanVersion,
		Received:    running.preview.Received + 1,
		Snapshot:    update.GetSnapshot(),
	}
	if score := update.GetScore(); score != nil {
		quality := score.GetOverallQuality()
		preview.Quality = &quality
	}
	if verdict := update.GetVerdict(); verdict != nil {
		preview.Verdict = verdict.GetTier().String()
	}
	running.preview = preview
	return preview.Received
}

// Preview is the best timetable a running job sent. The second value is
// false when the job is not running: it has finished and its result, if it
// kept one, is the job's.
func (r *Runner) Preview(jobID int64) (Preview, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	running, ok := r.running[jobID]
	if !ok {
		return Preview{}, false
	}
	return running.preview, true
}

// Broker is where a running job publishes. The second value is false when the
// job is not running, which is not an error: it has finished, and the caller
// answers from the row instead.
func (r *Runner) Broker(jobID int64) (*sse.Broker, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	running, ok := r.running[jobID]
	if !ok {
		return nil, false
	}
	return running.broker, true
}

// Cancel stops a running solve. It reports whether anything was running:
// cancelling a job that already finished is not an error, because the caller
// wanted it stopped and it is stopped.
func (r *Runner) Cancel(jobID int64) bool {
	r.mu.Lock()
	running, ok := r.running[jobID]
	r.mu.Unlock()
	if !ok {
		return false
	}
	// The node reads a cancelled context as a cancel, and the job row is
	// written by the goroutine on its way out.
	running.cancel()
	return true
}

// CancelPlan stops every solve of one plan and waits until their rows are
// closed, or until ctx ends. A cancel can take finishTimeout plus storing the
// timetable the solve had found.
func (r *Runner) CancelPlan(ctx context.Context, planID int64) error {
	return r.cancelWhere(ctx, func(running *live) bool { return running.planID == planID })
}

// CancelNode stops every solve on one node and waits the same way.
func (r *Runner) CancelNode(ctx context.Context, nodeID int64) error {
	return r.cancelWhere(ctx, func(running *live) bool { return running.nodeID == nodeID })
}

// Revoke stops this server dispatching to a node, then stops the node's solves
// and waits for their rows. The revoke is written first, so a solve admitted
// just before it is either cancelled here or refused before it is dialled.
func (r *Runner) Revoke(ctx context.Context, nodeID int64) error {
	if err := r.dispatcher.nodes.Revoke(ctx, nodeID); err != nil {
		return err
	}
	return r.CancelNode(ctx, nodeID)
}

func (r *Runner) cancelWhere(ctx context.Context, match func(*live) bool) error {
	r.mu.Lock()
	var waiting []chan struct{}
	for _, running := range r.running {
		if match(running) {
			running.cancel()
			waiting = append(waiting, running.done)
		}
	}
	r.mu.Unlock()

	for _, done := range waiting {
		select {
		case <-done:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return nil
}

// Running is how many solves this server is carrying, for a status line.
func (r *Runner) Running() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.running)
}

// Close stops every solve and waits for the rows to be written.
//
// It is a Closer for the lifecycle: a solve that is killed without its row
// being closed counts against its node's concurrency for ever.
func (r *Runner) Close() error {
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return nil
	}
	r.closed = true
	for _, running := range r.running {
		running.cancel()
	}
	r.mu.Unlock()

	r.done.Wait()
	return nil
}

// idString renders a 64-bit id the way the contract carries it, because a JSON
// number is a double in every browser.
func idString(id int64) string { return strconv.FormatInt(id, 10) }

// toUpdate reduces a SolveUpdate to what a progress indicator renders.
//
// It never reports a schedule: only the job row does that, and only from a
// solution frame that carried a snapshot.
func toUpdate(jobID int64, update *arrangorpcv1.SolveUpdate) Update {
	out := Update{
		JobID:          idString(jobID),
		Phase:          update.GetPhase().String(),
		Status:         update.GetStatus().String(),
		WallSeconds:    update.GetWallTimeSeconds(),
		Objective:      update.GetObjective(),
		SolutionsFound: update.GetSolutionsFound(),
		Message:        update.GetMessage(),
	}
	if progress := update.GetProgress(); progress != nil {
		out.Stage = progress.GetStage().String()
	}
	return out
}

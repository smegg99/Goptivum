// jobs/registry.go

package jobs

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"sync"

	arrangov1 "github.com/smegg99/arrango/backend/gen/arrangov1"
)

type State string

const (
	StateRunning   State = "running"
	StateDone      State = "done"
	StateCancelled State = "cancelled"
	StateError     State = "error"
)

// Job holds plumbing state for one solve: the latest update payload
// (opaque protojson bytes — the backend never interprets solver
// semantics) and SSE subscriber fanout.
type Job struct {
	ID string

	mu          sync.Mutex
	state       State
	model       *arrangov1.SchoolModel // solved model, for result rendering
	lastUpdate  []byte
	subscribers map[chan []byte]struct{}
	cancel      context.CancelFunc
}

type Registry struct {
	mu   sync.Mutex
	jobs map[string]*Job
}

func NewRegistry() *Registry {
	return &Registry{jobs: make(map[string]*Job)}
}

func (r *Registry) Create(cancel context.CancelFunc) *Job {
	job := &Job{
		ID:          newID(),
		state:       StateRunning,
		subscribers: make(map[chan []byte]struct{}),
		cancel:      cancel,
	}
	r.mu.Lock()
	r.jobs[job.ID] = job
	r.mu.Unlock()
	return job
}

func (r *Registry) Get(id string) (*Job, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	job, ok := r.jobs[id]
	return job, ok
}

func (j *Job) State() State {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.state
}

func (j *Job) LastUpdate() []byte {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.lastUpdate
}

func (j *Job) SetModel(model *arrangov1.SchoolModel) {
	j.mu.Lock()
	defer j.mu.Unlock()
	j.model = model
}

func (j *Job) Model() *arrangov1.SchoolModel {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.model
}

// Publish stores the payload as the latest state and fans it out. Slow
// subscribers skip intermediate updates instead of blocking the stream;
// they always receive the final state because Finish closes channels
// only after the last publish landed in lastUpdate.
func (j *Job) Publish(payload []byte) {
	j.mu.Lock()
	defer j.mu.Unlock()
	j.lastUpdate = payload
	for ch := range j.subscribers {
		select {
		case ch <- payload:
		default:
		}
	}
}

// Subscribe returns a channel of future updates plus the latest payload
// for replay (nil if none yet). The channel closes when the job finishes.
func (j *Job) Subscribe() (<-chan []byte, []byte, func()) {
	j.mu.Lock()
	defer j.mu.Unlock()
	ch := make(chan []byte, 64)
	replay := j.lastUpdate
	if j.state == StateRunning {
		j.subscribers[ch] = struct{}{}
	} else {
		close(ch)
	}
	unsubscribe := func() {
		j.mu.Lock()
		defer j.mu.Unlock()
		if _, ok := j.subscribers[ch]; ok {
			delete(j.subscribers, ch)
			close(ch)
		}
	}
	return ch, replay, unsubscribe
}

// Finish moves the job to a terminal state and closes all subscriber
// channels; subsequent Subscribe calls replay the final payload.
func (j *Job) Finish(state State) {
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.state != StateRunning {
		return
	}
	j.state = state
	for ch := range j.subscribers {
		close(ch)
	}
	j.subscribers = make(map[chan []byte]struct{})
}

// Cancel signals the solve context; the stream reader finishes the job.
func (j *Job) Cancel() {
	j.mu.Lock()
	cancel := j.cancel
	j.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}

func newID() string {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err) // crypto/rand failing is unrecoverable
	}
	return hex.EncodeToString(b[:])
}

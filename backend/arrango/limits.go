// arrango/limits.go

package arrango

import (
	"fmt"

	"github.com/smegg99/goptivum/solveclient"

	"github.com/smegg99/goptivum/backend/common/messages"
)

// MaxSolves is a sanity ceiling on solves at once, as solveclient.MaxWorkers is on workers.
const MaxSolves = 64

// Limits is this server's own cap on one node: its solves there at once, and the workers each solve gets.
type Limits struct {
	Solves  int
	Workers int
}

// DefaultLimits is what a newly paired node starts with: as many solves at
// once as the node allows (DefaultConcurrentSolves when it sets no limit or
// reports none) and solveclient.DefaultWorkers, lowered to the node's worker
// ceiling.
func DefaultLimits(ceilings *solveclient.Ceilings) Limits {
	limits := Limits{Solves: DefaultConcurrentSolves, Workers: int(solveclient.DefaultWorkers)}
	if ceilings == nil {
		return limits
	}
	if ceilings.Solves > 0 {
		limits.Solves = min(ceilings.Solves, MaxSolves)
	}
	return limits.Within(*ceilings)
}

// Within lowers each limit to the node's ceiling; it never raises one.
func (l Limits) Within(c solveclient.Ceilings) Limits {
	if c.Solves > 0 && l.Solves > c.Solves {
		l.Solves = c.Solves
	}
	if c.Workers > 0 && l.Workers > c.Workers {
		l.Workers = c.Workers
	}
	return l
}

// AllowedBy refuses limits above the node's ceilings with an *ErrAboveCeiling naming the first one crossed.
func (l Limits) AllowedBy(c solveclient.Ceilings) error {
	if c.Solves > 0 && l.Solves > c.Solves {
		return &ErrAboveCeiling{Ceiling: c.Solves}
	}
	if c.Workers > 0 && l.Workers > c.Workers {
		return &ErrAboveCeiling{Workers: true, Ceiling: c.Workers}
	}
	return nil
}

// ErrAboveCeiling is a limit above what the node allows this server: solves at once, or workers per solve when Workers is set.
type ErrAboveCeiling struct {
	Workers bool
	Ceiling int
}

func (e *ErrAboveCeiling) Error() string {
	what := "solves at once"
	if e.Workers {
		what = "workers per solve"
	}
	return fmt.Sprintf("arrango: the node allows this server at most %d %s: %v", e.Ceiling, what, e.fault())
}

func (e *ErrAboveCeiling) Unwrap() error { return e.fault() }

func (e *ErrAboveCeiling) fault() *messages.Fault {
	key := messages.KeyNodeSolvesAboveCeiling
	if e.Workers {
		key = messages.KeyNodeWorkersAboveCeiling
	}
	return messages.NewFault(key, map[string]any{"Ceiling": e.Ceiling})
}

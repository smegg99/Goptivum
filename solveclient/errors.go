// solveclient/errors.go

package solveclient

import (
	"errors"
	"fmt"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	arrangorpcv1 "github.com/smegg99/arrango/backend/gen/arrangorpcv1"
	arrangov1 "github.com/smegg99/arrango/backend/gen/arrangov1"
)

var (
	// ErrUnreachable is a node that could not be dialled or did not take the call.
	ErrUnreachable = errors.New("solveclient: the node did not answer")
	// ErrRefused is a node that answered it has no room for the call; one it could make room for queues it instead.
	ErrRefused = errors.New("solveclient: the node refused the call for want of capacity")
	// ErrPinMismatch is a node presenting a certificate that is not the pinned
	// one. It is never a retry: a regenerated node certificate and an impostor
	// are indistinguishable from here, so both demand a deliberate re-pair.
	ErrPinMismatch = errors.New("solveclient: the node presented a different certificate")
	// ErrNotALink is text that is not an arrango:// pairing link.
	ErrNotALink = errors.New("solveclient: not a pairing link")
	// ErrLinkSpent is a node refusing a link's token: expired, already used or
	// never issued, which the node answers alike on purpose.
	ErrLinkSpent = errors.New("solveclient: the node refused the pairing link")
	// ErrNotPaired is a node that answers and does not accept the caller: its
	// operator revoked it there, or it never enrolled.
	ErrNotPaired = errors.New("solveclient: the node does not accept this caller")
	// ErrClientTooOld is a node that needs a newer version of the caller.
	ErrClientTooOld = errors.New("solveclient: the node needs a newer version of this caller")
)

// The catalogue keys a Refusal carries. The module cannot word them; the
// program that imports it holds them in its own catalogue.
const (
	KeyNodeOutdated            = "node.outdated"
	KeySolveModelInvalid       = "solve.modelInvalid"
	KeySolveIncompleteBaseline = "solve.incompleteBaseline"
	KeySolveLimitTooLong       = "solve.limitTooLong"
)

// Refusal is an error a person reads, as a catalogue key and its parameters.
type Refusal interface {
	error
	Key() string
	Data() map[string]any
}

// RefusalKeys is every key a Refusal carries, for a catalogue check.
func RefusalKeys() []string {
	return []string{KeyNodeOutdated, KeySolveModelInvalid, KeySolveIncompleteBaseline, KeySolveLimitTooLong}
}

// ErrOutdated is a node that answers but whose build lacks what was asked for.
type ErrOutdated struct {
	Node       string
	Capability string
}

func (e *ErrOutdated) Error() string {
	return fmt.Sprintf("%s: node %s cannot %s", KeyNodeOutdated, e.Node, e.Capability)
}

// Key is the catalogue entry.
func (e *ErrOutdated) Key() string { return KeyNodeOutdated }

// Data names the missing capability.
func (e *ErrOutdated) Data() map[string]any { return map[string]any{"Capability": e.Capability} }

// ErrModelInvalid is a school the node checked and found unsolvable.
//
// Kinds and not sentences: Conflict.message is documented as debug only, and a
// client localizes from the kind and the structured references beside it.
type ErrModelInvalid struct {
	Conflicts []*arrangov1.Conflict
}

func (e *ErrModelInvalid) Error() string {
	return fmt.Sprintf("%s: %d conflicts", KeySolveModelInvalid, len(e.Conflicts))
}

// Key is the catalogue entry.
func (e *ErrModelInvalid) Key() string { return KeySolveModelInvalid }

// Data counts the conflicts.
func (e *ErrModelInvalid) Data() map[string]any { return map[string]any{"Count": len(e.Conflicts)} }

// Kinds is every distinct problem the node found, as its own enum names, for a
// caller that has to carry them somewhere the proto is not.
func (e *ErrModelInvalid) Kinds() []string {
	seen := map[string]bool{}
	var kinds []string
	for _, conflict := range e.Conflicts {
		kind := conflict.GetKind().String()
		if seen[kind] {
			continue
		}
		seen[kind] = true
		kinds = append(kinds, kind)
	}
	return kinds
}

// ErrIncompleteBaseline is an improve solve over a file whose timetable does
// not place every lesson. Refused before dispatch rather than discovered as a
// worse result later.
type ErrIncompleteBaseline struct {
	Lessons  int
	Unplaced int
}

func (e *ErrIncompleteBaseline) Error() string {
	return fmt.Sprintf("%s: %d of %d lessons unplaced", KeySolveIncompleteBaseline, e.Unplaced, e.Lessons)
}

// Key is the catalogue entry.
func (e *ErrIncompleteBaseline) Key() string { return KeySolveIncompleteBaseline }

// Data carries both counts. "Not fully placed" is not actionable; "fourteen of
// twelve hundred" tells a planner whether to place them or start over.
func (e *ErrIncompleteBaseline) Data() map[string]any {
	return map[string]any{"Unplaced": e.Unplaced, "Lessons": e.Lessons}
}

// refusal names why a node that was dialled did not take a call.
func refusal(node, call string, err error) error {
	answer := status.Convert(err)
	switch answer.Code() {
	case codes.Unauthenticated:
		return fmt.Errorf("%w: %s %s: %w", ErrNotPaired, node, call, err)
	case codes.FailedPrecondition:
		return fmt.Errorf("%w: %s %s: %w", ErrClientTooOld, node, call, err)

	}
	return fmt.Errorf("%w: %s %s: %w", ErrUnreachable, node, call, err)
}

// callError keeps a transport failure and a capacity refusal apart from a node that failed.
func callError(node, call string, err error) error {
	switch status.Code(err) {
	case codes.Unavailable:
		return fmt.Errorf("%w: %s %s: %w", ErrUnreachable, node, call, err)
	case codes.ResourceExhausted:
		return fmt.Errorf("%w: %s %s: %w", ErrRefused, node, call, err)
	}
	return fmt.Errorf("solveclient: node %s %s: %w", node, call, err)
}

// ErrLimitTooLong refuses a budget above every available node's peer ceiling.
type ErrLimitTooLong struct{ Seconds float64 }

func (e *ErrLimitTooLong) Error() string {
	return fmt.Sprintf("%s: maximum %.0f seconds", KeySolveLimitTooLong, e.Seconds)
}
func (e *ErrLimitTooLong) Key() string          { return KeySolveLimitTooLong }
func (e *ErrLimitTooLong) Data() map[string]any { return map[string]any{"max_seconds": e.Seconds} }

// CheckTimeLimit refuses an unreported ceiling or an excessive request before dispatch.
func CheckTimeLimit(node string, capacity Capacity, config *arrangorpcv1.SolverConfig) error {
	if capacity.Ceilings == nil || capacity.Ceilings.Seconds <= 0 {
		return &ErrOutdated{Node: node, Capability: "solve_time_ceiling"}
	}
	seconds := config.GetTotalTimeSeconds()
	if seconds <= 0 {
		seconds = 180
	}
	if seconds > capacity.Ceilings.Seconds {
		return &ErrLimitTooLong{Seconds: capacity.Ceilings.Seconds}
	}
	return nil
}

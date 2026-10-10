// core/checks/checks.go

// Package checks asks a node what a solve's input checks say about a plan version or result, one check per plan and per result at a time, and caches the answer by stored object and the rules sent.
package checks

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/smegg99/s99logger"
	"google.golang.org/protobuf/proto"

	arrangorpcv1 "github.com/smegg99/arrango/backend/gen/arrangorpcv1"
	arrangov1 "github.com/smegg99/arrango/backend/gen/arrangov1"

	"github.com/smegg99/goptivum/backend/common/logger"
	"github.com/smegg99/goptivum/backend/common/messages"
	goptivumv1 "github.com/smegg99/goptivum/document/gen/goptivumv1"
)

// Deadline bounds one check, dialling included: the node's child waits for memory without saying so.
const Deadline = 20 * time.Second

var (
	// ErrSuperseded is a check that a check of a newer version of the same plan replaced.
	ErrSuperseded = messages.NewFault(messages.KeyCheckSuperseded, nil)
	// ErrWaiting is a node that did not answer within the deadline.
	ErrWaiting = messages.NewFault(messages.KeyCheckWaitingForSolver, nil)
	// ErrDocument is a version whose stored document could not be read.
	ErrDocument = errors.New("checks: the document could not be read")
)

// Node asks a node for its check report.
type Node func(ctx context.Context, request *arrangorpcv1.CheckRequest) (*arrangorpcv1.CheckReport, error)

// Load reads the document a check is about.
type Load func(ctx context.Context) (*goptivumv1.Document, error)

// Subject is one plan version, or one result when ResultID is set, and the stored object it names.
type Subject struct {
	PlanID int64
	// Version is the version checked, or the one a result's solve read.
	Version   int64
	ResultID  int64
	ObjectKey string
	// Rules replace the document's own rules, as the rule profile a result's solve used did; nil keeps them.
	Rules *arrangov1.RuleConfig
}

// key is what the cache and a flight know a check by: the stored object, and the rules when they replace the document's.
func (s Subject) key() (string, error) {
	if s.Rules == nil {
		return s.ObjectKey, nil
	}
	encoded, err := proto.MarshalOptions{Deterministic: true}.Marshal(s.Rules)
	if err != nil {
		return "", fmt.Errorf("checks: the rules of %s: %w", s.ObjectKey, err)
	}
	sum := sha256.Sum256(encoded)
	return s.ObjectKey + "#rules:" + hex.EncodeToString(sum[:]), nil
}

// scope is what one flight at a time is kept for: a plan's versions, or one result.
type scope struct {
	plan   int64
	result int64
}

func (s Subject) scope() scope { return scope{plan: s.PlanID, result: s.ResultID} }

// Service runs at most one check per plan's versions and per result, and keeps recent reports.
type Service struct {
	node     Node
	deadline time.Duration
	cache    *cache

	mu      sync.Mutex
	flights map[scope]*flight
}

// flight is the one check running for a plan; done closes once report and err are set.
type flight struct {
	key     string
	version int64
	cancel  context.CancelCauseFunc
	done    chan struct{}
	report  Report
	err     error
}

// New binds the node call and the deadline each check gets.
func New(node Node, deadline time.Duration) *Service {
	return &Service{node: node, deadline: deadline, cache: newCache(), flights: map[scope]*flight{}}
}

// Check answers from the cache, joins the running check of the same object, or starts one, replacing a check of an older version; a result's check never replaces another.
func (s *Service) Check(ctx context.Context, subject Subject, load Load) (Report, error) {
	key, err := subject.key()
	if err != nil {
		return Report{}, err
	}
	if report, ok := s.cache.get(key); ok {
		logger.Log.Debug(s99logger.NewEvent(logger.EventCheckDone, subjectAttrs(subject,
			s99logger.Bool("cached", true), s99logger.Int("errors", report.Errors),
			s99logger.Int("warnings", report.Warnings), s99logger.Int("notes", report.Notes))...))
		return report, nil
	}
	running, err := s.join(ctx, key, subject, load)
	if err != nil {
		return Report{}, err
	}
	select {
	case <-running.done:
		return running.report, running.err
	case <-ctx.Done():
		return Report{}, ctx.Err()
	}
}

// join returns the flight this request waits on, or ErrSuperseded when a newer version's check runs.
func (s *Service) join(ctx context.Context, key string, subject Subject, load Load) (*flight, error) {
	for {
		s.mu.Lock()
		running := s.flights[subject.scope()]
		if running == nil || subject.ResultID == 0 || running.key == key {
			break
		}
		s.mu.Unlock()
		select {
		case <-running.done:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	defer s.mu.Unlock()
	if running := s.flights[subject.scope()]; running != nil {
		switch {
		case running.key == key:
			return running, nil
		case running.version > subject.Version:
			return nil, ErrSuperseded
		default:
			running.cancel(ErrSuperseded)
		}
	}
	// A flight that finished between the first cache read and this lock left its report here.
	if report, ok := s.cache.get(key); ok {
		done := &flight{done: make(chan struct{}), report: report}
		close(done.done)
		return done, nil
	}

	// Detached from the caller: a joiner, or the next request, still gets the answer when this caller leaves.
	flightCtx, cancel := context.WithCancelCause(context.WithoutCancel(ctx))
	flightCtx, stop := context.WithTimeoutCause(flightCtx, s.deadline, ErrWaiting)
	running := &flight{key: key, version: subject.Version, cancel: cancel, done: make(chan struct{})}
	s.flights[subject.scope()] = running
	go s.run(flightCtx, stop, running, subject, load)
	return running, nil
}

func (s *Service) run(ctx context.Context, stop context.CancelFunc, running *flight, subject Subject, load Load) {
	defer stop()
	started := time.Now()
	report, err := s.check(ctx, subject, load)
	if err != nil {
		switch cause := context.Cause(ctx); {
		case errors.Is(cause, ErrSuperseded):
			err = ErrSuperseded
		case errors.Is(cause, ErrWaiting):
			err = ErrWaiting
		}
	}
	// Cached before the flight is dropped, so no request finds neither and asks the node again.
	if err == nil {
		s.cache.put(running.key, report)
	}
	s.mu.Lock()
	if s.flights[subject.scope()] == running {
		delete(s.flights, subject.scope())
	}
	s.mu.Unlock()

	switch {
	case err == nil:
		logger.Log.Debug(s99logger.NewEvent(logger.EventCheckDone, subjectAttrs(subject,
			s99logger.Bool("cached", false), s99logger.Int("errors", report.Errors),
			s99logger.Int("warnings", report.Warnings), s99logger.Int("notes", report.Notes),
			s99logger.Bool("rated", report.Score != nil), s99logger.Duration("took", time.Since(started)))...))
	case errors.Is(err, ErrSuperseded):
	default:
		logger.Log.Warn(s99logger.NewEvent(logger.EventCheckFailed, subjectAttrs(subject,
			s99logger.Err(err), s99logger.Duration("took", time.Since(started)))...))
	}
	running.report, running.err = report, err
	close(running.done)
}

func (s *Service) check(ctx context.Context, subject Subject, load Load) (Report, error) {
	doc, err := load(ctx)
	if err != nil {
		return Report{}, fmt.Errorf("%w: %w", ErrDocument, err)
	}
	request := &arrangorpcv1.CheckRequest{Model: doc.GetModel(), Snapshot: doc.GetSchedule()}
	if subject.Rules != nil {
		// As the solve was sent them: a request's rules are layered over the
		// model's, so the model goes without its own. A copy, so the loaded
		// document keeps the plan's rules.
		request.Model = proto.CloneOf(doc.GetModel())
		request.Model.RuleConfig = nil
		request.RuleConfig = subject.Rules
	}
	answer, err := s.node(ctx, request)
	if err != nil {
		return Report{}, err
	}
	if drift, drifted := verdictDrift(answer); drifted {
		logger.Log.Warn(s99logger.NewEvent(logger.EventCheckCountsDrift, subjectAttrs(subject,
			s99logger.Int("errors", drift.Errors), s99logger.Int("warnings", drift.Warnings),
			s99logger.Int("notes", drift.Notes), s99logger.Int("verdict_errors", drift.VerdictErrors),
			s99logger.Int("verdict_warnings", drift.VerdictWarnings),
			s99logger.Int("verdict_infos", drift.VerdictInfos))...))
	}
	return Convert(answer, doc), nil
}

func subjectAttrs(subject Subject, more ...s99logger.Attr) []s99logger.Attr {
	attrs := []s99logger.Attr{s99logger.Any("plan", subject.PlanID), s99logger.Any("version", subject.Version)}
	if subject.ResultID != 0 {
		attrs = append(attrs, s99logger.Any("result", subject.ResultID))
	}
	if subject.Rules != nil {
		attrs = append(attrs, s99logger.Bool("profile", true))
	}
	return append(attrs, more...)
}

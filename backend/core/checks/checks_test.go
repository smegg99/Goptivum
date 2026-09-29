// core/checks/checks_test.go

package checks_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"google.golang.org/protobuf/proto"

	arrangorpcv1 "github.com/smegg99/arrango/backend/gen/arrangorpcv1"
	arrangov1 "github.com/smegg99/arrango/backend/gen/arrangov1"

	"github.com/smegg99/goptivum/backend/core/checks"
	"github.com/smegg99/goptivum/document"
	goptivumv1 "github.com/smegg99/goptivum/document/gen/goptivumv1"
)

// node is a stand-in for the node: it records every request and answers with
// whatever answer returns, which may block.
type node struct {
	mu       sync.Mutex
	requests []*arrangorpcv1.CheckRequest
	// cancelled counts calls whose context ended before they answered.
	cancelled int
	asked     chan struct{}
	answer    func(ctx context.Context, call int) (*arrangorpcv1.CheckReport, error)
}

func newNode(answer func(ctx context.Context, call int) (*arrangorpcv1.CheckReport, error)) *node {
	return &node{asked: make(chan struct{}, 16), answer: answer}
}

func (n *node) check(ctx context.Context, request *arrangorpcv1.CheckRequest) (*arrangorpcv1.CheckReport, error) {
	n.mu.Lock()
	n.requests = append(n.requests, request)
	call := len(n.requests)
	n.mu.Unlock()
	n.asked <- struct{}{}
	report, err := n.answer(ctx, call)
	if ctx.Err() != nil {
		n.mu.Lock()
		n.cancelled++
		n.mu.Unlock()
	}
	return report, err
}

func (n *node) calls() int {
	n.mu.Lock()
	defer n.mu.Unlock()
	return len(n.requests)
}

func (n *node) cancels() int {
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.cancelled
}

// waitAsked blocks until the node was asked once more.
func (n *node) waitAsked(t *testing.T) {
	t.Helper()
	select {
	case <-n.asked:
	case <-time.After(5 * time.Second):
		t.Fatal("the node was never asked")
	}
}

// overloaded is a report with one overloaded teacher.
func overloaded(_ context.Context, _ int) (*arrangorpcv1.CheckReport, error) {
	return &arrangorpcv1.CheckReport{Hard: []*arrangov1.Conflict{{
		Kind: arrangov1.Conflict_KIND_TEACHER_OVERLOADED, Needed: 31, Available: 30,
		Entities: []*arrangov1.EntityRef{{Kind: arrangov1.EntityKind_ENTITY_KIND_TEACHER, Id: 12}},
	}}}, nil
}

// untilCancelled holds the call until its context ends.
func untilCancelled(ctx context.Context, _ int) (*arrangorpcv1.CheckReport, error) {
	<-ctx.Done()
	return nil, ctx.Err()
}

func school() *goptivumv1.Document {
	doc := document.New(&arrangov1.SchoolModel{
		Name:    "Plan 2027",
		Lessons: []*arrangov1.LessonInstance{{Id: 7, Duration: 1}},
	})
	doc.Schedule = &arrangov1.ScheduleSnapshot{Lessons: []*arrangov1.ScheduledLesson{{
		LessonId: 7, Placement: &arrangov1.Placement{DayId: 1, StartPeriod: 0},
	}}}
	return doc
}

func loadOf(doc *goptivumv1.Document) checks.Load {
	return func(context.Context) (*goptivumv1.Document, error) { return doc, nil }
}

func subject(plan, version int64, key string) checks.Subject {
	return checks.Subject{PlanID: plan, Version: version, ObjectKey: key}
}

func TestTheNodeGetsTheDocumentsSchoolAndTimetable(t *testing.T) {
	node := newNode(overloaded)
	service := checks.New(node.check, checks.Deadline)
	doc := school()
	doc.IgnoredFindings = []*goptivumv1.IgnoredFinding{{
		Kind: "teacher_overloaded", Entities: []*goptivumv1.FindingEntity{{Kind: "teacher", Id: 12}},
	}}

	report, err := service.Check(t.Context(), subject(1, 2, "plans/a"), loadOf(doc))
	if err != nil {
		t.Fatalf("check: %v", err)
	}
	want := &arrangorpcv1.CheckRequest{Model: doc.GetModel(), Snapshot: doc.GetSchedule()}
	if node.calls() != 1 || !proto.Equal(node.requests[0], want) {
		t.Fatalf("the node was asked %v, want %v", node.requests, want)
	}
	if len(report.Findings) != 1 || !report.Findings[0].Ignored || report.Errors != 0 || report.Ignored != 1 {
		t.Errorf("report = %+v, want the one finding ignored by the document", report)
	}
}

// Identical documents share one result, in any plan, and a repeat never
// reaches the node.
func TestAResultIsCachedByTheStoredObject(t *testing.T) {
	node := newNode(overloaded)
	service := checks.New(node.check, checks.Deadline)
	ctx := t.Context()

	for _, s := range []checks.Subject{subject(1, 2, "plans/a"), subject(1, 3, "plans/a"), subject(9, 1, "plans/a")} {
		report, err := service.Check(ctx, s, loadOf(school()))
		if err != nil {
			t.Fatalf("check %+v: %v", s, err)
		}
		if report.Errors != 1 {
			t.Errorf("check %+v errors = %d, want 1", s, report.Errors)
		}
	}
	if node.calls() != 1 {
		t.Errorf("the node was asked %d times, want once", node.calls())
	}
	if _, err := service.Check(ctx, subject(1, 4, "plans/b"), loadOf(school())); err != nil {
		t.Fatalf("check: %v", err)
	}
	if node.calls() != 2 {
		t.Errorf("another document reached the node %d times in all, want 2", node.calls())
	}
}

// A second request for the document being checked waits for the same answer.
func TestTheSameDocumentJoinsTheRunningCheck(t *testing.T) {
	release := make(chan struct{})
	node := newNode(func(ctx context.Context, call int) (*arrangorpcv1.CheckReport, error) {
		<-release
		return overloaded(ctx, call)
	})
	service := checks.New(node.check, checks.Deadline)

	results := make(chan error, 2)
	go func() {
		_, err := service.Check(t.Context(), subject(1, 2, "plans/a"), loadOf(school()))
		results <- err
	}()
	node.waitAsked(t)
	go func() {
		_, err := service.Check(t.Context(), subject(1, 2, "plans/a"), loadOf(school()))
		results <- err
	}()
	// The joiner is not a node call, so there is nothing to wait on but time.
	time.Sleep(50 * time.Millisecond)
	close(release)
	for range 2 {
		if err := <-results; err != nil {
			t.Errorf("check: %v", err)
		}
	}
	if node.calls() != 1 {
		t.Errorf("the node was asked %d times, want once", node.calls())
	}
}

func TestANewerVersionStopsTheOlderCheck(t *testing.T) {
	node := newNode(func(ctx context.Context, call int) (*arrangorpcv1.CheckReport, error) {
		if call == 1 {
			return untilCancelled(ctx, call)
		}
		return overloaded(ctx, call)
	})
	service := checks.New(node.check, checks.Deadline)

	older := make(chan error, 1)
	go func() {
		_, err := service.Check(t.Context(), subject(1, 2, "plans/a"), loadOf(school()))
		older <- err
	}()
	node.waitAsked(t)

	report, err := service.Check(t.Context(), subject(1, 3, "plans/b"), loadOf(school()))
	if err != nil || report.Errors != 1 {
		t.Fatalf("newer check = %+v, %v", report, err)
	}
	select {
	case err := <-older:
		if !errors.Is(err, checks.ErrSuperseded) {
			t.Errorf("older check = %v, want ErrSuperseded", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the older check never answered")
	}
	if node.cancels() != 1 {
		t.Errorf("the node saw %d cancelled calls, want the older one", node.cancels())
	}

	// A superseded check is not a result: asking again reaches the node.
	if _, err := service.Check(t.Context(), subject(1, 2, "plans/a"), loadOf(school())); err != nil {
		t.Fatalf("check again: %v", err)
	}
	if node.calls() != 3 {
		t.Errorf("the node was asked %d times, want 3", node.calls())
	}
}

func TestAnOlderVersionIsRefusedWhileANewerRuns(t *testing.T) {
	release := make(chan struct{})
	node := newNode(func(ctx context.Context, call int) (*arrangorpcv1.CheckReport, error) {
		<-release
		return overloaded(ctx, call)
	})
	service := checks.New(node.check, checks.Deadline)

	newer := make(chan error, 1)
	go func() {
		_, err := service.Check(t.Context(), subject(1, 5, "plans/b"), loadOf(school()))
		newer <- err
	}()
	node.waitAsked(t)

	if _, err := service.Check(t.Context(), subject(1, 4, "plans/a"), loadOf(school())); !errors.Is(err, checks.ErrSuperseded) {
		t.Errorf("older check = %v, want ErrSuperseded at once", err)
	}
	// Another plan is not held up.
	close(release)
	if _, err := service.Check(t.Context(), subject(2, 1, "plans/c"), loadOf(school())); err != nil {
		t.Errorf("another plan's check: %v", err)
	}
	if err := <-newer; err != nil {
		t.Errorf("newer check: %v", err)
	}
	if node.calls() != 2 {
		t.Errorf("the node was asked %d times, want 2", node.calls())
	}
}

func TestASlowNodeIsWaitingForTheSolver(t *testing.T) {
	node := newNode(untilCancelled)
	service := checks.New(node.check, 50*time.Millisecond)

	started := time.Now()
	_, err := service.Check(t.Context(), subject(1, 2, "plans/a"), loadOf(school()))
	if !errors.Is(err, checks.ErrWaiting) {
		t.Fatalf("err = %v, want ErrWaiting", err)
	}
	if took := time.Since(started); took > 5*time.Second {
		t.Errorf("gave up after %v", took)
	}
	if node.cancels() != 1 {
		t.Errorf("the node saw %d cancelled calls, want 1", node.cancels())
	}
}

func TestAFailureIsNotCached(t *testing.T) {
	refused := errors.New("node: refused")
	node := newNode(func(ctx context.Context, call int) (*arrangorpcv1.CheckReport, error) {
		if call == 1 {
			return nil, refused
		}
		return overloaded(ctx, call)
	})
	service := checks.New(node.check, checks.Deadline)

	if _, err := service.Check(t.Context(), subject(1, 2, "plans/a"), loadOf(school())); !errors.Is(err, refused) {
		t.Fatalf("first check = %v, want the node's error", err)
	}
	report, err := service.Check(t.Context(), subject(1, 2, "plans/a"), loadOf(school()))
	if err != nil || report.Errors != 1 {
		t.Fatalf("second check = %+v, %v", report, err)
	}
}

func TestADocumentThatCannotBeReadIsErrDocument(t *testing.T) {
	node := newNode(overloaded)
	service := checks.New(node.check, checks.Deadline)
	gone := errors.New("store: gone")
	_, err := service.Check(t.Context(), subject(1, 2, "plans/a"),
		func(context.Context) (*goptivumv1.Document, error) { return nil, gone })
	if !errors.Is(err, checks.ErrDocument) || !errors.Is(err, gone) {
		t.Fatalf("err = %v, want ErrDocument wrapping the cause", err)
	}
	if node.calls() != 0 {
		t.Errorf("the node was asked %d times", node.calls())
	}
}

// The caller leaving does not stop the check: the answer is cached for the
// next request.
func TestACheckWhoseCallerLeftStillLandsInTheCache(t *testing.T) {
	release := make(chan struct{})
	node := newNode(func(ctx context.Context, call int) (*arrangorpcv1.CheckReport, error) {
		<-release
		return overloaded(ctx, call)
	})
	service := checks.New(node.check, checks.Deadline)

	ctx, cancel := context.WithCancel(t.Context())
	left := make(chan error, 1)
	go func() {
		_, err := service.Check(ctx, subject(1, 2, "plans/a"), loadOf(school()))
		left <- err
	}()
	node.waitAsked(t)
	cancel()
	if err := <-left; !errors.Is(err, context.Canceled) {
		t.Fatalf("the caller that left got %v, want context.Canceled", err)
	}
	close(release)

	deadline := time.Now().Add(5 * time.Second)
	for {
		report, err := service.Check(t.Context(), subject(1, 2, "plans/a"), loadOf(school()))
		if err != nil {
			t.Fatalf("check: %v", err)
		}
		if report.Errors == 1 && node.calls() == 1 {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("the node was asked %d times, want once", node.calls())
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// A result's check runs beside the plan's version check: neither replaces the
// other, and a result's older source version is no reason to refuse it.
func TestAResultsCheckNeverReplacesAVersionsCheck(t *testing.T) {
	release := make(chan struct{})
	node := newNode(func(ctx context.Context, call int) (*arrangorpcv1.CheckReport, error) {
		select {
		case <-release:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
		return overloaded(ctx, call)
	})
	service := checks.New(node.check, checks.Deadline)

	version := make(chan error, 1)
	go func() {
		_, err := service.Check(t.Context(), subject(1, 5, "plans/main"), loadOf(school()))
		version <- err
	}()
	node.waitAsked(t)

	result := checks.Subject{PlanID: 1, Version: 3, ResultID: 9, ObjectKey: "plans/result"}
	answered := make(chan error, 1)
	go func() {
		_, err := service.Check(t.Context(), result, loadOf(school()))
		answered <- err
	}()
	node.waitAsked(t)
	close(release)

	if err := <-version; err != nil {
		t.Errorf("the version's check = %v, want it answered", err)
	}
	if err := <-answered; err != nil {
		t.Errorf("the result's check = %v, want it answered", err)
	}
	if node.cancels() != 0 || node.calls() != 2 {
		t.Errorf("the node saw %d calls and %d cancels, want 2 and none", node.calls(), node.cancels())
	}
}

func gapsOff() *arrangov1.RuleConfig {
	return &arrangov1.RuleConfig{Overrides: []*arrangov1.RuleOverride{
		{Rule: "teacher_gaps", Mode: arrangov1.RuleMode_RULE_MODE_OFF},
	}}
}

// A result solved with a profile is judged the way its solve was: the model
// without its own rules and the profile as the request's. The document loaded
// is left as it is.
func TestAResultSolvedWithAProfileIsCheckedByItsRules(t *testing.T) {
	node := newNode(overloaded)
	service := checks.New(node.check, checks.Deadline)
	doc := school()
	doc.Model.RuleConfig = &arrangov1.RuleConfig{Profile: "dobry_plan"}
	result := checks.Subject{PlanID: 1, Version: 3, ResultID: 9, ObjectKey: "plans/result", Rules: gapsOff()}

	if _, err := service.Check(t.Context(), result, loadOf(doc)); err != nil {
		t.Fatalf("check: %v", err)
	}
	if node.calls() != 1 {
		t.Fatalf("the node was asked %d times, want once", node.calls())
	}
	sent := node.requests[0]
	if sent.GetModel().GetRuleConfig() != nil || !proto.Equal(sent.GetRuleConfig(), gapsOff()) {
		t.Errorf("sent model rules %v and request rules %v, want none and the profile's",
			sent.GetModel().GetRuleConfig(), sent.GetRuleConfig())
	}
	if len(sent.GetModel().GetLessons()) != 1 || !proto.Equal(sent.GetSnapshot(), doc.GetSchedule()) {
		t.Errorf("sent %v, want the document's school and timetable", sent)
	}
	if doc.GetModel().GetRuleConfig().GetProfile() != "dobry_plan" {
		t.Errorf("the loaded document's rules became %v", doc.GetModel().GetRuleConfig())
	}
}

// The cache key is the stored object and the rules sent: one document judged
// by two profiles, or by its own rules, is three checks, and a repeat is none.
func TestOneDocumentJudgedByOtherRulesIsAnotherCheck(t *testing.T) {
	node := newNode(overloaded)
	service := checks.New(node.check, checks.Deadline)
	ctx := t.Context()
	strict := &arrangov1.RuleConfig{Profile: "dobry_plan"}

	subjects := []checks.Subject{
		{PlanID: 1, Version: 3, ResultID: 9, ObjectKey: "plans/same", Rules: gapsOff()},
		{PlanID: 1, Version: 3, ResultID: 10, ObjectKey: "plans/same", Rules: strict},
		{PlanID: 1, Version: 3, ObjectKey: "plans/same"},
		{PlanID: 2, Version: 1, ResultID: 11, ObjectKey: "plans/same", Rules: gapsOff()},
	}
	for _, s := range subjects {
		if _, err := service.Check(ctx, s, loadOf(school())); err != nil {
			t.Fatalf("check %+v: %v", s, err)
		}
	}
	if node.calls() != 3 {
		t.Errorf("the node was asked %d times, want 3", node.calls())
	}
}

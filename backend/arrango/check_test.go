// arrango/check_test.go

package arrango_test

import (
	"errors"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"

	arrangorpcv1 "github.com/smegg99/arrango/backend/gen/arrangorpcv1"
	arrangov1 "github.com/smegg99/arrango/backend/gen/arrangov1"

	"github.com/smegg99/goptivum/backend/arrango"
	"github.com/smegg99/goptivum/backend/common/messages"
	"github.com/smegg99/goptivum/solveclient"
)

func newChecker(t *testing.T) (*arrango.Checker, *arrango.Registry) {
	t.Helper()
	registry, _, _, _ := newRegistry(t)
	self, err := solveclient.LoadIdentity(t.TempDir())
	if err != nil {
		t.Fatalf("identity: %v", err)
	}
	dialer, err := solveclient.NewDialer(self, "0.1.0")
	if err != nil {
		t.Fatalf("dialer: %v", err)
	}
	return arrango.NewChecker(registry, dialer), registry
}

func TestCheckAsksTheFirstActiveNode(t *testing.T) {
	checker, registry := newChecker(t)
	ctx := t.Context()
	request := &arrangorpcv1.CheckRequest{
		Model: &arrangov1.SchoolModel{Name: "Plan 2027"},
		Snapshot: &arrangov1.ScheduleSnapshot{Lessons: []*arrangov1.ScheduledLesson{{
			LessonId: 7, Placement: &arrangov1.Placement{DayId: 1, StartPeriod: 2},
		}}},
	}

	if _, err := checker.Check(ctx, request); !errors.Is(err, arrango.ErrNoNode) {
		t.Fatalf("with no node = %v, want ErrNoNode", err)
	}

	want := &arrangorpcv1.CheckReport{Hard: []*arrangov1.Conflict{{
		Kind: arrangov1.Conflict_KIND_TEACHER_OVERLOADED, Needed: 31, Available: 30,
	}}}
	node := startFakeNode(t, &fakeNode{report: want})
	if _, err := registry.Record(ctx, "check", node.paired()); err != nil {
		t.Fatalf("record: %v", err)
	}
	// Paired later, so never asked.
	if _, err := registry.Record(ctx, "later", paired(t, "127.0.0.1:1")); err != nil {
		t.Fatalf("record: %v", err)
	}

	got, err := checker.Check(ctx, request)
	if err != nil {
		t.Fatalf("check: %v", err)
	}
	if !proto.Equal(got, want) {
		t.Errorf("report = %v, want the node's %v", got, want)
	}
	asked := node.checkCalls()
	if len(asked) != 1 || !proto.Equal(asked[0], request) {
		t.Fatalf("the node was asked %v, want the one request %v", asked, request)
	}
}

// A node built before the check RPC answers everything else, so it is refused
// by name before the call rather than failing inside it.
func TestCheckRefusesANodeWithoutTheCapability(t *testing.T) {
	checker, registry := newChecker(t)
	ctx := t.Context()
	node := startFakeNode(t, &fakeNode{capabilities: []string{"solve", "validate", "score"}})
	if _, err := registry.Record(ctx, "old", node.paired()); err != nil {
		t.Fatalf("record: %v", err)
	}

	_, err := checker.Check(ctx, &arrangorpcv1.CheckRequest{Model: &arrangov1.SchoolModel{}})
	var outdated *solveclient.ErrOutdated
	if !errors.As(err, &outdated) || outdated.Capability != "check" {
		t.Fatalf("err = %v, want ErrOutdated naming check", err)
	}
	var fault messages.Keyed
	if !errors.As(err, &fault) || fault.Key() != messages.KeyNodeOutdated ||
		fault.Data()["Capability"] != "check" {
		t.Errorf("fault = %v, want %s with the capability", fault, messages.KeyNodeOutdated)
	}
	if calls := node.checkCalls(); len(calls) != 0 {
		t.Errorf("the node was asked to check %d time(s)", len(calls))
	}
}

// A node that will never fit the check refuses it; that is the node being
// unable to take it, not a failure of this server.
func TestCheckSaysWhenTheNodeRefusesForCapacity(t *testing.T) {
	checker, registry := newChecker(t)
	ctx := t.Context()
	node := startFakeNode(t, &fakeNode{checkErr: status.Error(codes.ResourceExhausted, "not enough memory")})
	if _, err := registry.Record(ctx, "full", node.paired()); err != nil {
		t.Fatalf("record: %v", err)
	}
	if _, err := checker.Check(ctx, &arrangorpcv1.CheckRequest{Model: &arrangov1.SchoolModel{}}); !errors.Is(err, solveclient.ErrRefused) {
		t.Fatalf("err = %v, want ErrRefused", err)
	}
}

func TestCheckSaysWhenTheNodeDoesNotAnswer(t *testing.T) {
	checker, registry := newChecker(t)
	ctx := t.Context()
	// Nothing listens on port 1.
	if _, err := registry.Record(ctx, "gone", paired(t, "127.0.0.1:1")); err != nil {
		t.Fatalf("record: %v", err)
	}
	if _, err := checker.Check(ctx, &arrangorpcv1.CheckRequest{Model: &arrangov1.SchoolModel{}}); !errors.Is(err, solveclient.ErrUnreachable) {
		t.Fatalf("err = %v, want ErrUnreachable", err)
	}
}

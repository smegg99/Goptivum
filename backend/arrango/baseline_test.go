// arrango/baseline_test.go

package arrango_test

import (
	"testing"

	"github.com/smegg99/goptivum/backend/arrango"
)

// A job says which result it reads, so a client draws its preview over that
// document; a job that reads a version says none.
func TestAJobRecordsTheResultItReads(t *testing.T) {
	registry, jobs, repository, store := newRegistry(t)
	ctx := t.Context()
	node, err := registry.Record(ctx, "node", paired(t, "127.0.0.1:19443"))
	if err != nil {
		t.Fatalf("record: %v", err)
	}
	plan := newPlan(t, repository, store)

	fromResult, err := jobs.Create(ctx, arrango.NewJob{
		PlanID: plan.ID, Version: 1, SourceResultID: 42, NodeID: node.ID, Workers: 2, StartedBy: owner,
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	fromVersion, err := jobs.Create(ctx, arrango.NewJob{
		PlanID: plan.ID, Version: 1, NodeID: node.ID, Workers: 2, StartedBy: owner,
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if fromResult.SourceResultID == nil || *fromResult.SourceResultID != 42 {
		t.Errorf("created %+v, want result 42 as its source", fromResult)
	}
	read, err := jobs.Get(ctx, fromResult.ID)
	if err != nil || read.SourceResultID == nil || *read.SourceResultID != 42 {
		t.Errorf("read back %+v, %v, want result 42 as its source", read, err)
	}
	if read, err := jobs.Get(ctx, fromVersion.ID); err != nil || read.SourceResultID != nil {
		t.Errorf("a job of a version reads %v, %v, want no result", read.SourceResultID, err)
	}
}

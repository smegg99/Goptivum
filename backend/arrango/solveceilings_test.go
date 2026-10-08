package arrango_test

import (
	"errors"
	"testing"
	"time"

	arrangorpcv1 "github.com/smegg99/arrango/backend/gen/arrangorpcv1"
	"github.com/smegg99/goptivum/backend/arrango"
	"github.com/smegg99/goptivum/solveclient"
)

func TestSolveTimeAdmissionSkipsShortNodesAndRefusesBeforeCreatingAJob(t *testing.T) {
	registry, jobs, repository, store := newRegistry(t)
	plan := newPlan(t, repository, store)
	d := newDispatcher(t, registry, jobs)
	short := startFakeNode(t, &fakeNode{status: policyStatus(1, 4)})
	limitedNode(t, registry, short, arrango.Limits{Solves: 1, Workers: 4})
	config := &arrangorpcv1.SolverConfig{TotalTimeSeconds: 3600, IgnorePreviousPlacements: true}
	_, _, err := d.Begin(t.Context(), arrango.Order{PlanID: plan.ID, Version: 1, Request: &arrangorpcv1.SolveRequest{Model: lessons(1, 1), Config: config}})
	var refused *solveclient.ErrLimitTooLong
	if !errors.As(err, &refused) || refused.Seconds != 900 {
		t.Fatalf("refusal = %v", err)
	}
	listed, err := jobs.ForPlan(t.Context(), plan.ID)
	if err != nil || len(listed) != 0 || len(short.solveRequests()) != 0 {
		t.Fatal("refused solve was dispatched or recorded", err)
	}
	time.Sleep(time.Millisecond)
	status := policyStatus(1, 4)
	status.Policy.MaxSolveSeconds = 86400
	long := limitedNode(t, registry, startFakeNode(t, &fakeNode{status: status}), arrango.Limits{Solves: 1, Workers: 4})
	got, err := d.Admit(t.Context(), 0, lessons(1, 1), config)
	if err != nil || got.Node.ID != long.ID {
		t.Fatalf("admit = %+v, %v", got, err)
	}
	if maximum := arrango.MaxSolveSeconds(t.Context(), registry, newTestDialer(t)); maximum != 86400 {
		t.Fatalf("maximum = %v", maximum)
	}
	if err := registry.Revoke(t.Context(), long.ID); err != nil {
		t.Fatal(err)
	}
	if maximum := arrango.MaxSolveSeconds(t.Context(), registry, newTestDialer(t)); maximum != 900 {
		t.Fatalf("revoked maximum = %v", maximum)
	}
}

func TestJobsKeepRequestedModeAndGrantedTime(t *testing.T) {
	registry, jobs, repository, store := newRegistry(t)
	plan := newPlan(t, repository, store)
	node := limitedNode(t, registry, startFakeNode(t, &fakeNode{}), arrango.Limits{Solves: 1, Workers: 4})
	job, err := jobs.Create(t.Context(), arrango.NewJob{PlanID: plan.ID, Version: 1, NodeID: node.ID, Workers: 4, TimeLimitSeconds: 3600, Improve: true, StallStopSeconds: 600})
	if err != nil {
		t.Fatal(err)
	}
	if err := jobs.Grant(t.Context(), job.ID, 900, 2, 600); err != nil {
		t.Fatal(err)
	}
	granted, err := jobs.Get(t.Context(), job.ID)
	if err != nil || granted.TimeLimitSeconds != 900 || granted.NumWorkers != 2 || !granted.Improve || granted.Status != arrango.StatusPending {
		t.Fatalf("granted = %+v, %v", granted, err)
	}
	if err := jobs.Finish(t.Context(), job.ID, arrango.Outcome{Status: arrango.StatusDone, TimeLimitSeconds: 900, Workers: 2}); err != nil {
		t.Fatal(err)
	}
	kept, err := jobs.Get(t.Context(), job.ID)
	if err != nil || kept.TimeLimitSeconds != 900 || !kept.Improve || kept.StallStopSeconds != 600 || kept.NumWorkers != 2 {
		t.Fatalf("kept = %+v, %v", kept, err)
	}
}

func TestAnUnreportedTimeCeilingCannotSilentlyCutASolve(t *testing.T) {
	registry, jobs, _, _ := newRegistry(t)
	node := limitedNode(t, registry, startFakeNode(t, &fakeNode{status: &arrangorpcv1.NodeStatusResponse{Cores: 8, AvailableBytes: 16 << 30}}), arrango.Limits{Solves: 1, Workers: 4})
	_, err := newDispatcher(t, registry, jobs).Admit(t.Context(), node.ID, lessons(1, 1), &arrangorpcv1.SolverConfig{TotalTimeSeconds: 3600, IgnorePreviousPlacements: true})
	var outdated *solveclient.ErrOutdated
	if !errors.As(err, &outdated) || outdated.Capability != "solve_time_ceiling" {
		t.Fatalf("refusal = %v", err)
	}
}

func TestPreflightErrorHasItsOwnJobReason(t *testing.T) {
	registry, jobs, repository, store := newRegistry(t)
	plan := newPlan(t, repository, store)
	node := limitedNode(t, registry, startFakeNode(t, &fakeNode{frames: []*arrangorpcv1.SolveUpdate{{Phase: arrangorpcv1.SolvePhase_SOLVE_PHASE_DONE, Status: arrangorpcv1.SolveStatus_SOLVE_STATUS_ERROR, PreflightRefused: true, Progress: &arrangorpcv1.SolveProgress{Stage: arrangorpcv1.SolveStage_SOLVE_STAGE_VALIDATE}}}}), arrango.Limits{Solves: 1, Workers: 4})
	result, err := newDispatcher(t, registry, jobs).Solve(t.Context(), order(plan.ID, node.ID, lessons(1, 1)), nil)
	if err != nil || result.Job.Reason != "solve.preflightFailed" {
		t.Fatalf("result = %+v, %v", result.Job, err)
	}
}

// Old cost semantics are refused before a job or solver request exists.
func TestSolveRefusesMissingRuleMetadataBeforeCreatingAJob(t *testing.T) {
	registry, jobs, repository, store := newRegistry(t)
	plan := newPlan(t, repository, store)
	peer := startFakeNode(t, &fakeNode{capabilities: []string{"solve", "solve_time_ceiling", "stall_stop"}})
	limitedNode(t, registry, peer, arrango.Limits{Solves: 1, Workers: 4})
	config := &arrangorpcv1.SolverConfig{TotalTimeSeconds: 900, IgnorePreviousPlacements: true}
	_, _, err := newDispatcher(t, registry, jobs).Begin(t.Context(), arrango.Order{PlanID: plan.ID, Version: 1, Request: &arrangorpcv1.SolveRequest{Model: lessons(1, 1), Config: config}})
	var outdated *solveclient.ErrOutdated
	if !errors.As(err, &outdated) || outdated.Capability != solveclient.CapabilityRuleMetadata {
		t.Fatalf("refusal = %v", err)
	}
	listed, err := jobs.ForPlan(t.Context(), plan.ID)
	if err != nil || len(listed) != 0 || len(peer.solveRequests()) != 0 {
		t.Fatalf("refused solve was recorded or dispatched: jobs=%v err=%v", listed, err)
	}
}

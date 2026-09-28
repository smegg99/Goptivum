// core/plans/exports_test.go

package plans_test

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"testing"
	"time"

	"github.com/smegg99/goptivum/backend/core/plans"
)

// versionFile is the kept file of one version in one format.
func versionFile(planID, version int64, format string) plans.Export {
	return plans.Export{
		ObjectKey: fmt.Sprintf("exports/%d/versions/%d/%s", planID, version, format),
		PlanID:    planID, Version: version, Format: format, SizeBytes: 7,
	}
}

// resultFile is the kept file of one result in one format.
func resultFile(planID, resultID int64, format string) plans.Export {
	return plans.Export{
		ObjectKey: fmt.Sprintf("exports/%d/results/%d/%s", planID, resultID, format),
		PlanID:    planID, ResultID: resultID, Format: format, SizeBytes: 11,
	}
}

func keepFile(t *testing.T, repo *plans.Repository, export plans.Export) plans.Export {
	t.Helper()
	kept, err := repo.KeepExport(context.Background(), export)
	if err != nil {
		t.Fatalf("keep %s: %v", export.ObjectKey, err)
	}
	return kept
}

// kept reports whether a key still names a kept file.
func kept(t *testing.T, repo *plans.Repository, key string) bool {
	t.Helper()
	_, err := repo.Export(context.Background(), key)
	if errors.Is(err, plans.ErrNoExport) {
		return false
	}
	if err != nil {
		t.Fatalf("export %s: %v", key, err)
	}
	return true
}

func TestAKeptExportReadsBackAndIsReplacedByItsKey(t *testing.T) {
	ctx := context.Background()
	repo := repository(t)
	plan := newPlan(t, repo, "identity-1", "Plan 2027")

	want := versionFile(plan.ID, 1, "goptivum.gpf")
	want.Variant = "age1school"
	first := keepFile(t, repo, want)
	if first.CreatedAt.IsZero() {
		t.Errorf("kept = %+v, want a creation time", first)
	}
	read, err := repo.Export(ctx, want.ObjectKey)
	if err != nil || !reflect.DeepEqual(read, first) {
		t.Fatalf("read back %+v, %v; kept %+v", read, err, first)
	}

	// Built again, for another key or none, the same file is one row.
	again := want
	again.Variant, again.SizeBytes = "", 9
	keepFile(t, repo, again)
	if read, err := repo.Export(ctx, want.ObjectKey); err != nil || read.Variant != "" || read.SizeBytes != 9 {
		t.Errorf("after a rebuild = %+v, %v", read, err)
	}

	if _, err := repo.Export(ctx, "exports/none"); !errors.Is(err, plans.ErrNoExport) {
		t.Errorf("an unknown key: err = %v, want ErrNoExport", err)
	}
}

// A file whose version or result is gone, or belongs to another plan, is
// refused, so no row names a file nothing will ever remove.
func TestAnExportOfWhatIsNotThereIsRefused(t *testing.T) {
	ctx := context.Background()
	repo, conn := repositoryWithConn(t)
	plan := newPlan(t, repo, "identity-1", "Plan 2027")
	other := newPlan(t, repo, "identity-1", "Inny")
	theirs := addResult(t, repo, conn, other.ID, "plans/theirs")

	if _, err := repo.KeepExport(ctx, versionFile(plan.ID, 5, "goptivum.gpf")); !errors.Is(err, plans.ErrNoVersion) {
		t.Errorf("a missing version: err = %v, want ErrNoVersion", err)
	}
	if _, err := repo.KeepExport(ctx, resultFile(plan.ID, 12345, "goptivum.gpf")); !errors.Is(err, plans.ErrNoResult) {
		t.Errorf("a missing result: err = %v, want ErrNoResult", err)
	}
	if _, err := repo.KeepExport(ctx, resultFile(plan.ID, theirs.ID, "goptivum.gpf")); !errors.Is(err, plans.ErrNoResult) {
		t.Errorf("another plan's result: err = %v, want ErrNoResult", err)
	}
	both := versionFile(plan.ID, 1, "goptivum.gpf")
	both.ResultID = theirs.ID
	if _, err := repo.KeepExport(ctx, both); err == nil {
		t.Error("a file of a version and a result at once was kept")
	}
	for _, key := range []string{
		versionFile(plan.ID, 5, "goptivum.gpf").ObjectKey,
		resultFile(plan.ID, 12345, "goptivum.gpf").ObjectKey,
		resultFile(plan.ID, theirs.ID, "goptivum.gpf").ObjectKey,
	} {
		if kept(t, repo, key) {
			t.Errorf("%s was kept", key)
		}
	}
}

// A version the backups cap removes takes its files with it: their keys wait
// for the sweep, and a version still kept keeps its own.
func TestTheBackupsCapRemovesTheFilesOfWhatItRemoves(t *testing.T) {
	repo, conn := repositoryWithConn(t)
	plan := newPlan(t, repo, "identity-1", "Plan 2027")
	setKeep(t, repo, plan.ID, 1)
	gpf1 := keepFile(t, repo, versionFile(plan.ID, 1, "goptivum.gpf"))
	html1 := keepFile(t, repo, versionFile(plan.ID, 1, "optivum.html"))
	upload(t, repo, plan.ID, 1)
	gpf2 := keepFile(t, repo, versionFile(plan.ID, 2, "goptivum.gpf"))
	upload(t, repo, plan.ID, 2)

	if got, want := numbers(t, repo, plan.ID), []int64{2, 3}; !reflect.DeepEqual(got, want) {
		t.Fatalf("timeline = %v, want %v", got, want)
	}
	waiting := garbage(t, conn)
	for _, key := range []string{gpf1.ObjectKey, html1.ObjectKey} {
		if !slices.Contains(waiting, key) {
			t.Errorf("garbage = %v, want %s", waiting, key)
		}
		if kept(t, repo, key) {
			t.Errorf("%s is still kept after its version went", key)
		}
	}
	if slices.Contains(waiting, gpf2.ObjectKey) || !kept(t, repo, gpf2.ObjectKey) {
		t.Errorf("the kept version lost its file: garbage = %v", waiting)
	}
}

// A result the results cap removes, and one purged from Trash, each take their
// files with them.
func TestResultsLeavingTakeTheirFiles(t *testing.T) {
	ctx := context.Background()
	repo, conn := repositoryWithConn(t)
	plan := newPlan(t, repo, "identity-1", "Plan 2027")
	setResultsKeep(t, repo, plan.ID, 1)

	capped := addResult(t, repo, conn, plan.ID, "plans/capped")
	cappedFile := keepFile(t, repo, resultFile(plan.ID, capped.ID, "optivum.html"))
	trashed := addResult(t, repo, conn, plan.ID, "plans/trashed")
	if kept(t, repo, cappedFile.ObjectKey) || !slices.Contains(garbage(t, conn), cappedFile.ObjectKey) {
		t.Errorf("the capped result's file: garbage = %v", garbage(t, conn))
	}

	trashedFile := keepFile(t, repo, resultFile(plan.ID, trashed.ID, "goptivum.gpf"))
	if err := repo.TrashResult(ctx, plan.ID, trashed.ID); err != nil {
		t.Fatalf("trash: %v", err)
	}
	if !kept(t, repo, trashedFile.ObjectKey) {
		t.Error("a result in Trash lost its file before its time was up")
	}
	if purged, err := repo.PurgeResults(ctx, time.Now().Add(31*24*time.Hour), 30); err != nil || purged != 1 {
		t.Fatalf("purge = %d, %v", purged, err)
	}
	if kept(t, repo, trashedFile.ObjectKey) || !slices.Contains(garbage(t, conn), trashedFile.ObjectKey) {
		t.Errorf("the purged result's file: garbage = %v", garbage(t, conn))
	}
}

// Deleting a project for good deletes its exports: the purge records every
// file of the plan, its versions' and its results', and leaves other plans'.
func TestThePurgeRemovesAPlansFiles(t *testing.T) {
	ctx := context.Background()
	repo, conn := repositoryWithConn(t)
	gone := newPlan(t, repo, "identity-1", "Do usunięcia")
	result := addResult(t, repo, conn, gone.ID, "plans/gone-result")
	versionGone := keepFile(t, repo, versionFile(gone.ID, 1, "goptivum.gpf"))
	resultGone := keepFile(t, repo, resultFile(gone.ID, result.ID, "optivum.html"))
	stays := newPlan(t, repo, "identity-1", "Plan 2027")
	staying := keepFile(t, repo, versionFile(stays.ID, 1, "goptivum.gpf"))

	if err := repo.Trash(ctx, gone.ID); err != nil {
		t.Fatalf("trash: %v", err)
	}
	if purged, err := repo.Purge(ctx, time.Now().AddDate(0, 0, 31), 30); err != nil || purged != 1 {
		t.Fatalf("purge: %d, %v", purged, err)
	}
	waiting := garbage(t, conn)
	for _, key := range []string{versionGone.ObjectKey, resultGone.ObjectKey} {
		if !slices.Contains(waiting, key) || kept(t, repo, key) {
			t.Errorf("%s: garbage = %v", key, waiting)
		}
	}
	if slices.Contains(waiting, staying.ObjectKey) || !kept(t, repo, staying.ObjectKey) {
		t.Errorf("another plan's file was removed: garbage = %v", waiting)
	}
}

// The sweep never deletes a file an export row names, and Storage counts kept
// files as stored objects rather than as waiting.
func TestTheSweepAndStorageKnowKeptFiles(t *testing.T) {
	ctx := context.Background()
	repo, conn := repositoryWithConn(t)
	plan := newPlan(t, repo, "identity-1", "Plan 2027")
	before, err := repo.Stored(ctx)
	if err != nil {
		t.Fatalf("stored: %v", err)
	}

	file := keepFile(t, repo, versionFile(plan.ID, 1, "goptivum.gpf"))
	now := time.Now().UTC()
	wait(t, conn, file.ObjectKey, now.Add(-2*time.Hour))
	after, err := repo.Stored(ctx)
	if err != nil {
		t.Fatalf("stored: %v", err)
	}
	if want := (plans.Stored{Objects: before.Objects + 1, Bytes: before.Bytes + file.SizeBytes, Waiting: before.Waiting}); after != want {
		t.Errorf("stored = %+v, want %+v", after, want)
	}

	store := &removals{}
	if deleted, err := repo.Sweep(ctx, now, store.remove); err != nil || deleted != 0 || len(store.keys) != 0 {
		t.Errorf("swept %d %v, %v; want the named file kept", deleted, store.keys, err)
	}
	if got := garbage(t, conn); len(got) != 0 {
		t.Errorf("garbage = %v, want the named key dropped from it", got)
	}
}

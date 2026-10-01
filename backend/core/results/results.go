// core/results/results.go

// Package results keeps a solved timetable as a result of its plan.
//
// It sits above the plan index and the document store because it needs both:
// the index says which document a version points at, and the store is where a
// result's document lives. The solving client knows about neither.
package results

import (
	"context"
	"fmt"

	arrangov1 "github.com/smegg99/arrango/backend/gen/arrangov1"

	"github.com/smegg99/goptivum/backend/arrango"
	"github.com/smegg99/goptivum/backend/core/documents"
	"github.com/smegg99/goptivum/backend/core/plans"
	"github.com/smegg99/goptivum/document"
)

// Keep stores the document the solve read with the timetable as a new result
// of the plan: the result sourceResult when it is not 0, else the version
// add.SourceVersion. The main file is never written, so a plan edited while
// the solve ran keeps both. add's ObjectKey and SizeBytes are the stored
// document's.
func Keep(ctx context.Context, index *plans.Repository, store *documents.Store,
	snapshot *arrangov1.ScheduleSnapshot, add plans.NewResult, sourceResult int64) (plans.Result, error) {

	if index == nil || store == nil {
		return plans.Result{}, fmt.Errorf("results: the plan index and the document store are both required")
	}
	if snapshot == nil {
		return plans.Result{}, fmt.Errorf("results: no timetable to keep")
	}
	from, err := sourceKey(ctx, index, add.PlanID, add.SourceVersion, sourceResult)
	if err != nil {
		return plans.Result{}, err
	}
	doc, err := store.Load(ctx, from)
	if err != nil {
		return plans.Result{}, err
	}
	doc.Schedule = snapshot

	encoded, err := document.Encode(doc)
	if err != nil {
		return plans.Result{}, err
	}
	return Add(ctx, index, store, encoded, add)
}

// Add stores an encoded result document as it is and lists it as a result of
// add.PlanID; add's ObjectKey and SizeBytes are the stored document's. A
// refused listing leaves the object to the sweep.
func Add(ctx context.Context, index *plans.Repository, store *documents.Store,
	encoded document.Encoded, add plans.NewResult) (plans.Result, error) {

	if index == nil || store == nil {
		return plans.Result{}, fmt.Errorf("results: the plan index and the document store are both required")
	}
	key, err := store.Put(ctx, encoded)
	if err != nil {
		return plans.Result{}, err
	}
	add.ObjectKey, add.SizeBytes = key, encoded.Size
	result, err := index.AddResult(ctx, add)
	if err != nil {
		// Best effort: a key that stays unlisted is only an object kept.
		_ = index.Discard(ctx, key)
		return plans.Result{}, err
	}
	return result, nil
}

// sourceKey is the object a solve read: a result's, which may have gone to
// Trash since and whose version may be gone, or a version's.
func sourceKey(ctx context.Context, index *plans.Repository, planID, version, sourceResult int64) (string, error) {
	if sourceResult != 0 {
		result, err := index.SourceResult(ctx, planID, sourceResult)
		return result.ObjectKey, err
	}
	stored, err := index.Version(ctx, planID, version)
	return stored.ObjectKey, err
}

// Persist is the solving client's hook into the plan index: it keeps a finished
// solve's timetable as a result of the job's plan, with who started the solve,
// the rule profile it used and what the node said about the timetable.
func Persist(index *plans.Repository, store *documents.Store) arrango.Persist {
	return func(ctx context.Context, job arrango.Job, found arrango.Result) (int64, error) {
		add := plans.NewResult{
			PlanID: job.PlanID, JobID: job.ID, SourceVersion: job.PlanVersion,
			StartedBy: job.StartedBy.ID, StartedByName: job.StartedBy.Name,
		}
		if profile := job.Profile; profile != nil {
			rules, err := arrango.EncodeRules(profile.Rules)
			if err != nil {
				return 0, err
			}
			add.RuleProfile = &plans.RuleProfile{Name: profile.Name, ID: profile.ID, Rules: rules}
		}
		if found.Score != nil {
			quality := found.Quality
			add.Quality = &quality
		}
		if found.Verdict != nil {
			add.Verdict = found.Verdict.GetTier().String()
		}
		var sourceResult int64
		if job.SourceResultID != nil {
			sourceResult = *job.SourceResultID
		}
		kept, err := Keep(ctx, index, store, found.Snapshot, add, sourceResult)
		return kept.ID, err
	}
}

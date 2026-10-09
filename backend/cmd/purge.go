// cmd/purge.go

package cmd

import (
	"context"
	"time"

	"github.com/smegg99/s99logger"

	periodic "github.com/smegg99/s99ab/work/jobs"

	"github.com/smegg99/goptivum/backend/common/logger"
	"github.com/smegg99/goptivum/backend/core/documents"
	"github.com/smegg99/goptivum/backend/core/plans"
)

// upkeepSpec is fixed rather than configured: a removal date is a day and a
// swept object waits an hour, so an hourly run is exact enough and there is
// nothing a school would tune.
const upkeepSpec = "@hourly"

const (
	trashPurgeJob  = "trash-purge"
	objectSweepJob = "object-sweep"
)

// startUpkeep runs the hourly work on stored projects until the scheduler is
// closed, each job once now and then every hour: the Trash purge, and the
// sweep that deletes objects no version, result or export names any more.
// trashDays is read at each purge, so a change applies at the next one.
func startUpkeep(ctx context.Context, index *plans.Repository, store *documents.Store,
	trashDays func() int) (*periodic.Scheduler, error) {
	scheduler := periodic.New(periodic.Options{Observe: func(run periodic.Run) {
		if run.Err == nil {
			return
		}
		event := logger.EventTrashPurgeFailed
		if run.Name == objectSweepJob {
			event = logger.EventObjectSweepFailed
		}
		logger.Log.Warn(s99logger.NewEvent(event, s99logger.String("error", run.Err.Error())))
	}})
	purge := func(ctx context.Context) error {
		days := trashDays()
		purged, err := index.PurgeTrash(ctx, time.Now(), days)
		if purged != (plans.Purged{}) {
			logger.Log.Info(s99logger.NewEvent(logger.EventTrashPurged,
				s99logger.Int("plans", int(purged.Plans)),
				s99logger.Int("checkpoints", int(purged.Checkpoints)),
				s99logger.Int("results", int(purged.Results)),
				s99logger.Int("trash_days", days)))
		}
		return err
	}
	sweep := func(ctx context.Context) error {
		deleted, err := index.Sweep(ctx, time.Now(), store.Delete)
		if deleted > 0 {
			logger.Log.Info(s99logger.NewEvent(logger.EventObjectsSwept,
				s99logger.Int("objects", deleted)))
		}
		return err
	}
	if err := scheduler.Add(trashPurgeJob, upkeepSpec, purge); err != nil {
		return nil, err
	}
	if err := scheduler.Add(objectSweepJob, upkeepSpec, sweep); err != nil {
		return nil, err
	}
	scheduler.Start(ctx)
	// A failure is logged by Observe and retried on the hour. The purge runs
	// first, so what it removes starts its hour before the sweep looks.
	_ = scheduler.RunNow(ctx, trashPurgeJob)
	_ = scheduler.RunNow(ctx, objectSweepJob)
	return scheduler, nil
}

// api/v1/storage.go

package v1

import (
	"context"
	"strconv"
	"time"

	"github.com/smegg99/s99logger"

	"github.com/smegg99/goptivum/backend/api/v1/gen"
	"github.com/smegg99/goptivum/backend/common/logger"
	"github.com/smegg99/goptivum/backend/core/audit"
	"github.com/smegg99/goptivum/backend/core/documents"
	"github.com/smegg99/goptivum/backend/core/plans"
	"github.com/smegg99/goptivum/backend/identity"
)

// Storage serves Backend > Storage over every owner's projects: what each
// stores, and emptying Trash, trimming backups and sweeping objects now.
// Every route is admin-only.
type Storage struct {
	index   *plans.Repository
	objects *documents.Store
	// accounts names the owners; nil leaves them unnamed.
	accounts *identity.Admin
	trail    *audit.Log
}

// NewStorage wires the storage routes over the index and the object store.
func NewStorage(index *plans.Repository, objects *documents.Store, accounts *identity.Admin,
	trail *audit.Log) *Storage {
	return &Storage{index: index, objects: objects, accounts: accounts, trail: trail}
}

func (s *Storage) serves() bool { return s != nil && s.index != nil && s.objects != nil }

// storageTarget is what a storage action is recorded against: the server's
// whole store.
var storageTarget = audit.Target{Kind: audit.Storage, ID: "server"}

// GetStorage is every project's usage, largest first, and the store's totals.
func (s *Storage) GetStorage(ctx context.Context, _ gen.GetStorageRequestObject) (gen.GetStorageResponseObject, error) {
	if !s.serves() {
		return gen.GetStorage404JSONResponse{NotFoundJSONResponse: gen.NotFoundJSONResponse(notFound())}, nil
	}
	usage, err := s.index.Usage(ctx)
	if err != nil {
		return gen.GetStorage503JSONResponse{UnavailableJSONResponse: gen.UnavailableJSONResponse(unavailable())}, nil
	}
	stored, err := s.index.Stored(ctx)
	if err != nil {
		return gen.GetStorage503JSONResponse{UnavailableJSONResponse: gen.UnavailableJSONResponse(unavailable())}, nil
	}
	names := accountNames(ctx, s.accounts)
	projects := make([]gen.ProjectStorage, 0, len(usage))
	for _, one := range usage {
		projects = append(projects, toProjectStorage(one, names))
	}
	return gen.GetStorage200JSONResponse{
		Projects: projects, StoredBytes: stored.Bytes, StoredObjects: stored.Objects,
		WaitingObjects: stored.Waiting,
	}, nil
}

func toProjectStorage(usage plans.Usage, names map[string]string) gen.ProjectStorage {
	out := gen.ProjectStorage{
		PlanId:   strconv.FormatInt(usage.Plan.ID, 10),
		Name:     usage.Plan.Name,
		Owner:    gen.Owner{Id: usage.Plan.OwnerIdentityID},
		MainFile: storedFiles(usage.MainFile),
		Backups:  storedFiles(usage.Backups),
		Results:  storedFiles(usage.Results),
		Trash:    storedFiles(usage.Trash),
	}
	if name := names[usage.Plan.OwnerIdentityID]; name != "" {
		out.Owner.Name = pointerTo(name)
	}
	if !usage.Plan.DeletedAt.IsZero() {
		out.DeletedAt = pointerTo(usage.Plan.DeletedAt)
	}
	return out
}

func storedFiles(files plans.Files) gen.StoredFiles {
	return gen.StoredFiles{Count: files.Count, Bytes: files.Bytes}
}

// EmptyTrash removes everything in every owner's Trash for good.
func (s *Storage) EmptyTrash(ctx context.Context, _ gen.EmptyTrashRequestObject) (gen.EmptyTrashResponseObject, error) {
	if !s.serves() {
		return gen.EmptyTrash404JSONResponse{NotFoundJSONResponse: gen.NotFoundJSONResponse(notFound())}, nil
	}
	purged, err := s.index.PurgeTrash(ctx, time.Now(), 0)
	if purged != (plans.Purged{}) {
		by, _ := callerID(ctx)
		logger.Log.Info(s99logger.NewEvent(logger.EventTrashEmptied,
			s99logger.Int("plans", int(purged.Plans)),
			s99logger.Int("checkpoints", int(purged.Checkpoints)),
			s99logger.Int("results", int(purged.Results)),
			s99logger.String("by", by)))
		audited(ctx, s.trail, audit.StorageTrashEmptied, storageTarget, map[string]string{
			"projects":    strconv.FormatInt(purged.Plans, 10),
			"checkpoints": strconv.FormatInt(purged.Checkpoints, 10),
			"results":     strconv.FormatInt(purged.Results, 10),
		})
	}
	if err != nil {
		return gen.EmptyTrash503JSONResponse{UnavailableJSONResponse: gen.UnavailableJSONResponse(unavailable())}, nil
	}
	return gen.EmptyTrash200JSONResponse{
		Projects: int(purged.Plans), Checkpoints: int(purged.Checkpoints), Results: int(purged.Results),
	}, nil
}

// TrimBackups applies every project's backups cap now.
func (s *Storage) TrimBackups(ctx context.Context, _ gen.TrimBackupsRequestObject) (gen.TrimBackupsResponseObject, error) {
	if !s.serves() {
		return gen.TrimBackups404JSONResponse{NotFoundJSONResponse: gen.NotFoundJSONResponse(notFound())}, nil
	}
	removed, err := s.index.TrimBackups(ctx)
	if removed > 0 {
		by, _ := callerID(ctx)
		logger.Log.Info(s99logger.NewEvent(logger.EventBackupsTrimmed,
			s99logger.Int("versions", int(removed)), s99logger.String("by", by)))
		audited(ctx, s.trail, audit.StorageBackupsTrimmed, storageTarget,
			map[string]string{"versions": strconv.FormatInt(removed, 10)})
	}
	if err != nil {
		return gen.TrimBackups503JSONResponse{UnavailableJSONResponse: gen.UnavailableJSONResponse(unavailable())}, nil
	}
	return gen.TrimBackups200JSONResponse{Versions: int(removed)}, nil
}

// SweepObjects runs the hourly sweep now. It is safe beside the scheduled
// one: each key is its own transaction that takes the key's row first, so two
// sweeps never delete one object twice.
func (s *Storage) SweepObjects(ctx context.Context, _ gen.SweepObjectsRequestObject) (gen.SweepObjectsResponseObject, error) {
	if !s.serves() {
		return gen.SweepObjects404JSONResponse{NotFoundJSONResponse: gen.NotFoundJSONResponse(notFound())}, nil
	}
	deleted, err := s.index.Sweep(ctx, time.Now(), s.objects.Delete)
	if err != nil {
		logger.Log.Warn(s99logger.NewEvent(logger.EventObjectSweepFailed, s99logger.Err(err)))
	}
	waiting, waitErr := s.index.Waiting(ctx)
	// Like every storage action, recorded only when it removed something.
	if deleted > 0 {
		logger.Log.Info(s99logger.NewEvent(logger.EventObjectsSwept, s99logger.Int("objects", deleted)))
		audited(ctx, s.trail, audit.StorageSwept, storageTarget, map[string]string{
			"deleted": strconv.Itoa(deleted), "waiting": strconv.Itoa(waiting),
		})
	}
	if err != nil || waitErr != nil {
		return gen.SweepObjects503JSONResponse{UnavailableJSONResponse: gen.UnavailableJSONResponse(unavailable())}, nil
	}
	return gen.SweepObjects200JSONResponse{Deleted: deleted, Waiting: waiting}, nil
}

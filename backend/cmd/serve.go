// cmd/serve.go

package cmd

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/smegg99/s99ab/data/db"
	"github.com/smegg99/s99ab/data/storage"
	"github.com/smegg99/s99ab/work/applife"
	"github.com/smegg99/s99logger"
	"github.com/spf13/cobra"

	apiv1 "github.com/smegg99/goptivum/backend/api/v1"
	"github.com/smegg99/goptivum/backend/common/logger"
	"github.com/smegg99/goptivum/backend/core/documents"
	"github.com/smegg99/goptivum/backend/core/plans"
	"github.com/smegg99/goptivum/backend/formats"
	optivumhtml "github.com/smegg99/goptivum/backend/formats/optivum/html"
	planxml "github.com/smegg99/goptivum/backend/formats/optivum/xml"
	"github.com/smegg99/goptivum/backend/migrations"
	"github.com/smegg99/goptivum/backend/version"
)

func newServeCmd(s *server) *cobra.Command {
	return &cobra.Command{
		Use:         "serve",
		Annotations: map[string]string{annotationConfig: "yes"},
		Args:        s.noArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return s.serve(cmd.Context())
		},
	}
}

func (s *server) serve(ctx context.Context) error {
	database := s.settings.Storage.Database

	conn, err := db.Open(db.Config{Driver: database.Driver, DSN: s.settings.DatabaseDSN()})
	if err != nil {
		return fmt.Errorf("open %s: %w", database.Driver, err)
	}
	if err := db.Migrate(conn, migrations.FS, database.Driver); err != nil {
		conn.Close()
		return fmt.Errorf("migrate: %w", err)
	}
	logger.Log.Debug(s99logger.NewEvent(logger.EventMigrationsDone,
		s99logger.String("driver", database.Driver)))

	objects, err := s.objects(ctx)
	if err != nil {
		conn.Close()
		return fmt.Errorf("open storage: %w", err)
	}
	logger.Log.Debug(s99logger.NewEvent(logger.EventStorageReady,
		s99logger.String("storage", s.storageKind())))

	handlers, err := apiv1.NewAPI(
		plans.NewRepository(conn, database.Driver),
		documents.NewStore(objects),
		formats.NewRegistry(planxml.New(), optivumhtml.NewImporter()),
	)
	if err != nil {
		conn.Close()
		return err
	}

	security := s.settings.Security
	api, err := apiv1.New(apiv1.Options{
		Build: apiv1.BuildInfo{
			Version: version.Release, Commit: version.Commit, BuildTime: version.BuildTime,
		},
		MinClientVersion:   s.settings.Api.MinClientVersion,
		Handlers:           handlers,
		Database:           conn.PingContext,
		TrustedProxies:     s.settings.Server.TrustedProxies,
		RateLimitPerMinute: security.RateLimitPerMinute,
		MaxRequestSize:     security.MaximumBodyBytes,
		MaxUploadSize:      security.MaximumUploadBytes,
	})
	if err != nil {
		conn.Close()
		return err
	}

	httpServer := &http.Server{
		Addr:              s.settings.Server.Address,
		Handler:           api.Handler(),
		ReadHeaderTimeout: seconds(s.settings.Server.ReadTimeoutSeconds),
		WriteTimeout:      seconds(s.settings.Server.WriteTimeoutSeconds),
		IdleTimeout:       seconds(s.settings.Server.IdleTimeoutSeconds),
	}
	// Closed in reverse: the server stops accepting before the database it was
	// serving from goes away.
	lifecycle := applife.New(applife.Options{
		Timeout: seconds(s.settings.Server.ShutdownTimeoutSeconds),
		Closers: []applife.Closer{
			applife.CloserFunc(conn.Close),
			applife.WithContext(httpServer.Shutdown),
		},
	})

	logger.Log.Info(s99logger.NewEvent(logger.EventServerStarted,
		s99logger.String("version", version.Release),
		s99logger.String("listen", s.settings.Server.Address),
		s99logger.String("driver", database.Driver),
		s99logger.String("storage", s.storageKind()),
		s99logger.String("config", s.path),
		s99logger.Bool("plan_routes", api.ServesPlans())))
	if !api.ServesPlans() {
		// Every plan route needs a session, and there are no sessions without
		// an identity provider. Mounting them anyway would mount them open.
		logger.Log.Warn(s99logger.NewEvent(logger.EventNoIdentity))
	}

	served := lifecycle.Serve(ctx, httpServer)
	logger.Log.Info(s99logger.NewEvent(logger.EventServerStopping))
	return errors.Join(served, lifecycle.Shutdown())
}

func seconds(value int64) time.Duration { return time.Duration(value) * time.Second }

// objects is the bucket when one is configured and a directory otherwise. A
// single-box or air-gapped school runs the directory.
func (s *server) objects(ctx context.Context) (storage.Storage, error) {
	documents := s.settings.Storage.Documents
	if documents.Endpoint == "" {
		return storage.NewLocalDir(documents.Dir, "/objects"), nil
	}
	return storage.NewBucket(ctx, storage.BucketConfig{
		Endpoint:  documents.Endpoint,
		Bucket:    documents.Bucket,
		AccessKey: documents.AccessKey.Reveal(),
		SecretKey: documents.SecretKey.Reveal(),
		UseSSL:    documents.UseSSL,
	})
}

func (s *server) storageKind() string {
	documents := s.settings.Storage.Documents
	if documents.Endpoint == "" {
		return "dir:" + documents.Dir
	}
	return "bucket:" + documents.Bucket
}

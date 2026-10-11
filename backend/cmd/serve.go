// cmd/serve.go

package cmd

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/smegg99/s99ab/auth/kratosauth"
	"github.com/smegg99/s99ab/data/db"
	"github.com/smegg99/s99ab/data/storage"
	"github.com/smegg99/s99ab/work/applife"
	"github.com/smegg99/s99config"
	"github.com/smegg99/s99logger"
	"github.com/spf13/cobra"

	apiv1 "github.com/smegg99/goptivum/backend/api/v1"
	"github.com/smegg99/goptivum/backend/arrango"
	"github.com/smegg99/goptivum/backend/common/config"
	"github.com/smegg99/goptivum/backend/common/logger"
	"github.com/smegg99/goptivum/backend/common/messages"
	"github.com/smegg99/goptivum/backend/core/audit"
	"github.com/smegg99/goptivum/backend/core/checks"
	"github.com/smegg99/goptivum/backend/core/documents"
	"github.com/smegg99/goptivum/backend/core/escrow"
	"github.com/smegg99/goptivum/backend/core/exports"
	"github.com/smegg99/goptivum/backend/core/library"
	"github.com/smegg99/goptivum/backend/core/lockout"
	"github.com/smegg99/goptivum/backend/core/plans"
	"github.com/smegg99/goptivum/backend/core/profiles"
	"github.com/smegg99/goptivum/backend/core/results"
	"github.com/smegg99/goptivum/backend/formats"
	"github.com/smegg99/goptivum/backend/formats/goptivum"
	optivumhtml "github.com/smegg99/goptivum/backend/formats/optivum/html"
	planxml "github.com/smegg99/goptivum/backend/formats/optivum/xml"
	"github.com/smegg99/goptivum/backend/identity"
	"github.com/smegg99/goptivum/backend/migrations"
	"github.com/smegg99/goptivum/backend/ui"
	"github.com/smegg99/goptivum/backend/version"
	"github.com/smegg99/goptivum/solveclient"
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
	defer conn.Close()
	if err := db.Migrate(conn, migrations.FS, database.Driver); err != nil {
		return fmt.Errorf("migrate: %w", err)
	}
	logger.Log.Debug(s99logger.NewEvent(logger.EventMigrationsDone,
		s99logger.String("driver", database.Driver)))

	objects, err := s.objects(ctx)
	if err != nil {
		return fmt.Errorf("open storage: %w", err)
	}
	logger.Log.Debug(s99logger.NewEvent(logger.EventStorageReady,
		s99logger.String("storage", s.storageKind())))

	// The school's own key, made once and kept. Without a passphrase escrow is
	// off and .gpf Identity mode needs the caller to bring recipients.
	keys := escrow.NewStore(conn, database.Driver, revealed(s.settings.Escrow.Passphrase))
	if keys.Enabled() {
		key, err := keys.Ensure(ctx)
		if err != nil {
			return err
		}
		logger.Log.Info(s99logger.NewEvent(logger.EventEscrowKeyReady,
			s99logger.String("recipient", key.Recipient)))
	} else {
		logger.Log.Warn(s99logger.NewEvent(logger.EventEscrowDisabled))
	}

	sessions, admin, ready, err := s.kratos()
	if err != nil {
		return err
	}

	// What an administrator changes while the server runs, kept beside
	// config.yaml. The lockout reads its limits on every sign-in; the upload
	// cap and the request limiter are built into the router once, below.
	stored, err := config.OpenSettings(s.path)
	if err != nil {
		return messages.WrapFault(err, messages.KeyCliCommandConfigFailed, map[string]any{"Detail": err})
	}
	guard := lockout.New(apiv1.LimitsOf(stored.Current().Lockout), time.Now)
	started := stored.Current()
	running := apiv1.Running{
		MaximumUploadBytes: started.Server.MaximumUploadBytes,
		RateLimitPerMinute: started.Server.RateLimitPerMinute,
	}
	trashDays := func() int { return stored.Current().Projects.TrashDays }
	trail := audit.New(conn, database.Driver)

	// The administrative half. A nil dependency means that half is not
	// configured and its routes answer 404 rather than pretending.
	nodes := arrango.NewRegistry(conn, database.Driver)
	dialer, err := s.arrangoDialer()
	if err != nil {
		return err
	}
	jobRows := arrango.NewJobs(conn, database.Driver)
	// Before the runner exists: no process can legitimately own an open row at
	// boot, and one left by a crash counts against its node for ever.
	if closed, err := jobRows.CloseOrphans(ctx); err == nil && closed > 0 {
		logger.Log.Warn(s99logger.NewEvent(logger.EventOrphanedJobsClosed,
			s99logger.Int("jobs", int(closed))))
	}

	// The settings routes push new caps to the index when they change.
	index := plans.NewRepository(conn, database.Driver, plans.Caps{
		BackupsKeep: started.Projects.BackupsKeep, ResultsKeep: started.Projects.ResultsKeep,
	})
	// Every write revives its key first, which is what keeps the sweep from
	// deleting an object between its write and the row that names it.
	store := documents.NewStore(objects).WithGarbage(index)

	var runner *arrango.Runner
	if dispatcher, err := arrango.NewDispatcher(dialer, nodes, jobRows, results.Persist(index, store), index.Live); err == nil {
		runner = arrango.NewRunner(dispatcher, jobRows)
	}
	defer closeRunner(runner)()
	// After the runner: deleting a plan stops its solves first.
	handlers, err := apiv1.NewAPI(index, store,
		formats.NewRegistry(planxml.New(), optivumhtml.NewImporter(),
			goptivum.NewImporter(s.settings.Application.Realm).WithKeys(keys.ForFiles())),
		runner, apiv1.Settings{
			TrashDays:     trashDays,
			MaxUploadSize: running.MaximumUploadBytes,
			Language:      s.settings.Application.Language,
		})
	if err != nil {
		return err
	}
	handlers.WithAudit(trail).WithAccounts(admin)
	// A .gpf is sealed to the school's own key when the server holds one, as
	// the importer opens it with that key; plain otherwise.
	exported, err := exports.New(index, store, objects,
		exports.Format{
			Exporter:    goptivum.NewExporter(s.settings.Application.Realm, "goptivum-server "+version.Release).WithKeys(keys.ForFiles()),
			ContentType: "application/octet-stream",
		},
		exports.Format{Exporter: optivumhtml.NewExporter(), ContentType: "application/zip"})
	if err != nil {
		return err
	}
	ruleProfiles := profiles.NewRepository(conn, database.Driver)
	contract := &apiv1.Handlers{
		API: handlers,
		Admin: apiv1.NewAdmin(admin, apiv1.Nodes{
			Registry: nodes, Dialer: dialer, Runner: runner, Jobs: jobRows, Plans: index,
		}).WithAudit(trail),
		Jobs:           apiv1.NewJobs(index, store, runner, jobRows, ruleProfiles),
		School:         apiv1.NewSchool(keys),
		Demo:           apiv1.NewDemo(handlers, nodes, dialer),
		Checks:         apiv1.NewChecks(handlers, checks.New(arrango.NewChecker(nodes, dialer).Check, checks.Deadline)),
		Exports:        apiv1.NewExports(handlers, exported),
		Rules:          apiv1.NewRules(arrango.NewRules(nodes, dialer).Catalogue, apiv1.RulesDeadline),
		Profiles:       apiv1.NewProfiles(ruleProfiles),
		Libraries:      apiv1.NewLibraries(library.NewRepository(conn, database.Driver, index)),
		Security:       apiv1.NewSecurity(stored, guard, s.settings.Identity.KratosConfig).WithAudit(trail),
		ServerSettings: apiv1.NewServerSettings(stored, index, running, trail),
		Storage:        apiv1.NewStorage(index, store, admin, trail),
		Logs:           apiv1.NewLogs(logger.Recent(), logger.Translator()),
		AuditTrail:     apiv1.NewAuditTrail(trail),
	}

	security := s.settings.Security
	settings := s.settings.Identity
	api, err := apiv1.New(apiv1.Options{
		Build: apiv1.BuildInfo{
			Version: version.Release, Commit: version.Commit, BuildTime: version.BuildTime,
		},
		MinClientVersion:         s.settings.Api.MinClientVersion,
		Addresses:                func() []string { return stored.Current().Server.PublicAddresses },
		MaxSolveSeconds:          func(ctx context.Context) float64 { return arrango.MaxSolveSeconds(ctx, nodes, dialer) },
		Handlers:                 contract,
		Accounts:                 admin,
		Database:                 conn.PingContext,
		Storage:                  store.Probe,
		Identity:                 ready,
		Kratos:                   sessions,
		KratosPublicURL:          settings.PublicURL,
		KratosRateLimitPerMinute: settings.RateLimitPerMinute,
		Lockout:                  guard,
		TrustedProxies:           s.settings.Server.TrustedProxies,
		RateLimitPerMinute:       running.RateLimitPerMinute,
		MaxRequestSize:           security.MaximumBodyBytes,
		MaxUploadSize:            running.MaximumUploadBytes,
		DemoSchool:               s.settings.Dev.DemoSchool,
	})
	if err != nil {
		return err
	}
	if api.ServesDemo() {
		logger.Log.Warn(s99logger.NewEvent(logger.EventDemoSchoolEnabled))
	}

	purger, err := startUpkeep(ctx, index, store, trashDays)
	if err != nil {
		return err
	}

	if admin != nil && settings.Bootstrap.Enabled {
		s.bootstrapAdmin(ctx, admin)
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
			// Before the database: a purge or a sweep in flight finishes on it.
			applife.CloserFunc(purger.Close),
			applife.WithContext(httpServer.Shutdown),
			// Before Shutdown, not after: an open event stream is an active
			// connection, and Shutdown would wait the whole timeout for it.
			// Closing the runner ends every solve, writes its row and closes its
			// broker, so the streams end and the connections go idle. Before the
			// database too: a solve killed without its row being written counts
			// against its node's concurrency for ever.
			applife.CloserFunc(closeRunner(runner)),
		},
	})

	logger.Log.Info(s99logger.NewEvent(logger.EventServerStarted,
		s99logger.String("version", version.Release),
		s99logger.String("listen", s.settings.Server.Address),
		s99logger.String("driver", database.Driver),
		s99logger.String("storage", s.storageKind()),
		s99logger.String("config", s.path),
		s99logger.Bool("plan_routes", api.ServesPlans()),
		s99logger.Bool("identity_proxy", api.ServesIdentity())))
	if !api.ServesPlans() {
		// Every plan route needs a session, and there are no sessions without
		// an identity provider. Mounting them anyway would mount them open.
		logger.Log.Warn(s99logger.NewEvent(logger.EventNoIdentity))
	}

	served := lifecycle.Serve(ctx, httpServer)
	logger.Log.Info(s99logger.NewEvent(logger.EventServerStopping))
	return errors.Join(served, lifecycle.Shutdown())
}

// revealed reads a secret handle, empty when it was never set. A Secret is a
// handle and not a string on purpose: it keeps the value out of a log line that
// prints the configuration.
func revealed(secret s99config.Secret) string {
	if secret == nil || !secret.IsSet() {
		return ""
	}
	return strings.TrimSpace(secret.Reveal())
}

func seconds(value int64) time.Duration { return time.Duration(value) * time.Second }

// closeRunner stops every in-flight solve, and is a no-op when no node can be
// reached at all.
func closeRunner(runner *arrango.Runner) func() error {
	return func() error {
		if runner == nil {
			return nil
		}
		return runner.Close()
	}
}

// kratos builds the session client, the admin client and the readiness probe.
//
// All three are nil when no public URL is configured, and that is the only
// state in which the plan routes stay unregistered. A configured but
// unreachable Kratos is an outage: the routes exist and answer 503.
func (s *server) kratos() (*kratosauth.Client, *identity.Admin, apiv1.Readiness, error) {
	settings := s.settings.Identity
	if settings.PublicURL == "" {
		return nil, nil, nil, nil
	}
	timeout := seconds(settings.TimeoutSeconds)

	sessions, err := kratosauth.New(kratosauth.Options{
		PublicURL: settings.PublicURL,
		Timeout:   timeout,
	})
	if err != nil {
		return nil, nil, nil, fmt.Errorf("identity: %w", err)
	}
	admin, err := identity.NewAdmin(settings.AdminURL, timeout)
	if err != nil {
		return nil, nil, nil, err
	}
	ready, err := identity.Probe(settings.PublicURL, timeout)
	if err != nil {
		return nil, nil, nil, err
	}
	return sessions, admin, ready, nil
}

// arrangoDialer builds the pinned dialer, creating this deployment's keypair
// on first use. A node pins that certificate, so the directory is durable
// state rather than a cache.
func (s *server) arrangoDialer() (*solveclient.Dialer, error) {
	self, err := solveclient.LoadIdentity(s.settings.Arrango.IdentityDir)
	if err != nil {
		return nil, err
	}
	return solveclient.NewDialer(self, version.Client())
}

// bootstrapAdmin mints the first administrator when the deployment has none.
//
// A failure here is never fatal: Kratos may simply not be up yet, and a server
// that refuses to start would also stop answering the discovery route that
// says what is wrong. The next boot tries again.
func (s *server) bootstrapAdmin(ctx context.Context, admin *identity.Admin) {
	settings := s.settings.Identity
	result, err := identity.Bootstrap(ctx, admin, identity.BootstrapParams{
		Username: settings.Bootstrap.Username,
		Email:    settings.Bootstrap.Email,
	})
	if err != nil {
		logger.Log.Warn(s99logger.NewEvent(logger.EventBootstrapFailed,
			s99logger.String("error", err.Error())))
		return
	}
	if !result.Created {
		logger.Log.Debug(s99logger.NewEvent(logger.EventBootstrapAdminExists,
			s99logger.String("username", result.Account.Username)))
		return
	}

	// Printed here and held nowhere: Kratos stores a hash, and this process
	// forgets the value when the function returns.
	logger.Log.Warn(s99logger.NewEvent(logger.EventBootstrapAdminCreated,
		s99logger.String("username", result.Account.Username),
		s99logger.String("password", result.Password)))
	s.printBootstrap(result)
}

// printBootstrap puts the generated password on stdout, where the log's own
// sink does not reach.
func (s *server) printBootstrap(result identity.BootstrapResult) {
	out, say := s.print(), s.say()
	out.Blank()
	out.Line(out.S.Title, "%s", messages.CliBootstrapTitle(say))
	out.Blank()
	out.Fields(
		ui.Field{Label: messages.CliBootstrapUsername(say), Value: result.Account.Username},
		ui.Field{Label: messages.CliBootstrapPassword(say), Value: result.Password},
	)
	out.Blank()
	out.Panel(out.S.Warn.Render(messages.CliBootstrapWarning(say)))
	out.Blank()
}

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

// api/v1/server.go

// Package v1 serves version 1 of the Goptivum HTTP contract, plus the one
// unversioned route a client reads before it knows the version.
//
// The route wiring is hand-written on purpose. The generated
// RegisterHandlers puts every operation on one router, and this API needs two
// different size caps: a JSON cap for most endpoints and a larger upload cap
// for the ones that carry a document (create, upload, import). One router
// cannot carry both.
package v1

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/smegg99/s99ab/api/apierr"
	"github.com/smegg99/s99ab/api/apihealth"
	"github.com/smegg99/s99ab/api/apimount"
	"github.com/smegg99/s99ab/auth/kratosauth"

	"github.com/smegg99/goptivum/backend/api/v1/gen"
	"github.com/smegg99/goptivum/backend/common/messages"
	"github.com/smegg99/goptivum/backend/core/lockout"
	"github.com/smegg99/goptivum/backend/identity"
)

// MaxUploadSize bounds an import or a document upload when the configuration
// says nothing. A real Optivum file is tens of kilobytes and a document about
// 25 KB compressed, so this is generous headroom rather than a limit anyone
// meets; apimount leaves it uncapped at zero, which is what this exists to
// avoid.
const MaxUploadSize = 16 << 20

// MaxRequestSize bounds a JSON request. The plan endpoints carry a name and a
// school year, nothing more.
const MaxRequestSize = 1 << 20

// BuildInfo is what this build is, reported honestly: empty means unknown.
type BuildInfo struct {
	Version   string
	Commit    string
	BuildTime string
}

// readinessTimeout bounds every readiness probe together.
const readinessTimeout = 5 * time.Second

// Readiness reports whether a dependency is reachable.
type Readiness func(context.Context) error

// Options carries what the server needs that is not a route.
type Options struct {
	// Build identifies this binary to clients and to support.
	Build BuildInfo
	// MinClientVersion is the oldest client this server accepts.
	MinClientVersion string
	// Addresses reads the other addresses this same server answers on,
	// published at /api/info so a client that reached it locally learns the
	// public one. Read per request; nil publishes none.
	Addresses func() []string
	// MaxSolveSeconds reads the maximum answering node ceiling; failures return zero.
	MaxSolveSeconds func(context.Context) float64
	// Handlers implements the generated contract. Required.
	Handlers gen.StrictServerInterface
	// Database, Storage and Identity are readiness probes; a nil one is not
	// probed.
	Database Readiness
	Storage  Readiness
	Identity Readiness
	// Kratos is nil until identities exist. Every guarded route stays
	// unregistered while it is, because the alternative is mounting them open.
	Kratos *kratosauth.Client
	// Accounts is the Kratos admin API. Without it the first-sign-in guard can
	// refuse but never clear, so an account that changes its password stays
	// refused.
	Accounts *identity.Admin
	// KratosPublicURL is Kratos's own address, which the /kratos/* proxy
	// forwards to. Empty mounts no proxy.
	KratosPublicURL string
	// KratosRateLimitPerMinute bounds the proxy, which apimount's limiter
	// never sees. Zero disables it, which a login route should never be.
	KratosRateLimitPerMinute int
	// Lockout counts failed sign-ins through the proxy and holds back a name
	// or an address that failed too often. Nil counts nothing.
	Lockout *lockout.Guard
	// TrustedProxies defaults to loopback inside apimount.
	TrustedProxies     []string
	RateLimitPerMinute int
	// MaxRequestSize and MaxUploadSize fall back to this package's own limits.
	// Zero would leave apimount uncapped, which is the one value neither may
	// have.
	MaxRequestSize int64
	MaxUploadSize  int64
	// DemoSchool registers the development-only POST /plans/demo. Off, the
	// route does not exist.
	DemoSchool bool
}

// Server owns the engine and the groups apimount built.
type Server struct {
	engine *gin.Engine
	groups apimount.Groups
	// guarded reports whether the versioned routes are registered, which they
	// are not without Kratos.
	guarded bool
	// demo reports whether the demo school route is registered.
	demo bool
	// identity reports whether the /kratos/* proxy is mounted.
	identity bool
	// accounts is the Kratos admin API, and is what lets the first-sign-in
	// guard tell an account that changed its password from one that did not.
	accounts *identity.Admin
}

func New(options Options) (*Server, error) {
	if options.Handlers == nil {
		return nil, fmt.Errorf("api: no handlers")
	}

	gin.SetMode(gin.ReleaseMode)
	engine := gin.New()
	engine.HandleMethodNotAllowed = true
	// A recovered panic still answers with the envelope, never an empty 500.
	engine.Use(gin.CustomRecovery(func(c *gin.Context, _ any) {
		apierr.Respond(c, apierr.New(apierr.Internal, messages.KeyErrorInternal))
	}))
	engine.NoMethod(func(c *gin.Context) {
		apierr.Respond(c, apierr.New(apierr.NotFound, "error.methodNotAllowed"))
	})
	engine.NoRoute(func(c *gin.Context) {
		apierr.Respond(c, apierr.New(apierr.NotFound, "error.notFound"))
	})

	// Before Mount, and on the bare engine. Mount attaches the session
	// middleware and the rate limiter to the group it returns, never to the
	// engine's own routes, so a sibling route structurally cannot inherit
	// them. That is what lets a client discover this server while Kratos and
	// Postgres are down. Moving this inside the group would silently make
	// discovery depend on identity.
	server := &Server{engine: engine, accounts: options.Accounts}
	engine.GET("/api/info", server.handleInfo(options))

	// Also before Mount, and for the same reason plus one of its own: the
	// route that issues a session cannot require one.
	if options.KratosPublicURL != "" {
		if err := server.registerKratosProxy(
			options.KratosPublicURL, options.KratosRateLimitPerMinute, options.Lockout); err != nil {
			return nil, err
		}
	}

	checks := []apihealth.Check{}
	if options.Database != nil {
		checks = append(checks, apihealth.Check{Name: "database", Fn: options.Database})
	}
	if options.Storage != nil {
		checks = append(checks, apihealth.Check{Name: "storage", Fn: options.Storage})
	}
	if options.Identity != nil {
		checks = append(checks, apihealth.Check{Name: "kratos", Fn: options.Identity})
	}

	groups, err := apimount.Mount(engine, apimount.Options{
		RateLimitPerMinute: options.RateLimitPerMinute,
		Kratos:             options.Kratos,
		MaxRequestSize:     orDefault(options.MaxRequestSize, MaxRequestSize),
		MaxUploadSize:      orDefault(options.MaxUploadSize, MaxUploadSize),
		TrustedProxies:     options.TrustedProxies,
		Health: apihealth.Options{
			Version:   options.Build.Version,
			Commit:    options.Build.Commit,
			BuildTime: options.Build.BuildTime,
			Checks:    checks,
			// A hung dependency must not hang the probe that exists to notice
			// it. Without this every check waits on its own client's timeout,
			// and an orchestrator reading /ready waits with it.
			Timeout: readinessTimeout,
		},
	})
	if err != nil {
		return nil, fmt.Errorf("api: mount: %w", err)
	}
	server.groups = groups
	server.register(options.Handlers, options.DemoSchool)
	return server, nil
}

// badRequest is a request the generated binding could not read. apierr has no
// kind for it, because a well-behaved client never sends one.
var badRequest = apierr.Kind{Code: "bad_request", Status: http.StatusBadRequest}

// strictOptions make every failure the generated boundary answers by itself an
// envelope. Its defaults write {"msg": ...}, a second error shape no client
// is generated against.
func strictOptions() gen.StrictGinServerOptions {
	return gen.StrictGinServerOptions{
		RequestErrorHandlerFunc: func(c *gin.Context, err error) {
			apierr.Respond(c, apierr.Wrap(err, badRequest, messages.KeyErrorBadRequest))
		},
		// Unknown errors become 500 error.internal with the cause logged, not
		// sent.
		HandlerErrorFunc:         func(c *gin.Context, err error) { apierr.Respond(c, err) },
		ResponseErrorHandlerFunc: func(c *gin.Context, err error) { apierr.Respond(c, err) },
	}
}

// bindError answers a path or query value the wrapper could not bind, such as
// page=abc. Without it the wrapper's nil handler is called and gin answers an
// empty 500.
func bindError(c *gin.Context, err error, _ int) {
	apierr.Respond(c, apierr.Wrap(err, badRequest, messages.KeyErrorBadRequest))
}

// register wires each operation onto the group it needs.
//
// Uploads alone is not the upload half of Guarded. An application reaching for
// that obvious pair mounts its file routes open to anyone, and that is the one
// mistake here that does not show up in testing: the routes work, and they
// work for everybody. So without Kratos the routes are simply absent.
func (s *Server) register(handlers gen.StrictServerInterface, demo bool) {
	if s.groups.Guarded == nil || s.groups.GuardedUploads == nil {
		return
	}
	wrapper := gen.ServerInterfaceWrapper{
		Handler:      gen.NewStrictHandlerWithOptions(requestScoped{next: handlers}, nil, strictOptions()),
		ErrorHandler: bindError,
	}

	// Before any route is registered, so nothing added later can miss it. Both
	// groups, because GuardedUploads is a sibling and not a child: attaching
	// this to one of them would leave import and upload open to an account
	// that has not changed its generated password.
	s.groups.Guarded.Use(requirePasswordChanged(s.accounts))
	s.groups.GuardedUploads.Use(requirePasswordChanged(s.accounts))

	s.groups.Guarded.GET("/plans", wrapper.ListPlans)
	s.groups.Guarded.GET("/plans/:planId", wrapper.GetPlan)
	s.groups.Guarded.PATCH("/plans/:planId", wrapper.UpdatePlan)
	s.groups.Guarded.DELETE("/plans/:planId", wrapper.DeletePlan)
	s.groups.Guarded.POST("/plans/:planId/restore", wrapper.RestorePlan)
	// Any signed-in account copies any live plan: a fork, or a duplicate of
	// its own.
	s.groups.Guarded.POST("/plans/:planId/fork", wrapper.ForkPlan)
	s.groups.Guarded.GET("/trash", wrapper.ListTrash)
	// Every account reads every plan, so every account reads what changed.
	s.groups.Guarded.GET("/changes", wrapper.ListChanges)
	s.groups.Guarded.GET("/plans/:planId/versions", wrapper.ListVersions)
	// A download returns bytes rather than accepting them, and the size cap
	// bounds requests, not responses.
	s.groups.Guarded.GET("/plans/:planId/versions/:version", wrapper.DownloadVersion)
	s.groups.Guarded.PUT("/plans/:planId/versions/:version/checkpoint", wrapper.NameCheckpoint)
	s.groups.Guarded.DELETE("/plans/:planId/versions/:version/checkpoint", wrapper.DeleteCheckpoint)
	s.groups.Guarded.POST("/plans/:planId/versions/:version/checkpoint/restore", wrapper.RestoreCheckpoint)
	s.groups.Guarded.GET("/plans/:planId/versions/:version/check", wrapper.CheckVersion)
	s.groups.Guarded.GET("/plans/:planId/results", wrapper.ListResults)
	s.groups.Guarded.GET("/plans/:planId/results/:resultId", wrapper.DownloadResult)
	s.groups.Guarded.PATCH("/plans/:planId/results/:resultId", wrapper.UpdateResult)
	s.groups.Guarded.DELETE("/plans/:planId/results/:resultId", wrapper.DeleteResult)
	s.groups.Guarded.POST("/plans/:planId/results/:resultId/restore", wrapper.RestoreResult)
	s.groups.Guarded.GET("/plans/:planId/results/:resultId/check", wrapper.CheckResult)
	// Exporting is reading: any signed-in account exports any live plan. Like
	// a download, the file is a response, which the size cap does not bound.
	s.groups.Guarded.GET("/plans/:planId/versions/:version/export/:format", wrapper.ExportVersion)
	s.groups.Guarded.GET("/plans/:planId/results/:resultId/export/:format", wrapper.ExportResult)
	// A planner's Rules station reads it, so it needs a session and no role.
	s.groups.Guarded.GET("/rules", wrapper.GetRuleCatalogue)
	// The school shares its profiles, so they need a session and no role; the
	// handlers let only a profile's creator and administrators change one.
	s.groups.Guarded.GET("/rules/profiles", wrapper.ListRuleProfiles)
	s.groups.Guarded.POST("/rules/profiles", wrapper.CreateRuleProfile)
	s.groups.Guarded.PATCH("/rules/profiles/:profileId", wrapper.UpdateRuleProfile)
	s.groups.Guarded.DELETE("/rules/profiles/:profileId", wrapper.DeleteRuleProfile)
	// Every account arranges its own Library; the handlers read and save only
	// the caller's.
	s.groups.Guarded.GET("/account/library", wrapper.GetAccountLibrary)
	s.groups.Guarded.PUT("/account/library", wrapper.SaveAccountLibrary)

	// createPlan may carry a whole first document, so it takes the upload cap.
	s.groups.GuardedUploads.POST("/plans", wrapper.CreatePlan)
	s.groups.GuardedUploads.POST("/plans/:planId/versions", wrapper.UploadVersion)
	s.groups.GuardedUploads.POST("/plans/:planId/results", wrapper.UploadResult)
	s.groups.GuardedUploads.POST("/import", wrapper.ImportPlan)
	// The preview reads the same file, so it takes the same cap.
	s.groups.GuardedUploads.POST("/import/preview", wrapper.PreviewImport)

	// Development only. Absent unless the configuration asks for it, so a real
	// school's server has no route that fetches a synthetic school.
	if demo {
		s.groups.Guarded.POST("/plans/demo", wrapper.CreateDemoPlan)
		s.demo = true
	}

	// RequireMeta reads metadata_public and never traits, and answers 404 to a
	// signed-in caller who fails it: a 403 would confirm the back office is
	// real to a planner probing for it.
	admin := s.groups.Guarded.Group("", kratosauth.RequireMeta("role", identity.RoleAdmin))
	admin.GET("/users", wrapper.ListUsers)
	admin.POST("/users", wrapper.CreateUser)
	admin.GET("/users/:userId", wrapper.GetUser)
	admin.PATCH("/users/:userId", wrapper.UpdateUser)
	admin.POST("/users/:userId/password", wrapper.ResetUserPassword)
	admin.GET("/users/:userId/sessions", wrapper.ListUserSessions)
	admin.DELETE("/users/:userId/sessions", wrapper.RevokeUserSessions)
	admin.DELETE("/users/:userId/sessions/:sessionId", wrapper.RevokeUserSession)
	admin.GET("/security/lockout", wrapper.GetLockout)
	admin.PATCH("/security/lockout", wrapper.UpdateLockout)
	admin.GET("/security/password-rules", wrapper.GetPasswordRules)
	admin.GET("/nodes", wrapper.ListNodes)
	admin.POST("/nodes", wrapper.PairNode)
	admin.GET("/nodes/:nodeId", wrapper.GetNode)
	admin.PATCH("/nodes/:nodeId", wrapper.UpdateNode)
	admin.DELETE("/nodes/:nodeId", wrapper.RevokeNode)
	admin.GET("/nodes/:nodeId/status", wrapper.GetNodeStatus)
	admin.GET("/nodes/:nodeId/jobs", wrapper.ListNodeJobs)
	admin.GET("/server/settings", wrapper.GetServerSettings)
	admin.PATCH("/server/settings", wrapper.UpdateServerSettings)
	admin.GET("/storage", wrapper.GetStorage)
	admin.POST("/storage/trash/empty", wrapper.EmptyTrash)
	admin.POST("/storage/backups/trim", wrapper.TrimBackups)
	admin.POST("/storage/sweep", wrapper.SweepObjects)
	admin.GET("/logs", wrapper.ListLogs)
	admin.GET("/audit", wrapper.ListAudit)
	admin.PUT("/plans/:planId/owner", wrapper.TransferPlan)

	// The school's own key needs a session and no role: one key for the whole
	// school was the decision, so a planner who cannot reach it cannot open
	// the school's own files.
	s.groups.Guarded.GET("/school/key", wrapper.GetSchoolKey)
	s.groups.Guarded.POST("/school/key/release", wrapper.ReleaseSchoolKey)

	// Solving is a planner's work, so these sit on Guarded rather than behind
	// the role check.
	s.groups.Guarded.GET("/plans/:planId/jobs", wrapper.ListJobs)
	s.groups.Guarded.POST("/plans/:planId/jobs", wrapper.CreateJob)
	s.groups.Guarded.GET("/jobs", wrapper.ListAllJobs)
	s.groups.Guarded.GET("/jobs/:jobId", wrapper.GetJob)
	s.groups.Guarded.DELETE("/jobs/:jobId", wrapper.CancelJob)
	s.groups.Guarded.GET("/jobs/:jobId/preview", wrapper.GetJobPreview)
	if jobs := s.jobsHandler(handlers); jobs != nil {
		// Hand-wired: it writes text/event-stream, and the strict handler's
		// one-response-object shape cannot carry a stream.
		s.groups.Guarded.GET("/jobs/:jobId/events", jobs.watch)
	}

	s.guarded = true
}

// jobsHandler reaches the solving half, which owns the one route that is not
// generated.
func (s *Server) jobsHandler(handlers gen.StrictServerInterface) *Jobs {
	if combined, ok := handlers.(*Handlers); ok {
		return combined.Jobs
	}
	return nil
}

// orDefault keeps a size cap from ever being zero, which apimount reads as
// uncapped.
func orDefault(value, fallback int64) int64 {
	if value <= 0 {
		return fallback
	}
	return value
}

// Handler is the engine, for http.Server and for tests.
func (s *Server) Handler() http.Handler { return s.engine }

// ServesPlans reports whether the versioned routes are registered. They are
// not until identities exist.
func (s *Server) ServesPlans() bool { return s.guarded }

// ServesIdentity reports whether the /kratos/* proxy is mounted.
func (s *Server) ServesIdentity() bool { return s.identity }

// ServesDemo reports whether the demo school route is registered.
func (s *Server) ServesDemo() bool { return s.demo }

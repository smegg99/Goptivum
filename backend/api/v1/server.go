// api/v1/server.go

// Package v1 serves version 1 of the Goptivum HTTP contract, plus the one
// unversioned route a client reads before it knows the version.
//
// The route wiring is hand-written on purpose. The generated
// RegisterHandlers puts every operation on one router, and this API needs two
// different size caps: a JSON cap for the plan endpoints and a larger upload
// cap for import. One router cannot carry both.
package v1

import (
	"context"
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/smegg99/s99ab/api/apierr"
	"github.com/smegg99/s99ab/api/apihealth"
	"github.com/smegg99/s99ab/api/apimount"
	"github.com/smegg99/s99ab/auth/kratosauth"

	"github.com/smegg99/goptivum/backend/api/v1/gen"
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

// Readiness reports whether a dependency is reachable.
type Readiness func(context.Context) error

// Options carries what the server needs that is not a route.
type Options struct {
	// Build identifies this binary to clients and to support.
	Build BuildInfo
	// MinClientVersion is the oldest client this server accepts.
	MinClientVersion string
	// Handlers implements the generated contract. Required.
	Handlers gen.StrictServerInterface
	// Database and Storage are readiness probes; a nil one is not probed.
	Database Readiness
	Storage  Readiness
	// Kratos is nil until identities exist. Every guarded route stays
	// unregistered while it is, because the alternative is mounting them open.
	Kratos *kratosauth.Client
	// TrustedProxies defaults to loopback inside apimount.
	TrustedProxies     []string
	RateLimitPerMinute int
	// MaxRequestSize and MaxUploadSize fall back to this package's own limits.
	// Zero would leave apimount uncapped, which is the one value neither may
	// have.
	MaxRequestSize int64
	MaxUploadSize  int64
}

// Server owns the engine and the groups apimount built.
type Server struct {
	engine *gin.Engine
	groups apimount.Groups
	// guarded reports whether the versioned routes are registered, which they
	// are not without Kratos.
	guarded bool
}

// New wires the engine. The order here is load-bearing and the comments say
// why; see also the note on Mount in ref-s99ab.md.
func New(options Options) (*Server, error) {
	if options.Handlers == nil {
		return nil, fmt.Errorf("api: no handlers")
	}

	gin.SetMode(gin.ReleaseMode)
	engine := gin.New()
	engine.HandleMethodNotAllowed = true
	engine.Use(gin.Recovery())
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
	server := &Server{engine: engine}
	engine.GET("/api/info", server.handleInfo(options))

	checks := []apihealth.Check{}
	if options.Database != nil {
		checks = append(checks, apihealth.Check{Name: "postgres", Fn: options.Database})
	}
	if options.Storage != nil {
		checks = append(checks, apihealth.Check{Name: "storage", Fn: options.Storage})
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
		},
	})
	if err != nil {
		return nil, fmt.Errorf("api: mount: %w", err)
	}
	server.groups = groups
	server.register(options.Handlers)
	return server, nil
}

// register wires each operation onto the group it needs.
//
// Uploads alone is not the upload half of Guarded. An application reaching for
// that obvious pair mounts its file routes open to anyone, and that is the one
// mistake here that does not show up in testing: the routes work, and they
// work for everybody. So without Kratos the routes are simply absent.
func (s *Server) register(handlers gen.StrictServerInterface) {
	if s.groups.Guarded == nil || s.groups.GuardedUploads == nil {
		return
	}
	wrapper := gen.ServerInterfaceWrapper{Handler: gen.NewStrictHandler(handlers, nil)}

	s.groups.Guarded.GET("/plans", wrapper.ListPlans)
	s.groups.Guarded.POST("/plans", wrapper.CreatePlan)
	s.groups.Guarded.GET("/plans/:planId", wrapper.GetPlan)
	s.groups.Guarded.DELETE("/plans/:planId", wrapper.DeletePlan)
	s.groups.Guarded.GET("/plans/:planId/versions", wrapper.ListVersions)
	// A download returns bytes rather than accepting them, and the size cap
	// bounds requests, not responses.
	s.groups.Guarded.GET("/plans/:planId/versions/:version", wrapper.DownloadVersion)

	s.groups.GuardedUploads.POST("/plans/:planId/versions", wrapper.UploadVersion)
	s.groups.GuardedUploads.POST("/import", wrapper.ImportPlan)
	s.guarded = true
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

// api/routes.go

package api

import (
	"github.com/gin-gonic/gin"

	arrangov1 "github.com/smegg99/arrango/backend/gen/arrangov1"
	"github.com/smegg99/arrango/backend/jobs"
)

type Server struct {
	solver   arrangov1.SolverServiceClient
	registry *jobs.Registry
}

func NewServer(solver arrangov1.SolverServiceClient,
	registry *jobs.Registry) *Server {
	return &Server{solver: solver, registry: registry}
}

func (s *Server) Router() *gin.Engine {
	router := gin.New()
	router.Use(gin.Recovery())
	apiGroup := router.Group("/api")
	{
		apiGroup.GET("/school", s.handleGetSchool)
		apiGroup.POST("/validate", s.handleValidate)
		apiGroup.POST("/score", s.handleScore)
		apiGroup.POST("/import", s.handleImport)
		apiGroup.POST("/export", s.handleExport)
		apiGroup.POST("/jobs", s.handleStartJob)
		apiGroup.GET("/jobs/:id", s.handleGetJob)
		apiGroup.POST("/jobs/:id/cancel", s.handleCancelJob)
		apiGroup.GET("/jobs/:id/events", s.handleJobEvents)

		// Simple, name-based API — see docs/API.md.
		apiGroup.POST("/simple/solve", s.handleSimpleSolve)
		apiGroup.GET("/simple/jobs/:id", s.handleSimpleJob)
		apiGroup.POST("/simple/jobs/:id/cancel", s.handleCancelJob)
	}
	return router
}

// api/simple.go

package api

import (
	"context"
	"net/http"

	"github.com/gin-gonic/gin"
	"google.golang.org/protobuf/encoding/protojson"

	arrangov1 "github.com/smegg99/arrango/backend/gen/arrangov1"
	"github.com/smegg99/arrango/backend/jobs"
	"github.com/smegg99/arrango/backend/simple"
)

// simpleSolveRequest is the raw demand data plus solver knobs.
type simpleSolveRequest struct {
	simple.School
	TimeLimitSeconds float64 `json:"timeLimitSeconds"`
	Workers          uint32  `json:"workers"`
	RandomSeed       int64   `json:"randomSeed"`
	FromScratch      bool    `json:"fromScratch"`
}

func (s *Server) handleSimpleSolve(c *gin.Context) {
	var body simpleSolveRequest
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	model, err := simple.ToModel(body.School)
	if err != nil {
		c.JSON(http.StatusUnprocessableEntity, gin.H{"error": err.Error()})
		return
	}
	params := &arrangov1.SolveParams{
		MaxTimeSeconds: body.TimeLimitSeconds,
		NumWorkers:     body.Workers,
		RandomSeed:     body.RandomSeed,
		FromScratch:    body.FromScratch,
	}
	if params.MaxTimeSeconds <= 0 {
		params.MaxTimeSeconds = 60
	}

	ctx, cancel := context.WithCancel(context.Background())
	job := s.registry.Create(cancel)
	job.SetModel(model)
	go jobs.Run(ctx, s.solver,
		&arrangov1.SolveRequest{Model: model, Params: params}, job)
	c.JSON(http.StatusOK, gin.H{"id": job.ID})
}

func (s *Server) handleSimpleJob(c *gin.Context) {
	job, ok := s.registry.Get(c.Param("id"))
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "unknown job"})
		return
	}
	response := gin.H{"id": job.ID, "state": job.State()}
	if last := job.LastUpdate(); last != nil && job.Model() != nil {
		update := &arrangov1.SolveUpdate{}
		if err := protojson.Unmarshal(last, update); err == nil {
			response["result"] = simple.RenderResult(job.Model(), update)
		}
	}
	c.JSON(http.StatusOK, response)
}

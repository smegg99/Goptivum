// api/handlers.go

package api

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"google.golang.org/protobuf/encoding/protojson"

	arrangov1 "github.com/smegg99/arrango/backend/gen/arrangov1"
	"github.com/smegg99/arrango/backend/jobs"
	"github.com/smegg99/arrango/backend/optivum"
)

var marshaler = protojson.MarshalOptions{EmitUnpopulated: true}

type startJobRequest struct {
	Preset string          `json:"preset"`
	Seed   uint64          `json:"seed"`
	Model  json.RawMessage `json:"model"`
	Params json.RawMessage `json:"params"`
	Config json.RawMessage `json:"config"`
}

func presetFromString(preset string) arrangov1.DemoPreset {
	if v, ok := arrangov1.DemoPreset_value[preset]; ok {
		return arrangov1.DemoPreset(v)
	}
	return arrangov1.DemoPreset_DEMO_PRESET_PRODUCTION
}

func (s *Server) handleGetSchool(c *gin.Context) {
	seed, _ := strconv.ParseUint(c.Query("seed"), 10, 64)
	req := &arrangov1.DemoRequest{
		Preset: presetFromString(c.Query("preset")),
		Seed:   seed,
	}
	model, err := s.solver.GetDemoSchool(c.Request.Context(), req)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
		return
	}
	payload, err := marshaler.Marshal(model)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.Data(http.StatusOK, "application/json", payload)
}

func (s *Server) handleValidate(c *gin.Context) {
	body, err := io.ReadAll(c.Request.Body)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	req := &arrangov1.ValidateRequest{}
	if err := protojson.Unmarshal(body, req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	report, err := s.solver.Validate(c.Request.Context(), req)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
		return
	}
	payload, err := marshaler.Marshal(report)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.Data(http.StatusOK, "application/json", payload)
}

func (s *Server) handleScore(c *gin.Context) {
	body, err := io.ReadAll(c.Request.Body)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	req := &arrangov1.ScoreRequest{}
	if err := protojson.Unmarshal(body, req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	report, err := s.solver.Score(c.Request.Context(), req)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
		return
	}
	payload, err := marshaler.Marshal(report)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.Data(http.StatusOK, "application/json", payload)
}

func (s *Server) handleStartJob(c *gin.Context) {
	var body startJobRequest
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	req := &arrangov1.SolveRequest{Model: &arrangov1.SchoolModel{}}
	if len(body.Model) > 0 {
		if err := protojson.Unmarshal(body.Model, req.Model); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "model: " + err.Error()})
			return
		}
	} else {
		model, err := s.solver.GetDemoSchool(c.Request.Context(),
			&arrangov1.DemoRequest{
				Preset: presetFromString(body.Preset),
				Seed:   body.Seed,
			})
		if err != nil {
			c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
			return
		}
		req.Model = model
	}
	req.Params = &arrangov1.SolveParams{}
	if len(body.Params) > 0 {
		if err := protojson.Unmarshal(body.Params, req.Params); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "params: " + err.Error()})
			return
		}
	}
	// Optional runtime SolverConfig: forwarded verbatim to the solver, which
	// resolves and echoes it. The backend interprets none of its fields.
	if len(body.Config) > 0 {
		req.Config = &arrangov1.SolverConfig{}
		if err := protojson.Unmarshal(body.Config, req.Config); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "config: " + err.Error()})
			return
		}
	}

	ctx, cancel := context.WithCancel(context.Background())
	job := s.registry.Create(cancel)
	job.SetModel(req.Model)
	go jobs.Run(ctx, s.solver, req, job)

	modelJSON, err := marshaler.Marshal(req.Model)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"id":    job.ID,
		"model": json.RawMessage(modelJSON),
	})
}

func (s *Server) handleGetJob(c *gin.Context) {
	job, ok := s.registry.Get(c.Param("id"))
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "unknown job"})
		return
	}
	response := gin.H{"id": job.ID, "state": job.State()}
	if last := job.LastUpdate(); last != nil {
		response["lastUpdate"] = json.RawMessage(last)
	}
	c.JSON(http.StatusOK, response)
}

func (s *Server) handleCancelJob(c *gin.Context) {
	job, ok := s.registry.Get(c.Param("id"))
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "unknown job"})
		return
	}
	job.Cancel()
	// Give the stream reader a moment so the response reflects reality.
	deadline := time.Now().Add(2 * time.Second)
	for job.State() == jobs.StateRunning && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	c.JSON(http.StatusOK, gin.H{"id": job.ID, "state": job.State()})
}

// handleImport parses an uploaded Optivum HTML-export zip into a solvable
// model + imported snapshot, then has the solver validate the snapshot.
func (s *Server) handleImport(c *gin.Context) {
	file, _, err := c.Request.FormFile("archive")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "missing archive file: " + err.Error()})
		return
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, 64<<20))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	result, err := optivum.ParseZip(data)
	if err != nil {
		c.JSON(http.StatusUnprocessableEntity, gin.H{"error": err.Error()})
		return
	}
	validation, err := s.solver.Validate(c.Request.Context(),
		&arrangov1.ValidateRequest{Model: result.Model, Snapshot: result.Snapshot})
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
		return
	}
	modelJSON, err := marshaler.Marshal(result.Model)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	snapshotJSON, err := marshaler.Marshal(result.Snapshot)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	validationJSON, err := marshaler.Marshal(validation)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	warnings := result.Warnings
	if warnings == nil {
		warnings = []string{}
	}
	c.JSON(http.StatusOK, gin.H{
		"model":      json.RawMessage(modelJSON),
		"snapshot":   json.RawMessage(snapshotJSON),
		"validation": json.RawMessage(validationJSON),
		"warnings":   warnings,
		"summary":    result.Summary,
	})
}

// handleExport renders {model, snapshot} as a simplified Optivum-style
// HTML zip for download.
func (s *Server) handleExport(c *gin.Context) {
	body, err := io.ReadAll(c.Request.Body)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	req := &arrangov1.ValidateRequest{} // same shape: model + snapshot
	if err := protojson.Unmarshal(body, req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	data, err := optivum.ExportZip(req.Model, req.Snapshot)
	if err != nil {
		c.JSON(http.StatusUnprocessableEntity, gin.H{"error": err.Error()})
		return
	}
	c.Header("Content-Disposition", `attachment; filename="arrango-plan.zip"`)
	c.Data(http.StatusOK, "application/zip", data)
}

// api/sse.go

package api

import (
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"
)

// handleJobEvents streams solver updates as SSE `update` events, replaying
// the latest known state to late subscribers, and closes with a `done`
// event carrying the terminal job state.
func (s *Server) handleJobEvents(c *gin.Context) {
	job, ok := s.registry.Get(c.Param("id"))
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "unknown job"})
		return
	}

	c.Writer.Header().Set("Content-Type", "text/event-stream")
	c.Writer.Header().Set("Cache-Control", "no-cache")
	c.Writer.Header().Set("Connection", "keep-alive")

	updates, replay, unsubscribe := job.Subscribe()
	defer unsubscribe()

	write := func(event string, data []byte) bool {
		if _, err := fmt.Fprintf(c.Writer, "event: %s\ndata: %s\n\n",
			event, data); err != nil {
			return false
		}
		c.Writer.Flush()
		return true
	}

	if replay != nil {
		if !write("update", replay) {
			return
		}
	}
	for {
		select {
		case payload, open := <-updates:
			if !open {
				write("done", []byte(fmt.Sprintf(`{"state":%q}`, job.State())))
				return
			}
			if !write("update", payload) {
				return
			}
		case <-c.Request.Context().Done():
			return
		}
	}
}

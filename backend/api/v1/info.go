// api/v1/info.go

package v1

import (
	"net/http"
	"slices"

	"github.com/gin-gonic/gin"

	"github.com/smegg99/goptivum/backend/api/v1/gen"
)

// APIVersion is the versioned namespace this server serves.
const APIVersion = "v1"

// handleInfo answers the one frozen, unversioned route. It reads compiled-in
// values and settings in memory. The optional one-second ceiling query cannot
// turn a dependency failure into a discovery failure, so it still answers
// while Postgres, Kratos and object storage are all stopped. A client that
// cannot reach it knows the address is wrong; a client that reaches something
// else knows by product.
func (s *Server) handleInfo(options Options) gin.HandlerFunc {
	return func(c *gin.Context) {
		info := gen.Info{
			Product:          gen.Goptivum,
			ApiVersion:       APIVersion,
			MinClientVersion: options.MinClientVersion,
			ServerVersion:    options.Build.Version,
			// The cap apimount enforces, fixed when the server started: a
			// 413 on a declared length carries no details, so this is where a
			// client learns it.
			MaximumUploadBytes: pointerTo(orDefault(options.MaxUploadSize, MaxUploadSize)),
		}
		// Absent rather than empty when the school published none: an empty
		// array and a missing field mean the same thing to a client, and the
		// shorter one is what a deployment reachable at one address should
		// answer with. Read per request, so a change applies at once.
		if options.Addresses != nil {
			if addresses := slices.Clone(options.Addresses()); len(addresses) > 0 {
				info.Addresses = &addresses
			}
		}
		if options.MaxSolveSeconds != nil {
			info.MaxSolveSeconds = pointerTo(options.MaxSolveSeconds(c.Request.Context()))
		}
		c.JSON(http.StatusOK, info)
	}
}

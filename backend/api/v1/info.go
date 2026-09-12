// api/v1/info.go

package v1

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/smegg99/goptivum/backend/api/v1/gen"
)

// APIVersion is the versioned namespace this server serves.
const APIVersion = "v1"

// handleInfo answers the one frozen, unversioned route. It reads compiled-in
// values and touches no dependency, so it answers while Postgres, Kratos and
// object storage are all stopped. A client that cannot reach it knows the
// address is wrong; a client that reaches something else knows by product.
func (s *Server) handleInfo(options Options) gin.HandlerFunc {
	info := gen.Info{
		Product:          gen.Goptivum,
		ApiVersion:       APIVersion,
		MinClientVersion: options.MinClientVersion,
		ServerVersion:    options.Build.Version,
	}
	return func(c *gin.Context) {
		c.JSON(http.StatusOK, info)
	}
}

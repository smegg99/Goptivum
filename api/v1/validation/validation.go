// api/v1/validation/validation.go
package validation

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"smegg.me/goptivum/api/v1/types"
)

func RequestSizeLimit(maxSize int64) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxSize)
		c.Next()

		if c.Writer.Status() == http.StatusRequestEntityTooLarge {
			c.AbortWithStatusJSON(http.StatusRequestEntityTooLarge, types.ErrorResponse{
				Error: "request body too large",
			})
			return
		}
	}
}

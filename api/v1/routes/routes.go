// api/v1/routes/routes.go
package routes

import (
	"github.com/gin-gonic/gin"
)

func Initialize(router *gin.Engine) {
	apiV1 := router.Group("/api/v1")

	SetupAuthRoutes(router, apiV1)
}

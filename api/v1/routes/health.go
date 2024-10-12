// routes/health.go
package routes

import (
	"smuggr.xyz/optivum-bsf/api/v1/handlers"

	"github.com/gin-gonic/gin"
)

func SetupHealthRoutes(router *gin.Engine, rootGroup *gin.RouterGroup) {
	healthGroup := rootGroup.Group("/health")
	{
		healthGroup.GET("/ping", handlers.PingHandler)
	}
}
// routes/routes.go
package routes

import (
	"os"

	"smuggr.xyz/optivum-bsf/api/v1/handlers"

	"github.com/didip/tollbooth"
	"github.com/didip/tollbooth_gin"
	"github.com/gin-contrib/static"
	"github.com/gin-gonic/gin"
)

func Initialize(defaultRouter *gin.Engine, refreshChan chan string) {
	defaultLimiter := tollbooth.NewLimiter(1, nil)
	defaultRouter.Use(static.Serve("/", static.LocalFile(os.Getenv("DIST_PATH"), false)))

	rootGroup := defaultRouter.Group("/api/v1")
	rootGroup.Use(tollbooth_gin.LimitHandler(defaultLimiter))

	handlers.Initialize()

	SetupGenericRoutes(defaultRouter, rootGroup, refreshChan)
	SetupScheduleRoutes(defaultRouter, rootGroup)
	SetupWeatherRoutes(defaultRouter, rootGroup)

	defaultRouter.NoRoute(func(c *gin.Context) {
		c.File(os.Getenv("DIST_PATH") + "/index.html")
	})
}

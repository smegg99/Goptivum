// api/v1/api.go
package v1

import (
	"fmt"
	"os"
	"strconv"

	"smuggr.xyz/goptivum/api/v1/routes"
	"smuggr.xyz/goptivum/api/v1/middleware"
	"smuggr.xyz/goptivum/common/config"
	"smuggr.xyz/goptivum/common/models"

	"github.com/gin-contrib/cors"
	"github.com/gin-contrib/gzip"
	"github.com/gin-gonic/gin"
)

var DefaultRouter *gin.Engine
var Config *config.APIConfig

func Initialize(scheduleChannels *models.ScheduleChannels) chan error {
	fmt.Println("initializing api/v1")

	Config = &config.Global.API
	gin.SetMode(os.Getenv("GIN_MODE"))

	DefaultRouter = gin.Default()
	DefaultRouter.Use(middleware.NormalizeTrailingSlashMiddleware())
	// DefaultRouter.UseRawPath = true
	// DefaultRouter.RedirectTrailingSlash = false

	DefaultRouter.Use(cors.New(cors.Config{
		AllowOrigins:     []string{"http://localhost:3000", "http://localhost:3002", "http://localhost:3001", "https://zsem.smuggr.xyz"},
		AllowMethods:     []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
		AllowHeaders:     []string{"Origin", "Content-Type", "X-Auth-Token"},
		ExposeHeaders:    []string{"Content-Length"},
		AllowCredentials: true,
	}))

	DefaultRouter.Use(gzip.Gzip(gzip.DefaultCompression))
	routes.Initialize(DefaultRouter, scheduleChannels, &models.OtherChannels{
		Clients: make(chan int64),
	})

	errCh := make(chan error)
	go func() {
		err := DefaultRouter.Run(":" + strconv.Itoa(int(Config.Port)))
		errCh <- err
	}()

	return errCh
}

// api/v1/api.go
package v1

import (
	"smegg.me/goptivum/api/v1/middleware"
	"smegg.me/goptivum/api/v1/routes"
	"smegg.me/goptivum/api/v1/validation"
	"smegg.me/goptivum/common/config"
	"smegg.me/goptivum/common/logger"

	"github.com/gin-contrib/cors"
	"github.com/gin-contrib/gzip"
	"github.com/gin-gonic/gin"
	"github.com/spf13/viper"
	swaggerFiles "github.com/swaggo/files"
	ginSwagger "github.com/swaggo/gin-swagger"
)

var DefaultRouter *gin.Engine

// @title Goptivum API
// @version 1.0
// @description API for Goptivum. Authentication uses JWT Bearer tokens. Include the access token in the Authorization header as 'Bearer {token}' for authenticated endpoints.
// @host localhost:3001
// @BasePath /api/v1
// @schemes http https
// @securityDefinitions.apikey BearerAuth
// @in header
// @name Authorization
// @description Type "Bearer" followed by a space and the access token.
func Initialize() chan error {
	logger.Info("initializing api/v1")
	mode := viper.GetString("GIN_MODE")
	gin.SetMode(mode)
	logger.Infof("gin mode set to: %s", gin.Mode())

	DefaultRouter = gin.Default()

	DefaultRouter.Use(validation.RequestSizeLimit(config.Global.API.MaxRequestSize))
	DefaultRouter.Use(middleware.SecurityMiddleware())
	DefaultRouter.Use(middleware.RateLimitMiddleware(config.Global.API.RateLimitPerMinute))

	DefaultRouter.Use(middleware.RequestLoggingMiddleware())
	DefaultRouter.Use(middleware.ErrorLoggingMiddleware())

	DefaultRouter.Use(middleware.NormalizeTrailingSlashMiddleware())
	// DefaultRouter.UseRawPath = true
	// DefaultRouter.RedirectTrailingSlash = false

	DefaultRouter.Use(cors.New(middleware.CORSConfig(config.Global.API.AllowOrigins)))

	DefaultRouter.Use(gzip.Gzip(gzip.DefaultCompression))
	routes.Initialize(DefaultRouter)

	if gin.Mode() == gin.DebugMode || viper.GetBool("ENABLE_SWAGGER") {
		logger.Info("swagger ui enabled at /swagger/index.html")
		url := ginSwagger.URL("/swagger/doc.json")
		DefaultRouter.GET("/swagger/*any", ginSwagger.WrapHandler(swaggerFiles.Handler, url))
	} else {
		logger.Info("swagger ui disabled (set GIN_MODE=debug or ENABLE_SWAGGER=true to enable)")
	}

	apiPort := viper.GetString("API_PORT")
	logger.Infof("starting api server on port %s", apiPort)

	errCh := make(chan error)
	go func() {
		err := DefaultRouter.Run(":" + apiPort)
		if err != nil {
			logger.Errorf("api server error: %v", err)
		}
		errCh <- err
	}()

	return errCh
}

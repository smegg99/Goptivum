// routes/atmosphere.go
package routes

import (
	"smuggr.xyz/optivum-bsf/api/v1/handlers"
	"github.com/gin-gonic/gin"
)

func SetupWeatherRoutes(router *gin.Engine, rootGroup *gin.RouterGroup) {
	weatherGroup := rootGroup.Group("/weather")
	{
		weatherGroup.GET("/forecast", handlers.WeatherForecastHandler)
		weatherGroup.GET("/current", handlers.CurrentWeatherHandler)
	}

	airGroup := rootGroup.Group("/air")
	{
		airGroup.GET("/current", handlers.CurrentAirPollutionHandler)
	}
}

// routes/atmosphere.go
package routes

import (
	"github.com/gin-gonic/gin"
	"smuggr.xyz/goptivum/api/v1/handlers"
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

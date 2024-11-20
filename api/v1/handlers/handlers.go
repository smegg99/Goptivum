// handlers/handlers.go
package handlers

import (
	"fmt"
	"net/http"
	"os"
	"strings"

	"smuggr.xyz/goptivum/common/config"
	"smuggr.xyz/goptivum/common/models"
	"smuggr.xyz/goptivum/common/utils"

	"github.com/gin-gonic/gin"
	"google.golang.org/protobuf/proto"
)

var Config *config.APIConfig
var ScraperConfig *config.ScraperConfig

func Respond(c *gin.Context, code int, data interface{}) {
	accept := c.GetHeader("Accept")
	switch {
	case strings.Contains(accept, "application/protobuf"):
		protoMsg, ok := data.(proto.Message)
		if !ok {
			c.ProtoBuf(http.StatusInternalServerError, models.APIResponse{
				Message: "internal server error",
				Success: false,
			})
			return
		}
		c.ProtoBuf(code, protoMsg)
	case strings.Contains(accept, "application/json"):
		fallthrough
	default:
		c.JSON(code, data)
	}
}

func PingHandler(c *gin.Context) {
	Respond(c, http.StatusOK, models.APIResponse{
		Message: "pong",
		Success: true,
	})
}

func APIHealthHandler(c *gin.Context) {
	scraperHealthy := true
	weatherHealthy := true

	if !utils.CheckURL(ScraperConfig.BaseUrl) {
		scraperHealthy = false
	}

	url := fmt.Sprintf(ScraperConfig.BaseUrl + ScraperConfig.Endpoints.DivisionsList)
	if !utils.CheckURL(url) {
		scraperHealthy = false
	}

	url = fmt.Sprintf(ScraperConfig.BaseUrl + ScraperConfig.Endpoints.TeachersList)
	if !utils.CheckURL(url) {
		scraperHealthy = false
	}

	url = fmt.Sprintf(ScraperConfig.BaseUrl + ScraperConfig.Endpoints.RoomsList)
	if !utils.CheckURL(url) {
		scraperHealthy = false
	}

	lang := c.DefaultQuery("lang", "en")
	units := c.DefaultQuery("units", "metric")
	apiKey := os.Getenv("OPENWEATHER_API_KEY")
	url = fmt.Sprintf("%s%s",
		Config.OpenWeather.BaseUrl,
		fmt.Sprintf(Config.OpenWeather.Endpoints.ForecastWeather, Config.OpenWeather.Lat, Config.OpenWeather.Lon, apiKey, lang, units, 40),
	)

	if !utils.CheckURL(url) {
		weatherHealthy = false
	}

	allHealthy := scraperHealthy && weatherHealthy

	Respond(c, http.StatusOK, models.HealthResponse{
		Scraper: scraperHealthy,	
		Weather: weatherHealthy,
		All:     allHealthy,
	})
}

func Initialize() {
	fmt.Println("initializing handlers")
	Config = &config.Global.API
	ScraperConfig = &config.Global.Scraper
}

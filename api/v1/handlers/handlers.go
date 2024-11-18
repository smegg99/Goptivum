// handlers/handlers.go
package handlers

import (
	"fmt"
	"net/http"
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

func ScraperHealthHandler(c *gin.Context) {
	if !utils.CheckURL(ScraperConfig.BaseUrl) {
		Respond(c, http.StatusInternalServerError, models.APIResponse{
			Message: "scraper is not healthy",
			Success: false,
		})
		return
	}

	url := fmt.Sprintf(ScraperConfig.BaseUrl + ScraperConfig.Endpoints.DivisionsList)
	if !utils.CheckURL(url) {
		Respond(c, http.StatusInternalServerError, models.APIResponse{
			Message: "divisions list is not healthy",
			Success: false,
		})
		return
	}

	url = fmt.Sprintf(ScraperConfig.BaseUrl + ScraperConfig.Endpoints.TeachersList)
	if !utils.CheckURL(url) {
		Respond(c, http.StatusInternalServerError, models.APIResponse{
			Message: "teachers list is not healthy",
			Success: false,
		})
		return
	}

	url = fmt.Sprintf(ScraperConfig.BaseUrl + ScraperConfig.Endpoints.RoomsList)
	if !utils.CheckURL(url) {
		Respond(c, http.StatusInternalServerError, models.APIResponse{
			Message: "rooms list is not healthy",
			Success: false,
		})
		return
	}

	Respond(c, http.StatusOK, models.APIResponse{
		Message: "scraper is healthy",
		Success: true,
	})
}

func ClientsAnalyticsHandler(c *gin.Context) {
	Respond(c, http.StatusOK, models.APIResponse{
		Message: "clients analytics",
		Success: true,
	})
}

func Initialize() {
	fmt.Println("initializing handlers")
	Config = &config.Global.API
	ScraperConfig = &config.Global.Scraper
}

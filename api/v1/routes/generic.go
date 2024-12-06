// routes/generic.go
package routes

import (
	"fmt"

	"smuggr.xyz/goptivum/api/v1/handlers"
	"smuggr.xyz/goptivum/common/models"
	"smuggr.xyz/goptivum/core/sse"

	"github.com/gin-gonic/gin"
)

func SetupGenericRoutes(router *gin.Engine, rootGroup *gin.RouterGroup, scheduleChannels *models.ScheduleChannels, otherChannels *models.OtherChannels) {
	healthGroup := rootGroup.Group("/health")
	{
		healthGroup.GET("/ping", handlers.PingHandler)
		healthGroup.GET("", handlers.APIHealthHandler)
		healthGroup.GET("/", handlers.APIHealthHandler)
	}

	clientUnregisterCallback := func() {
		otherChannels.Clients <- -1
	}

	clientRegisterCallback := func() {
		otherChannels.Clients <- 1
	}

	var ClientsHub = sse.NewHub(Config.MaxSSEClientsAnalytics, clientUnregisterCallback, clientRegisterCallback)
	var DutiesHub = sse.NewHub(Config.MaxSSEClients, nil, nil)
	var DivisionsHub = sse.NewHub(Config.MaxSSEClients, nil, nil)
	var TeachersHub = sse.NewHub(Config.MaxSSEClients, nil, nil)
	var RoomsHub = sse.NewHub(Config.MaxSSEClients, nil, nil)

	go ClientsHub.Run()
	go DutiesHub.Run()
	go DivisionsHub.Run()
	go TeachersHub.Run()
	go RoomsHub.Run()

	analyticsGroup := rootGroup.Group("/analytics")
	{
		analyticsGroup.GET("/clients", func(c *gin.Context) {
			clients := ClientsHub.GetConnectedClients()
			if clients >= Config.MaxSSEClientsAnalytics {
				handlers.Respond(c, 200, models.APIResponse{
					Message: fmt.Sprintf(">%d", clients),
					Success: true,
				})
				return
			}

			handlers.Respond(c, 200, models.APIResponse{
				Message: fmt.Sprintf("%d", clients),
				Success: true,
			})
		})
	}

	sseGroup := rootGroup.Group("/events")
	{
		sseGroup.GET("/clients", func(c *gin.Context) {
			ClientsHub.Handler()(c.Writer, c.Request)
		})

		sseGroup.GET("/duties", func(c *gin.Context) {
			DutiesHub.Handler()(c.Writer, c.Request)
		})

		sseGroup.GET("/divisions", func(c *gin.Context) {
			DivisionsHub.Handler()(c.Writer, c.Request)
		})

		sseGroup.GET("/teachers", func(c *gin.Context) {
			TeachersHub.Handler()(c.Writer, c.Request)
		})

		sseGroup.GET("/rooms", func(c *gin.Context) {
			RoomsHub.Handler()(c.Writer, c.Request)
		})
	}

	go func() {
		for message := range otherChannels.Clients {
			fmt.Println("broadcasting refresh for clients hub:", message)
			ClientsHub.Broadcast(message)
		}
	}()

	go func() {
		for message := range scheduleChannels.Duties {
			fmt.Println("broadcasting refresh for duties hub:", message)
			DutiesHub.Broadcast(message)
		}
	}()

	go func() {
		for message := range scheduleChannels.Divisons {
			fmt.Println("broadcasting refresh for divisions hub:", message)
			DivisionsHub.Broadcast(message)
		}
	}()

	go func() {
		for message := range scheduleChannels.Teachers {
			fmt.Println("broadcasting refresh for teachers hub:", message)
			TeachersHub.Broadcast(message)
		}
	}()

	go func() {
		for message := range scheduleChannels.Rooms {
			fmt.Println("broadcasting refresh for rooms hub:", message)
			RoomsHub.Broadcast(message)
		}
	}()
}

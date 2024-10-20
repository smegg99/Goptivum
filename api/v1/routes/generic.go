// routes/generic.go
package routes

import (
	"fmt"

	"smuggr.xyz/optivum-bsf/api/v1/handlers"
	"smuggr.xyz/optivum-bsf/core/sse"

	"github.com/gin-gonic/gin"
)

func SetupGenericRoutes(router *gin.Engine, rootGroup *gin.RouterGroup, refreshChan chan string) {
	healthGroup := rootGroup.Group("/health")
	{
		healthGroup.GET("/ping", handlers.PingHandler)
	}

	var Hub = sse.NewHub()
	go Hub.Run()	
	
	sseGroup := rootGroup.Group("/events")
	{
		sseGroup.GET("/connect", func(c *gin.Context) {
			Hub.Handler()(c.Writer, c.Request)
		})
	}

	go func() {
		for message := range refreshChan {
			fmt.Println("sending message to clients", message)
			Hub.Broadcast(message)
		}
	}()
}
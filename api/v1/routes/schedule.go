// routes/schedule.go
package routes

import (
	"github.com/gin-gonic/gin"
)

func SetupScheduleRoutes(router *gin.Engine, rootGroup *gin.RouterGroup) {
	divisionGroup := rootGroup.Group("/division")
	{
		divisionGroup.GET("/:id")
	}

	teacherGroup := rootGroup.Group("/teacher")
	{
		teacherGroup.GET("/:id")
	}

	roomGroup := rootGroup.Group("/room")
	{
		roomGroup.GET("/:id")
	}
}

// routes/schedule.go
package routes

import (
	"github.com/gin-gonic/gin"
)

func SetupScheduleRoutes(router *gin.Engine, rootGroup *gin.RouterGroup) {
	divisionGroup := rootGroup.Group("/division")
	{
		divisionGroup.GET("/:designator")
	}
	divisionsGroup := rootGroup.Group("/divisions")
	{
		divisionsGroup.GET("/")
	}

	teacherGroup := rootGroup.Group("/teacher")
	{
		teacherGroup.GET("/:designator")
	}
	teachersGroup := rootGroup.Group("/teachers")
	{
		teachersGroup.GET("/")
	}

	roomGroup := rootGroup.Group("/room")
	{
		roomGroup.GET("/:designator")
	}
	roomsGroup := rootGroup.Group("/rooms")
	{
		roomsGroup.GET("/")	
	}
}

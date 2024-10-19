// routes/schedule.go
package routes

import (
	"smuggr.xyz/optivum-bsf/api/v1/handlers"

	"github.com/gin-gonic/gin"
)

func SetupScheduleRoutes(router *gin.Engine, rootGroup *gin.RouterGroup) {
	divisionGroup := rootGroup.Group("/division")
	{
		divisionGroup.GET("/:index", handlers.GetDivisionHandler)
	}
	divisionsGroup := rootGroup.Group("/divisions")
	{
		divisionsGroup.GET("/")
	}

	teacherGroup := rootGroup.Group("/teacher")
	{
		teacherGroup.GET("/:index", handlers.GetTeacherHandler)
	}
	teachersGroup := rootGroup.Group("/teachers")
	{
		teachersGroup.GET("/")
	}

	roomGroup := rootGroup.Group("/room")
	{
		roomGroup.GET("/:index", handlers.GetRoomHandler)
	}
	roomsGroup := rootGroup.Group("/rooms")
	{
		roomsGroup.GET("/")	
	}
}

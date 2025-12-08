// api/v1/routes/auth.go
package routes

import (
	"smegg.me/goptivum/api/v1/handlers"
	"smegg.me/goptivum/api/v1/middleware"

	"github.com/gin-gonic/gin"
)

func SetupAuthRoutes(_ *gin.Engine, root *gin.RouterGroup) {
	g := root.Group("/auth")

	// Public routes
	g.POST("/login", handlers.Login)
	g.POST("/refresh", handlers.Refresh)

	// Protected routes - require valid JWT
	g.GET("/me", middleware.JWTAuthRequired(), handlers.Me)
}

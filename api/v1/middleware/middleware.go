// middleware/middleware.go
package middleware

import "github.com/gin-gonic/gin"

func NormalizeTrailingSlashMiddleware() gin.HandlerFunc {
    return func(c *gin.Context) {
        path := c.Request.URL.Path
        if len(path) > 1 && path[len(path)-1] == '/' {
            c.Request.URL.Path = path[:len(path)-1]
        }
        c.Next()
    }
}
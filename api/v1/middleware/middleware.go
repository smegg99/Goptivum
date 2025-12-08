// api/v1/middleware/middleware.go
package middleware

import (
	"strings"
	"time"

	apiErrors "smegg.me/goptivum/api/v1/errors"
	"smegg.me/goptivum/common/logger"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
	"github.com/ulule/limiter/v3"
	ginlimiter "github.com/ulule/limiter/v3/drivers/middleware/gin"
	"github.com/ulule/limiter/v3/drivers/store/memory"
)

func NormalizeTrailingSlashMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		path := c.Request.URL.Path
		if len(path) > 1 && path[len(path)-1] == '/' {
			c.Request.URL.Path = path[:len(path)-1]
		}
		c.Next()
	}
}

func RequestLoggingMiddleware() gin.HandlerFunc {
	return gin.LoggerWithConfig(gin.LoggerConfig{
		Formatter: func(param gin.LogFormatterParams) string {
			go func() {
				logger.LogRequest(
					param.Method,
					param.Path,
					param.ClientIP,
					param.Latency,
					param.StatusCode,
				)
			}()
			return ""
		},
	})
}

func ErrorLoggingMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Next()

		if len(c.Errors) > 0 {
			for _, err := range c.Errors {
				logger.ErrorWithFields("request error", map[string]any{
					"error":  err.Error(),
					"type":   err.Type,
					"method": c.Request.Method,
					"path":   c.Request.URL.Path,
					"ip":     c.ClientIP(),
				})
			}
		}
	}
}

func SecurityMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		isDebug := gin.Mode() == gin.DebugMode

		c.Header("X-Content-Type-Options", "nosniff")
		c.Header("Referrer-Policy", "strict-origin-when-cross-origin")

		if isDebug {
			c.Header("X-Frame-Options", "SAMEORIGIN")
			// Looser CSP to avoid blocking tools like Swagger/UI in dev environments
			c.Header("Content-Security-Policy", "default-src 'self' 'unsafe-inline' 'unsafe-eval' data: blob:; frame-ancestors 'self'; base-uri 'self'; form-action 'self'")
		} else {
			c.Header("X-Frame-Options", "DENY")
			c.Header("X-XSS-Protection", "1; mode=block")
			c.Header("Strict-Transport-Security", "max-age=63072000; includeSubDomains; preload")
			c.Header("Permissions-Policy", "geolocation=(), microphone=(), camera=(), payment=(), usb=(), magnetometer=(), gyroscope=(), speaker=(), fullscreen=(), display-capture=(), web-share=(), cross-origin-isolated=()")
			c.Header("Content-Security-Policy", "default-src 'none'; frame-ancestors 'none'; base-uri 'none'; form-action 'none'")
			c.Header("Cross-Origin-Embedder-Policy", "require-corp")
			c.Header("Cross-Origin-Opener-Policy", "same-origin")
			c.Header("Cross-Origin-Resource-Policy", "same-origin")
		}

		// TODO: Remember to change this after doing routing, it disables caching for some API routes
		if strings.Contains(c.Request.URL.Path, "/admin") ||
			strings.Contains(c.Request.URL.Path, "/auth") ||
			strings.Contains(c.Request.URL.Path, "/session") ||
			strings.Contains(c.Request.URL.Path, "/rewards") ||
			strings.Contains(c.Request.URL.Path, "/transactions") ||
			strings.Contains(c.Request.URL.Path, "/companies") {
			c.Header("Cache-Control", "no-store, no-cache, must-revalidate, private")
			c.Header("Pragma", "no-cache")
			c.Header("Expires", "0")
		} else {
			c.Header("Cache-Control", "public, max-age=300")
		}

		c.Header("Server", "")
		c.Header("X-Powered-By", "")

		c.Next()
	}
}

func RateLimitMiddleware(rateLimitPerMinute int64) gin.HandlerFunc {
	rate := limiter.Rate{
		Period: time.Minute,
		Limit:  rateLimitPerMinute,
	}

	store := memory.NewStore()
	instance := limiter.New(store, rate)

	return ginlimiter.NewMiddleware(instance, ginlimiter.WithErrorHandler(func(c *gin.Context, err error) {
		apiErrors.AbortWithError(c, apiErrors.ErrRateLimitExceeded)
	}))
}

func CORSConfig(allowedOrigins []string) cors.Config {
	return cors.Config{
		AllowOrigins:     allowedOrigins,
		AllowMethods:     []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"},
		AllowHeaders:     []string{"Origin", "Content-Type", "Accept", "User-Agent", "Cache-Control", "Authorization", "X-CSRF-Token"},
		ExposeHeaders:    []string{"Content-Length", "Content-Type"},
		AllowCredentials: true,
		MaxAge:           12 * time.Hour,
	}
}

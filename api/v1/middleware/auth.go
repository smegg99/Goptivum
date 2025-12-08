// api/v1/middleware/auth.go
package middleware

import (
	"strings"

	"github.com/gin-gonic/gin"

	apiErrors "smegg.me/goptivum/api/v1/errors"
	"smegg.me/goptivum/core/auth"
	"smegg.me/goptivum/core/models"
)

const (
	CtxLogin = "login"
	CtxRole  = "role"
)

func JWTAuthRequired() gin.HandlerFunc {
	return func(c *gin.Context) {
		authHeader := c.GetHeader("Authorization")
		if authHeader == "" {
			apiErrors.AbortWithError(c, apiErrors.ErrMissingAuthHeader)
			return
		}

		parts := strings.SplitN(authHeader, " ", 2)
		if len(parts) != 2 || parts[0] != "Bearer" {
			apiErrors.AbortWithError(c, apiErrors.ErrInvalidAuthFormat)
			return
		}

		tokenString := parts[1]

		claims, err := auth.ValidateToken(tokenString, auth.TokenTypeAccess)
		if err != nil {
			apiErrors.AbortWithError(c, apiErrors.ErrInvalidToken)
			return
		}

		c.Set(CtxLogin, claims.Login)
		c.Set(CtxRole, claims.Role)

		c.Next()
	}
}

func AdminRequired() gin.HandlerFunc {
	return func(c *gin.Context) {
		roleValue, exists := c.Get(CtxRole)
		if !exists {
			apiErrors.AbortWithError(c, apiErrors.ErrUnauthorized)
			return
		}

		role, ok := roleValue.(uint)
		if !ok || models.AccountRole(role) != models.AccountRoleAdmin {
			apiErrors.AbortWithError(c, apiErrors.ErrAdminRequired)
			return
		}

		c.Next()
	}
}

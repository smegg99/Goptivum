package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"
	apiErrors "smegg.me/goptivum/api/v1/errors"
	"smegg.me/goptivum/api/v1/middleware"
	"smegg.me/goptivum/api/v1/types"
	"smegg.me/goptivum/core/datastore"
	"smegg.me/goptivum/core/repositories"
)

// Me godoc
// @Summary Get current user info
// @Description Returns information about the currently authenticated user based on the JWT token
// @Tags auth
// @Produce json
// @Security BearerAuth
// @Success 200 {object} map[string]interface{} "Success - Current user information"
// @Failure 401 {object} types.ErrorResponse "Unauthorized - Invalid or missing token"
// @Failure 404 {object} types.ErrorResponse "Not Found - User not found"
// @Router /me [get]
func Me(c *gin.Context) {
	login, exists := c.Get(middleware.CtxLogin)
	if !exists {
		apiErrors.AbortWithError(c, apiErrors.ErrUnauthorized)
		return
	}

	loginStr, ok := login.(string)
	repo := repositories.GetAccountRepository(datastore.DB)
	account, err := repo.GetAccountByLogin(loginStr)
	if !ok || err != nil || account == nil {
		apiErrors.AbortWithError(c, apiErrors.ErrAccountNotFound)
		return
	}

	res := types.MeResponse{
		Login: account.Login,
		Role:  account.Role,
	}

	c.JSON(http.StatusOK, res)
}

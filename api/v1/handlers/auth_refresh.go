// api/v1/handlers/auth_refresh.go
package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"
	apiErrors "smegg.me/goptivum/api/v1/errors"
	"smegg.me/goptivum/api/v1/types"
	"smegg.me/goptivum/common/config"
	"smegg.me/goptivum/common/logger"
	"smegg.me/goptivum/core/auth"
	"smegg.me/goptivum/core/datastore"
	"smegg.me/goptivum/core/repositories"
)

// Refresh godoc
// @Summary Refresh access token
// @Description Use a valid refresh token to obtain a new access token. This should be called when the access token expires.
// @Tags auth
// @Accept json
// @Produce json
// @Param body body types.RefreshRequest true "Refresh token"
// @Success 200 {object} types.LoginResponse "New access token generated successfully"
// @Failure 400 {object} types.ErrorResponse "Bad Request - Invalid request format"
// @Failure 401 {object} types.ErrorResponse "Unauthorized - Invalid or expired refresh token"
// @Failure 500 {object} types.ErrorResponse "Internal Server Error - Token generation failure"
// @Router /auth/refresh [post]
func Refresh(c *gin.Context) {
	var req types.RefreshRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		logger.Errorf("failed to bind refresh request: %v", err)
		apiErrors.RespondWithError(c, apiErrors.ErrInvalidRequest)
		return
	}

	claims, err := auth.ValidateToken(req.RefreshToken, auth.TokenTypeRefresh)
	if err != nil {
		logger.Errorf("invalid refresh token: %v", err)
		apiErrors.RespondWithError(c, apiErrors.ErrInvalidToken)
		return
	}

	accountRepo := repositories.GetAccountRepository(datastore.DB)
	account, err := accountRepo.GetAccountByLogin(claims.Login)
	if err != nil || account == nil {
		logger.Errorf("failed to get account %s: %v", claims.Login, err)
		apiErrors.RespondWithError(c, apiErrors.ErrAccountNotFound)
		return
	}

	accessToken, err := auth.RefreshAccessToken(req.RefreshToken, account.Login, uint(account.Role))
	if err != nil {
		logger.Errorf("failed to refresh access token for account %s: %v", account.Login, err)
		apiErrors.RespondWithError(c, apiErrors.ErrTokenGenerationFail)
		return
	}

	res := types.LoginResponse{
		AccessToken:  accessToken,
		RefreshToken: req.RefreshToken,
		ExpiresIn:    config.GetConfig().Auth.JWT.AccessTokenExpiry,
		TokenType:    "Bearer",
	}

	logger.Infof("access token refreshed for account %s", account.Login)
	c.JSON(http.StatusOK, res)
}

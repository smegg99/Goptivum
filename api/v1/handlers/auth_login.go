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

// Login godoc
// @Summary Authenticate and receive JWT tokens
// @Description Authenticates an account using login and password. On success returns access and refresh JWT tokens. The access token should be used for API requests and the refresh token to get new access tokens when they expire.
// @Tags auth
// @Accept json
// @Produce json
// @Param body body types.LoginRequest true "Login credentials containing login and password"
// @Success 200 {object} types.LoginResponse "Authentication successful; JWT tokens returned"
// @Failure 400 {object} types.ErrorResponse "Bad Request - Invalid request format"
// @Failure 401 {object} types.ErrorResponse "Unauthorized - Invalid credentials"
// @Failure 500 {object} types.ErrorResponse "Internal Server Error - Token generation failure"
// @Router /auth/login [post]
func Login(c *gin.Context) {
	var req types.LoginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		logger.Errorf("failed to bind login request: %v", err)
		apiErrors.RespondWithError(c, apiErrors.ErrInvalidRequest)
		return
	}

	accountRepo := repositories.GetAccountRepository(datastore.DB)
	account, err := accountRepo.GetAccountByLogin(req.Login)
	if err != nil || account == nil || !account.CheckPassword(req.Password) {
		if err != nil {
			logger.Errorf("failed to get account by login %s: %v", req.Login, err)
		}
		apiErrors.RespondWithError(c, apiErrors.ErrInvalidCredentials)
		return
	}

	accessToken, refreshToken, err := auth.GenerateTokenPair(account.Login, uint(account.Role))
	if err != nil {
		logger.Errorf("failed to generate JWT tokens for account %s: %v", account.Login, err)
		apiErrors.RespondWithError(c, apiErrors.ErrTokenGenerationFail)
		return
	}

	res := types.LoginResponse{
		AccessToken:  accessToken,
		RefreshToken: refreshToken,
		ExpiresIn:    config.GetConfig().Auth.JWT.AccessTokenExpiry,
		TokenType:    "Bearer",
	}

	logger.Infof("account %s logged in successfully", account.Login)
	c.JSON(http.StatusOK, res)
}

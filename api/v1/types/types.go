package types

import "smegg.me/goptivum/core/models"

// LoginRequest represents the payload for a login request
// swagger:model LoginRequest
type LoginRequest struct {
	Login    string `json:"login" binding:"required,min=3,max=255" example:"admin"`
	Password string `json:"password" binding:"required" example:"strongPa55!"`
}

// LoginResponse represents a successful login with JWT tokens
// swagger:model LoginResponse
type LoginResponse struct {
	AccessToken  string `json:"access_token" example:"eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9..."`
	RefreshToken string `json:"refresh_token" example:"eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9..."`
	ExpiresIn    int    `json:"expires_in" example:"900"`
	TokenType    string `json:"token_type" example:"Bearer"`
}

// RefreshRequest represents the payload for a refresh request
// swagger:model RefreshRequest
type RefreshRequest struct {
	RefreshToken string `json:"refresh_token" binding:"required" example:"eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9"`
}

// MeResponse represents information about the currently authenticated user
// swagger:model MeResponse
type MeResponse struct {
	Login string             `json:"login" example:"admin"`
	Role  models.AccountRole `json:"role"  example:"1"`
}

// ErrorResponse represents a generic error response
// swagger:model ErrorResponse
type ErrorResponse struct {
	Error string `json:"error" example:"invalid request"`
}

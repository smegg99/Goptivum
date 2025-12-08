// core/auth/jwt.go
package auth

import (
	"errors"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/spf13/viper"
	"smegg.me/goptivum/common/config"
)

type TokenType string

const (
	TokenTypeAccess  TokenType = "access"
	TokenTypeRefresh TokenType = "refresh"
)

type Claims struct {
	Login     string    `json:"login"`
	Role      uint      `json:"role"`
	TokenType TokenType `json:"token_type"`
	jwt.RegisteredClaims
}

type AccountInfo interface {
	GetLogin() string
	GetRole() uint
}

func GenerateTokenPair(login string, role uint) (accessToken, refreshToken string, err error) {
	secret := []byte(viper.GetString("JWT_SECRET"))
	if len(secret) == 0 {
		return "", "", errors.New("JWT_SECRET not configured")
	}

	cfg := config.GetConfig().Auth.JWT

	accessToken, err = generateToken(login, role, TokenTypeAccess, time.Duration(cfg.AccessTokenExpiry)*time.Second, secret)
	if err != nil {
		return "", "", fmt.Errorf("failed to generate access token: %w", err)
	}

	refreshToken, err = generateToken(login, role, TokenTypeRefresh, time.Duration(cfg.RefreshTokenExpiry)*time.Second, secret)
	if err != nil {
		return "", "", fmt.Errorf("failed to generate refresh token: %w", err)
	}

	return accessToken, refreshToken, nil
}

// generateToken creates a JWT token with specified type and expiry
func generateToken(login string, role uint, tokenType TokenType, expiry time.Duration, secret []byte) (string, error) {
	now := time.Now()

	claims := &Claims{
		Login:     login,
		Role:      role,
		TokenType: tokenType,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(now.Add(expiry)),
			IssuedAt:  jwt.NewNumericDate(now),
			NotBefore: jwt.NewNumericDate(now),
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString(secret)
}

// ValidateToken validates a JWT token and returns the claims
func ValidateToken(tokenString string, expectedType TokenType) (*Claims, error) {
	secret := []byte(viper.GetString("JWT_SECRET"))
	if len(secret) == 0 {
		return nil, errors.New("JWT_SECRET not configured")
	}

	token, err := jwt.ParseWithClaims(tokenString, &Claims{}, func(token *jwt.Token) (interface{}, error) {
		// Verify signing method
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}
		return secret, nil
	})

	if err != nil {
		return nil, fmt.Errorf("failed to parse token: %w", err)
	}

	claims, ok := token.Claims.(*Claims)
	if !ok || !token.Valid {
		return nil, errors.New("invalid token")
	}

	// Verify token type
	if claims.TokenType != expectedType {
		return nil, fmt.Errorf("invalid token type: expected %s, got %s", expectedType, claims.TokenType)
	}

	return claims, nil
}

func RefreshAccessToken(refreshTokenString string, login string, role uint) (string, error) {
	_, err := ValidateToken(refreshTokenString, TokenTypeRefresh)
	if err != nil {
		return "", fmt.Errorf("invalid refresh token: %w", err)
	}

	secret := []byte(viper.GetString("JWT_SECRET"))
	cfg := config.GetConfig().Auth.JWT

	return generateToken(login, role, TokenTypeAccess, time.Duration(cfg.AccessTokenExpiry)*time.Second, secret)
}

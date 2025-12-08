// api/v1/errors/types.go
package errors

import "net/http"

type ErrorCode string

type HTTPError struct {
	Code       ErrorCode `json:"code"`
	Message    string    `json:"message"`
	StatusCode int       `json:"-"`
}

func (e HTTPError) Error() string {
	return e.Message
}

func (e HTTPError) String() string {
	return e.Message
}

func NewHTTPError(code ErrorCode, message string, statusCode int) HTTPError {
	return HTTPError{
		Code:       code,
		Message:    message,
		StatusCode: statusCode,
	}
}

func (e HTTPError) WithMessage(message string) HTTPError {
	return HTTPError{
		Code:       e.Code,
		Message:    message,
		StatusCode: e.StatusCode,
	}
}

const (
	// Authentication error codes
	ErrCodeMissingAuthHeader   ErrorCode = "AUTH_MISSING_HEADER"
	ErrCodeInvalidAuthFormat   ErrorCode = "AUTH_INVALID_FORMAT"
	ErrCodeInvalidToken        ErrorCode = "AUTH_INVALID_TOKEN"       // #nosec G101
	ErrCodeExpiredToken        ErrorCode = "AUTH_EXPIRED_TOKEN"       // #nosec G101
	ErrCodeInvalidCredentials  ErrorCode = "AUTH_INVALID_CREDENTIALS" // #nosec G101
	ErrCodeTokenGenerationFail ErrorCode = "AUTH_TOKEN_GENERATION_FAILED"

	// Authorization error codes
	ErrCodeUnauthorized  ErrorCode = "AUTHZ_UNAUTHORIZED"
	ErrCodeAdminRequired ErrorCode = "AUTHZ_ADMIN_REQUIRED"
	ErrCodeInvalidRole   ErrorCode = "AUTHZ_INVALID_ROLE"

	// Validation error codes
	ErrCodeInvalidRequest ErrorCode = "VALIDATION_INVALID_REQUEST"

	// Resource error codes
	ErrCodeAccountNotFound ErrorCode = "ACCOUNT_NOT_FOUND"

	// Rate limiting error codes
	ErrCodeRateLimitExceeded ErrorCode = "RATE_LIMIT_EXCEEDED"

	// Internal error codes
	ErrCodeInternalServerError ErrorCode = "INTERNAL_SERVER_ERROR"
)

var (
	// Authentication errors
	ErrMissingAuthHeader   = NewHTTPError(ErrCodeMissingAuthHeader, "missing authorization header", http.StatusUnauthorized)
	ErrInvalidAuthFormat   = NewHTTPError(ErrCodeInvalidAuthFormat, "invalid authorization header format", http.StatusUnauthorized)
	ErrInvalidToken        = NewHTTPError(ErrCodeInvalidToken, "invalid or expired token", http.StatusUnauthorized)
	ErrExpiredToken        = NewHTTPError(ErrCodeExpiredToken, "token has expired", http.StatusUnauthorized)
	ErrInvalidCredentials  = NewHTTPError(ErrCodeInvalidCredentials, "invalid credentials", http.StatusUnauthorized)
	ErrTokenGenerationFail = NewHTTPError(ErrCodeTokenGenerationFail, "failed to generate authentication tokens", http.StatusInternalServerError)

	// Authorization errors
	ErrUnauthorized  = NewHTTPError(ErrCodeUnauthorized, "unauthorized", http.StatusUnauthorized)
	ErrAdminRequired = NewHTTPError(ErrCodeAdminRequired, "admin role required", http.StatusForbidden)

	// Validation errors
	ErrInvalidRequest = NewHTTPError(ErrCodeInvalidRequest, "invalid request", http.StatusBadRequest)

	// Resource errors
	ErrAccountNotFound = NewHTTPError(ErrCodeAccountNotFound, "account not found", http.StatusNotFound)

	// Rate limiting errors
	ErrRateLimitExceeded = NewHTTPError(ErrCodeRateLimitExceeded, "rate limit exceeded", http.StatusTooManyRequests)

	// Internal errors
	ErrInternalServerError = NewHTTPError(ErrCodeInternalServerError, "internal server error", http.StatusInternalServerError)
)


// Package executor provides LLM provider executor implementations that translate
// and send inference requests to upstream AI provider APIs.
package executor

import (
	"gorouter/internal/shared"
)

// Package-level sentinel errors that wrap shared.AppError for structured
// error handling across the executor layer.
var (
	// ErrExecutorNotFound is returned when no executor is registered for the
	// given provider type or request format.
	ErrExecutorNotFound = &shared.AppError{
		Code:    shared.ErrNotFound,
		Message: "no executor found for the given provider type and request format",
	}

	// ErrUnsupportedProviderType is returned when the provider type is not
	// recognised by the executor factory.
	ErrUnsupportedProviderType = &shared.AppError{
		Code:    shared.ErrValidation,
		Message: "unsupported provider type",
	}
)

// NewUpstreamError creates an AppError representing an upstream API failure.
// The HTTP status code from the upstream response is preserved so that callers
// can distinguish 4xx (client) from 5xx (server) failures.
func NewUpstreamError(statusCode int, body string) *shared.AppError {
	return &shared.AppError{
		Code:       shared.ErrInternal,
		Message:    "upstream API returned error",
		HTTPStatus: statusCode,
		Details: map[string]interface{}{
			"status_code": statusCode,
			"body":        truncateString(body, 1024),
		},
	}
}

// NewStreamFailedError is returned when a streaming request could not be
// initiated (e.g. the upstream body could not be read).
func NewStreamFailedError(err error) *shared.AppError {
	return &shared.AppError{
		Code:    shared.ErrInternal,
		Message: "stream creation failed",
		Err:     err,
	}
}

// NewExecutorTimeoutError is returned when the upstream request exceeds the
// configured timeout.
func NewExecutorTimeoutError(err error) *shared.AppError {
	return &shared.AppError{
		Code:    shared.ErrTimeout,
		Message: "executor request timed out",
		Err:     err,
	}
}

// NewAuthError is returned when the upstream returns a 401 or 403.
func NewAuthError(statusCode int, body string) *shared.AppError {
	return &shared.AppError{
		Code:       shared.ErrUnauthorized,
		Message:    "upstream authentication failed",
		HTTPStatus: statusCode,
		Details: map[string]interface{}{
			"status_code": statusCode,
			"body":        truncateString(body, 512),
		},
	}
}

// NewRateLimitError is returned when the upstream returns a 429.
func NewRateLimitError(statusCode int, body string) *shared.AppError {
	return &shared.AppError{
		Code:       shared.ErrRateLimited,
		Message:    "upstream rate limit exceeded",
		HTTPStatus: statusCode,
		Details: map[string]interface{}{
			"status_code": statusCode,
			"body":        truncateString(body, 512),
		},
	}
}

func truncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

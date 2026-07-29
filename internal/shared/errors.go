// Package shared provides common types and utilities used across gorouter.
package shared

import (
	"encoding/json"
	"errors"
	"net/http"
)

// ErrorCode is a machine-readable error category that maps to HTTP status codes.
type ErrorCode string

const (
	// ErrInternal indicates an unexpected internal failure.
	ErrInternal ErrorCode = "INTERNAL_ERROR"
	// ErrValidation indicates invalid input from the caller.
	ErrValidation ErrorCode = "VALIDATION_ERROR"
	// ErrNotFound indicates the requested resource does not exist.
	ErrNotFound ErrorCode = "NOT_FOUND"
	// ErrUnauthorized indicates missing or invalid authentication.
	ErrUnauthorized ErrorCode = "UNAUTHORIZED"
	// ErrForbidden indicates the authenticated actor lacks permission.
	ErrForbidden ErrorCode = "FORBIDDEN"
	// ErrConflict indicates a resource conflict (e.g. duplicate).
	ErrConflict ErrorCode = "CONFLICT"
	// ErrRateLimited indicates the caller exceeded a rate limit.
	ErrRateLimited ErrorCode = "RATE_LIMITED"
	// ErrTimeout indicates an operation timed out.
	ErrTimeout ErrorCode = "TIMEOUT"
)

// AppError is a structured error with a machine-readable code, human message,
// optional details, and an optional wrapped error.
//
// The Err field and sensitive Details are redacted from JSON serialization to
// prevent leaking internal information to API consumers.
type AppError struct {
	Code       ErrorCode   `json:"code"`
	Message    string      `json:"message"`
	Details    interface{} `json:"details,omitempty"`
	HTTPStatus int         `json:"-"`             // not serialized
	Err        error       `json:"-"`             // wrapped error, redacted from JSON
	RequestID  string      `json:"request_id,omitempty"`
}

// Error implements the error interface.
func (e *AppError) Error() string {
	if e == nil {
		return ""
	}
	if e.Err != nil {
		return e.Message + ": " + e.Err.Error()
	}
	return e.Message
}

// Unwrap returns the wrapped error for errors.Is / errors.As support.
func (e *AppError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

// HTTPStatusCode returns the HTTP status code. If HTTPStatus was explicitly set
// (> 0) it takes priority; otherwise the code is derived from the ErrorCode.
func (e *AppError) HTTPStatusCode() int {
	if e == nil {
		return http.StatusInternalServerError
	}
	if e.HTTPStatus > 0 {
		return e.HTTPStatus
	}
	return MapHTTPStatus(e.Code)
}

// RedactDetails returns a copy of AppError with sensitive fields cleared
// (Err and RequestID). The Details field is preserved since its sensitivity
// depends on the struct schema (callers should use `json:"-"` tags on
// sensitive Detail fields). The original AppError is never modified
// (copy-on-redact).
func (e *AppError) RedactDetails() *AppError {
	if e == nil {
		return nil
	}
	cp := *e
	cp.Err = nil
	cp.RequestID = ""
	return &cp
}

// MarshalJSON implements json.Marshaler so that sensitive fields (Err,
// RequestID) are excluded from JSON output.
func (e *AppError) MarshalJSON() ([]byte, error) {
	// Use a type alias to prevent infinite recursion.
	type appError AppError
	redacted := e.RedactDetails()
	return json.Marshal((*appError)(redacted))
}

// NewAppError creates a new AppError. Provide 0 for httpStatus to use the
// default mapping from code.
func NewAppError(code ErrorCode, message string, httpStatus int, err error) *AppError {
	return &AppError{
		Code:       code,
		Message:    message,
		HTTPStatus: httpStatus,
		Err:        err,
	}
}

// AsAppError extracts an *AppError from the error chain. Returns nil, false if
// the chain does not contain an AppError.
func AsAppError(err error) (*AppError, bool) {
	if err == nil {
		return nil, false
	}
	var appErr *AppError
	if errors.As(err, &appErr) {
		return appErr, true
	}
	return nil, false
}

// IsAppError returns true if the error chain contains an *AppError.
func IsAppError(err error) bool {
	_, ok := AsAppError(err)
	return ok
}

// MapHTTPStatus returns the default HTTP status code for the given ErrorCode.
func MapHTTPStatus(code ErrorCode) int {
	switch code {
	case ErrValidation:
		return http.StatusBadRequest
	case ErrUnauthorized:
		return http.StatusUnauthorized
	case ErrForbidden:
		return http.StatusForbidden
	case ErrNotFound:
		return http.StatusNotFound
	case ErrConflict:
		return http.StatusConflict
	case ErrRateLimited:
		return http.StatusTooManyRequests
	case ErrTimeout:
		return http.StatusGatewayTimeout
	case ErrInternal:
		return http.StatusInternalServerError
	default:
		return http.StatusInternalServerError
	}
}

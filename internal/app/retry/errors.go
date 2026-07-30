package retry

import (
	"context"
	"errors"

	"gorouter/internal/shared"
)

// ErrorClass classifies an error into a broad category for retry decisions.
type ErrorClass int

const (
	ErrorClassUnknown   ErrorClass = iota
	ErrorClassRetryable ErrorClass = iota
	ErrorClassTerminal  ErrorClass = iota
	ErrorClassAuth      ErrorClass = iota
)

// ErrorCode values used by the retry package for error classification.
// These may also be set by executor packages that the retry layer wraps.
const (
	ErrCodeUpstream        shared.ErrorCode = "UPSTREAM_ERROR"
	ErrCodeStreamFailed    shared.ErrorCode = "STREAM_FAILED"
	ErrCodeExecutorTimeout shared.ErrorCode = "EXECUTOR_TIMEOUT"
	ErrCodeInvalidRequest  shared.ErrorCode = "INVALID_REQUEST"
)

// IsRetryable checks whether err is a retryable error based on its error code.
func IsRetryable(err error) bool {
	if err == nil {
		return false
	}

	appErr, ok := shared.AsAppError(err)
	if !ok {
		return false
	}

	switch appErr.Code {
	case ErrCodeUpstream:
		if appErr.HTTPStatus >= 500 && appErr.HTTPStatus < 600 {
			return true
		}
		return false
	case ErrCodeStreamFailed, ErrCodeExecutorTimeout:
		return true
	case shared.ErrTimeout:
		return true
	default:
		return false
	}
}

// ClassifyError categorises an error into an ErrorClass.
func ClassifyError(err error) ErrorClass {
	if err == nil {
		return ErrorClassUnknown
	}

	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return ErrorClassTerminal
	}

	appErr, ok := shared.AsAppError(err)
	if !ok {
		return ErrorClassUnknown
	}

	switch appErr.Code {
	case ErrCodeUpstream, ErrCodeStreamFailed, ErrCodeExecutorTimeout, shared.ErrTimeout:
		return ErrorClassRetryable
	case shared.ErrUnauthorized, shared.ErrForbidden:
		return ErrorClassAuth
	case shared.ErrValidation, ErrCodeInvalidRequest:
		return ErrorClassTerminal
	default:
		if appErr.HTTPStatus >= 400 && appErr.HTTPStatus < 500 {
			return ErrorClassTerminal
		}
		return ErrorClassUnknown
	}
}

// Package translate provides format detection and request translation for AI model
// API requests. It converts between wire-protocol formats (OpenAI Chat, Codex
// Responses) and the engine's canonical internal representation.
package translate

import (
	"gorouter/internal/shared"
)

// Package-level sentinel errors that wrap shared.AppError with the appropriate
// ErrorCode. Callers can inspect them via errors.Is or shared.AsAppError.
var (
	// ErrUnsupportedFormat is returned when the request body does not match any
	// known wire format.
	ErrUnsupportedFormat = &shared.AppError{
		Code:    shared.ErrValidation,
		Message: "unsupported request format — body does not match any known format",
	}

	// ErrInvalidRequest is returned when a required field (e.g. model, messages)
	// is missing from the request body.
	ErrInvalidRequest = &shared.AppError{
		Code:    shared.ErrValidation,
		Message: "invalid request — missing required fields",
	}

	// ErrTranslationFailed is returned when an internal error occurs during
	// request body translation (e.g. JSON marshal/unmarshal failure).
	ErrTranslationFailed = &shared.AppError{
		Code:    shared.ErrInternal,
		Message: "request translation failed",
	}
)

// Package orchestrator provides the top-level coordination pipeline that ties
// together model resolution, format detection, translation, executor dispatch,
// retry/fallback, and streaming lifecycle for all incoming LLM inference requests.
package orchestrator

import (
	"gorouter/internal/shared"
)

// Orchestrator-specific sentinel errors that wrap shared.AppError for structured
// error handling across the pipeline.
var (
	// ErrModelResolutionFailed is returned when the model reference cannot be
	// parsed or resolved against the configured providers.
	ErrModelResolutionFailed = &shared.AppError{
		Code:    shared.ErrNotFound,
		Message: "model resolution failed — provider or model not found",
	}

	// ErrFormatDetectionFailed is returned when the request body does not match
	// any known wire protocol format.
	ErrFormatDetectionFailed = &shared.AppError{
		Code:    shared.ErrValidation,
		Message: "format detection failed — unrecognised request format",
	}

	// ErrTranslationFailed is returned when request body translation fails
	// (e.g. JSON parse error, missing required fields).
	ErrTranslationFailed = &shared.AppError{
		Code:    shared.ErrInternal,
		Message: "request translation failed",
	}

	// ErrAccountSelection is returned when no suitable provider account can be
	// selected for the resolved model.
	ErrAccountSelection = &shared.AppError{
		Code:    shared.ErrNotFound,
		Message: "account selection failed — no available account for provider",
	}

	// ErrExecutorNotFound is returned when no executor is registered for the
	// resolved provider type and request format combination.
	ErrExecutorNotFound = &shared.AppError{
		Code:    shared.ErrNotFound,
		Message: "executor not found for provider type and request format",
	}

	// ErrStreamNotFound is returned when CancelStream is called with a
	// request ID that does not match any active stream.
	ErrStreamNotFound = &shared.AppError{
		Code:    shared.ErrNotFound,
		Message: "stream not found for the given request ID",
	}

	// ErrPipelineTimeout is returned when the overall request pipeline
	// exceeds its configured deadline.
	ErrPipelineTimeout = &shared.AppError{
		Code:    shared.ErrTimeout,
		Message: "pipeline request timed out",
	}
)

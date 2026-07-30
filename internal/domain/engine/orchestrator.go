package engine

import (
	"context"

	"github.com/google/uuid"
)

// ExecutePipeline defines the primary request execution path through which all
// incoming LLM inference requests are routed, mapped, and dispatched.
type ExecutePipeline interface {
	// ExecuteRequest processes a single request end-to-end: selects a provider
	// account, invokes the appropriate executor, and returns the response.
	ExecuteRequest(ctx context.Context, req *Request) (*Response, error)
}

// Orchestrator coordinates the full lifecycle of request execution including
// pipeline dispatch, stream lifecycle management, and cancellation.
type Orchestrator interface {
	ExecutePipeline

	// CancelStream terminates an active streaming request identified by its
	// request UUID. It is safe to call multiple times.
	CancelStream(ctx context.Context, requestID uuid.UUID) error
}

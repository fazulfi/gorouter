package engine

import (
	"context"

	"gorouter/internal/domain/provider"
)

// Executor translates and sends a single LLM inference request to a provider account.
// Implementations handle format-specific mapping, wire protocol translation, and
// SSE stream management.
type Executor interface {
	// Execute sends a non-streaming request and returns the complete response.
	Execute(ctx context.Context, req *Request, account *provider.Account) (*Response, error)

	// ExecuteStream sends a streaming request and returns a response carrying an
	// active Stream reference. The caller must consume chunks from the stream.
	ExecuteStream(ctx context.Context, req *Request, account *provider.Account) (*Response, error)

	// SupportsFormat reports whether this executor can handle the given request format.
	SupportsFormat(format RequestFormat) bool

	// ProviderType returns the provider backend this executor targets.
	ProviderType() provider.ProviderType
}

// ExecutorFactory returns the appropriate Executor for a given provider-type and
// request-format combination.
type ExecutorFactory interface {
	// GetExecutor retrieves an executor capable of handling the specified provider
	// type and request format. The boolean indicates whether a matching executor
	// was found.
	GetExecutor(ptype provider.ProviderType, format RequestFormat) (Executor, bool)
}

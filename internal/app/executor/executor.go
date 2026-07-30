package executor

import (
	"net/http"

	"gorouter/internal/domain/engine"
	"gorouter/internal/domain/provider"
)

// ProviderCodex is a synthetic provider type for the specialised Codex (OpenAI
// Responses API) executor. The domain package does not define this constant,
// so it is declared here for routing inside the executor factory.
const ProviderCodex provider.ProviderType = "codex"

// Factory implements engine.ExecutorFactory by selecting the correct Executor
// for a given provider type and request format combination.
type Factory struct {
	transport http.RoundTripper
}

// NewFactory creates a new executor Factory with the given HTTP transport.
// Pass http.DefaultTransport or a wrapped transport for custom dial / proxy
// behaviour.
func NewFactory(transport http.RoundTripper) *Factory {
	return &Factory{transport: transport}
}

// GetExecutor returns the executor best suited for the given provider type and
// request format. The boolean indicates whether a matching executor was found.
func (f *Factory) GetExecutor(ptype provider.ProviderType, format engine.RequestFormat) (engine.Executor, bool) {
	switch {
	case format == engine.FormatCodexResponses:
		return NewCodexExecutor(f.transport), true
	case ptype == provider.ProviderOpenAI || ptype == ProviderCodex:
		switch format {
		case engine.FormatOpenAIChat, engine.FormatOpenAICompat, engine.FormatCodexResponses:
			if format == engine.FormatCodexResponses {
				return NewCodexExecutor(f.transport), true
			}
			return NewOpenAIChatExecutor(f.transport), true
		default:
			// Fallback: OpenAI-compatible executor works for any format the
			// provider doesn't handle natively.
			return NewOpenAIChatExecutor(f.transport), true
		}
	case format == engine.FormatOpenAIChat || format == engine.FormatOpenAICompat:
		// Non-OpenAI providers may still use OpenAI-compatible wire format.
		return NewOpenAIChatExecutor(f.transport), true
	default:
		return nil, false
	}
}

// Supports reports whether the factory has an executor for the given provider
// type and request format. This is a convenience wrapper around GetExecutor.
func (f *Factory) Supports(ptype provider.ProviderType, format engine.RequestFormat) bool {
	_, ok := f.GetExecutor(ptype, format)
	return ok
}

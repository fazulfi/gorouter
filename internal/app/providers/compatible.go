// Package providers provides application-layer services for managing
// dashboard-defined compatible provider accounts and model discovery.
package providers

import (
	"context"
	"fmt"

	"gorouter/internal/domain/engine"
	"gorouter/internal/domain/provider"
	"gorouter/internal/engine/providers/generic"
)

// CompatibleService manages dynamic compatible providers that are added through
// the dashboard or admin API. It supports OpenAI-compatible and
// Anthropic-compatible providers with custom base URLs, model discovery, and
// executor construction.
//
// Custom provider URLs are intentionally unrestricted per decisions #136, #137,
// and #376 — no SSRF, loopback, LAN, private-range, or DNS-rebinding validation
// is applied.
type CompatibleService struct {
	client *generic.Client
}

// NewCompatibleService creates a CompatibleService with a default HTTP client.
func NewCompatibleService() *CompatibleService {
	return &CompatibleService{
		client: generic.NewClient(),
	}
}

// NewCompatibleServiceWithClient creates a CompatibleService with a custom client.
func NewCompatibleServiceWithClient(client *generic.Client) *CompatibleService {
	return &CompatibleService{
		client: client,
	}
}

// clientWithBaseURL returns a new generic.Client with the same transport,
// timeout, and CredentialResolver as the service's client but with the given
// base URL. It uses Client.WithBaseURL to guarantee all runtime configuration
// is preserved.
// If baseURL is empty, returns the original client.
func (s *CompatibleService) clientWithBaseURL(baseURL string) *generic.Client {
	if baseURL == "" {
		return s.client
	}
	return s.client.WithBaseURL(baseURL)
}

// NewOpenAIExecutor creates an engine.Executor for an OpenAI-compatible provider
// with the given base URL and options.
func (s *CompatibleService) NewOpenAIExecutor(baseURL string, opts ...generic.CompatibleOpenAIOption) *generic.CompatibleOpenAIExecutor {
	return generic.NewCompatibleOpenAIExecutor(s.clientWithBaseURL(baseURL), opts...)
}

// NewAnthropicExecutor creates an engine.Executor for an Anthropic-compatible
// provider with the given base URL.
func (s *CompatibleService) NewAnthropicExecutor(baseURL string, opts ...generic.CompatibleAnthropicOption) *generic.CompatibleAnthropicExecutor {
	return generic.NewCompatibleAnthropicExecutor(s.clientWithBaseURL(baseURL), opts...)
}

// ListModels discovers available models from a compatible provider.
func (s *CompatibleService) ListModels(ctx context.Context, account *provider.Account) ([]generic.CompatibleProviderModel, error) {
	return generic.ListModels(ctx, s.client, account)
}

// ProbeModel checks if a specific model is available on the provider.
func (s *CompatibleService) ProbeModel(ctx context.Context, account *provider.Account, modelID string) (bool, error) {
	return generic.ProbeModel(ctx, s.client, account, modelID)
}

// ProbeEmbeddings checks if the provider supports embeddings.
func (s *CompatibleService) ProbeEmbeddings(ctx context.Context, account *provider.Account) (bool, error) {
	return generic.ProbeEmbeddings(ctx, s.client, account)
}

// ExecutorForProvider returns the appropriate generic executor for a given
// provider type and base URL.
func (s *CompatibleService) ExecutorForProvider(ptype provider.ProviderType, baseURL string, format engine.RequestFormat) (engine.Executor, error) {
	switch ptype {
	case provider.ProviderOpenAI, provider.ProviderCustom:
		return s.NewOpenAIExecutor(baseURL, generic.WithOpenAIFormat(format)), nil
	case provider.ProviderAnthropic:
		return s.NewAnthropicExecutor(baseURL), nil
	default:
		return nil, fmt.Errorf("unsupported provider type for compatible service: %s", ptype)
	}
}

package generic

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"gorouter/internal/domain/provider"
)

// CompatibleProviderModel represents a model advertised by a compatible provider's
// /v1/models endpoint. The shape matches the OpenAI /v1/models response format,
// which many compatible providers also expose.
type CompatibleProviderModel struct {
	ID      string `json:"id"`
	Object  string `json:"object,omitempty"`
	Created int64  `json:"created,omitempty"`
	OwnedBy string `json:"owned_by,omitempty"`
}

// ListModelsResponse is the standard shape of a /v1/models list response.
type ListModelsResponse struct {
	Object string                    `json:"object"`
	Data   []CompatibleProviderModel `json:"data"`
}

// ListModels calls GET /v1/models on the provider and returns the advertised
// model list. A non-2xx response is returned as an error. Credential is
// injected automatically from the account.
func ListModels(ctx context.Context, client *Client, account *provider.Account) ([]CompatibleProviderModel, error) {
	req, err := client.BuildRequest(ctx, http.MethodGet, "/v1/models", nil, account)
	if err != nil {
		return nil, fmt.Errorf("build models request: %w", err)
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("list models request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("list models: unexpected status %d: %s", resp.StatusCode, truncateString(string(body), 512))
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("list models read body: %w", err)
	}

	var listResp ListModelsResponse
	if err := json.Unmarshal(body, &listResp); err != nil {
		return nil, fmt.Errorf("list models parse response: %w", err)
	}

	return listResp.Data, nil
}

// ProbeModel checks whether a specific model ID is available on the provider
// by listing models and searching for a match. Returns true if the model was
// found.
func ProbeModel(ctx context.Context, client *Client, account *provider.Account, modelID string) (bool, error) {
	models, err := ListModels(ctx, client, account)
	if err != nil {
		return false, fmt.Errorf("probe model: %w", err)
	}
	for _, m := range models {
		if m.ID == modelID {
			return true, nil
		}
	}
	return false, nil
}

// ProbeEmbeddings checks whether the provider supports embeddings by calling
// GET /v1/models and searching for known embedding model IDs or by calling
// GET /v1/embeddings with a minimal probe.
func ProbeEmbeddings(ctx context.Context, client *Client, account *provider.Account) (bool, error) {
	// First check /v1/models for embedding-related models.
	models, err := ListModels(ctx, client, account)
	if err == nil {
		for _, m := range models {
			if isEmbeddingModel(m.ID) {
				return true, nil
			}
		}
	}

	// Fallback: check if /v1/embeddings endpoint responds.
	probeBody := []byte(`{"input": "probe","model":"text-embedding-ada-002"}`)
	req, err := client.BuildRequest(ctx, http.MethodPost, "/v1/embeddings", probeBody, account)
	if err != nil {
		return false, nil // probe failed; embeddings not confirmed
	}

	resp, err := client.Do(req)
	if err != nil {
		return false, nil
	}
	resp.Body.Close()

	// Any 2xx means the endpoint exists.
	return resp.StatusCode >= 200 && resp.StatusCode < 300, nil
}

// isEmbeddingModel returns true if the model ID looks like an embedding model.
func isEmbeddingModel(id string) bool {
	lower := id
	return stringsContains(lower, "embedding") || stringsContains(lower, "embeddings")
}

// stringsContains is a helper to avoid importing strings for a single function.
func stringsContains(s, substr string) bool {
	return len(s) >= len(substr) && containsString(s, substr)
}

func containsString(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

func truncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

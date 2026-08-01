// Package web implements the web search and web fetch modalities
// (audit/01-http-contracts.md:552-553, upstream src/sse/handlers/search.js
// and fetch.js).
package web

import (
	"encoding/json"
	"fmt"

	"gorouter/internal/domain/provider"
	"gorouter/internal/engine/media"
)

// SearchRequest is the web search body (upstream src/sse/handlers/search.js
// request fields: query, max_results, search_type, country, language,
// time_range, offset, domain_filter, content_options, provider_options).
type SearchRequest struct {
	Query           string          `json:"query"`
	MaxResults      int             `json:"max_results,omitempty"`
	SearchType      string          `json:"search_type,omitempty"`
	Country         string          `json:"country,omitempty"`
	Language        string          `json:"language,omitempty"`
	TimeRange       string          `json:"time_range,omitempty"`
	Offset          int             `json:"offset,omitempty"`
	DomainFilter    []string        `json:"domain_filter,omitempty"`
	ContentOptions  json.RawMessage `json:"content_options,omitempty"`
	ProviderOptions json.RawMessage `json:"provider_options,omitempty"`
}

// SearchResult is one unified search result (upstream
// open-sse/handlers/search/normalizers.js unified shape: title, url, snippet).
type SearchResult struct {
	Title   string `json:"title"`
	URL     string `json:"url"`
	Snippet string `json:"snippet"`
}

// SearchResponse is the unified search results body
// (audit/01-http-contracts.md:552 "JSON results").
type SearchResponse struct {
	Query   string         `json:"query"`
	Results []SearchResult `json:"results"`
}

// DecodeSearchRequest parses a web search body.
func DecodeSearchRequest(body []byte) (*SearchRequest, error) {
	if len(body) == 0 {
		return nil, fmt.Errorf("web: %w", media.ErrMalformedRequest)
	}
	var req SearchRequest
	if err := json.Unmarshal(body, &req); err != nil {
		return nil, fmt.Errorf("web: %w", media.ErrMalformedRequest)
	}
	if req.Query == "" {
		return nil, fmt.Errorf("web: %w: missing query", media.ErrMalformedRequest)
	}
	return &req, nil
}

// EncodeSearchRequest serializes a web search body.
func EncodeSearchRequest(req *SearchRequest) ([]byte, error) {
	if req == nil {
		return nil, fmt.Errorf("web: %w: nil request", media.ErrMalformedRequest)
	}
	return json.Marshal(req)
}

// DecodeSearchResponse parses unified search results.
func DecodeSearchResponse(body []byte) (*SearchResponse, error) {
	if len(body) == 0 {
		return nil, fmt.Errorf("web: %w", media.ErrMalformedRequest)
	}
	var resp SearchResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("web: %w", media.ErrMalformedRequest)
	}
	return &resp, nil
}

// FetchRequest is the web fetch body (upstream src/sse/handlers/fetch.js
// fields: url, format, max_characters).
type FetchRequest struct {
	URL           string `json:"url"`
	Format        string `json:"format,omitempty"`
	MaxCharacters int    `json:"max_characters,omitempty"`
}

// FetchResponse is the extracted-content result
// (audit/01-http-contracts.md:553 "JSON extracted content").
type FetchResponse struct {
	URL       string `json:"url"`
	Content   string `json:"content"`
	MediaType string `json:"media_type,omitempty"`
}

// DecodeFetchRequest parses a web fetch body.
func DecodeFetchRequest(body []byte) (*FetchRequest, error) {
	if len(body) == 0 {
		return nil, fmt.Errorf("web: %w", media.ErrMalformedRequest)
	}
	var req FetchRequest
	if err := json.Unmarshal(body, &req); err != nil {
		return nil, fmt.Errorf("web: %w", media.ErrMalformedRequest)
	}
	if req.URL == "" {
		return nil, fmt.Errorf("web: %w: missing url", media.ErrMalformedRequest)
	}
	return &req, nil
}

// EncodeFetchRequest serializes a web fetch body.
func EncodeFetchRequest(req *FetchRequest) ([]byte, error) {
	if req == nil {
		return nil, fmt.Errorf("web: %w: nil request", media.ErrMalformedRequest)
	}
	return json.Marshal(req)
}

// DecodeFetchResponse parses extracted content.
func DecodeFetchResponse(body []byte) (*FetchResponse, error) {
	if len(body) == 0 {
		return nil, fmt.Errorf("web: %w", media.ErrMalformedRequest)
	}
	var resp FetchResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("web: %w", media.ErrMalformedRequest)
	}
	return &resp, nil
}

// ProviderSearchConfig is the per-provider search configuration extracted
// from the upstream registry (decolua/9router @
// 79918c7830695bbca4a45c9fea4a42c3e9fd73d1).
type ProviderSearchConfig struct {
	DefaultMaxResults int
	MaxMaxResults     int
	TimeoutMs         int
	CacheTTLMs        int
	Authority         string
}

func searchConfig(pt provider.ProviderType, def, max, timeout, ttl int, authority string) ProviderSearchConfig {
	return ProviderSearchConfig{DefaultMaxResults: def, MaxMaxResults: max, TimeoutMs: timeout, CacheTTLMs: ttl, Authority: authority}
}

// SearchConfigs maps proven webSearch providers to their upstream registry
// limits (defaultMaxResults, maxMaxResults, timeoutMs, cacheTTLMs).
var SearchConfigs = map[provider.ProviderType]ProviderSearchConfig{
	"brave-search": searchConfig("brave-search", 5, 20, 10000, 300000, "upstream open-sse/providers/registry/brave-search.js:30-33"),
	"exa":          searchConfig("exa", 5, 100, 10000, 300000, "upstream open-sse/providers/registry/exa.js:31-34"),
	"google-pse":   searchConfig("google-pse", 5, 10, 10000, 300000, "upstream open-sse/providers/registry/google-pse.js:30-33"),
	"linkup":       searchConfig("linkup", 5, 50, 10000, 300000, "upstream open-sse/providers/registry/linkup.js:29-32"),
	"searchapi":    searchConfig("searchapi", 5, 100, 10000, 300000, "upstream open-sse/providers/registry/searchapi.js:30-33"),
	"searxng":      searchConfig("searxng", 5, 50, 10000, 180000, "upstream open-sse/providers/registry/searxng.js:30-33"),
	"serper":       searchConfig("serper", 5, 100, 10000, 300000, "upstream open-sse/providers/registry/serper.js:30-33"),
	"tavily":       searchConfig("tavily", 5, 20, 10000, 300000, "upstream open-sse/providers/registry/tavily.js:31-34"),
	"youcom":       searchConfig("youcom", 5, 100, 10000, 300000, "upstream open-sse/providers/registry/youcom.js:30-33"),
}

// SearchConfigFor returns the proven search config for a provider; providers
// without a registry search-config block (openai, xai, minimax, perplexity,
// perplexity-agent) fall back to the upstream handler clamps
// (search/index.js:75 maxResults clamp, :96 timeout).
func SearchConfigFor(pt provider.ProviderType) (ProviderSearchConfig, bool) {
	cfg, ok := SearchConfigs[pt]
	if ok {
		return cfg, true
	}
	return ProviderSearchConfig{
		DefaultMaxResults: 5,
		MaxMaxResults:     100,
		TimeoutMs:         10000,
		CacheTTLMs:        0,
		Authority:         "upstream open-sse/handlers/search/index.js:75,96 (handler defaults)",
	}, true
}

package web

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"time"

	"gorouter/internal/domain/provider"
	"gorouter/internal/engine/media"
)

// SearchEndpoint is the web search path (audit/01-http-contracts.md:552;
// upstream src/app/api/v1/search/route.js:17-20).
const SearchEndpoint = "/v1/search"

// FetchEndpoint is the web fetch path (audit/01-http-contracts.md:553;
// upstream src/app/api/v1/web/fetch/route.js:17-20).
const FetchEndpoint = "/v1/web/fetch"

// GlobalTimeout is the upstream search handler round-trip ceiling
// (open-sse/handlers/search/index.js:14 GLOBAL_TIMEOUT_MS = 15000).
const GlobalTimeout = 15 * time.Second

// FetchTimeout is the upstream fetch handler round-trip ceiling
// (open-sse/handlers/fetch/index.js:4 DEFAULT_TIMEOUT_MS = 15000).
const FetchTimeout = 15 * time.Second

// MaxFetchCharacters is the largest upstream fetch maxCharacters ceiling
// (open-sse/providers/registry/firecrawl.js:31 and jina-reader.js:31,
// maxCharacters: 200000).
const MaxFetchCharacters = 200000

// Executor executes web search and fetch requests.
type Executor struct {
	client       *media.Client
	searchPath   string
	fetchPath    string
	maxResponse  int64
	ceilingAuth  string
	searchCfgFor func(provider.ProviderType) (ProviderSearchConfig, bool)
}

// Option configures an Executor.
type Option func(*Executor)

// WithResponseCeiling bounds upstream response bodies.
func WithResponseCeiling(max int64, authority string) Option {
	return func(e *Executor) { e.maxResponse, e.ceilingAuth = max, authority }
}

// NewExecutor creates a web executor with default endpoints, the platform
// body ceiling, and the upstream search-config table.
func NewExecutor(client *media.Client, opts ...Option) *Executor {
	e := &Executor{
		client:       client,
		searchPath:   SearchEndpoint,
		fetchPath:    FetchEndpoint,
		maxResponse:  media.BodyCeiling,
		ceilingAuth:  media.BodyCeilingAuthority,
		searchCfgFor: SearchConfigFor,
	}
	for _, opt := range opts {
		opt(e)
	}
	return e
}

// Search runs a web search. max_results is clamped to the provider's proven
// upstream maximum (registry maxMaxResults / handler clamp at
// search/index.js:75).
func (e *Executor) Search(ctx context.Context, req *SearchRequest, pt provider.ProviderType, account *provider.Account) (*SearchResponse, error) {
	if req == nil {
		return nil, fmt.Errorf("web: %w: nil request", media.ErrMalformedRequest)
	}
	cfg, _ := e.searchCfgFor(pt)
	if req.MaxResults <= 0 {
		req.MaxResults = cfg.DefaultMaxResults
	}
	if req.MaxResults > cfg.MaxMaxResults {
		req.MaxResults = cfg.MaxMaxResults
	}
	body, err := EncodeSearchRequest(req)
	if err != nil {
		return nil, err
	}
	httpReq, err := e.client.BuildRequest(ctx, http.MethodPost, e.searchPath, bytes.NewReader(body), "application/json", account)
	if err != nil {
		return nil, err
	}
	if cfg.TimeoutMs > 0 {
		ctx, cancel := context.WithTimeout(ctx, time.Duration(cfg.TimeoutMs)*time.Millisecond)
		defer cancel()
		httpReq = httpReq.WithContext(ctx)
	}
	resp, err := e.client.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if err := classifyStatus(resp); err != nil {
		return nil, err
	}
	data, err := media.ReadLimited(resp.Body, e.maxResponse, e.ceilingAuth)
	if err != nil {
		return nil, err
	}
	return DecodeSearchResponse(data)
}

// Fetch extracts content from a URL. max_characters is bounded by the
// largest proven upstream ceiling (MaxFetchCharacters).
func (e *Executor) Fetch(ctx context.Context, req *FetchRequest, account *provider.Account) (*FetchResponse, error) {
	if req == nil {
		return nil, fmt.Errorf("web: %w: nil request", media.ErrMalformedRequest)
	}
	if req.MaxCharacters <= 0 || req.MaxCharacters > MaxFetchCharacters {
		req.MaxCharacters = MaxFetchCharacters
	}
	body, err := EncodeFetchRequest(req)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, FetchTimeout)
	defer cancel()
	httpReq, err := e.client.BuildRequest(ctx, http.MethodPost, e.fetchPath, bytes.NewReader(body), "application/json", account)
	if err != nil {
		return nil, err
	}
	resp, err := e.client.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if err := classifyStatus(resp); err != nil {
		return nil, err
	}
	data, err := media.ReadLimited(resp.Body, e.maxResponse, e.ceilingAuth)
	if err != nil {
		return nil, err
	}
	return DecodeFetchResponse(data)
}

func classifyStatus(resp *http.Response) error {
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return nil
	}
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
	msg := media.RedactErrorString(string(body))
	switch resp.StatusCode {
	case http.StatusUnauthorized, http.StatusForbidden:
		return media.NewAuthError(resp.StatusCode, msg)
	case http.StatusTooManyRequests:
		return media.NewRateLimitError(resp.StatusCode, msg)
	default:
		return media.NewUpstreamError(resp.StatusCode, msg)
	}
}

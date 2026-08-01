package embeddings

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"

	"gorouter/internal/domain/provider"
	"gorouter/internal/engine/media"
)

// DefaultEndpoint is the OpenAI-compatible embeddings path, proven by the
// endpoint detector (internal/engine/formats/detect.go:32 maps /v1/embeddings
// → FormatOpenAICompat).
const DefaultEndpoint = "/v1/embeddings"

// Executor executes embeddings requests against an OpenAI-compatible
// embeddings endpoint (audit/01-http-contracts.md:544).
type Executor struct {
	client      *media.Client
	endpoint    string
	maxResponse int64
	ceilingAuth string
}

// Option configures an Executor.
type Option func(*Executor)

// WithEndpoint overrides the embeddings endpoint path.
func WithEndpoint(path string) Option {
	return func(e *Executor) { e.endpoint = path }
}

// WithResponseCeiling bounds the upstream response body.
func WithResponseCeiling(max int64, authority string) Option {
	return func(e *Executor) {
		e.maxResponse = max
		e.ceilingAuth = authority
	}
}

// NewExecutor creates an embeddings executor. The client base URL must be
// configured by the caller (provider configuration); media never invents a
// default host (fail closed via media.ErrBaseURLUnconfigured).
func NewExecutor(client *media.Client, opts ...Option) *Executor {
	e := &Executor{
		client:      client,
		endpoint:    DefaultEndpoint,
		maxResponse: media.BodyCeiling,
		ceilingAuth: media.BodyCeilingAuthority,
	}
	for _, opt := range opts {
		opt(e)
	}
	return e
}

// Execute runs the embeddings request and returns the parsed response.
// Context cancellation aborts the upstream request; upstream error bodies are
// classified as auth/rate-limit/upstream typed errors with credential
// redaction (media.NewAuthError etc.).
func (e *Executor) Execute(ctx context.Context, req *Request, account *provider.Account) (*Response, error) {
	if req == nil {
		return nil, fmt.Errorf("embeddings: %w: nil request", media.ErrMalformedRequest)
	}
	body, err := EncodeRequest(req)
	if err != nil {
		return nil, err
	}
	httpReq, err := e.client.BuildRequest(ctx, http.MethodPost, e.endpoint, bytes.NewReader(body), "application/json", account)
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
	return DecodeResponse(data)
}

// classifyStatus maps non-2xx upstream statuses to typed media errors with
// truncated, redacted bodies (mirrors generic provider checkResponseStatus).
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

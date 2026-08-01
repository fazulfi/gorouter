package generic

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"time"

	"gorouter/internal/domain/engine"
	"gorouter/internal/domain/engine/stream"
	"gorouter/internal/domain/provider"
	"gorouter/internal/shared"
)

// CompatibleOpenAIExecutor implements engine.Executor for any OpenAI-compatible
// provider. It sends requests to /v1/chat/completions and handles both
// streaming (SSE) and non-streaming responses.
//
// Features:
//   - Custom base URL (unrestricted per decisions #136, #137, #376)
//   - Custom model preservation
//   - 401/403 → definitive auth rejection
//   - 429/5xx → retryable upstream errors
//   - Credential redaction from error metadata
//   - Response-body close guarantee
type CompatibleOpenAIExecutor struct {
	client    *Client
	model     string // optional override; empty = use req.Model
	streamCfg StreamingConfig
	format    engine.RequestFormat // preferred format
}

// CompatibleOpenAIOption configures a CompatibleOpenAIExecutor.
type CompatibleOpenAIOption func(*CompatibleOpenAIExecutor)

// WithOpenAIModel sets a model override for the executor.
func WithOpenAIModel(model string) CompatibleOpenAIOption {
	return func(e *CompatibleOpenAIExecutor) {
		e.model = model
	}
}

// WithOpenAIFormat sets the preferred request format.
func WithOpenAIFormat(f engine.RequestFormat) CompatibleOpenAIOption {
	return func(e *CompatibleOpenAIExecutor) {
		e.format = f
	}
}

// WithOpenAIStreamingConfig replaces the default streaming timeout config.
func WithOpenAIStreamingConfig(cfg StreamingConfig) CompatibleOpenAIOption {
	return func(e *CompatibleOpenAIExecutor) {
		e.streamCfg = cfg
	}
}

// NewCompatibleOpenAIExecutor creates a new CompatibleOpenAIExecutor.
// The client provides the HTTP transport and base URL; options customise
// model, streaming, and format preferences.
func NewCompatibleOpenAIExecutor(client *Client, opts ...CompatibleOpenAIOption) *CompatibleOpenAIExecutor {
	e := &CompatibleOpenAIExecutor{
		client:    client,
		streamCfg: DefaultStreamingConfig(),
		format:    engine.FormatOpenAIChat,
	}
	for _, opt := range opts {
		opt(e)
	}
	return e
}

// SupportsFormat returns true for OpenAI chat and compat formats.
func (e *CompatibleOpenAIExecutor) SupportsFormat(format engine.RequestFormat) bool {
	return format == engine.FormatOpenAIChat || format == engine.FormatOpenAICompat
}

// ProviderType returns ProviderOpenAI.
func (e *CompatibleOpenAIExecutor) ProviderType() provider.ProviderType {
	return provider.ProviderOpenAI
}

// Execute sends a non-streaming request to /v1/chat/completions and returns
// the complete parsed response.
func (e *CompatibleOpenAIExecutor) Execute(ctx context.Context, req *engine.Request, account *provider.Account) (*engine.Response, error) {
	body := selectBody(req)

	httpReq, err := e.client.BuildRequest(ctx, http.MethodPost, "/v1/chat/completions", body, account)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}

	// Inject stream=false into the payload if not already set.
	httpReq, err = injectStreamParam(httpReq, false)
	if err != nil {
		return nil, fmt.Errorf("inject stream param: %w", err)
	}

	resp, err := e.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("execute request: %w", err)
	}
	defer resp.Body.Close()

	if err := checkResponseStatus(resp); err != nil {
		return nil, err
	}

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response body: %w", err)
	}

	return parseResponse(req, resp, respBody)
}

// ExecuteStream sends a streaming request to /v1/chat/completions and returns
// a response with an active stream. The caller must consume chunks from the
// stream until it is closed.
func (e *CompatibleOpenAIExecutor) ExecuteStream(ctx context.Context, req *engine.Request, account *provider.Account) (*engine.Response, error) {
	body := selectBody(req)

	httpReq, err := e.client.BuildRequest(ctx, http.MethodPost, "/v1/chat/completions", body, account)
	if err != nil {
		return nil, fmt.Errorf("build stream request: %w", err)
	}

	httpReq, err = injectStreamParam(httpReq, true)
	if err != nil {
		return nil, fmt.Errorf("inject stream param: %w", err)
	}
	httpReq.Header.Set("Accept", "text/event-stream")

	resp, err := e.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("execute stream request: %w", err)
	}

	if err := checkResponseStatus(resp); err != nil {
		_ = resp.Body.Close()
		return nil, err
	}

	st := stream.NewStream(ctx, 64)

	// Stream directly from resp.Body — no buffering the entire response.
	go streamSSE(ctx, st, resp.Body, resp.Body, e.streamCfg, e.streamCfg.FirstChunkTimeout)

	return &engine.Response{
		RequestID:  req.ID,
		Stream:     st,
		Model:      resolveModel(e.model, req),
		StatusCode: resp.StatusCode,
		Headers:    flattenHeaders(resp.Header),
	}, nil
}

// -- shared helpers -----------------------------------------------------------

// selectBody returns MappedBody if present, otherwise RawBody.
func selectBody(req *engine.Request) []byte {
	if len(req.MappedBody) > 0 {
		return req.MappedBody
	}
	return req.RawBody
}

// resolveModel returns the model to use, preferring executor override.
func resolveModel(override string, req *engine.Request) string {
	if override != "" {
		return override
	}
	if req != nil && req.Model != "" {
		return req.Model
	}
	return "gpt-4o"
}

// injectStreamParam reads the JSON body, sets stream=<v>, and returns a new
// request with the modified body. If the body is not valid JSON the request is
// returned unchanged.
func injectStreamParam(req *http.Request, stream bool) (*http.Request, error) {
	if req.Body == nil {
		return req, nil
	}
	body, err := io.ReadAll(req.Body)
	if err != nil {
		return req, nil //nolint:nilerr // best-effort
	}
	_ = req.Body.Close()

	var payload map[string]interface{}
	if err := json.Unmarshal(body, &payload); err != nil {
		// Not JSON — leave as-is.
		req.Body = io.NopCloser(bytes.NewReader(body))
		return req, nil
	}

	payload["stream"] = stream

	newBody, err := json.Marshal(payload)
	if err != nil {
		return req, nil //nolint:nilerr // best-effort
	}

	req.Body = io.NopCloser(bytes.NewReader(newBody))
	req.ContentLength = int64(len(newBody))
	return req, nil
}

// checkResponseStatus checks the HTTP response status code and returns an
// appropriate AppError for non-2xx responses.
func checkResponseStatus(resp *http.Response) error {
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		bodyBytes, _ := io.ReadAll(resp.Body)
		bodyStr := string(bodyBytes)
		_ = resp.Body.Close()

		switch resp.StatusCode {
		case http.StatusUnauthorized, http.StatusForbidden:
			return newAuthError(resp.StatusCode, bodyStr)
		case http.StatusTooManyRequests:
			return newRateLimitError(resp.StatusCode, bodyStr)
		default:
			return newUpstreamError(resp.StatusCode, bodyStr)
		}
	}
	return nil
}

// parseResponse unmarshals the upstream JSON response into an engine.Response.
func parseResponse(req *engine.Request, resp *http.Response, body []byte) (*engine.Response, error) {
	var raw struct {
		ID      string `json:"id"`
		Object  string `json:"object"`
		Created int64  `json:"created"`
		Model   string `json:"model"`
		Usage   *struct {
			PromptTokens     int `json:"prompt_tokens"`
			CompletionTokens int `json:"completion_tokens"`
			TotalTokens      int `json:"total_tokens"`
		} `json:"usage"`
		Choices []struct {
			Index        int    `json:"index"`
			FinishReason string `json:"finish_reason"`
			Message      *struct {
				Role    string `json:"role"`
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}

	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("parse response JSON: %w", err)
	}

	engResp := &engine.Response{
		RequestID:  req.ID,
		Body:       body,
		Model:      raw.Model,
		StatusCode: resp.StatusCode,
		Headers:    flattenHeaders(resp.Header),
	}

	if raw.Usage != nil {
		engResp.Usage = &engine.Usage{
			PromptTokens:     raw.Usage.PromptTokens,
			CompletionTokens: raw.Usage.CompletionTokens,
			TotalTokens:      raw.Usage.TotalTokens,
		}
	}

	return engResp, nil
}

// flattenHeaders converts an http.Header map into a plain map[string]string.
func flattenHeaders(h http.Header) map[string]string {
	out := make(map[string]string, len(h))
	for k, v := range h {
		if len(v) > 0 {
			out[k] = v[0]
		}
	}
	return out
}

// -- streaming ----------------------------------------------------------------

// StreamingConfig controls timeout behaviour for streaming SSE responses.
type StreamingConfig struct {
	FirstChunkTimeout time.Duration
	StallTimeout      time.Duration
	DisconnectGrace   time.Duration
}

// DefaultStreamingConfig returns a StreamingConfig with upstream-standard defaults.
func DefaultStreamingConfig() StreamingConfig {
	return StreamingConfig{
		FirstChunkTimeout: 200 * time.Second,
		StallTimeout:      6 * time.Minute,
		DisconnectGrace:   500 * time.Millisecond,
	}
}

// streamSSE reads SSE lines from reader, parses them, and pushes chunks
// into the given stream. Terminal event is [DONE].
func streamSSE(
	ctx context.Context,
	st *stream.Stream,
	reader io.Reader,
	closer io.Closer,
	cfg StreamingConfig,
	firstChunkTimeout time.Duration,
) {
	closerOnce := &atomicClose{}

	defer func() {
		closerOnce.Close(closer)
		if r := recover(); r != nil {
			st.Cancel(fmt.Errorf("panic in generic SSE streamer: %v", r))
		}
	}()

	var gotFirstChunk atomic.Bool

	// Context cancellation monitor.
	go func() {
		select {
		case <-ctx.Done():
			closerOnce.Close(closer)
		case <-st.Done():
		}
	}()

	// First-chunk timeout.
	if firstChunkTimeout > 0 {
		go func() {
			timer := time.NewTimer(firstChunkTimeout)
			defer timer.Stop()
			select {
			case <-timer.C:
				if !gotFirstChunk.Load() {
					closerOnce.Close(closer)
				}
			case <-st.Done():
			}
		}()
	}

	var lastByteTime atomic.Int64
	lastByteTime.Store(time.Now().UnixNano())

	if cfg.StallTimeout > 0 {
		go func() {
			ticker := time.NewTicker(1 * time.Second)
			defer ticker.Stop()
			for {
				select {
				case <-ticker.C:
					if gotFirstChunk.Load() {
						lbt := time.Unix(0, lastByteTime.Load())
						if time.Since(lbt) > cfg.StallTimeout {
							closerOnce.Close(closer)
							return
						}
					}
				case <-st.Done():
					return
				}
			}
		}()
	}

	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 0, 64*1024), 256*1024)

	var eventType string

	for scanner.Scan() {
		if cfg.StallTimeout > 0 {
			lastByteTime.Store(time.Now().UnixNano())
		}

		line := scanner.Text()

		if line == "" {
			eventType = ""
			continue
		}

		if strings.HasPrefix(line, "event: ") {
			eventType = strings.TrimPrefix(line, "event: ")
			continue
		}

		if strings.HasPrefix(line, "data: ") {
			data := strings.TrimPrefix(line, "data: ")
			gotFirstChunk.Store(true)

			if data == "[DONE]" {
				st.Push(stream.Chunk{IsFinal: true})
				st.Close()
				return
			}

			chunk := stream.Chunk{
				Data:  []byte(data),
				Event: eventType,
			}

			if !st.Push(chunk) {
				return
			}
			continue
		}
	}

	err := scanner.Err()
	if err != nil || !gotFirstChunk.Load() {
		if !gotFirstChunk.Load() {
			st.Cancel(stream.ErrPeekTimeout)
			return
		}
		if err != nil {
			select {
			case <-ctx.Done():
				st.Cancel(ctx.Err())
			case <-st.Done():
			default:
				st.Cancel(fmt.Errorf("SSE scanner error: %w", err))
			}
			return
		}
	}

	st.Push(stream.Chunk{IsFinal: true})
	st.Close()
}

// atomicClose wraps an io.Closer so it can be called multiple times safely.
type atomicClose struct {
	closed atomic.Bool
}

func (a *atomicClose) Close(closer io.Closer) {
	if a.closed.Swap(true) {
		return
	}
	_ = closer.Close()
}

// -- error constructors -------------------------------------------------------

// newAuthError creates an AppError for 401/403 responses.
func newAuthError(statusCode int, body string) *shared.AppError {
	return &shared.AppError{
		Code:       shared.ErrUnauthorized,
		Message:    "upstream authentication failed",
		HTTPStatus: statusCode,
		Details: map[string]interface{}{
			"status_code": statusCode,
			"body":        truncateString(body, 512),
		},
	}
}

// newRateLimitError creates an AppError for 429 responses.
func newRateLimitError(statusCode int, body string) *shared.AppError {
	return &shared.AppError{
		Code:       shared.ErrRateLimited,
		Message:    "upstream rate limit exceeded",
		HTTPStatus: statusCode,
		Details: map[string]interface{}{
			"status_code": statusCode,
			"body":        truncateString(body, 512),
		},
	}
}

// newUpstreamError creates an AppError for other non-2xx responses.
func newUpstreamError(statusCode int, body string) *shared.AppError {
	return &shared.AppError{
		Code:       shared.ErrInternal,
		Message:    "upstream API returned error",
		HTTPStatus: statusCode,
		Details: map[string]interface{}{
			"status_code": statusCode,
			"body":        truncateString(body, 1024),
		},
	}
}

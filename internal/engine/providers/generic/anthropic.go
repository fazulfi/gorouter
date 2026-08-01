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
)

// CompatibleAnthropicExecutor implements engine.Executor for any
// Anthropic-compatible provider. It sends requests to /v1/messages and handles
// both streaming (SSE) and non-streaming responses using the Anthropic
// Messages API wire format.
//
// Features:
//   - Custom base URL (unrestricted per decisions #136, #137, #376)
//   - Custom model preservation
//   - 401/403 → definitive auth rejection
//   - 429/5xx → retryable upstream errors
//   - x-api-key header authentication
//   - Credential redaction from error metadata
//   - Response-body close guarantee
type CompatibleAnthropicExecutor struct {
	client    *Client
	model     string // optional override; empty = use req.Model
	streamCfg StreamingConfig
}

// CompatibleAnthropicOption configures a CompatibleAnthropicExecutor.
type CompatibleAnthropicOption func(*CompatibleAnthropicExecutor)

// WithAnthropicModel sets a model override for the executor.
func WithAnthropicModel(model string) CompatibleAnthropicOption {
	return func(e *CompatibleAnthropicExecutor) {
		e.model = model
	}
}

// WithAnthropicStreamingConfig replaces the default streaming timeout config.
func WithAnthropicStreamingConfig(cfg StreamingConfig) CompatibleAnthropicOption {
	return func(e *CompatibleAnthropicExecutor) {
		e.streamCfg = cfg
	}
}

// NewCompatibleAnthropicExecutor creates a new CompatibleAnthropicExecutor.
func NewCompatibleAnthropicExecutor(client *Client, opts ...CompatibleAnthropicOption) *CompatibleAnthropicExecutor {
	e := &CompatibleAnthropicExecutor{
		client:    client,
		streamCfg: DefaultStreamingConfig(),
	}
	for _, opt := range opts {
		opt(e)
	}
	return e
}

// SupportsFormat returns true for FormatAnthropic.
func (e *CompatibleAnthropicExecutor) SupportsFormat(format engine.RequestFormat) bool {
	return format == engine.FormatAnthropic
}

// ProviderType returns ProviderAnthropic.
func (e *CompatibleAnthropicExecutor) ProviderType() provider.ProviderType {
	return provider.ProviderAnthropic
}

// Execute sends a non-streaming request to /v1/messages and returns the
// complete parsed response.
func (e *CompatibleAnthropicExecutor) Execute(ctx context.Context, req *engine.Request, account *provider.Account) (*engine.Response, error) {
	body := selectBody(req)

	// Build request — the Client handles x-api-key auth for anthropic_api_key
	// auth type. Override the auth type so the client uses the right header.
	acct := *account
	if acct.AuthType == "" || acct.AuthType == "api_key" {
		acct.AuthType = "anthropic_api_key"
	}

	httpReq, err := e.client.BuildRequest(ctx, http.MethodPost, "/v1/messages", body, &acct)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}

	resp, err := e.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("execute request: %w", err)
	}
	defer resp.Body.Close()

	if err := checkAnthropicResponseStatus(resp); err != nil {
		return nil, err
	}

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response body: %w", err)
	}

	return parseAnthropicResponse(req, resp, respBody)
}

// ExecuteStream sends a streaming request to /v1/messages and returns a
// response with an active stream.
func (e *CompatibleAnthropicExecutor) ExecuteStream(ctx context.Context, req *engine.Request, account *provider.Account) (*engine.Response, error) {
	body := selectBody(req)

	acct := *account
	if acct.AuthType == "" || acct.AuthType == "api_key" {
		acct.AuthType = "anthropic_api_key"
	}

	httpReq, err := e.client.BuildRequest(ctx, http.MethodPost, "/v1/messages", body, &acct)
	if err != nil {
		return nil, fmt.Errorf("build stream request: %w", err)
	}

	httpReq, err = injectAnthropicStreamParam(httpReq, true)
	if err != nil {
		return nil, fmt.Errorf("inject stream param: %w", err)
	}
	httpReq.Header.Set("Accept", "text/event-stream")
	httpReq.Header.Set("anthropic-version", "2023-06-01")

	resp, err := e.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("execute stream request: %w", err)
	}

	if err := checkAnthropicResponseStatus(resp); err != nil {
		_ = resp.Body.Close()
		return nil, err
	}

	st := stream.NewStream(ctx, 64)

	go streamAnthropicSSE(ctx, st, resp.Body, resp.Body, e.streamCfg, e.streamCfg.FirstChunkTimeout)

	return &engine.Response{
		RequestID:  req.ID,
		Stream:     st,
		Model:      resolveModel(e.model, req),
		StatusCode: resp.StatusCode,
		Headers:    flattenHeaders(resp.Header),
	}, nil
}

// checkAnthropicResponseStatus checks status with Anthropic-specific error codes.
func checkAnthropicResponseStatus(resp *http.Response) error {
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

// injectAnthropicStreamParam sets stream=true in the JSON body.
func injectAnthropicStreamParam(req *http.Request, stream bool) (*http.Request, error) {
	if req.Body == nil {
		return req, nil
	}
	body, err := io.ReadAll(req.Body)
	if err != nil {
		return req, nil //nolint:nilerr
	}
	_ = req.Body.Close()

	var payload map[string]interface{}
	if err := json.Unmarshal(body, &payload); err != nil {
		req.Body = io.NopCloser(bytes.NewReader(body))
		return req, nil
	}

	payload["stream"] = stream

	newBody, err := json.Marshal(payload)
	if err != nil {
		return req, nil //nolint:nilerr
	}

	req.Body = io.NopCloser(bytes.NewReader(newBody))
	req.ContentLength = int64(len(newBody))
	return req, nil
}

// parseAnthropicResponse unmarshals an Anthropic Messages API response.
func parseAnthropicResponse(req *engine.Request, resp *http.Response, body []byte) (*engine.Response, error) {
	var raw struct {
		ID      string `json:"id"`
		Type    string `json:"type"`
		Role    string `json:"role"`
		Model   string `json:"model"`
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text,omitempty"`
		} `json:"content"`
		StopReason string `json:"stop_reason"`
		Usage      *struct {
			InputTokens  int `json:"input_tokens"`
			OutputTokens int `json:"output_tokens"`
		} `json:"usage"`
	}

	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("parse Anthropic response JSON: %w", err)
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
			PromptTokens:     raw.Usage.InputTokens,
			CompletionTokens: raw.Usage.OutputTokens,
			TotalTokens:      raw.Usage.InputTokens + raw.Usage.OutputTokens,
		}
	}

	return engResp, nil
}

// streamAnthropicSSE reads SSE lines from the Anthropic Events API and pushes
// chunks into the stream. Terminal event is message_stop.
func streamAnthropicSSE(
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
			st.Cancel(fmt.Errorf("panic in Anthropic SSE streamer: %v", r))
		}
	}()

	var gotFirstChunk atomic.Bool

	go func() {
		select {
		case <-ctx.Done():
			closerOnce.Close(closer)
		case <-st.Done():
		}
	}()

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

			// Anthropic terminal events.
			if eventType == "message_stop" || eventType == "error" {
				st.Push(stream.Chunk{
					Data:    []byte(data),
					Event:   eventType,
					IsFinal: true,
				})
				if eventType == "error" {
					st.Cancel(fmt.Errorf("Anthropic stream error: %s", data))
				} else {
					st.Close()
				}
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
				st.Cancel(fmt.Errorf("Anthropic SSE scanner error: %w", err))
			}
			return
		}
	}

	st.Push(stream.Chunk{IsFinal: true})
	st.Close()
}

// Ensure CompatibleAnthropicExecutor implements engine.Executor.
var _ engine.Executor = (*CompatibleAnthropicExecutor)(nil)

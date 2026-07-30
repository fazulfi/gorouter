package executor

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"gorouter/internal/domain/engine"
	"gorouter/internal/domain/engine/stream"
	"gorouter/internal/domain/provider"
)

// OpenAIChatExecutor implements engine.Executor for OpenAI-compatible chat
// completion APIs (/v1/chat/completions). This is the default executor used
// for most providers since the vast majority expose an OpenAI-compatible
// wire protocol.
type OpenAIChatExecutor struct {
	client  *http.Client
	model   string // optional override; empty means use the model from the request
}

// NewOpenAIChatExecutor creates an OpenAIChatExecutor. The transport is used
// to create the underlying http.Client.
func NewOpenAIChatExecutor(transport http.RoundTripper) *OpenAIChatExecutor {
	return &OpenAIChatExecutor{
		client: &http.Client{Transport: transport},
	}
}

// SupportsFormat returns true for OpenAI chat and compat formats.
func (e *OpenAIChatExecutor) SupportsFormat(format engine.RequestFormat) bool {
	return format == engine.FormatOpenAIChat || format == engine.FormatOpenAICompat
}

// ProviderType returns ProviderOpenAI.
func (e *OpenAIChatExecutor) ProviderType() provider.ProviderType {
	return provider.ProviderOpenAI
}

// Execute sends a non-streaming chat completion request and returns the
// complete parsed response.
func (e *OpenAIChatExecutor) Execute(ctx context.Context, req *engine.Request, account *provider.Account) (*engine.Response, error) {
	body := e.selectBody(req)

	httpReq, err := e.buildRequest(ctx, req, account, body, false)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}

	resp, err := e.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("execute request: %w", err)
	}
	defer resp.Body.Close()

	if err := e.checkResponseStatus(resp); err != nil {
		return nil, err
	}

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response body: %w", err)
	}

	return e.parseResponse(req, resp, respBody)
}

// ExecuteStream sends a streaming chat completion request and returns a
// response carrying an active stream reference. The caller must consume
// chunks from the stream until it is closed.
func (e *OpenAIChatExecutor) ExecuteStream(ctx context.Context, req *engine.Request, account *provider.Account) (*engine.Response, error) {
	body := e.selectBody(req)

	httpReq, err := e.buildRequest(ctx, req, account, body, true)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}

	resp, err := e.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("execute stream request: %w", err)
	}

	if err := e.checkResponseStatus(resp); err != nil {
		_ = resp.Body.Close()
		return nil, err
	}

	// Read the entire response body as bytes so we can parse SSE from it.
	raw, err := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if err != nil {
		return nil, fmt.Errorf("read stream response body: %w", err)
	}

	st := stream.NewStream(ctx, 64)

	// Spawn a goroutine that parses SSE lines from the full body and pushes
	// chunks into the stream.
	go e.readSSEIntoStream(ctx, st, raw)

	return &engine.Response{
		RequestID: req.ID,
		Stream:    st,
		Model:     e.resolveModel(req),
		StatusCode: resp.StatusCode,
		Headers:   flattenHeaders(resp.Header),
	}, nil
}

// buildRequest constructs an *http.Request for the OpenAI chat completions API.
func (e *OpenAIChatExecutor) buildRequest(ctx context.Context, req *engine.Request, account *provider.Account, body []byte, stream bool) (*http.Request, error) {
	// Determine base URL.
	baseURL := e.resolveBaseURL(req)

	// Build the JSON payload.
	var payload map[string]interface{}
	if err := json.Unmarshal(body, &payload); err != nil {
		// If we can't parse as JSON, send the raw body as-is.
		payload = nil
	}

	if payload == nil {
		payload = make(map[string]interface{})
	}
	payload["stream"] = stream

	// Inject model if not already set.
	if _, ok := payload["model"]; !ok {
		payload["model"] = e.resolveModel(req)
	}

	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("marshal payload: %w", err)
	}

	url := fmt.Sprintf("%s/v1/chat/completions", strings.TrimRight(baseURL, "/"))

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payloadBytes))
	if err != nil {
		return nil, fmt.Errorf("new request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+resolveAPIKey(account))
	if stream {
		httpReq.Header.Set("Accept", "text/event-stream")
	}

	// Copy any original headers that should be forwarded.
	for k, v := range req.Headers {
		if k == "Content-Type" || k == "Authorization" || k == "Accept" {
			continue
		}
		httpReq.Header.Set(k, v)
	}

	return httpReq, nil
}

// selectBody returns the mapped body if present, otherwise the raw body.
func (e *OpenAIChatExecutor) selectBody(req *engine.Request) []byte {
	if len(req.MappedBody) > 0 {
		return req.MappedBody
	}
	return req.RawBody
}

// resolveModel returns the model to use for the request.
func (e *OpenAIChatExecutor) resolveModel(req *engine.Request) string {
	if e.model != "" {
		return e.model
	}
	if req.Model != "" {
		return req.Model
	}
	return "gpt-4o" // sensible default
}

// resolveBaseURL returns the base URL to use. When the request doesn't carry
// provider info via headers, it falls back to a sensible default.
func (e *OpenAIChatExecutor) resolveBaseURL(req *engine.Request) string {
	if v, ok := req.Headers["X-Base-URL"]; ok && v != "" {
		return v
	}
	return "https://api.openai.com"
}

// resolveAPIKey extracts the API key from the account. If the account has a
// CredentialRef that looks like a bearer token it is used directly; otherwise
// the ref is returned as-is (caller is responsible for resolving credentials).
func resolveAPIKey(account *provider.Account) string {
	if account == nil {
		return ""
	}
	return account.CredentialRef
}

// checkResponseStatus checks the HTTP response status code and returns an
// appropriate AppError for non-2xx responses.
func (e *OpenAIChatExecutor) checkResponseStatus(resp *http.Response) error {
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		bodyBytes, _ := io.ReadAll(resp.Body)
		bodyStr := string(bodyBytes)
		_ = resp.Body.Close()

		switch resp.StatusCode {
		case http.StatusUnauthorized, http.StatusForbidden:
			return NewAuthError(resp.StatusCode, bodyStr)
		case http.StatusTooManyRequests:
			return NewRateLimitError(resp.StatusCode, bodyStr)
		default:
			return NewUpstreamError(resp.StatusCode, bodyStr)
		}
	}
	return nil
}

// parseResponse unmarshals the upstream JSON response into an engine.Response.
func (e *OpenAIChatExecutor) parseResponse(req *engine.Request, resp *http.Response, body []byte) (*engine.Response, error) {
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
			Index        int `json:"index"`
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

// readSSEIntoStream reads an SSE-encoded byte slice and pushes each parsed
// chunk into the given stream. It runs in a goroutine and signals stream
// completion when the SSE stream ends.
func (e *OpenAIChatExecutor) readSSEIntoStream(ctx context.Context, st *stream.Stream, raw []byte) {
	defer func() {
		if r := recover(); r != nil {
			st.Cancel(fmt.Errorf("panic in SSE reader: %v", r))
		}
	}()

	scanner := bufio.NewScanner(bytes.NewReader(raw))
	// SSE lines can be long; set a generous buffer.
	scanner.Buffer(make([]byte, 0, 64*1024), 256*1024)

	var eventType string

	for scanner.Scan() {
		line := scanner.Text()

		// Empty line separates SSE events.
		if line == "" {
			eventType = ""
			continue
		}

		// Event type line.
		if strings.HasPrefix(line, "event: ") {
			eventType = strings.TrimPrefix(line, "event: ")
			continue
		}

		// Data line.
		if strings.HasPrefix(line, "data: ") {
			data := strings.TrimPrefix(line, "data: ")

			// "[DONE]" signals normal stream completion.
			if data == "[DONE]" {
				st.Close()
				return
			}

			chunk := stream.Chunk{
				Data:  []byte(data),
				Event: eventType,
			}

			if !st.Push(chunk) {
				// Stream was cancelled or closed.
				return
			}
			continue
		}

		// Ignore other SSE fields (e.g. "id:", "retry:").
	}

	if err := scanner.Err(); err != nil {
		st.Cancel(fmt.Errorf("SSE scanner error: %w", err))
		return
	}

	// Normal end of input without [DONE] — close cleanly.
	st.Close()
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

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

// peekBytes is the number of bytes to read from the Codex response body
// during the initial peek to detect SSE-level error conditions before
// handing the stream to the caller.
const codexPeekBytes = 256 * 1024

// codexSSERetryPatterns are SSE error messages that should trigger a retry
// on the same account.
var codexSSERetryPatterns = []string{
	"server_is_overloaded",
	"service_unavailable_error",
}

// codexSSEAccountFallbackPatterns are SSE error messages that indicate the
// current account is at capacity and a different account should be used.
var codexSSEAccountFallbackPatterns = []string{
	"selected model is at capacity",
	"model_at_capacity",
}

// responsesAPIAllowlist contains the only fields the Codex Responses API
// accepts. Anything else is stripped during transformation.
var responsesAPIAllowlist = map[string]bool{
	"model":           true,
	"input":           true,
	"instructions":    true,
	"tools":           true,
	"tool_choice":     true,
	"stream":          true,
	"store":           true,
	"reasoning":       true,
	"service_tier":    true,
	"include":         true,
	"prompt_cache_key": true,
	"client_metadata": true,
	"text":            true,
}

// CodexExecutor implements engine.Executor for OpenAI's Codex Responses API
// (/v1/responses). It handles the specialised Responses API SSE format and
// performs request normalisation that the standard chat-completions executor
// does not.
type CodexExecutor struct {
	client *http.Client
	model  string // optional override
}

// NewCodexExecutor creates a CodexExecutor with the given HTTP transport.
func NewCodexExecutor(transport http.RoundTripper) *CodexExecutor {
	return &CodexExecutor{
		client: &http.Client{Transport: transport},
	}
}

// SupportsFormat returns true for FormatCodexResponses.
func (e *CodexExecutor) SupportsFormat(format engine.RequestFormat) bool {
	return format == engine.FormatCodexResponses
}

// ProviderType returns the codex provider type.
func (e *CodexExecutor) ProviderType() provider.ProviderType {
	return ProviderCodex
}

// Execute sends a non-streaming request to the Codex Responses API. Since the
// Codex API always uses streaming internally, this method reads the full SSE
// stream and assembles a single response.
func (e *CodexExecutor) Execute(ctx context.Context, req *engine.Request, account *provider.Account) (*engine.Response, error) {
	httpReq, err := e.buildRequest(ctx, req, account, false)
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

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response body: %w", err)
	}

	// For non-streaming, return the raw response body. The caller is
	// responsible for interpreting it.
	return &engine.Response{
		RequestID:  req.ID,
		Body:       body,
		Model:      e.resolveModel(req),
		StatusCode: resp.StatusCode,
		Headers:    flattenHeaders(resp.Header),
	}, nil
}

// ExecuteStream sends a streaming request to the Codex Responses API and
// returns a response with an active stream. It implements the "peek" pattern:
// the first chunk is read synchronously before returning so that connection
// errors are surfaced immediately.
func (e *CodexExecutor) ExecuteStream(ctx context.Context, req *engine.Request, account *provider.Account) (*engine.Response, error) {
	httpReq, err := e.buildRequest(ctx, req, account, true)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}

	resp, err := e.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("execute stream request: %w", err)
	}

	if err := e.checkResponseStatus(resp); err != nil {
		resp.Body.Close()
		return nil, err
	}

	// Read the full body so we can peek at the first event synchronously.
	raw, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		return nil, fmt.Errorf("read stream response body: %w", err)
	}

	// Peek at the first event to detect SSE-level errors.
	firstEvent, firstData, err := e.peekFirstEvent(raw)
	if err != nil {
		return nil, fmt.Errorf("peek first event: %w", err)
	}

	if isCodexSSEError(firstData) {
		return nil, fmt.Errorf("codex SSE error: %s", extractCodexErrorMessage(firstData))
	}

	st := stream.NewStream(ctx, 64)

	// Push the first event if valid, then spawn a goroutine for the rest.
	done := make(chan struct{}, 1)
	go e.readCodexSSEIntoStream(st, raw, firstEvent, done)

	// Wait for first event to be pushed so the caller can immediately range.
	<-done

	return &engine.Response{
		RequestID:  req.ID,
		Stream:     st,
		Model:      e.resolveModel(req),
		StatusCode: resp.StatusCode,
		Headers:    flattenHeaders(resp.Header),
	}, nil
}

// buildRequest constructs an *http.Request for the Codex Responses API.
func (e *CodexExecutor) buildRequest(ctx context.Context, req *engine.Request, account *provider.Account, streamEnabled bool) (*http.Request, error) {
	baseURL := e.resolveBaseURL(req)
	body := e.selectBody(req)
	payload := e.transformRequest(body, streamEnabled)

	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("marshal payload: %w", err)
	}

	url := fmt.Sprintf("%s/v1/responses", strings.TrimRight(baseURL, "/"))

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payloadBytes))
	if err != nil {
		return nil, fmt.Errorf("new request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+resolveAPIKey(account))
	httpReq.Header.Set("Accept", "text/event-stream")

	// Codex-specific identity headers.
	httpReq.Header.Set("session_id", resolveSessionID(account))
	httpReq.Header.Set("originator", "gorouter")

	// Forward any provider-specific headers from the original request.
	for k, v := range req.Headers {
		switch k {
		case "Content-Type", "Authorization", "Accept", "session_id", "originator":
			continue
		}
		httpReq.Header.Set(k, v)
	}

	return httpReq, nil
}

// transformRequest normalises the request body for the Codex Responses API.
// It injects defaults, removes unsupported fields, and ensures the payload
// passes the backend's validation.
func (e *CodexExecutor) transformRequest(body []byte, stream bool) map[string]interface{} {
	payload := make(map[string]interface{})

	if len(body) > 0 {
		if err := json.Unmarshal(body, &payload); err != nil {
			payload = make(map[string]interface{})
		}
	}

	// Normalise the "input" field — Codex requires an array.
	e.normaliseInput(payload)

	// Inject model if missing.
	if _, ok := payload["model"]; !ok {
		payload["model"] = e.model
	}
	if pm, ok := payload["model"].(string); ok && pm == "" {
		payload["model"] = "gpt-4o-codex"
	}

	// Set stream flag.
	payload["stream"] = stream

	// Ensure store is false — Codex cannot resolve stored items when store is
	// true and we don't provide a previous_response_id.
	payload["store"] = false

	// Inject default instructions if missing.
	if instr, ok := payload["instructions"].(string); !ok || instr == "" {
		payload["instructions"] = "You are a helpful assistant."
	}

	// Strip unsupported parameters.
	for k := range payload {
		if !responsesAPIAllowlist[k] {
			delete(payload, k)
		}
	}

	return payload
}

// normaliseInput converts the input field to an array if it's a string, and
// ensures the array is non-empty.
func (e *CodexExecutor) normaliseInput(payload map[string]interface{}) {
	input, ok := payload["input"]
	if !ok || input == nil {
		payload["input"] = []interface{}{
			map[string]interface{}{
				"type":    "message",
				"role":    "user",
				"content": []interface{}{map[string]interface{}{"type": "input_text", "text": "..."}},
			},
		}
		return
	}

	switch v := input.(type) {
	case string:
		payload["input"] = []interface{}{
			map[string]interface{}{
				"type":    "message",
				"role":    "user",
				"content": []interface{}{map[string]interface{}{"type": "input_text", "text": v}},
			},
		}
	case []interface{}:
		if len(v) == 0 {
			payload["input"] = []interface{}{
				map[string]interface{}{
					"type":    "message",
					"role":    "user",
					"content": []interface{}{map[string]interface{}{"type": "input_text", "text": "..."}},
				},
			}
		}
	}
}

// selectBody returns the mapped body if present, otherwise the raw body.
func (e *CodexExecutor) selectBody(req *engine.Request) []byte {
	if len(req.MappedBody) > 0 {
		return req.MappedBody
	}
	return req.RawBody
}

// resolveModel returns the model to use for the request.
func (e *CodexExecutor) resolveModel(req *engine.Request) string {
	if e.model != "" {
		return e.model
	}
	if req.Model != "" {
		return req.Model
	}
	return "gpt-4o-codex"
}

// resolveBaseURL returns the base URL. When the request carries an X-Base-URL
// header that takes precedence.
func (e *CodexExecutor) resolveBaseURL(req *engine.Request) string {
	if v, ok := req.Headers["X-Base-URL"]; ok && v != "" {
		return v
	}
	return "https://api.openai.com"
}

// resolveSessionID returns a stable session identifier for the account.
func resolveSessionID(account *provider.Account) string {
	if account == nil {
		return "default"
	}
	return account.ID.String()
}

// checkResponseStatus checks the HTTP response status code.
func (e *CodexExecutor) checkResponseStatus(resp *http.Response) error {
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		bodyBytes, _ := io.ReadAll(resp.Body)
		bodyStr := string(bodyBytes)
		resp.Body.Close()

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

// peekFirstEvent scans the raw SSE body and returns the first event type and
// data payload. It consumes only the first full event (event + data pair) by
// reporting the byte offset at which remaining data begins.
func (e *CodexExecutor) peekFirstEvent(raw []byte) (eventType, data string, err error) {
	scanner := bufio.NewScanner(bytes.NewReader(raw))
	scanner.Buffer(make([]byte, 0, 64*1024), codexPeekBytes)

	for scanner.Scan() {
		line := scanner.Text()

		if strings.HasPrefix(line, "event: ") {
			eventType = strings.TrimPrefix(line, "event: ")
			continue
		}

		if strings.HasPrefix(line, "data: ") {
			data = strings.TrimPrefix(line, "data: ")
			return eventType, data, nil
		}

		// Empty line resets event type.
		if line == "" {
			eventType = ""
		}
	}

	return "", "", scanner.Err()
}

// isCodexSSEError checks whether an SSE data payload signals a Codex API error.
func isCodexSSEError(data string) bool {
	lower := strings.ToLower(data)
	for _, pat := range codexSSEAccountFallbackPatterns {
		if strings.Contains(lower, pat) {
			return true
		}
	}
	for _, pat := range codexSSERetryPatterns {
		if strings.Contains(lower, pat) {
			return true
		}
	}
	return false
}

// extractCodexErrorMessage extracts a human-readable error message from the
// SSE error data. If parsing fails it returns a generic fallback.
func extractCodexErrorMessage(data string) string {
	var parsed struct {
		Error *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal([]byte(data), &parsed); err == nil && parsed.Error != nil && parsed.Error.Message != "" {
		return parsed.Error.Message
	}
	return "upstream SSE error"
}

// readCodexSSEIntoStream parses all SSE events from raw bytes and pushes them
// into the stream. It runs in a goroutine and signals completion via an
// optional done channel for the first event.
func (e *CodexExecutor) readCodexSSEIntoStream(st *stream.Stream, raw []byte, firstEvent string, done chan<- struct{}) {
	defer func() {
		if r := recover(); r != nil {
			st.Cancel(fmt.Errorf("panic in Codex SSE reader: %v", r))
		}
	}()

	scanner := bufio.NewScanner(bytes.NewReader(raw))
	scanner.Buffer(make([]byte, 0, 64*1024), 256*1024)

	var eventType string
	var firstEventPushed bool

	for scanner.Scan() {
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

			// Skip the first event if we already peeked at it.
			if !firstEventPushed {
				firstEventPushed = true
				if data == firstEvent {
					if done != nil {
						done <- struct{}{}
					}
					continue
				}
			}

			if data == "[DONE]" {
				st.Close()
				if done != nil {
					close(done)
				}
				return
			}

			chunk := stream.Chunk{
				Data:  []byte(data),
				Event: eventType,
			}

			if isCodexSSEError(data) {
				chunk.Error = fmt.Errorf("codex SSE error: %s", extractCodexErrorMessage(data))
				st.Push(chunk)
				st.Cancel(chunk.Error)
				if done != nil {
					close(done)
				}
				return
			}

			if !st.Push(chunk) {
				if done != nil {
					close(done)
				}
				return
			}

			if done != nil {
				done <- struct{}{}
				close(done)
				done = nil
			}
			continue
		}
	}

	if err := scanner.Err(); err != nil {
		st.Cancel(fmt.Errorf("Codex SSE scanner error: %w", err))
	}
	if done != nil {
		close(done)
	}

	st.Close()
}

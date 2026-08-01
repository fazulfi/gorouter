package specialized

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

const codexPeekBytes = 256 * 1024

var codexSSERetryPatterns = []string{
	"server_is_overloaded",
	"service_unavailable_error",
}

var codexSSEAccountFallbackPatterns = []string{
	"selected model is at capacity",
	"model_at_capacity",
}

var responsesAPIAllowlist = map[string]bool{
	"model":            true,
	"input":            true,
	"instructions":     true,
	"tools":            true,
	"tool_choice":      true,
	"stream":           true,
	"store":            true,
	"reasoning":        true,
	"service_tier":     true,
	"include":          true,
	"prompt_cache_key": true,
	"client_metadata":  true,
	"text":             true,
}

// CodexExecutor implements engine.Executor for OpenAI's Codex Responses API
// (/v1/responses). It handles the specialised Responses API SSE format and
// performs request normalisation that generic chat-completions executors do
// not. This is the engine home of the executor previously living in
// internal/app/executor/codex.go (design §3.1 step 2).
type CodexExecutor struct {
	client       *http.Client
	model        string
	streamingCfg StreamingConfig

	// baseURL overrides the default upstream URL. Tests only; client-supplied
	// X-Base-URL headers are never honored per decision #376.
	baseURL string
}

// NewCodexExecutor creates a CodexExecutor with the given HTTP transport.
func NewCodexExecutor(transport http.RoundTripper) *CodexExecutor {
	return &CodexExecutor{
		client:       &http.Client{Transport: transport},
		streamingCfg: DefaultStreamingConfig(),
	}
}

// SetStreamingConfig replaces the default streaming timeout config.
func (e *CodexExecutor) SetStreamingConfig(cfg StreamingConfig) {
	e.streamingCfg = cfg
}

// SetBaseURL overrides the executor's upstream URL. Tests only.
func (e *CodexExecutor) SetBaseURL(url string) {
	e.baseURL = url
}

// SupportsFormat returns true for FormatCodexResponses.
func (e *CodexExecutor) SupportsFormat(format engine.RequestFormat) bool {
	return format == engine.FormatCodexResponses
}

// ProviderType returns the codex provider type.
func (e *CodexExecutor) ProviderType() provider.ProviderType {
	return provider.ProviderCodex
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

	return &engine.Response{
		RequestID:  req.ID,
		Body:       body,
		Model:      e.resolveModel(req),
		StatusCode: resp.StatusCode,
		Headers:    flattenHeaders(resp.Header),
	}, nil
}

// ExecuteStream sends a streaming request to the Codex Responses API and
// returns a response with an active stream. The first event is read
// synchronously so SSE-level errors surface before returning; the remaining
// body streams incrementally — no io.ReadAll on success.
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
		_ = resp.Body.Close()
		return nil, err
	}

	bufReader := bufio.NewReaderSize(resp.Body, 64*1024)

	firstEvent, firstData, err := e.peekFirstEventFromReader(bufReader)
	if err != nil {
		_ = resp.Body.Close()
		return nil, fmt.Errorf("peek first event: %w", err)
	}

	if isCodexSSEError(firstData) {
		_ = resp.Body.Close()
		return nil, fmt.Errorf("codex SSE error: %s", extractCodexErrorMessage(firstData))
	}

	st := stream.NewStream(ctx, 64)

	if firstData == "[DONE]" {
		_ = resp.Body.Close()
		st.Close()
		return &engine.Response{
			RequestID:  req.ID,
			Stream:     st,
			Model:      e.resolveModel(req),
			StatusCode: resp.StatusCode,
			Headers:    flattenHeaders(resp.Header),
		}, nil
	}

	if firstData != "" {
		st.Push(copyChunkData(stream.Chunk{
			Data:  []byte(firstData),
			Event: firstEvent,
		}))
	}

	// The peek consumed only the first data line; remaining data lines of the
	// same event are still buffered. Prepend a synthetic event line so
	// streamSSE labels those lines with the peeked event type.
	remainder := io.Reader(bufReader)
	if firstEvent != "" {
		remainder = io.MultiReader(strings.NewReader("event: "+firstEvent+"\n"), bufReader)
	}

	go streamSSE(ctx, st, remainder, resp.Body, e.streamingCfg, 0)

	return &engine.Response{
		RequestID:  req.ID,
		Stream:     st,
		Model:      e.resolveModel(req),
		StatusCode: resp.StatusCode,
		Headers:    flattenHeaders(resp.Header),
	}, nil
}

func (e *CodexExecutor) buildRequest(ctx context.Context, req *engine.Request, account *provider.Account, streamEnabled bool) (*http.Request, error) {
	baseURL := e.resolveBaseURL(req)
	payload := e.transformRequest(selectBody(req), streamEnabled)
	if _, ok := payload["model"]; !ok {
		payload["model"] = e.resolveModel(req)
	}

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

	httpReq.Header.Set("session_id", resolveSessionID(account))
	httpReq.Header.Set("originator", "gorouter")

	for k, v := range req.Headers {
		switch k {
		case "Content-Type", "Authorization", "Accept", "session_id", "originator":
			continue
		}
		httpReq.Header.Set(k, v)
	}

	return httpReq, nil
}

// transformRequest normalises the request body for the Codex Responses API:
// it injects defaults, removes unsupported fields, and ensures the payload
// passes the backend's validation.
func (e *CodexExecutor) transformRequest(body []byte, stream bool) map[string]interface{} {
	payload := make(map[string]interface{})

	if len(body) > 0 {
		if err := json.Unmarshal(body, &payload); err != nil {
			payload = make(map[string]interface{})
		}
	}

	e.normaliseInput(payload)

	if pm, ok := payload["model"].(string); ok && pm == "" {
		payload["model"] = "gpt-4o-codex"
	}

	payload["stream"] = stream

	// Codex cannot resolve stored items when store is true and we don't
	// provide a previous_response_id.
	payload["store"] = false

	if instr, ok := payload["instructions"].(string); !ok || instr == "" {
		payload["instructions"] = "You are a helpful assistant."
	}

	for k := range payload {
		if !responsesAPIAllowlist[k] {
			delete(payload, k)
		}
	}

	return payload
}

// normaliseInput converts the input field to an array if it's a string and
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

func (e *CodexExecutor) resolveModel(req *engine.Request) string {
	m := resolveModel(e.model, req)
	if m == "gpt-4o" {
		return "gpt-4o-codex"
	}
	return m
}

func (e *CodexExecutor) resolveBaseURL(req *engine.Request) string {
	if e.baseURL != "" {
		return e.baseURL
	}
	return "https://api.openai.com"
}

func (e *CodexExecutor) checkResponseStatus(resp *http.Response) error {
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		bodyBytes, _ := io.ReadAll(resp.Body)
		bodyStr := string(bodyBytes)

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

func resolveAPIKey(account *provider.Account) string {
	if account == nil {
		return ""
	}
	return account.CredentialRef
}

func resolveSessionID(account *provider.Account) string {
	if account == nil {
		return "default"
	}
	return account.ID.String()
}

func copyChunkData(c stream.Chunk) stream.Chunk {
	data := make([]byte, len(c.Data))
	copy(data, c.Data)
	c.Data = data
	return c
}

// peekFirstEventFromReader reads the first SSE event from a buffered reader,
// leaving the reader positioned right after the first data line so that
// remaining data lines of the same event stay available to streamSSE.
func (e *CodexExecutor) peekFirstEventFromReader(r *bufio.Reader) (eventType, data string, err error) {
	peeked, err := r.Peek(codexPeekBytes)
	if err != nil && err != bufio.ErrBufferFull {
		if len(peeked) == 0 {
			return "", "", nil
		}
		return "", "", err
	}

	scanner := bufio.NewScanner(bytes.NewReader(peeked))
	scanner.Buffer(make([]byte, 0, 64*1024), codexPeekBytes)

	offset := 0
	for scanner.Scan() {
		line := scanner.Text()
		next := offset + len(line)

		if strings.HasPrefix(line, "event: ") {
			eventType = strings.TrimPrefix(line, "event: ")
			offset = next + 1
			continue
		}

		if strings.HasPrefix(line, "data: ") {
			data = strings.TrimPrefix(line, "data: ")
			consumed := next + 1
			if consumed > len(peeked) {
				consumed = len(peeked)
			}
			if _, err := r.Discard(consumed); err != nil {
				return eventType, data, err
			}
			return eventType, data, nil
		}

		offset = next + 1
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

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
	"sync/atomic"
	"time"

	"gorouter/internal/domain/engine"
	"gorouter/internal/domain/engine/stream"
	"gorouter/internal/domain/provider"
)

// ---------------------------------------------------------------------------
// AzureExecutor — OpenAI-compatible with Azure-specific URL/api-key headers
// ---------------------------------------------------------------------------

// AzureExecutor implements engine.Executor for Azure OpenAI deployments.
// Format: /openai/deployments/{deployment}/chat/completions?api-version={version}
// Auth: api-key header (not Bearer)
type AzureExecutor struct {
	client    *http.Client
	model     string
	streamCfg StreamingConfig
}

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

func NewAzureExecutor(transport http.RoundTripper) *AzureExecutor {
	return &AzureExecutor{
		client:    &http.Client{Transport: transport},
		streamCfg: DefaultStreamingConfig(),
	}
}

func (e *AzureExecutor) SupportsFormat(format engine.RequestFormat) bool {
	return format == engine.FormatOpenAIChat || format == engine.FormatOpenAICompat
}

func (e *AzureExecutor) ProviderType() provider.ProviderType {
	return provider.ProviderAzure
}

func (e *AzureExecutor) SetStreamingConfig(cfg StreamingConfig) {
	e.streamCfg = cfg
}

func (e *AzureExecutor) Execute(ctx context.Context, req *engine.Request, account *provider.Account) (*engine.Response, error) {
	body := selectBody(req)
	httpReq, err := e.buildRequest(ctx, req, account, body, false)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
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

func (e *AzureExecutor) ExecuteStream(ctx context.Context, req *engine.Request, account *provider.Account) (*engine.Response, error) {
	body := selectBody(req)
	httpReq, err := e.buildRequest(ctx, req, account, body, true)
	if err != nil {
		return nil, fmt.Errorf("build stream request: %w", err)
	}
	httpReq.Header.Set("Accept", "text/event-stream")
	_ = httpReq // prevent unused
	resp, err := e.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("execute stream request: %w", err)
	}
	if err := checkResponseStatus(resp); err != nil {
		resp.Body.Close()
		return nil, err
	}
	st := stream.NewStream(ctx, 64)
	go streamSSE(ctx, st, resp.Body, resp.Body, e.streamCfg, e.streamCfg.FirstChunkTimeout)
	return &engine.Response{
		RequestID:  req.ID,
		Stream:     st,
		Model:      resolveModel(e.model, req),
		StatusCode: resp.StatusCode,
		Headers:    flattenHeaders(resp.Header),
	}, nil
}

func (e *AzureExecutor) buildRequest(ctx context.Context, req *engine.Request, account *provider.Account, body []byte, enableStream bool) (*http.Request, error) {
	azureEndpoint := "https://api.openai.com"
	apiVersion := "2024-10-01-preview"
	deployment := resolveModel(e.model, req)
	if account != nil {
		if v, ok := req.Headers["X-Azure-Endpoint"]; ok && v != "" {
			azureEndpoint = v
		}
		if v, ok := req.Headers["X-Azure-Api-Version"]; ok && v != "" {
			apiVersion = v
		}
	}

	var payload map[string]interface{}
	if err := json.Unmarshal(body, &payload); err != nil {
		payload = make(map[string]interface{})
	}
	payload["stream"] = enableStream
	if _, ok := payload["model"]; !ok {
		payload["model"] = deployment
	}
	payloadBytes, _ := json.Marshal(payload)

	url := fmt.Sprintf("%s/openai/deployments/%s/chat/completions?api-version=%s",
		strings.TrimRight(azureEndpoint, "/"), deployment, apiVersion)

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payloadBytes))
	if err != nil {
		return nil, fmt.Errorf("new request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if account != nil {
		httpReq.Header.Set("api-key", account.CredentialRef)
	}
	if org, ok := req.Headers["X-Azure-Organization"]; ok {
		httpReq.Header.Set("OpenAI-Organization", org)
	}
	for k, v := range req.Headers {
		if k == "Content-Type" || k == "Authorization" || k == "Accept" || k == "X-Azure-Endpoint" || k == "X-Azure-Api-Version" || k == "X-Azure-Organization" {
			continue
		}
		httpReq.Header.Set(k, v)
	}
	return httpReq, nil
}

// ---------------------------------------------------------------------------
// GitHubExecutor — GitHub Copilot with chat/responses/messages endpoints
// ---------------------------------------------------------------------------

// GitHubExecutor implements engine.Executor for GitHub Copilot.
// Routes: /chat/completions (gpt/gemini/grok) or /v1/messages (claude)
// Uses copilotToken from account credentials.
type GitHubExecutor struct {
	client     *http.Client
	model      string
	streamCfg  StreamingConfig
	knownCodex map[string]bool
}

func NewGitHubExecutor(transport http.RoundTripper) *GitHubExecutor {
	return &GitHubExecutor{
		client:     &http.Client{Transport: transport},
		streamCfg:  DefaultStreamingConfig(),
		knownCodex: make(map[string]bool),
	}
}

func (e *GitHubExecutor) SupportsFormat(format engine.RequestFormat) bool {
	return format == engine.FormatOpenAIChat || format == engine.FormatOpenAICompat
}

func (e *GitHubExecutor) ProviderType() provider.ProviderType {
	return provider.ProviderGithubModels
}

func isClaudeModel(model string) bool {
	return strings.Contains(strings.ToLower(model), "claude")
}

func supportsResponsesEndpoint(model string) bool {
	m := strings.ToLower(model)
	return !strings.Contains(m, "gemini") && !strings.Contains(m, "claude")
}

func (e *GitHubExecutor) Execute(ctx context.Context, req *engine.Request, account *provider.Account) (*engine.Response, error) {
	model := resolveModel(e.model, req)
	body := selectBody(req)

	// Claude models route to /v1/messages
	if isClaudeModel(model) {
		return e.executeMessages(ctx, req, account, model, body)
	}

	// Try /chat/completions first
	resp, err := e.executeChat(ctx, req, account, model, body, false)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode == http.StatusBadRequest && supportsResponsesEndpoint(model) {
		return e.executeResponses(ctx, req, account, model, body, false)
	}
	return resp, nil
}

func (e *GitHubExecutor) ExecuteStream(ctx context.Context, req *engine.Request, account *provider.Account) (*engine.Response, error) {
	model := resolveModel(e.model, req)
	body := selectBody(req)

	if isClaudeModel(model) {
		return e.executeMessagesStream(ctx, req, account, model, body)
	}

	resp, err := e.executeChat(ctx, req, account, model, body, true)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode == http.StatusBadRequest && supportsResponsesEndpoint(model) {
		return e.executeResponses(ctx, req, account, model, body, true)
	}
	return resp, nil
}

func (e *GitHubExecutor) copilotHeaders(account *provider.Account, enableStream bool) map[string]string {
	token := ""
	if account != nil {
		token = account.CredentialRef
	}
	h := map[string]string{
		"Authorization":          "Bearer " + token,
		"Content-Type":           "application/json",
		"copilot-integration-id": "vscode-chat",
		"editor-version":         "vscode/1.96.0",
		"editor-plugin-version":  "copilot-chat/0.24.0",
		"User-Agent":             "GitHubCopilot/1.96.0",
		"openai-intent":          "conversation-panel",
		"x-github-api-version":   "2025-01-01",
		"X-Initiator":            "user",
		"anthropic-version":      "2023-06-01",
	}
	if enableStream {
		h["Accept"] = "text/event-stream"
	} else {
		h["Accept"] = "application/json"
	}
	return h
}

func (e *GitHubExecutor) executeChat(ctx context.Context, req *engine.Request, account *provider.Account, model string, body []byte, enableStream bool) (*engine.Response, error) {
	baseURL := "https://api.githubcopilot.com"
	if v, ok := req.Headers["X-Base-URL"]; ok && v != "" {
		baseURL = v
	}

	var payload map[string]interface{}
	json.Unmarshal(body, &payload)
	if payload == nil {
		payload = make(map[string]interface{})
	}
	payload["stream"] = enableStream
	payloadBytes, _ := json.Marshal(payload)

	httpReq, _ := http.NewRequestWithContext(ctx, http.MethodPost,
		strings.TrimRight(baseURL, "/")+"/chat/completions",
		bytes.NewReader(payloadBytes))

	h := e.copilotHeaders(account, enableStream)
	for k, v := range h {
		httpReq.Header.Set(k, v)
	}

	resp, err := e.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("github chat request: %w", err)
	}
	return e.handleChatResponse(ctx, req, resp, model, enableStream)
}

// isResponsesFallbackMarker reports whether a 400 response body carries one of
// the upstream escalation markers that require retrying the request against
// the /responses endpoint (github.js execute(), lines 154-161).
func isResponsesFallbackMarker(body []byte) bool {
	s := strings.ToLower(string(body))
	return strings.Contains(s, "not accessible via the /chat/completions endpoint") ||
		strings.Contains(s, "the requested model is not supported")
}

// handleChatResponse processes a /chat/completions response. A 400 carrying an
// upstream escalation marker is returned as a synthetic response (no error) so
// Execute/ExecuteStream can observe StatusCode and retry against /responses;
// every other non-2xx status is mapped to an AppError via checkResponseStatus.
// Successful responses stream (enableStream) or carry the full body.
func (e *GitHubExecutor) handleChatResponse(ctx context.Context, req *engine.Request, resp *http.Response, model string, enableStream bool) (*engine.Response, error) {
	if resp.StatusCode == http.StatusBadRequest {
		respBody, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if supportsResponsesEndpoint(model) && isResponsesFallbackMarker(respBody) {
			return &engine.Response{
				RequestID:  req.ID,
				Body:       respBody,
				Model:      model,
				StatusCode: http.StatusBadRequest,
				Headers:    flattenHeaders(resp.Header),
			}, nil
		}
		return nil, newUpstreamError(http.StatusBadRequest, string(respBody))
	}
	if err := checkResponseStatus(resp); err != nil {
		resp.Body.Close()
		return nil, err
	}
	if enableStream {
		st := stream.NewStream(ctx, 64)
		go streamSSE(ctx, st, resp.Body, resp.Body, e.streamCfg, e.streamCfg.FirstChunkTimeout)
		return &engine.Response{
			RequestID:  req.ID,
			Stream:     st,
			Model:      model,
			StatusCode: resp.StatusCode,
			Headers:    flattenHeaders(resp.Header),
		}, nil
	}
	respBody, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}
	return &engine.Response{
		RequestID:  req.ID,
		Body:       respBody,
		Model:      model,
		StatusCode: resp.StatusCode,
		Headers:    flattenHeaders(resp.Header),
	}, nil
}

func (e *GitHubExecutor) executeResponses(ctx context.Context, req *engine.Request, account *provider.Account, model string, body []byte, enableStream bool) (*engine.Response, error) {
	baseURL := "https://api.githubcopilot.com"
	if v, ok := req.Headers["X-Base-URL"]; ok && v != "" {
		baseURL = v
	}

	var payload map[string]interface{}
	json.Unmarshal(body, &payload)
	if payload == nil {
		payload = make(map[string]interface{})
	}
	payload["stream"] = enableStream
	payloadBytes, _ := json.Marshal(payload)

	httpReq, _ := http.NewRequestWithContext(ctx, http.MethodPost,
		strings.TrimRight(baseURL, "/")+"/responses",
		bytes.NewReader(payloadBytes))

	h := e.copilotHeaders(account, enableStream)
	for k, v := range h {
		httpReq.Header.Set(k, v)
	}

	resp, err := e.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("github responses request: %w", err)
	}
	if err := checkResponseStatus(resp); err != nil {
		resp.Body.Close()
		return nil, err
	}
	if enableStream {
		st := stream.NewStream(ctx, 64)
		go streamSSE(ctx, st, resp.Body, resp.Body, e.streamCfg, e.streamCfg.FirstChunkTimeout)
		return &engine.Response{
			RequestID:  req.ID,
			Stream:     st,
			Model:      model,
			StatusCode: resp.StatusCode,
			Headers:    flattenHeaders(resp.Header),
		}, nil
	}
	respBody, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}
	return &engine.Response{
		RequestID:  req.ID,
		Body:       respBody,
		Model:      model,
		StatusCode: resp.StatusCode,
		Headers:    flattenHeaders(resp.Header),
	}, nil
}

func (e *GitHubExecutor) executeMessages(ctx context.Context, req *engine.Request, account *provider.Account, model string, body []byte) (*engine.Response, error) {
	baseURL := "https://api.githubcopilot.com"
	if v, ok := req.Headers["X-Base-URL"]; ok && v != "" {
		baseURL = v
	}

	var payload map[string]interface{}
	json.Unmarshal(body, &payload)
	if payload == nil {
		payload = make(map[string]interface{})
	}
	payloadBytes, _ := json.Marshal(payload)

	httpReq, _ := http.NewRequestWithContext(ctx, http.MethodPost,
		strings.TrimRight(baseURL, "/")+"/v1/messages",
		bytes.NewReader(payloadBytes))

	h := e.copilotHeaders(account, false)
	for k, v := range h {
		httpReq.Header.Set(k, v)
	}

	resp, err := e.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("github messages request: %w", err)
	}
	return e.handleResponse(req, resp, model, false)
}

func (e *GitHubExecutor) executeMessagesStream(ctx context.Context, req *engine.Request, account *provider.Account, model string, body []byte) (*engine.Response, error) {
	baseURL := "https://api.githubcopilot.com"
	if v, ok := req.Headers["X-Base-URL"]; ok && v != "" {
		baseURL = v
	}

	var payload map[string]interface{}
	json.Unmarshal(body, &payload)
	if payload == nil {
		payload = make(map[string]interface{})
	}
	payload["stream"] = true
	payloadBytes, _ := json.Marshal(payload)

	httpReq, _ := http.NewRequestWithContext(ctx, http.MethodPost,
		strings.TrimRight(baseURL, "/")+"/v1/messages",
		bytes.NewReader(payloadBytes))

	h := e.copilotHeaders(account, true)
	for k, v := range h {
		httpReq.Header.Set(k, v)
	}

	resp, err := e.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("github messages stream request: %w", err)
	}
	if err := checkResponseStatus(resp); err != nil {
		resp.Body.Close()
		return nil, err
	}
	st := stream.NewStream(ctx, 64)
	go streamSSE(ctx, st, resp.Body, resp.Body, e.streamCfg, e.streamCfg.FirstChunkTimeout)
	return &engine.Response{
		RequestID:  req.ID,
		Stream:     st,
		Model:      model,
		StatusCode: resp.StatusCode,
		Headers:    flattenHeaders(resp.Header),
	}, nil
}

func (e *GitHubExecutor) handleResponse(req *engine.Request, resp *http.Response, model string, enableStream bool) (*engine.Response, error) {
	if err := checkResponseStatus(resp); err != nil {
		resp.Body.Close()
		return nil, err
	}
	respBody, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}
	return &engine.Response{
		RequestID:  req.ID,
		Body:       respBody,
		Model:      model,
		StatusCode: resp.StatusCode,
		Headers:    flattenHeaders(resp.Header),
	}, nil
}

// ---------------------------------------------------------------------------
// QwenExecutor — OpenAI-compatible with Qwen-specific headers
// ---------------------------------------------------------------------------

type QwenExecutor struct {
	client    *http.Client
	model     string
	streamCfg StreamingConfig
}

func NewQwenExecutor(transport http.RoundTripper) *QwenExecutor {
	return &QwenExecutor{
		client:    &http.Client{Transport: transport},
		streamCfg: DefaultStreamingConfig(),
	}
}

func (e *QwenExecutor) SupportsFormat(format engine.RequestFormat) bool {
	return format == engine.FormatGemini
}

func (e *QwenExecutor) ProviderType() provider.ProviderType {
	return provider.ProviderQwen
}

func (e *QwenExecutor) Execute(ctx context.Context, req *engine.Request, account *provider.Account) (*engine.Response, error) {
	body := selectBody(req)
	httpReq, err := e.buildRequest(ctx, req, account, body, false)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
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

func (e *QwenExecutor) ExecuteStream(ctx context.Context, req *engine.Request, account *provider.Account) (*engine.Response, error) {
	body := selectBody(req)
	httpReq, err := e.buildRequest(ctx, req, account, body, true)
	if err != nil {
		return nil, fmt.Errorf("build stream request: %w", err)
	}
	httpReq.Header.Set("Accept", "text/event-stream")
	resp, err := e.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("execute stream request: %w", err)
	}
	if err := checkResponseStatus(resp); err != nil {
		resp.Body.Close()
		return nil, err
	}
	st := stream.NewStream(ctx, 64)
	go streamSSE(ctx, st, resp.Body, resp.Body, e.streamCfg, e.streamCfg.FirstChunkTimeout)
	return &engine.Response{
		RequestID:  req.ID,
		Stream:     st,
		Model:      resolveModel(e.model, req),
		StatusCode: resp.StatusCode,
		Headers:    flattenHeaders(resp.Header),
	}, nil
}

func (e *QwenExecutor) buildRequest(ctx context.Context, req *engine.Request, account *provider.Account, body []byte, enableStream bool) (*http.Request, error) {
	baseURL := "https://dashscope.aliyuncs.com"
	if v, ok := req.Headers["X-Base-URL"]; ok && v != "" {
		baseURL = v
	}

	var payload map[string]interface{}
	if err := json.Unmarshal(body, &payload); err != nil {
		payload = make(map[string]interface{})
	}
	payload["stream"] = enableStream
	if _, ok := payload["model"]; !ok {
		payload["model"] = resolveModel(e.model, req)
	}
	payloadBytes, _ := json.Marshal(payload)

	httpReq, _ := http.NewRequestWithContext(ctx, http.MethodPost,
		strings.TrimRight(baseURL, "/")+"/compatible-endpoint/v1/chat/completions",
		bytes.NewReader(payloadBytes))

	httpReq.Header.Set("Content-Type", "application/json")
	if enableStream {
		httpReq.Header.Set("Accept", "text/event-stream")
	}
	if account != nil {
		httpReq.Header.Set("Authorization", "Bearer "+account.CredentialRef)
	}
	return httpReq, nil
}

// ---------------------------------------------------------------------------
// OllamaLocalExecutor — OpenAI-compatible for local Ollama
// ---------------------------------------------------------------------------

type OllamaLocalExecutor struct {
	client    *http.Client
	model     string
	streamCfg StreamingConfig
}

func NewOllamaLocalExecutor(transport http.RoundTripper) *OllamaLocalExecutor {
	return &OllamaLocalExecutor{
		client:    &http.Client{Transport: transport},
		streamCfg: DefaultStreamingConfig(),
	}
}

func (e *OllamaLocalExecutor) SupportsFormat(format engine.RequestFormat) bool {
	return format == engine.FormatOpenAIChat || format == engine.FormatOpenAICompat
}

func (e *OllamaLocalExecutor) ProviderType() provider.ProviderType {
	return provider.ProviderOllamaLocal
}

func (e *OllamaLocalExecutor) Execute(ctx context.Context, req *engine.Request, account *provider.Account) (*engine.Response, error) {
	body := selectBody(req)
	baseURL := "http://localhost:11434"
	if v, ok := req.Headers["X-Base-URL"]; ok && v != "" {
		baseURL = v
	}

	var payload map[string]interface{}
	json.Unmarshal(body, &payload)
	if payload == nil {
		payload = make(map[string]interface{})
	}
	payload["stream"] = false
	if _, ok := payload["model"]; !ok {
		payload["model"] = resolveModel(e.model, req)
	}
	payloadBytes, _ := json.Marshal(payload)

	httpReq, _ := http.NewRequestWithContext(ctx, http.MethodPost,
		strings.TrimRight(baseURL, "/")+"/v1/chat/completions",
		bytes.NewReader(payloadBytes))
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := e.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("ollama request: %w", err)
	}
	defer resp.Body.Close()
	if err := checkResponseStatus(resp); err != nil {
		return nil, err
	}
	respBody, _ := io.ReadAll(resp.Body)
	return parseResponse(req, resp, respBody)
}

func (e *OllamaLocalExecutor) ExecuteStream(ctx context.Context, req *engine.Request, account *provider.Account) (*engine.Response, error) {
	body := selectBody(req)
	baseURL := "http://localhost:11434"
	if v, ok := req.Headers["X-Base-URL"]; ok && v != "" {
		baseURL = v
	}

	var payload map[string]interface{}
	json.Unmarshal(body, &payload)
	if payload == nil {
		payload = make(map[string]interface{})
	}
	payload["stream"] = true
	if _, ok := payload["model"]; !ok {
		payload["model"] = resolveModel(e.model, req)
	}
	payloadBytes, _ := json.Marshal(payload)

	httpReq, _ := http.NewRequestWithContext(ctx, http.MethodPost,
		strings.TrimRight(baseURL, "/")+"/v1/chat/completions",
		bytes.NewReader(payloadBytes))
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "text/event-stream")

	resp, err := e.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("ollama stream request: %w", err)
	}
	if err := checkResponseStatus(resp); err != nil {
		resp.Body.Close()
		return nil, err
	}
	st := stream.NewStream(ctx, 64)
	go streamSSE(ctx, st, resp.Body, resp.Body, e.streamCfg, e.streamCfg.FirstChunkTimeout)
	return &engine.Response{
		RequestID:  req.ID,
		Stream:     st,
		Model:      resolveModel(e.model, req),
		StatusCode: resp.StatusCode,
		Headers:    flattenHeaders(resp.Header),
	}, nil
}

// ---------------------------------------------------------------------------
// CommandCodeExecutor — OpenAI-compatible (NDJSON via SSE stream)
// ---------------------------------------------------------------------------

type CommandCodeExecutor struct {
	client    *http.Client
	model     string
	streamCfg StreamingConfig
}

func NewCommandCodeExecutor(transport http.RoundTripper) *CommandCodeExecutor {
	return &CommandCodeExecutor{
		client:    &http.Client{Transport: transport},
		streamCfg: DefaultStreamingConfig(),
	}
}

func (e *CommandCodeExecutor) SupportsFormat(format engine.RequestFormat) bool {
	return format == engine.FormatGemini
}

func (e *CommandCodeExecutor) ProviderType() provider.ProviderType {
	return provider.ProviderCommandcode
}

func (e *CommandCodeExecutor) Execute(ctx context.Context, req *engine.Request, account *provider.Account) (*engine.Response, error) {
	return e.executeOpenAI(ctx, req, account, false)
}

func (e *CommandCodeExecutor) ExecuteStream(ctx context.Context, req *engine.Request, account *provider.Account) (*engine.Response, error) {
	return e.executeOpenAI(ctx, req, account, true)
}

func (e *CommandCodeExecutor) executeOpenAI(ctx context.Context, req *engine.Request, account *provider.Account, enableStream bool) (*engine.Response, error) {
	body := selectBody(req)
	baseURL := "https://api.commandcode.ai"
	if v, ok := req.Headers["X-Base-URL"]; ok && v != "" {
		baseURL = v
	}

	var payload map[string]interface{}
	json.Unmarshal(body, &payload)
	if payload == nil {
		payload = make(map[string]interface{})
	}
	payload["stream"] = enableStream
	if _, ok := payload["model"]; !ok {
		payload["model"] = resolveModel(e.model, req)
	}
	payloadBytes, _ := json.Marshal(payload)

	httpReq, _ := http.NewRequestWithContext(ctx, http.MethodPost,
		strings.TrimRight(baseURL, "/")+"/v1/chat/completions",
		bytes.NewReader(payloadBytes))
	httpReq.Header.Set("Content-Type", "application/json")
	if enableStream {
		httpReq.Header.Set("Accept", "text/event-stream")
	}
	if account != nil {
		httpReq.Header.Set("Authorization", "Bearer "+account.CredentialRef)
	}

	resp, err := e.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("execute request: %w", err)
	}

	if enableStream {
		if err := checkResponseStatus(resp); err != nil {
			resp.Body.Close()
			return nil, err
		}
		st := stream.NewStream(ctx, 64)
		go streamSSE(ctx, st, resp.Body, resp.Body, e.streamCfg, e.streamCfg.FirstChunkTimeout)
		return &engine.Response{
			RequestID:  req.ID,
			Stream:     st,
			Model:      resolveModel(e.model, req),
			StatusCode: resp.StatusCode,
			Headers:    flattenHeaders(resp.Header),
		}, nil
	}

	defer resp.Body.Close()
	if err := checkResponseStatus(resp); err != nil {
		return nil, err
	}
	respBody, _ := io.ReadAll(resp.Body)
	return parseResponse(req, resp, respBody)
}

// ---------------------------------------------------------------------------
// XiaomiTokenplanExecutor — OpenAI-compatible
// ---------------------------------------------------------------------------

type XiaomiTokenplanExecutor struct {
	client    *http.Client
	model     string
	streamCfg StreamingConfig
}

func NewXiaomiTokenplanExecutor(transport http.RoundTripper) *XiaomiTokenplanExecutor {
	return &XiaomiTokenplanExecutor{
		client:    &http.Client{Transport: transport},
		streamCfg: DefaultStreamingConfig(),
	}
}

func (e *XiaomiTokenplanExecutor) SupportsFormat(format engine.RequestFormat) bool {
	return format == engine.FormatGemini
}

func (e *XiaomiTokenplanExecutor) ProviderType() provider.ProviderType {
	return provider.ProviderXiaomiTokenplan
}

func (e *XiaomiTokenplanExecutor) Execute(ctx context.Context, req *engine.Request, account *provider.Account) (*engine.Response, error) {
	return e.executeOpenAI(ctx, req, account, false)
}

func (e *XiaomiTokenplanExecutor) ExecuteStream(ctx context.Context, req *engine.Request, account *provider.Account) (*engine.Response, error) {
	return e.executeOpenAI(ctx, req, account, true)
}

func (e *XiaomiTokenplanExecutor) executeOpenAI(ctx context.Context, req *engine.Request, account *provider.Account, enableStream bool) (*engine.Response, error) {
	body := selectBody(req)
	baseURL := "https://api.minimaxi.com"
	if v, ok := req.Headers["X-Base-URL"]; ok && v != "" {
		baseURL = v
	}

	var payload map[string]interface{}
	json.Unmarshal(body, &payload)
	if payload == nil {
		payload = make(map[string]interface{})
	}
	payload["stream"] = enableStream
	if _, ok := payload["model"]; !ok {
		payload["model"] = resolveModel(e.model, req)
	}
	payloadBytes, _ := json.Marshal(payload)

	httpReq, _ := http.NewRequestWithContext(ctx, http.MethodPost,
		strings.TrimRight(baseURL, "/")+"/v1/chat/completions",
		bytes.NewReader(payloadBytes))
	httpReq.Header.Set("Content-Type", "application/json")
	if enableStream {
		httpReq.Header.Set("Accept", "text/event-stream")
	}
	if account != nil {
		httpReq.Header.Set("Authorization", "Bearer "+account.CredentialRef)
	}

	resp, err := e.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("execute request: %w", err)
	}

	if enableStream {
		if err := checkResponseStatus(resp); err != nil {
			resp.Body.Close()
			return nil, err
		}
		st := stream.NewStream(ctx, 64)
		go streamSSE(ctx, st, resp.Body, resp.Body, e.streamCfg, e.streamCfg.FirstChunkTimeout)
		return &engine.Response{
			RequestID:  req.ID,
			Stream:     st,
			Model:      resolveModel(e.model, req),
			StatusCode: resp.StatusCode,
			Headers:    flattenHeaders(resp.Header),
		}, nil
	}

	defer resp.Body.Close()
	if err := checkResponseStatus(resp); err != nil {
		return nil, err
	}
	respBody, _ := io.ReadAll(resp.Body)
	return parseResponse(req, resp, respBody)
}

// ---------------------------------------------------------------------------
// MimoFreeExecutor — OpenAI-compatible (free tier, no auth)
// ---------------------------------------------------------------------------

type MimoFreeExecutor struct {
	client    *http.Client
	model     string
	streamCfg StreamingConfig
}

func NewMimoFreeExecutor(transport http.RoundTripper) *MimoFreeExecutor {
	return &MimoFreeExecutor{
		client:    &http.Client{Transport: transport},
		streamCfg: DefaultStreamingConfig(),
	}
}

func (e *MimoFreeExecutor) SupportsFormat(format engine.RequestFormat) bool {
	return format == engine.FormatGemini
}

func (e *MimoFreeExecutor) ProviderType() provider.ProviderType {
	return provider.ProviderMimoFree
}

func (e *MimoFreeExecutor) Execute(ctx context.Context, req *engine.Request, account *provider.Account) (*engine.Response, error) {
	body := selectBody(req)
	baseURL := "https://api.mimo.ai"
	if v, ok := req.Headers["X-Base-URL"]; ok && v != "" {
		baseURL = v
	}

	var payload map[string]interface{}
	json.Unmarshal(body, &payload)
	if payload == nil {
		payload = make(map[string]interface{})
	}
	payload["stream"] = false
	if _, ok := payload["model"]; !ok {
		payload["model"] = resolveModel(e.model, req)
	}
	payloadBytes, _ := json.Marshal(payload)

	httpReq, _ := http.NewRequestWithContext(ctx, http.MethodPost,
		strings.TrimRight(baseURL, "/")+"/v1/chat/completions",
		bytes.NewReader(payloadBytes))
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := e.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("mimo request: %w", err)
	}
	defer resp.Body.Close()
	if err := checkResponseStatus(resp); err != nil {
		return nil, err
	}
	respBody, _ := io.ReadAll(resp.Body)
	return parseResponse(req, resp, respBody)
}

func (e *MimoFreeExecutor) ExecuteStream(ctx context.Context, req *engine.Request, account *provider.Account) (*engine.Response, error) {
	body := selectBody(req)
	baseURL := "https://api.mimo.ai"
	if v, ok := req.Headers["X-Base-URL"]; ok && v != "" {
		baseURL = v
	}

	var payload map[string]interface{}
	json.Unmarshal(body, &payload)
	if payload == nil {
		payload = make(map[string]interface{})
	}
	payload["stream"] = true
	if _, ok := payload["model"]; !ok {
		payload["model"] = resolveModel(e.model, req)
	}
	payloadBytes, _ := json.Marshal(payload)

	httpReq, _ := http.NewRequestWithContext(ctx, http.MethodPost,
		strings.TrimRight(baseURL, "/")+"/v1/chat/completions",
		bytes.NewReader(payloadBytes))
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "text/event-stream")

	resp, err := e.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("mimo stream request: %w", err)
	}
	if err := checkResponseStatus(resp); err != nil {
		resp.Body.Close()
		return nil, err
	}
	st := stream.NewStream(ctx, 64)
	go streamSSE(ctx, st, resp.Body, resp.Body, e.streamCfg, e.streamCfg.FirstChunkTimeout)
	return &engine.Response{
		RequestID:  req.ID,
		Stream:     st,
		Model:      resolveModel(e.model, req),
		StatusCode: resp.StatusCode,
		Headers:    flattenHeaders(resp.Header),
	}, nil
}

// ---------------------------------------------------------------------------
// CodeBuddyExecutor — OpenAI-compatible
// ---------------------------------------------------------------------------

type CodeBuddyExecutor struct {
	client    *http.Client
	model     string
	streamCfg StreamingConfig
}

func NewCodeBuddyExecutor(transport http.RoundTripper) *CodeBuddyExecutor {
	return &CodeBuddyExecutor{
		client:    &http.Client{Transport: transport},
		streamCfg: DefaultStreamingConfig(),
	}
}

func (e *CodeBuddyExecutor) SupportsFormat(format engine.RequestFormat) bool {
	return format == engine.FormatGemini
}

func (e *CodeBuddyExecutor) ProviderType() provider.ProviderType {
	return provider.ProviderCodebuddyCn
}

func (e *CodeBuddyExecutor) Execute(ctx context.Context, req *engine.Request, account *provider.Account) (*engine.Response, error) {
	return e.executeOpenAI(ctx, req, account, false)
}

func (e *CodeBuddyExecutor) ExecuteStream(ctx context.Context, req *engine.Request, account *provider.Account) (*engine.Response, error) {
	return e.executeOpenAI(ctx, req, account, true)
}

func (e *CodeBuddyExecutor) executeOpenAI(ctx context.Context, req *engine.Request, account *provider.Account, enableStream bool) (*engine.Response, error) {
	body := selectBody(req)
	baseURL := "https://api.codebuddy.ai"
	if v, ok := req.Headers["X-Base-URL"]; ok && v != "" {
		baseURL = v
	}

	var payload map[string]interface{}
	json.Unmarshal(body, &payload)
	if payload == nil {
		payload = make(map[string]interface{})
	}
	payload["stream"] = enableStream
	if _, ok := payload["model"]; !ok {
		payload["model"] = resolveModel(e.model, req)
	}
	payloadBytes, _ := json.Marshal(payload)

	httpReq, _ := http.NewRequestWithContext(ctx, http.MethodPost,
		strings.TrimRight(baseURL, "/")+"/v1/chat/completions",
		bytes.NewReader(payloadBytes))
	httpReq.Header.Set("Content-Type", "application/json")
	if enableStream {
		httpReq.Header.Set("Accept", "text/event-stream")
	}
	if account != nil {
		httpReq.Header.Set("Authorization", "Bearer "+account.CredentialRef)
	}

	resp, err := e.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("execute request: %w", err)
	}

	if enableStream {
		if err := checkResponseStatus(resp); err != nil {
			resp.Body.Close()
			return nil, err
		}
		st := stream.NewStream(ctx, 64)
		go streamSSE(ctx, st, resp.Body, resp.Body, e.streamCfg, e.streamCfg.FirstChunkTimeout)
		return &engine.Response{
			RequestID:  req.ID,
			Stream:     st,
			Model:      resolveModel(e.model, req),
			StatusCode: resp.StatusCode,
			Headers:    flattenHeaders(resp.Header),
		}, nil
	}

	defer resp.Body.Close()
	if err := checkResponseStatus(resp); err != nil {
		return nil, err
	}
	respBody, _ := io.ReadAll(resp.Body)
	return parseResponse(req, resp, respBody)
}

// streamSSE reads SSE lines from reader, parses them, and pushes chunks
// into the given stream. Terminal event is [DONE].
// Guards: context cancellation, first-chunk timeout, stall timeout, exactly one terminal.
func streamSSE(
	ctx context.Context,
	st *stream.Stream,
	reader io.Reader,
	closer io.Closer,
	cfg StreamingConfig,
	firstChunkTimeout time.Duration,
) {
	ac := &atomicClose{}
	defer func() {
		ac.Close(closer)
		if r := recover(); r != nil {
			st.Cancel(fmt.Errorf("panic in SSE streamer: %v", r))
		}
	}()

	var gotFirstChunk atomic.Bool

	// Context cancellation: close the underlying reader when parent cancels.
	go func() {
		select {
		case <-ctx.Done():
			ac.Close(closer)
		case <-st.Done():
		}
	}()

	// First-chunk timeout: close reader if no data arrives within deadline.
	if firstChunkTimeout > 0 {
		go func() {
			timer := time.NewTimer(firstChunkTimeout)
			defer timer.Stop()
			select {
			case <-timer.C:
				if !gotFirstChunk.Load() {
					ac.Close(closer)
				}
			case <-st.Done():
			}
		}()
	}

	// Stall timeout: close reader when data stops flowing after first chunk.
	var lastByteTime atomic.Int64
	if cfg.StallTimeout > 0 {
		lastByteTime.Store(time.Now().UnixNano())
		go func() {
			ticker := time.NewTicker(1 * time.Second)
			defer ticker.Stop()
			for {
				select {
				case <-ticker.C:
					if gotFirstChunk.Load() {
						lbt := time.Unix(0, lastByteTime.Load())
						if time.Since(lbt) > cfg.StallTimeout {
							ac.Close(closer)
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
				ac.Close(closer)
				return
			}
			if !st.Push(stream.Chunk{Data: []byte(data), Event: eventType}) {
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

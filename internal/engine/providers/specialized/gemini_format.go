package specialized

import (
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

// ---------------------------------------------------------------------------
// GeminiCLIExecutor — Google Gemini native format
// ---------------------------------------------------------------------------

// GeminiCLIExecutor implements engine.Executor for Google Gemini API via CLI auth.
// Format: {baseUrl}:streamGenerateContent?alt=sse or :generateContent
// Auth: Bearer accessToken
// Body wrapped in { project, model, request }
type GeminiCLIExecutor struct {
	client    *http.Client
	model     string
	streamCfg StreamingConfig
}

func NewGeminiCLIExecutor(transport http.RoundTripper) *GeminiCLIExecutor {
	return &GeminiCLIExecutor{
		client:    &http.Client{Transport: transport},
		streamCfg: DefaultStreamingConfig(),
	}
}

func (e *GeminiCLIExecutor) SupportsFormat(format engine.RequestFormat) bool {
	return format == engine.FormatGemini
}

func (e *GeminiCLIExecutor) ProviderType() provider.ProviderType {
	return provider.ProviderGeminiCli
}

func (e *GeminiCLIExecutor) buildRequest(ctx context.Context, req *engine.Request, account *provider.Account, body []byte, enableStream bool) (*http.Request, error) {
	baseURL := "https://generativelanguage.googleapis.com"
	if v, ok := req.Headers["X-Base-URL"]; ok && v != "" {
		baseURL = v
	}

	var payload map[string]interface{}
	json.Unmarshal(body, &payload)
	if payload == nil {
		payload = make(map[string]interface{})
	}

	model := resolveModel(e.model, req)

	// Gemini wraps in { project, model, request }
	wrapped := map[string]interface{}{
		"model":   model,
		"request": payload,
	}
	if account != nil {
		wrapped["project"] = account.ID.String()
	}

	action := "generateContent"
	if enableStream {
		action = "streamGenerateContent?alt=sse"
	}
	url := fmt.Sprintf("%s/v1/models:%s", strings.TrimRight(baseURL, "/"), action)

	wrappedBytes, _ := json.Marshal(wrapped)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(wrappedBytes))
	if err != nil {
		return nil, fmt.Errorf("new request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	if account != nil {
		httpReq.Header.Set("Authorization", "Bearer "+account.CredentialRef)
	}
	httpReq.Header.Set("User-Agent", "GeminiCLI/1.0")
	httpReq.Header.Set("X-Goog-Api-Client", "gemini-cli")
	if enableStream {
		httpReq.Header.Set("Accept", "text/event-stream")
	} else {
		httpReq.Header.Set("Accept", "application/json")
	}

	return httpReq, nil
}

func (e *GeminiCLIExecutor) Execute(ctx context.Context, req *engine.Request, account *provider.Account) (*engine.Response, error) {
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
		return nil, fmt.Errorf("read response: %w", err)
	}
	return &engine.Response{
		RequestID:  req.ID,
		Body:       respBody,
		Model:      resolveModel(e.model, req),
		StatusCode: resp.StatusCode,
		Headers:    flattenHeaders(resp.Header),
	}, nil
}

func (e *GeminiCLIExecutor) ExecuteStream(ctx context.Context, req *engine.Request, account *provider.Account) (*engine.Response, error) {
	body := selectBody(req)
	httpReq, err := e.buildRequest(ctx, req, account, body, true)
	if err != nil {
		return nil, fmt.Errorf("build stream request: %w", err)
	}
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

// ---------------------------------------------------------------------------
// AntigravityExecutor — Google Vertex AI via Antigravity
// ---------------------------------------------------------------------------

// AntigravityExecutor implements engine.Executor for Antigravity (Google Vertex AI).
// Format: {baseUrl}/v1internal:streamGenerateContent?alt=sse or :generateContent
// Uses Gemini request format with custom project/requestId/userAgent wrapping.
type AntigravityExecutor struct {
	client    *http.Client
	model     string
	streamCfg StreamingConfig
}

func NewAntigravityExecutor(transport http.RoundTripper) *AntigravityExecutor {
	return &AntigravityExecutor{
		client:    &http.Client{Transport: transport},
		streamCfg: DefaultStreamingConfig(),
	}
}

func (e *AntigravityExecutor) SupportsFormat(format engine.RequestFormat) bool {
	return format == engine.FormatGemini
}

func (e *AntigravityExecutor) ProviderType() provider.ProviderType {
	return provider.ProviderAntigravity
}

func (e *AntigravityExecutor) buildRequest(ctx context.Context, req *engine.Request, account *provider.Account, body []byte, enableStream bool) (*http.Request, error) {
	baseURL := "https://api.antigravity.ai"
	if v, ok := req.Headers["X-Base-URL"]; ok && v != "" {
		baseURL = v
	}

	var payload map[string]interface{}
	json.Unmarshal(body, &payload)
	if payload == nil {
		payload = make(map[string]interface{})
	}

	model := resolveModel(e.model, req)
	action := "generateContent"
	if enableStream {
		action = "streamGenerateContent?alt=sse"
	}
	url := fmt.Sprintf("%s/v1internal:%s", strings.TrimRight(baseURL, "/"), action)

	wrapped := map[string]interface{}{
		"model":       model,
		"project":     "antigravity",
		"userAgent":   "antigravity",
		"requestType": "agent",
		"requestId":   req.ID.String(),
		"request":     payload,
	}

	wrappedBytes, _ := json.Marshal(wrapped)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(wrappedBytes))
	if err != nil {
		return nil, fmt.Errorf("new request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("User-Agent", "Antigravity/1.0")
	if account != nil {
		httpReq.Header.Set("Authorization", "Bearer "+account.CredentialRef)
	}
	if enableStream {
		httpReq.Header.Set("Accept", "text/event-stream")
	}

	return httpReq, nil
}

func (e *AntigravityExecutor) Execute(ctx context.Context, req *engine.Request, account *provider.Account) (*engine.Response, error) {
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
	respBody, _ := io.ReadAll(resp.Body)
	model := resolveModel(e.model, req)
	return &engine.Response{
		RequestID:  req.ID,
		Body:       respBody,
		Model:      model,
		StatusCode: resp.StatusCode,
		Headers:    flattenHeaders(resp.Header),
	}, nil
}

func (e *AntigravityExecutor) ExecuteStream(ctx context.Context, req *engine.Request, account *provider.Account) (*engine.Response, error) {
	body := selectBody(req)
	httpReq, err := e.buildRequest(ctx, req, account, body, true)
	if err != nil {
		return nil, fmt.Errorf("build stream request: %w", err)
	}
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

// ---------------------------------------------------------------------------
// VertexExecutor — Google Vertex AI (vertex + vertex-partner)
// ---------------------------------------------------------------------------

// VertexExecutor implements engine.Executor for Google Vertex AI.
// Supports both "vertex" and "vertex-partner" provider types.
// Format: {baseUrl}/v1/projects/{project}/locations/{location}/...
type VertexExecutor struct {
	client    *http.Client
	model     string
	partner   bool
	streamCfg StreamingConfig
}

func NewVertexExecutor(transport http.RoundTripper, partner bool) *VertexExecutor {
	return &VertexExecutor{
		client:    &http.Client{Transport: transport},
		streamCfg: DefaultStreamingConfig(),
		partner:   partner,
	}
}

func (e *VertexExecutor) SupportsFormat(format engine.RequestFormat) bool {
	return format == engine.FormatGemini
}

func (e *VertexExecutor) ProviderType() provider.ProviderType {
	if e.partner {
		return provider.ProviderVertexPartner
	}
	return provider.ProviderVertex
}

func (e *VertexExecutor) Execute(ctx context.Context, req *engine.Request, account *provider.Account) (*engine.Response, error) {
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
	respBody, _ := io.ReadAll(resp.Body)
	return &engine.Response{
		RequestID:  req.ID,
		Body:       respBody,
		Model:      resolveModel(e.model, req),
		StatusCode: resp.StatusCode,
		Headers:    flattenHeaders(resp.Header),
	}, nil
}

func (e *VertexExecutor) ExecuteStream(ctx context.Context, req *engine.Request, account *provider.Account) (*engine.Response, error) {
	body := selectBody(req)
	httpReq, err := e.buildRequest(ctx, req, account, body, true)
	if err != nil {
		return nil, fmt.Errorf("build stream request: %w", err)
	}
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

func (e *VertexExecutor) buildRequest(ctx context.Context, req *engine.Request, account *provider.Account, body []byte, enableStream bool) (*http.Request, error) {
	baseURL := "https://us-central1-aiplatform.googleapis.com"
	if v, ok := req.Headers["X-Base-URL"]; ok && v != "" {
		baseURL = v
	}

	model := resolveModel(e.model, req)
	project := "default-project"
	location := "us-central1"

	action := "generateContent"
	if enableStream {
		action = "streamGenerateContent"
	}

	url := fmt.Sprintf("%s/v1/projects/%s/locations/%s/publishers/google/models/%s:%s",
		strings.TrimRight(baseURL, "/"), project, location, model, action)

	var payload map[string]interface{}
	json.Unmarshal(body, &payload)
	if payload == nil {
		payload = make(map[string]interface{})
	}
	payloadBytes, _ := json.Marshal(payload)

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payloadBytes))
	if err != nil {
		return nil, fmt.Errorf("new request: %w", err)
	}

	if enableStream {
		httpReq.Header.Set("Accept", "text/event-stream")
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if account != nil {
		httpReq.Header.Set("Authorization", "Bearer "+account.CredentialRef)
	}

	return httpReq, nil
}

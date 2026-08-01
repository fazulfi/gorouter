package specialized

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"gorouter/internal/domain/engine"
	"gorouter/internal/domain/engine/stream"
	"gorouter/internal/domain/provider"
)

// ---------------------------------------------------------------------------
// IFlowExecutor — HMAC-SHA256 signature auth
// ---------------------------------------------------------------------------

// IFlowExecutor implements engine.Executor for iFlow API with HMAC-SHA256
// request signing. The signature is computed over user-agent:session-id:timestamp
// using the API key as the HMAC secret.
type IFlowExecutor struct {
	client    *http.Client
	model     string
	streamCfg StreamingConfig
}

func NewIFlowExecutor(transport http.RoundTripper) *IFlowExecutor {
	return &IFlowExecutor{
		client:    &http.Client{Transport: transport},
		streamCfg: DefaultStreamingConfig(),
	}
}

func (e *IFlowExecutor) SupportsFormat(format engine.RequestFormat) bool {
	return format == engine.FormatGemini
}

func (e *IFlowExecutor) ProviderType() provider.ProviderType {
	return provider.ProviderIflow
}

func (e *IFlowExecutor) buildRequest(ctx context.Context, req *engine.Request, account *provider.Account, body []byte, enableStream bool) (*http.Request, error) {
	baseURL := "https://api.iflow.ai"
	if v, ok := req.Headers["X-Base-URL"]; ok && v != "" {
		baseURL = v
	}

	var payload map[string]interface{}
	_ = json.Unmarshal(body, &payload)
	if payload == nil {
		payload = make(map[string]interface{})
	}
	payload["stream"] = enableStream
	if _, ok := payload["model"]; !ok {
		payload["model"] = resolveModel(e.model, req)
	}
	// Inject stream_options for streaming requests
	if enableStream {
		if _, ok := payload["stream_options"]; !ok {
			payload["stream_options"] = map[string]interface{}{"include_usage": true}
		}
	}
	payloadBytes, _ := json.Marshal(payload)

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost,
		strings.TrimRight(baseURL, "/")+"/v1/chat/completions",
		bytes.NewReader(payloadBytes))
	if err != nil {
		return nil, fmt.Errorf("new request: %w", err)
	}

	// Build HMAC signature
	apiKey := ""
	if account != nil {
		apiKey = account.CredentialRef
	}
	sessionID := fmt.Sprintf("session-%d", time.Now().UnixNano())
	timestamp := time.Now().UnixMilli()
	userAgent := "iFlow-Cli/1.0"
	sigPayload := fmt.Sprintf("%s:%s:%d", userAgent, sessionID, timestamp)
	sig := e.sign(sigPayload, apiKey)

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+apiKey)
	httpReq.Header.Set("session-id", sessionID)
	httpReq.Header.Set("x-iflow-timestamp", fmt.Sprintf("%d", timestamp))
	httpReq.Header.Set("x-iflow-signature", sig)
	httpReq.Header.Set("User-Agent", userAgent)
	if enableStream {
		httpReq.Header.Set("Accept", "text/event-stream")
	}

	return httpReq, nil
}

func (e *IFlowExecutor) sign(payload, key string) string {
	if key == "" {
		return ""
	}
	mac := hmac.New(sha256.New, []byte(key))
	mac.Write([]byte(payload))
	return hex.EncodeToString(mac.Sum(nil))
}

func (e *IFlowExecutor) Execute(ctx context.Context, req *engine.Request, account *provider.Account) (*engine.Response, error) {
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
	return parseResponse(req, resp, respBody)
}

func (e *IFlowExecutor) ExecuteStream(ctx context.Context, req *engine.Request, account *provider.Account) (*engine.Response, error) {
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
		_ = resp.Body.Close()
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
// GrokCliExecutor — Grok CLI (custom auth)
// ---------------------------------------------------------------------------

// GrokCliExecutor implements engine.Executor for Grok CLI.
// Uses xAI API with Bearer token auth and custom model selection.
type GrokCliExecutor struct {
	client    *http.Client
	model     string
	streamCfg StreamingConfig
}

func NewGrokCliExecutor(transport http.RoundTripper) *GrokCliExecutor {
	return &GrokCliExecutor{
		client:    &http.Client{Transport: transport},
		streamCfg: DefaultStreamingConfig(),
	}
}

func (e *GrokCliExecutor) SupportsFormat(format engine.RequestFormat) bool {
	return format == engine.FormatGemini
}

func (e *GrokCliExecutor) ProviderType() provider.ProviderType {
	return provider.ProviderGrokCli
}

func (e *GrokCliExecutor) Execute(ctx context.Context, req *engine.Request, account *provider.Account) (*engine.Response, error) {
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
	_ = json.Unmarshal(respBody, &raw)

	engResp := &engine.Response{
		RequestID:  req.ID,
		Body:       respBody,
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

func (e *GrokCliExecutor) ExecuteStream(ctx context.Context, req *engine.Request, account *provider.Account) (*engine.Response, error) {
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
		_ = resp.Body.Close()
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

func (e *GrokCliExecutor) buildRequest(ctx context.Context, req *engine.Request, account *provider.Account, body []byte, enableStream bool) (*http.Request, error) {
	baseURL := "https://api.x.ai"
	if v, ok := req.Headers["X-Base-URL"]; ok && v != "" {
		baseURL = v
	}

	var payload map[string]interface{}
	_ = json.Unmarshal(body, &payload)
	if payload == nil {
		payload = make(map[string]interface{})
	}
	payload["stream"] = enableStream
	if _, ok := payload["model"]; !ok {
		payload["model"] = resolveModel(e.model, req)
	}
	payloadBytes, _ := json.Marshal(payload)

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost,
		strings.TrimRight(baseURL, "/")+"/v1/chat/completions",
		bytes.NewReader(payloadBytes))
	if err != nil {
		return nil, fmt.Errorf("new request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	if account != nil {
		httpReq.Header.Set("Authorization", "Bearer "+account.CredentialRef)
	}
	if enableStream {
		httpReq.Header.Set("Accept", "text/event-stream")
	}

	return httpReq, nil
}

// ---------------------------------------------------------------------------
// QoderExecutor — OpenAI-compatible
// ---------------------------------------------------------------------------

type QoderExecutor struct {
	client    *http.Client
	model     string
	streamCfg StreamingConfig
}

func NewQoderExecutor(transport http.RoundTripper) *QoderExecutor {
	return &QoderExecutor{
		client:    &http.Client{Transport: transport},
		streamCfg: DefaultStreamingConfig(),
	}
}

func (e *QoderExecutor) SupportsFormat(format engine.RequestFormat) bool {
	return format == engine.FormatGemini
}

func (e *QoderExecutor) ProviderType() provider.ProviderType {
	return provider.ProviderQoder
}

func (e *QoderExecutor) Execute(ctx context.Context, req *engine.Request, account *provider.Account) (*engine.Response, error) {
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
	return parseResponse(req, resp, respBody)
}

func (e *QoderExecutor) ExecuteStream(ctx context.Context, req *engine.Request, account *provider.Account) (*engine.Response, error) {
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
		_ = resp.Body.Close()
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

func (e *QoderExecutor) buildRequest(ctx context.Context, req *engine.Request, account *provider.Account, body []byte, enableStream bool) (*http.Request, error) {
	baseURL := "https://api.qoder.ai"
	if v, ok := req.Headers["X-Base-URL"]; ok && v != "" {
		baseURL = v
	}

	var payload map[string]interface{}
	_ = json.Unmarshal(body, &payload)
	if payload == nil {
		payload = make(map[string]interface{})
	}
	payload["stream"] = enableStream
	if _, ok := payload["model"]; !ok {
		payload["model"] = resolveModel(e.model, req)
	}
	payloadBytes, _ := json.Marshal(payload)

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost,
		strings.TrimRight(baseURL, "/")+"/v1/chat/completions",
		bytes.NewReader(payloadBytes))
	if err != nil {
		return nil, fmt.Errorf("new request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	if account != nil {
		httpReq.Header.Set("Authorization", "Bearer "+account.CredentialRef)
	}
	return httpReq, nil
}

// ---------------------------------------------------------------------------
// KimchiExecutor — OpenAI-compatible
// ---------------------------------------------------------------------------

type KimchiExecutor struct {
	client    *http.Client
	model     string
	streamCfg StreamingConfig
}

func NewKimchiExecutor(transport http.RoundTripper) *KimchiExecutor {
	return &KimchiExecutor{
		client:    &http.Client{Transport: transport},
		streamCfg: DefaultStreamingConfig(),
	}
}

func (e *KimchiExecutor) SupportsFormat(format engine.RequestFormat) bool {
	return format == engine.FormatGemini
}

func (e *KimchiExecutor) ProviderType() provider.ProviderType {
	return provider.ProviderKimchi
}

func (e *KimchiExecutor) Execute(ctx context.Context, req *engine.Request, account *provider.Account) (*engine.Response, error) {
	return e.executeOpenAI(ctx, req, account, false)
}

func (e *KimchiExecutor) ExecuteStream(ctx context.Context, req *engine.Request, account *provider.Account) (*engine.Response, error) {
	return e.executeOpenAI(ctx, req, account, true)
}

func (e *KimchiExecutor) executeOpenAI(ctx context.Context, req *engine.Request, account *provider.Account, enableStream bool) (*engine.Response, error) {
	body := selectBody(req)
	baseURL := "https://api.kimchi.ai"
	if v, ok := req.Headers["X-Base-URL"]; ok && v != "" {
		baseURL = v
	}
	var payload map[string]interface{}
	_ = json.Unmarshal(body, &payload)
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
	if account != nil {
		httpReq.Header.Set("Authorization", "Bearer "+account.CredentialRef)
	}
	resp, err := e.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("execute request: %w", err)
	}
	if enableStream {
		if err := checkResponseStatus(resp); err != nil {
			_ = resp.Body.Close()
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

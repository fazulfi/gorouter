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
// DeepSeekExecutor — specialized/deepseek, FormatOpenAIChat + FormatOpenAICompat
// ---------------------------------------------------------------------------

type DeepSeekExecutor struct {
	client    *http.Client
	model     string
	streamCfg StreamingConfig
}

func NewDeepSeekExecutor(transport http.RoundTripper) *DeepSeekExecutor {
	return &DeepSeekExecutor{
		client:    &http.Client{Transport: transport},
		streamCfg: DefaultStreamingConfig(),
	}
}

func (e *DeepSeekExecutor) SupportsFormat(format engine.RequestFormat) bool {
	return format == engine.FormatOpenAIChat || format == engine.FormatOpenAICompat
}

func (e *DeepSeekExecutor) ProviderType() provider.ProviderType {
	return provider.ProviderDeepseek
}

func (e *DeepSeekExecutor) Execute(ctx context.Context, req *engine.Request, account *provider.Account) (*engine.Response, error) {
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

func (e *DeepSeekExecutor) ExecuteStream(ctx context.Context, req *engine.Request, account *provider.Account) (*engine.Response, error) {
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

func (e *DeepSeekExecutor) buildRequest(ctx context.Context, req *engine.Request, account *provider.Account, body []byte, streamEnabled bool) (*http.Request, error) {
	baseURL := "https://api.deepseek.com"
	if v, ok := req.Headers["X-Base-URL"]; ok && v != "" {
		baseURL = v
	}
	var payload map[string]interface{}
	_ = json.Unmarshal(body, &payload)
	if payload == nil {
		payload = make(map[string]interface{})
	}
	payload["stream"] = streamEnabled
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
	if streamEnabled {
		httpReq.Header.Set("Accept", "text/event-stream")
	} else {
		httpReq.Header.Set("Accept", "application/json")
	}
	if account != nil {
		httpReq.Header.Set("Authorization", "Bearer "+account.CredentialRef)
	}
	return httpReq, nil
}

// ---------------------------------------------------------------------------
// FireworksExecutor — specialized/fireworks, FormatOpenAIChat + FormatOpenAICompat
// ---------------------------------------------------------------------------

type FireworksExecutor struct {
	client    *http.Client
	model     string
	streamCfg StreamingConfig
}

func NewFireworksExecutor(transport http.RoundTripper) *FireworksExecutor {
	return &FireworksExecutor{
		client:    &http.Client{Transport: transport},
		streamCfg: DefaultStreamingConfig(),
	}
}

func (e *FireworksExecutor) SupportsFormat(format engine.RequestFormat) bool {
	return format == engine.FormatOpenAIChat || format == engine.FormatOpenAICompat
}

func (e *FireworksExecutor) ProviderType() provider.ProviderType {
	return provider.ProviderFireworks
}

func (e *FireworksExecutor) Execute(ctx context.Context, req *engine.Request, account *provider.Account) (*engine.Response, error) {
	return e.executeOpenAI(ctx, req, account, false)
}

func (e *FireworksExecutor) ExecuteStream(ctx context.Context, req *engine.Request, account *provider.Account) (*engine.Response, error) {
	return e.executeOpenAI(ctx, req, account, true)
}

func (e *FireworksExecutor) executeOpenAI(ctx context.Context, req *engine.Request, account *provider.Account, streamEnabled bool) (*engine.Response, error) {
	body := selectBody(req)
	baseURL := "https://api.fireworks.ai"
	if v, ok := req.Headers["X-Base-URL"]; ok && v != "" {
		baseURL = v
	}
	var payload map[string]interface{}
	_ = json.Unmarshal(body, &payload)
	if payload == nil {
		payload = make(map[string]interface{})
	}
	payload["stream"] = streamEnabled
	if _, ok := payload["model"]; !ok {
		payload["model"] = resolveModel(e.model, req)
	}
	payloadBytes, _ := json.Marshal(payload)
	httpReq, _ := http.NewRequestWithContext(ctx, http.MethodPost,
		strings.TrimRight(baseURL, "/")+"/v1/chat/completions",
		bytes.NewReader(payloadBytes))
	httpReq.Header.Set("Content-Type", "application/json")
	if streamEnabled {
		httpReq.Header.Set("Accept", "text/event-stream")
	}
	if account != nil {
		httpReq.Header.Set("Authorization", "Bearer "+account.CredentialRef)
	}
	resp, err := e.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("execute request: %w", err)
	}
	if streamEnabled {
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

// ---------------------------------------------------------------------------
// GroqExecutor — specialized/groq, FormatOpenAIChat + FormatOpenAICompat
// ---------------------------------------------------------------------------

type GroqExecutor struct {
	client    *http.Client
	model     string
	streamCfg StreamingConfig
}

func NewGroqExecutor(transport http.RoundTripper) *GroqExecutor {
	return &GroqExecutor{
		client:    &http.Client{Transport: transport},
		streamCfg: DefaultStreamingConfig(),
	}
}

func (e *GroqExecutor) SupportsFormat(format engine.RequestFormat) bool {
	return format == engine.FormatOpenAIChat || format == engine.FormatOpenAICompat
}

func (e *GroqExecutor) ProviderType() provider.ProviderType {
	return provider.ProviderGroq
}

func (e *GroqExecutor) Execute(ctx context.Context, req *engine.Request, account *provider.Account) (*engine.Response, error) {
	return e.executeOpenAI(ctx, req, account, false)
}

func (e *GroqExecutor) ExecuteStream(ctx context.Context, req *engine.Request, account *provider.Account) (*engine.Response, error) {
	return e.executeOpenAI(ctx, req, account, true)
}

func (e *GroqExecutor) executeOpenAI(ctx context.Context, req *engine.Request, account *provider.Account, streamEnabled bool) (*engine.Response, error) {
	body := selectBody(req)
	baseURL := "https://api.groq.com"
	if v, ok := req.Headers["X-Base-URL"]; ok && v != "" {
		baseURL = v
	}
	var payload map[string]interface{}
	_ = json.Unmarshal(body, &payload)
	if payload == nil {
		payload = make(map[string]interface{})
	}
	payload["stream"] = streamEnabled
	if _, ok := payload["model"]; !ok {
		payload["model"] = resolveModel(e.model, req)
	}
	payloadBytes, _ := json.Marshal(payload)
	httpReq, _ := http.NewRequestWithContext(ctx, http.MethodPost,
		strings.TrimRight(baseURL, "/")+"/v1/chat/completions",
		bytes.NewReader(payloadBytes))
	httpReq.Header.Set("Content-Type", "application/json")
	if streamEnabled {
		httpReq.Header.Set("Accept", "text/event-stream")
	}
	if account != nil {
		httpReq.Header.Set("Authorization", "Bearer "+account.CredentialRef)
	}
	resp, err := e.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("execute request: %w", err)
	}
	if streamEnabled {
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

// ---------------------------------------------------------------------------
// MistralExecutor — specialized/mistral, FormatOpenAIChat + FormatOpenAICompat
// ---------------------------------------------------------------------------

type MistralExecutor struct {
	client    *http.Client
	model     string
	streamCfg StreamingConfig
}

func NewMistralExecutor(transport http.RoundTripper) *MistralExecutor {
	return &MistralExecutor{
		client:    &http.Client{Transport: transport},
		streamCfg: DefaultStreamingConfig(),
	}
}

func (e *MistralExecutor) SupportsFormat(format engine.RequestFormat) bool {
	return format == engine.FormatOpenAIChat || format == engine.FormatOpenAICompat
}

func (e *MistralExecutor) ProviderType() provider.ProviderType {
	return provider.ProviderMistral
}

func (e *MistralExecutor) Execute(ctx context.Context, req *engine.Request, account *provider.Account) (*engine.Response, error) {
	return e.executeOpenAI(ctx, req, account, false)
}

func (e *MistralExecutor) ExecuteStream(ctx context.Context, req *engine.Request, account *provider.Account) (*engine.Response, error) {
	return e.executeOpenAI(ctx, req, account, true)
}

func (e *MistralExecutor) executeOpenAI(ctx context.Context, req *engine.Request, account *provider.Account, streamEnabled bool) (*engine.Response, error) {
	body := selectBody(req)
	baseURL := "https://api.mistral.ai"
	if v, ok := req.Headers["X-Base-URL"]; ok && v != "" {
		baseURL = v
	}
	var payload map[string]interface{}
	_ = json.Unmarshal(body, &payload)
	if payload == nil {
		payload = make(map[string]interface{})
	}
	payload["stream"] = streamEnabled
	if _, ok := payload["model"]; !ok {
		payload["model"] = resolveModel(e.model, req)
	}
	payloadBytes, _ := json.Marshal(payload)
	httpReq, _ := http.NewRequestWithContext(ctx, http.MethodPost,
		strings.TrimRight(baseURL, "/")+"/v1/chat/completions",
		bytes.NewReader(payloadBytes))
	httpReq.Header.Set("Content-Type", "application/json")
	if streamEnabled {
		httpReq.Header.Set("Accept", "text/event-stream")
	}
	if account != nil {
		httpReq.Header.Set("Authorization", "Bearer "+account.CredentialRef)
	}
	resp, err := e.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("execute request: %w", err)
	}
	if streamEnabled {
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

// ---------------------------------------------------------------------------
// PerplexityExecutor — specialized/perplexity, FormatOpenAICompat
// ---------------------------------------------------------------------------

type PerplexityExecutor struct {
	client    *http.Client
	model     string
	streamCfg StreamingConfig
}

func NewPerplexityExecutor(transport http.RoundTripper) *PerplexityExecutor {
	return &PerplexityExecutor{
		client:    &http.Client{Transport: transport},
		streamCfg: DefaultStreamingConfig(),
	}
}

func (e *PerplexityExecutor) SupportsFormat(format engine.RequestFormat) bool {
	return format == engine.FormatOpenAICompat
}

func (e *PerplexityExecutor) ProviderType() provider.ProviderType {
	return provider.ProviderPerplexity
}

func (e *PerplexityExecutor) Execute(ctx context.Context, req *engine.Request, account *provider.Account) (*engine.Response, error) {
	body := selectBody(req)
	baseURL := "https://api.perplexity.ai"
	if v, ok := req.Headers["X-Base-URL"]; ok && v != "" {
		baseURL = v
	}
	var payload map[string]interface{}
	_ = json.Unmarshal(body, &payload)
	if payload == nil {
		payload = make(map[string]interface{})
	}
	payload["stream"] = false
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

func (e *PerplexityExecutor) ExecuteStream(ctx context.Context, req *engine.Request, account *provider.Account) (*engine.Response, error) {
	body := selectBody(req)
	baseURL := "https://api.perplexity.ai"
	if v, ok := req.Headers["X-Base-URL"]; ok && v != "" {
		baseURL = v
	}
	var payload map[string]interface{}
	_ = json.Unmarshal(body, &payload)
	if payload == nil {
		payload = make(map[string]interface{})
	}
	payload["stream"] = true
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
	httpReq.Header.Set("Accept", "text/event-stream")
	if account != nil {
		httpReq.Header.Set("Authorization", "Bearer "+account.CredentialRef)
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
// TogetherExecutor — specialized/together, FormatOpenAIChat + FormatOpenAICompat
// ---------------------------------------------------------------------------

type TogetherExecutor struct {
	client    *http.Client
	model     string
	streamCfg StreamingConfig
}

func NewTogetherExecutor(transport http.RoundTripper) *TogetherExecutor {
	return &TogetherExecutor{
		client:    &http.Client{Transport: transport},
		streamCfg: DefaultStreamingConfig(),
	}
}

func (e *TogetherExecutor) SupportsFormat(format engine.RequestFormat) bool {
	return format == engine.FormatOpenAIChat || format == engine.FormatOpenAICompat
}

func (e *TogetherExecutor) ProviderType() provider.ProviderType {
	return provider.ProviderTogether
}

func (e *TogetherExecutor) Execute(ctx context.Context, req *engine.Request, account *provider.Account) (*engine.Response, error) {
	return e.executeOpenAI(ctx, req, account, false)
}

func (e *TogetherExecutor) ExecuteStream(ctx context.Context, req *engine.Request, account *provider.Account) (*engine.Response, error) {
	return e.executeOpenAI(ctx, req, account, true)
}

func (e *TogetherExecutor) executeOpenAI(ctx context.Context, req *engine.Request, account *provider.Account, streamEnabled bool) (*engine.Response, error) {
	body := selectBody(req)
	baseURL := "https://api.together.xyz"
	if v, ok := req.Headers["X-Base-URL"]; ok && v != "" {
		baseURL = v
	}
	var payload map[string]interface{}
	_ = json.Unmarshal(body, &payload)
	if payload == nil {
		payload = make(map[string]interface{})
	}
	payload["stream"] = streamEnabled
	if _, ok := payload["model"]; !ok {
		payload["model"] = resolveModel(e.model, req)
	}
	payloadBytes, _ := json.Marshal(payload)
	httpReq, _ := http.NewRequestWithContext(ctx, http.MethodPost,
		strings.TrimRight(baseURL, "/")+"/v1/chat/completions",
		bytes.NewReader(payloadBytes))
	httpReq.Header.Set("Content-Type", "application/json")
	if streamEnabled {
		httpReq.Header.Set("Accept", "text/event-stream")
	}
	if account != nil {
		httpReq.Header.Set("Authorization", "Bearer "+account.CredentialRef)
	}
	resp, err := e.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("execute request: %w", err)
	}
	if streamEnabled {
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

// ---------------------------------------------------------------------------
// XAIExecutor — specialized/xai, FormatOpenAICompat
// ---------------------------------------------------------------------------

type XAIExecutor struct {
	client    *http.Client
	model     string
	streamCfg StreamingConfig
}

func NewXAIExecutor(transport http.RoundTripper) *XAIExecutor {
	return &XAIExecutor{
		client:    &http.Client{Transport: transport},
		streamCfg: DefaultStreamingConfig(),
	}
}

func (e *XAIExecutor) SupportsFormat(format engine.RequestFormat) bool {
	return format == engine.FormatOpenAICompat
}

func (e *XAIExecutor) ProviderType() provider.ProviderType {
	return provider.ProviderXai
}

func (e *XAIExecutor) Execute(ctx context.Context, req *engine.Request, account *provider.Account) (*engine.Response, error) {
	return e.executeOpenAI(ctx, req, account, false)
}

func (e *XAIExecutor) ExecuteStream(ctx context.Context, req *engine.Request, account *provider.Account) (*engine.Response, error) {
	return e.executeOpenAI(ctx, req, account, true)
}

func (e *XAIExecutor) executeOpenAI(ctx context.Context, req *engine.Request, account *provider.Account, streamEnabled bool) (*engine.Response, error) {
	body := selectBody(req)
	baseURL := "https://api.x.ai"
	if v, ok := req.Headers["X-Base-URL"]; ok && v != "" {
		baseURL = v
	}
	var payload map[string]interface{}
	_ = json.Unmarshal(body, &payload)
	if payload == nil {
		payload = make(map[string]interface{})
	}
	payload["stream"] = streamEnabled
	if _, ok := payload["model"]; !ok {
		payload["model"] = resolveModel(e.model, req)
	}
	payloadBytes, _ := json.Marshal(payload)
	httpReq, _ := http.NewRequestWithContext(ctx, http.MethodPost,
		strings.TrimRight(baseURL, "/")+"/v1/chat/completions",
		bytes.NewReader(payloadBytes))
	httpReq.Header.Set("Content-Type", "application/json")
	if streamEnabled {
		httpReq.Header.Set("Accept", "text/event-stream")
	}
	if account != nil {
		httpReq.Header.Set("Authorization", "Bearer "+account.CredentialRef)
	}
	resp, err := e.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("execute request: %w", err)
	}
	if streamEnabled {
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

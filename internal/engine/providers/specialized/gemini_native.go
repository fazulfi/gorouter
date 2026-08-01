package specialized

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"gorouter/internal/domain/engine"
	"gorouter/internal/domain/engine/stream"
	"gorouter/internal/domain/provider"
	"gorouter/internal/shared"
)

// geminiNativeBaseURL is the pinned transport.baseUrl from registry/gemini.js
// (decolua/9router 79918c7830695bbca4a45c9fea4a42c3e9fd73d1).
const geminiNativeBaseURL = "https://generativelanguage.googleapis.com/v1beta/models"

// GeminiExecutor implements engine.Executor for the native Google Gemini API
// (provider.ProviderGemini). Wire contract per registry/gemini.js and
// open-sse/executors/default.js (pinned commit):
//
//   - URL: {baseUrl}/{model}:generateContent (non-stream) or
//     {baseUrl}/{model}:streamGenerateContent?alt=sse (stream)
//   - Body: raw GenerateContentRequest passthrough — never wrapped
//   - Auth (split descriptor): api_key accounts send the raw key via
//     x-goog-api-key; oauth accounts send Authorization: Bearer
type GeminiExecutor struct {
	client    *http.Client
	model     string
	streamCfg StreamingConfig
}

func NewGeminiExecutor(transport http.RoundTripper) *GeminiExecutor {
	return &GeminiExecutor{
		client:    &http.Client{Transport: transport},
		streamCfg: DefaultStreamingConfig(),
	}
}

func (e *GeminiExecutor) SupportsFormat(format engine.RequestFormat) bool {
	return format == engine.FormatGemini
}

func (e *GeminiExecutor) ProviderType() provider.ProviderType {
	return provider.ProviderGemini
}

func (e *GeminiExecutor) buildRequest(ctx context.Context, req *engine.Request, account *provider.Account, body []byte, enableStream bool) (*http.Request, error) {
	baseURL := geminiNativeBaseURL
	if v, ok := req.Headers["X-Base-URL"]; ok && v != "" {
		baseURL = v
	}

	model := resolveModel(e.model, req)
	action := "generateContent"
	if enableStream {
		action = "streamGenerateContent?alt=sse"
	}
	url := fmt.Sprintf("%s/%s:%s", strings.TrimRight(baseURL, "/"), model, action)

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("new request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	applyGeminiNativeAuth(httpReq.Header, account)
	if enableStream {
		httpReq.Header.Set("Accept", "text/event-stream")
	}

	return httpReq, nil
}

// applyGeminiNativeAuth selects the pinned split auth descriptor branch from
// the account authority (Account.AuthType). api_key accounts send the raw key
// via x-goog-api-key; oauth accounts send Authorization: Bearer. The
// credential value only ever reaches the designated header — never the URL,
// body, or surfaced errors.
func applyGeminiNativeAuth(h http.Header, account *provider.Account) {
	if account == nil {
		return
	}
	if account.AuthType == "oauth" {
		h.Set("Authorization", "Bearer "+account.CredentialRef)
		return
	}
	h.Set("x-goog-api-key", account.CredentialRef)
}

func (e *GeminiExecutor) Execute(ctx context.Context, req *engine.Request, account *provider.Account) (*engine.Response, error) {
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
		return nil, redactGeminiError(err, geminiCredential(account))
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

func (e *GeminiExecutor) ExecuteStream(ctx context.Context, req *engine.Request, account *provider.Account) (*engine.Response, error) {
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
		return nil, redactGeminiError(err, geminiCredential(account))
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

// geminiCredential returns the credential value the executor injects on the
// wire. Mirrors the generic package default resolver (CredentialRef treated
// as the plaintext credential, matching Phase 2 behaviour).
func geminiCredential(account *provider.Account) string {
	if account == nil {
		return ""
	}
	return account.CredentialRef
}

// redactGeminiError scrubs the account credential from AppError message and
// Details body before surfacing (decisions #127, #145). Upstream error bodies
// may echo the key/token; the value must never reach callers or logs.
func redactGeminiError(err error, cred string) error {
	if err == nil || cred == "" {
		return err
	}
	var ae *shared.AppError
	if errors.As(err, &ae) {
		clone := *ae
		clone.Message = strings.ReplaceAll(clone.Message, cred, "***")
		if details, ok := clone.Details.(map[string]interface{}); ok {
			if body, ok := details["body"].(string); ok {
				redacted := map[string]interface{}{}
				for k, v := range details {
					redacted[k] = v
				}
				redacted["body"] = strings.ReplaceAll(body, cred, "***")
				clone.Details = redacted
			}
		}
		return &clone
	}
	return fmt.Errorf("%s", strings.ReplaceAll(err.Error(), cred, "***"))
}

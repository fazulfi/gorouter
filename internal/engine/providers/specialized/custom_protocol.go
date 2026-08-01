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
	"sync"
	"sync/atomic"
	"time"

	"gorouter/internal/domain/engine"
	"gorouter/internal/domain/engine/stream"
	"gorouter/internal/domain/provider"
)

// ---------------------------------------------------------------------------
// CursorExecutor — Custom SSE protocol (executor_path: specialized/cursor)
// ---------------------------------------------------------------------------

type CursorExecutor struct {
	client    *http.Client
	model     string
	streamCfg StreamingConfig
}

func NewCursorExecutor(transport http.RoundTripper) *CursorExecutor {
	return &CursorExecutor{
		client:    &http.Client{Transport: transport},
		streamCfg: DefaultStreamingConfig(),
	}
}

func (e *CursorExecutor) SupportsFormat(format engine.RequestFormat) bool {
	return format == engine.FormatOpenAICompat
}

func (e *CursorExecutor) ProviderType() provider.ProviderType {
	return provider.ProviderCursor
}

func (e *CursorExecutor) buildRequest(ctx context.Context, req *engine.Request, account *provider.Account, body []byte, streamEnabled bool) (*http.Request, error) {
	baseURL := "https://api.cursor.ai"
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
	if account != nil {
		httpReq.Header.Set("Authorization", "Bearer "+account.CredentialRef)
	}
	httpReq.Header.Set("User-Agent", "cursor/1.0")
	httpReq.Header.Set("Cursor-Client", "ide")
	if streamEnabled {
		httpReq.Header.Set("Accept", "text/event-stream")
	}
	return httpReq, nil
}

func (e *CursorExecutor) Execute(ctx context.Context, req *engine.Request, account *provider.Account) (*engine.Response, error) {
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

func (e *CursorExecutor) ExecuteStream(ctx context.Context, req *engine.Request, account *provider.Account) (*engine.Response, error) {
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
// KiroExecutor — Custom EventStream protocol (executor_path: specialized/kiro)
// ---------------------------------------------------------------------------

type KiroExecutor struct {
	client    *http.Client
	model     string
	streamCfg StreamingConfig
}

func NewKiroExecutor(transport http.RoundTripper) *KiroExecutor {
	return &KiroExecutor{
		client:    &http.Client{Transport: transport},
		streamCfg: DefaultStreamingConfig(),
	}
}

func (e *KiroExecutor) SupportsFormat(format engine.RequestFormat) bool {
	return format == engine.FormatOpenAICompat
}

func (e *KiroExecutor) ProviderType() provider.ProviderType {
	return provider.ProviderKiro
}

func (e *KiroExecutor) Execute(ctx context.Context, req *engine.Request, account *provider.Account) (*engine.Response, error) {
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

func (e *KiroExecutor) ExecuteStream(ctx context.Context, req *engine.Request, account *provider.Account) (*engine.Response, error) {
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
	go e.streamEventStream(ctx, st, resp.Body, resp.Body)
	return &engine.Response{
		RequestID:  req.ID,
		Stream:     st,
		Model:      resolveModel(e.model, req),
		StatusCode: resp.StatusCode,
		Headers:    flattenHeaders(resp.Header),
	}, nil
}

func (e *KiroExecutor) streamEventStream(ctx context.Context, st *stream.Stream, reader io.Reader, closer io.Closer) {
	var closeOnce sync.Once
	closeCloser := func() { closeOnce.Do(func() { _ = closer.Close() }) }

	defer func() {
		closeCloser()
		if r := recover(); r != nil {
			st.Cancel(fmt.Errorf("panic in Kiro EventStream streamer: %v", r))
		}
	}()

	var gotFirstChunk atomic.Bool

	go func() {
		select {
		case <-ctx.Done():
			closeCloser()
		case <-st.Done():
		}
	}()

	if e.streamCfg.FirstChunkTimeout > 0 {
		go func() {
			timer := time.NewTimer(e.streamCfg.FirstChunkTimeout)
			defer timer.Stop()
			select {
			case <-timer.C:
				if !gotFirstChunk.Load() {
					closeCloser()
				}
			case <-st.Done():
			}
		}()
	}

	var lastByteTime atomic.Int64
	if e.streamCfg.StallTimeout > 0 {
		lastByteTime.Store(time.Now().UnixNano())
		go func() {
			ticker := time.NewTicker(1 * time.Second)
			defer ticker.Stop()
			for {
				select {
				case <-ticker.C:
					if gotFirstChunk.Load() {
						lbt := time.Unix(0, lastByteTime.Load())
						if time.Since(lbt) > e.streamCfg.StallTimeout {
							closeCloser()
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

	for scanner.Scan() {
		if e.streamCfg.StallTimeout > 0 {
			lastByteTime.Store(time.Now().UnixNano())
		}
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		gotFirstChunk.Store(true)
		if !st.Push(stream.Chunk{Data: []byte(line), Event: "eventstream"}) {
			return
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
				st.Cancel(fmt.Errorf("Kiro EventStream scanner error: %w", err))
			}
			return
		}
	}

	st.Push(stream.Chunk{IsFinal: true})
	st.Close()
}

func (e *KiroExecutor) buildRequest(ctx context.Context, req *engine.Request, account *provider.Account, body []byte, streamEnabled bool) (*http.Request, error) {
	baseURL := "https://api.kiro.ai"
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
	if account != nil {
		httpReq.Header.Set("Authorization", "Bearer "+account.CredentialRef)
		httpReq.Header.Set("User-Agent", "kiro-cli/1.0.0")
	}
	if streamEnabled {
		httpReq.Header.Set("Accept", "text/event-stream")
	}
	return httpReq, nil
}

// ---------------------------------------------------------------------------
// GetExecutor — registry that maps matrix provider_type to specialized executor
// ---------------------------------------------------------------------------

func GetExecutor(ptype provider.ProviderType, transport http.RoundTripper) (engine.Executor, bool) {
	switch ptype {
	case provider.ProviderCodex:
		return NewCodexExecutor(transport), true
	case provider.ProviderAzure:
		return NewAzureExecutor(transport), true
	case provider.ProviderGithubModels:
		return NewGitHubExecutor(transport), true
	case provider.ProviderGeminiCli:
		return NewGeminiCLIExecutor(transport), true
	case provider.ProviderGemini:
		return NewGeminiExecutor(transport), true
	case provider.ProviderIflow:
		return NewIFlowExecutor(transport), true
	case provider.ProviderQoder:
		return NewQoderExecutor(transport), true
	case provider.ProviderKiro:
		return NewKiroExecutor(transport), true
	case provider.ProviderKimchi:
		return NewKimchiExecutor(transport), true
	case provider.ProviderCursor:
		return NewCursorExecutor(transport), true
	case provider.ProviderAntigravity:
		return NewAntigravityExecutor(transport), true
	case provider.ProviderCommandcode:
		return NewCommandCodeExecutor(transport), true
	case provider.ProviderGrokCli:
		return NewGrokCliExecutor(transport), true
	case provider.ProviderGrokWeb:
		return NewGrokWebExecutor(transport), true
	case provider.ProviderMimoFree:
		return NewMimoFreeExecutor(transport), true
	case provider.ProviderMmf:
		return NewMimoFreeExecutor(transport), true
	case provider.ProviderOllamaLocal:
		return NewOllamaLocalExecutor(transport), true
	case provider.ProviderPerplexityWeb:
		return NewPerplexityWebExecutor(transport), true
	case provider.ProviderQwen:
		return NewQwenExecutor(transport), true
	case provider.ProviderVertex:
		return NewVertexExecutor(transport, false), true
	case provider.ProviderVertexPartner:
		return NewVertexExecutor(transport, true), true
	case provider.ProviderXiaomiTokenplan:
		return NewXiaomiTokenplanExecutor(transport), true
	case provider.ProviderCodebuddyCn:
		return NewCodeBuddyExecutor(transport), true
	case provider.ProviderPerplexity:
		return NewPerplexityExecutor(transport), true
	case provider.ProviderPerplexityAgent:
		return NewPerplexityExecutor(transport), true
	case provider.ProviderDeepseek:
		return NewDeepSeekExecutor(transport), true
	case provider.ProviderFireworks:
		return NewFireworksExecutor(transport), true
	case provider.ProviderGroq:
		return NewGroqExecutor(transport), true
	case provider.ProviderMistral:
		return NewMistralExecutor(transport), true
	case provider.ProviderTogether:
		return NewTogetherExecutor(transport), true
	case provider.ProviderXai:
		return NewXAIExecutor(transport), true
	default:
		return nil, false
	}
}

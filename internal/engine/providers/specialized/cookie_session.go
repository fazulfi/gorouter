package specialized

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"encoding/binary"
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
// GrokWebExecutor — Browser cookie session (NDJSON stream)
// ---------------------------------------------------------------------------

// GrokWebExecutor implements engine.Executor for Grok.com web chat.
// Uses SSO cookie auth and a custom NDJSON streaming protocol through
// the grok.com chat API. Converts Grok NDJSON events to OpenAI SSE chunks.
type GrokWebExecutor struct {
	client    *http.Client
	model     string
	streamCfg StreamingConfig
}

// grokModelMap maps upstream model names to Grok internal identifiers.
var grokModelMap = map[string]struct {
	grokModel  string
	modelMode  string
	isThinking bool
}{
	"grok-3":            {grokModel: "grok-3", modelMode: "MODEL_MODE_GROK_3", isThinking: false},
	"grok-3-mini":       {grokModel: "grok-3", modelMode: "MODEL_MODE_GROK_3_MINI_THINKING", isThinking: true},
	"grok-3-thinking":   {grokModel: "grok-3", modelMode: "MODEL_MODE_GROK_3_THINKING", isThinking: true},
	"grok-4":            {grokModel: "grok-4", modelMode: "MODEL_MODE_GROK_4", isThinking: false},
	"grok-4-mini":       {grokModel: "grok-4-mini", modelMode: "MODEL_MODE_GROK_4_MINI_THINKING", isThinking: true},
	"grok-4-thinking":   {grokModel: "grok-4", modelMode: "MODEL_MODE_GROK_4_THINKING", isThinking: true},
	"grok-4-heavy":      {grokModel: "grok-4", modelMode: "MODEL_MODE_HEAVY", isThinking: true},
	"grok-4.1-mini":     {grokModel: "grok-4-1-thinking-1129", modelMode: "MODEL_MODE_GROK_4_1_MINI_THINKING", isThinking: true},
	"grok-4.1-fast":     {grokModel: "grok-4-1-thinking-1129", modelMode: "MODEL_MODE_FAST", isThinking: false},
	"grok-4.1-expert":   {grokModel: "grok-4-1-thinking-1129", modelMode: "MODEL_MODE_EXPERT", isThinking: true},
	"grok-4.1-thinking": {grokModel: "grok-4-1-thinking-1129", modelMode: "MODEL_MODE_GROK_4_1_THINKING", isThinking: true},
}

func NewGrokWebExecutor(transport http.RoundTripper) *GrokWebExecutor {
	return &GrokWebExecutor{
		client:    &http.Client{Transport: transport},
		streamCfg: DefaultStreamingConfig(),
	}
}

func (e *GrokWebExecutor) SupportsFormat(format engine.RequestFormat) bool {
	return format == engine.FormatOpenAIChat || format == engine.FormatOpenAICompat
}

func (e *GrokWebExecutor) ProviderType() provider.ProviderType {
	return provider.ProviderGrokWeb
}

func (e *GrokWebExecutor) Execute(ctx context.Context, req *engine.Request, account *provider.Account) (*engine.Response, error) {
	body := selectBody(req)
	var payload struct {
		Messages []struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"messages"`
	}
	if err := json.Unmarshal(body, &payload); err != nil || len(payload.Messages) == 0 {
		return errorResponse("Missing or empty messages array"), nil
	}

	model := resolveModel(e.model, req)
	mi, ok := grokModelMap[model]
	if !ok {
		mi = grokModelMap["grok-4.1-fast"]
	}

	message := parseGrokMessages(payload.Messages)
	if message == "" {
		return errorResponse("Empty query after processing"), nil
	}

	grokPayload := map[string]interface{}{
		"temporary":                   true,
		"modelName":                   mi.grokModel,
		"modelMode":                   mi.modelMode,
		"message":                     message,
		"fileAttachments":             []interface{}{},
		"imageAttachments":            []interface{}{},
		"disableSearch":               false,
		"enableImageGeneration":       false,
		"returnImageBytes":            false,
		"returnRawGrokInXaiRequest":   false,
		"enableImageStreaming":        false,
		"imageGenerationCount":        0,
		"forceConcise":                false,
		"toolOverrides":               map[string]interface{}{},
		"enableSideBySide":            true,
		"sendFinalMetadata":           true,
		"isReasoning":                 false,
		"disableTextFollowUps":        false,
		"disableMemory":               true,
		"forceSideBySide":             false,
		"isAsyncChat":                 false,
		"disableSelfHarmShortCircuit": false,
		"deviceEnvInfo": map[string]interface{}{
			"darkModeEnabled":  false,
			"devicePixelRatio": 2,
			"screenWidth":      2056,
			"screenHeight":     1329,
			"viewportWidth":    2056,
			"viewportHeight":   1083,
		},
	}

	headers := map[string]string{
		"Content-Type":  "application/json",
		"Accept":        "*/*",
		"Origin":        "https://grok.com",
		"Referer":       "https://grok.com/",
		"User-Agent":    "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36",
		"X-Request-ID":  randomHex(16),
		"Cache-Control": "no-cache",
	}
	if account != nil && account.CredentialRef != "" {
		token := account.CredentialRef
		if strings.HasPrefix(token, "sso=") {
			token = token[4:]
		}
		headers["Cookie"] = "sso=" + token
	}

	grokBytes, _ := json.Marshal(grokPayload)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost,
		"https://grok.com/rest/app/chat", bytes.NewReader(grokBytes))
	if err != nil {
		return nil, fmt.Errorf("new request: %w", err)
	}
	for k, v := range headers {
		httpReq.Header.Set(k, v)
	}

	resp, err := e.client.Do(httpReq)
	if err != nil {
		return errorResponse(fmt.Sprintf("Grok connection failed: %v", err)), nil
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		errMsg := fmt.Sprintf("Grok returned HTTP %d", resp.StatusCode)
		if resp.StatusCode == 401 || resp.StatusCode == 403 {
			errMsg = "Grok auth failed - SSO cookie may be expired"
		} else if resp.StatusCode == 429 {
			errMsg = "Grok rate limited"
		}
		return &engine.Response{
			RequestID:  req.ID,
			Body:       []byte(fmt.Sprintf(`{"error":{"message":%q,"type":"upstream_error"}}`, errMsg)),
			Model:      model,
			StatusCode: resp.StatusCode,
			Headers:    flattenHeaders(resp.Header),
		}, nil
	}

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return errorResponse(fmt.Sprintf("Grok: read body: %v", err)), nil
	}

	content, parseErr := parseGrokNDJSONResponse(respBody)
	if parseErr != nil {
		return errorResponse(fmt.Sprintf("Grok: parse response: %v", parseErr)), nil
	}

	oaiResp := buildChatCompletion(model, content)
	return &engine.Response{
		RequestID:  req.ID,
		Body:       oaiResp,
		Model:      model,
		StatusCode: http.StatusOK,
		Headers:    flattenHeaders(resp.Header),
	}, nil
}

func (e *GrokWebExecutor) ExecuteStream(ctx context.Context, req *engine.Request, account *provider.Account) (*engine.Response, error) {
	body := selectBody(req)
	var payload struct {
		Messages []struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"messages"`
	}
	if err := json.Unmarshal(body, &payload); err != nil || len(payload.Messages) == 0 {
		return errorResponse("Missing or empty messages array"), nil
	}

	model := resolveModel(e.model, req)
	mi, ok := grokModelMap[model]
	if !ok {
		mi = grokModelMap["grok-4.1-fast"]
	}

	message := parseGrokMessages(payload.Messages)
	if message == "" {
		return errorResponse("Empty query after processing"), nil
	}

	grokPayload := map[string]interface{}{
		"temporary":                   true,
		"modelName":                   mi.grokModel,
		"modelMode":                   mi.modelMode,
		"message":                     message,
		"fileAttachments":             []interface{}{},
		"imageAttachments":            []interface{}{},
		"disableSearch":               false,
		"enableImageGeneration":       false,
		"returnImageBytes":            false,
		"returnRawGrokInXaiRequest":   false,
		"enableImageStreaming":        false,
		"imageGenerationCount":        0,
		"forceConcise":                false,
		"toolOverrides":               map[string]interface{}{},
		"enableSideBySide":            true,
		"sendFinalMetadata":           true,
		"isReasoning":                 false,
		"disableTextFollowUps":        false,
		"disableMemory":               true,
		"forceSideBySide":             false,
		"isAsyncChat":                 false,
		"disableSelfHarmShortCircuit": false,
		"deviceEnvInfo": map[string]interface{}{
			"darkModeEnabled":  false,
			"devicePixelRatio": 2,
			"screenWidth":      2056,
			"screenHeight":     1329,
			"viewportWidth":    2056,
			"viewportHeight":   1083,
		},
	}

	headers := map[string]string{
		"Content-Type":  "application/json",
		"Accept":        "*/*",
		"Origin":        "https://grok.com",
		"Referer":       "https://grok.com/",
		"User-Agent":    "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36",
		"Cache-Control": "no-cache",
	}
	if account != nil && account.CredentialRef != "" {
		token := account.CredentialRef
		if strings.HasPrefix(token, "sso=") {
			token = token[4:]
		}
		headers["Cookie"] = "sso=" + token
	}

	grokBytes, _ := json.Marshal(grokPayload)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost,
		"https://grok.com/rest/app/chat", bytes.NewReader(grokBytes))
	if err != nil {
		return nil, fmt.Errorf("new request: %w", err)
	}
	for k, v := range headers {
		httpReq.Header.Set(k, v)
	}

	resp, err := e.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("grok request: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		errMsg := fmt.Sprintf("Grok returned HTTP %d", resp.StatusCode)
		if resp.StatusCode == 401 || resp.StatusCode == 403 {
			errMsg = "Grok auth failed - SSO cookie may be expired"
		} else if resp.StatusCode == 429 {
			errMsg = "Grok rate limited"
		}
		return &engine.Response{
			RequestID:  req.ID,
			Body:       []byte(fmt.Sprintf(`{"error":{"message":%q,"type":"upstream_error"}}`, errMsg)),
			Model:      model,
			StatusCode: resp.StatusCode,
			Headers:    flattenHeaders(resp.Header),
		}, nil
	}

	st := stream.NewStream(ctx, 64)
	go e.streamGrokNDJSON(ctx, st, resp.Body, resp.Body)
	return &engine.Response{
		RequestID:  req.ID,
		Stream:     st,
		Model:      model,
		StatusCode: resp.StatusCode,
		Headers:    flattenHeaders(resp.Header),
	}, nil
}

func (e *GrokWebExecutor) streamGrokNDJSON(ctx context.Context, st *stream.Stream, reader io.Reader, closer io.Closer) {
	var closeOnce sync.Once
	closeCloser := func() { closeOnce.Do(func() { _ = closer.Close() }) }

	defer func() {
		closeCloser()
		if r := recover(); r != nil {
			st.Cancel(fmt.Errorf("panic in Grok NDJSON streamer: %v", r))
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
		if !st.Push(stream.Chunk{Data: []byte(line)}) {
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
				st.Cancel(fmt.Errorf("Grok NDJSON scanner error: %w", err))
			}
			return
		}
	}

	st.Push(stream.Chunk{IsFinal: true})
	st.Close()
}

func parseGrokMessages(messages []struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}) string {
	var parts []string
	for _, msg := range messages {
		role := msg.Role
		if role == "developer" {
			role = "system"
		}
		content := msg.Content
		if content == "" {
			continue
		}
		parts = append(parts, role+": "+content)
	}
	return strings.Join(parts, "\n\n")
}

func randomHex(bytes int) string {
	b := make([]byte, bytes)
	rand.Read(b)
	return fmt.Sprintf("%x", b)
}

// ---------------------------------------------------------------------------
// PerplexityWebExecutor — Browser cookie session (SSE)
// ---------------------------------------------------------------------------

// PerplexityWebExecutor implements engine.Executor for Perplexity.ai web chat.
// Uses session token (__Secure-next-auth.session-token) cookie auth and
// Perplexity's custom SSE protocol. Converts PPLX events to OpenAI SSE chunks.
type PerplexityWebExecutor struct {
	client    *http.Client
	model     string
	streamCfg StreamingConfig
}

var pplxModelMap = map[string][2]string{
	"pplx-auto":     {"concise", "pplx_pro"},
	"pplx-sonar":    {"copilot", "experimental"},
	"pplx-gpt":      {"copilot", "gpt54"},
	"pplx-gemini":   {"copilot", "gemini31pro_high"},
	"pplx-sonnet":   {"copilot", "claude46sonnet"},
	"pplx-opus":     {"copilot", "claude46opus"},
	"pplx-nemotron": {"copilot", "nv_nemotron_3_super"},
}

func NewPerplexityWebExecutor(transport http.RoundTripper) *PerplexityWebExecutor {
	return &PerplexityWebExecutor{
		client:    &http.Client{Transport: transport},
		streamCfg: DefaultStreamingConfig(),
	}
}

func (e *PerplexityWebExecutor) SupportsFormat(format engine.RequestFormat) bool {
	return format == engine.FormatOpenAIChat || format == engine.FormatOpenAICompat
}

func (e *PerplexityWebExecutor) ProviderType() provider.ProviderType {
	return provider.ProviderPerplexityWeb
}

func (e *PerplexityWebExecutor) Execute(ctx context.Context, req *engine.Request, account *provider.Account) (*engine.Response, error) {
	return e.executePPLX(ctx, req, account, false)
}

func (e *PerplexityWebExecutor) ExecuteStream(ctx context.Context, req *engine.Request, account *provider.Account) (*engine.Response, error) {
	return e.executePPLX(ctx, req, account, true)
}

func (e *PerplexityWebExecutor) executePPLX(ctx context.Context, req *engine.Request, account *provider.Account, enableStream bool) (*engine.Response, error) {
	body := selectBody(req)
	var payload struct {
		Messages []struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"messages"`
	}
	json.Unmarshal(body, &payload)

	model := resolveModel(e.model, req)
	pref := pplxModelMap[model]
	if pref == [2]string{} {
		pref = [2]string{"copilot", model}
	}
	pplxMode, modelPref := pref[0], pref[1]

	var query string
	if len(payload.Messages) > 0 {
		query = payload.Messages[len(payload.Messages)-1].Content
	}

	pplxBody := map[string]interface{}{
		"query_str": query,
		"params": map[string]interface{}{
			"query_str":           query,
			"search_focus":        "internet",
			"mode":                pplxMode,
			"model_preference":    modelPref,
			"sources":             []string{"web"},
			"attachments":         []interface{}{},
			"version":             "2.18",
			"language":            "en-US",
			"timezone":            "UTC",
			"is_incognito":        true,
			"use_schematized_api": true,
		},
	}

	pplxBytes, _ := json.Marshal(pplxBody)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost,
		"https://www.perplexity.ai/sse/chat", bytes.NewReader(pplxBytes))
	if err != nil {
		return nil, fmt.Errorf("new request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "text/event-stream")
	httpReq.Header.Set("Origin", "https://www.perplexity.ai")
	httpReq.Header.Set("Referer", "https://www.perplexity.ai/")
	httpReq.Header.Set("User-Agent", "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36")
	httpReq.Header.Set("X-App-ApiClient", "default")
	httpReq.Header.Set("X-App-ApiVersion", "2.18")

	if account != nil {
		if account.AuthType == "oauth" || strings.HasPrefix(account.CredentialRef, "Bearer ") {
			httpReq.Header.Set("Authorization", account.CredentialRef)
		} else {
			httpReq.Header.Set("Cookie", "__Secure-next-auth.session-token="+account.CredentialRef)
		}
	}

	resp, err := e.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("pplx request: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		errMsg := fmt.Sprintf("Perplexity returned HTTP %d", resp.StatusCode)
		if resp.StatusCode == 401 || resp.StatusCode == 403 {
			errMsg = "Perplexity auth failed - session cookie may be expired"
		}
		return &engine.Response{
			RequestID:  req.ID,
			Body:       []byte(fmt.Sprintf(`{"error":{"message":%q,"type":"upstream_error"}}`, errMsg)),
			Model:      model,
			StatusCode: resp.StatusCode,
			Headers:    flattenHeaders(resp.Header),
		}, nil
	}

	if enableStream {
		st := stream.NewStream(ctx, 64)
		go e.streamPPLXSSE(ctx, st, resp.Body, resp.Body)
		return &engine.Response{
			RequestID:  req.ID,
			Stream:     st,
			Model:      model,
			StatusCode: resp.StatusCode,
			Headers:    flattenHeaders(resp.Header),
		}, nil
	}

	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)
	return &engine.Response{
		RequestID:  req.ID,
		Body:       respBody,
		Model:      model,
		StatusCode: resp.StatusCode,
		Headers:    flattenHeaders(resp.Header),
	}, nil
}

func (e *PerplexityWebExecutor) streamPPLXSSE(ctx context.Context, st *stream.Stream, reader io.Reader, closer io.Closer) {
	var closeOnce sync.Once
	closeCloser := func() { closeOnce.Do(func() { _ = closer.Close() }) }

	defer func() {
		closeCloser()
		if r := recover(); r != nil {
			st.Cancel(fmt.Errorf("panic in Perplexity SSE streamer: %v", r))
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

	var eventType string
	for scanner.Scan() {
		if e.streamCfg.StallTimeout > 0 {
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
				closeCloser()
				return
			}
			if !st.Push(stream.Chunk{Data: []byte(data), Event: eventType}) {
				return
			}
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
				st.Cancel(fmt.Errorf("PPLX SSE scanner error: %w", err))
			}
			return
		}
	}

	st.Push(stream.Chunk{IsFinal: true})
	st.Close()
}

func errorResponse(msg string) *engine.Response {
	return &engine.Response{
		Body:       []byte(fmt.Sprintf(`{"error":{"message":%q,"type":"invalid_request"}}`, msg)),
		StatusCode: http.StatusBadRequest,
	}
}

func init() {
	// Ensure crypto/rand is used for secure randomness
	var b [8]byte
	rand.Read(b[:])
	binary.LittleEndian.PutUint64(b[:], uint64(time.Now().UnixNano()))
}

// grokNDJSONLine represents a single NDJSON line from the Grok streaming API.
type grokNDJSONLine struct {
	Result *struct {
		Response *struct {
			ModelResponse *struct {
				Message string `json:"message"`
			} `json:"modelResponse"`
		} `json:"response"`
	} `json:"result"`
	Error *struct {
		Message string `json:"message"`
		Code    int    `json:"code"`
	} `json:"error"`
}

// parseGrokNDJSONResponse parses a complete Grok NDJSON response body and
// extracts the final assistant message content. Returns an error if no valid
// response content is found or the API returned an error line.
func parseGrokNDJSONResponse(body []byte) (string, error) {
	scanner := bufio.NewScanner(bytes.NewReader(body))
	var lastContent string
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var obj grokNDJSONLine
		if err := json.Unmarshal([]byte(line), &obj); err != nil {
			continue
		}
		if obj.Error != nil && obj.Error.Message != "" {
			return "", fmt.Errorf("Grok API error: %s", sanitizeErrorString(obj.Error.Message))
		}
		if obj.Result != nil && obj.Result.Response != nil &&
			obj.Result.Response.ModelResponse != nil &&
			obj.Result.Response.ModelResponse.Message != "" {
			lastContent = obj.Result.Response.ModelResponse.Message
		}
	}
	if err := scanner.Err(); err != nil {
		return "", fmt.Errorf("NDJSON scan: %w", err)
	}
	if lastContent == "" {
		return "", fmt.Errorf("no response content found in Grok NDJSON")
	}
	return lastContent, nil
}

func buildChatCompletion(model, content string) []byte {
	resp := map[string]interface{}{
		"object": "chat.completion",
		"model":  model,
		"choices": []map[string]interface{}{
			{
				"index": 0,
				"message": map[string]string{
					"role":    "assistant",
					"content": content,
				},
				"finish_reason": "stop",
			},
		},
	}
	b, _ := json.Marshal(resp)
	return b
}

// credentialPatterns lists substrings that indicate credential leakage
// and must be redacted from error messages before surfacing to callers.
var credentialPatterns = []string{
	"sso=",
	"Bearer ",
	"credential_ref=",
	"api_key=",
	"session-token=",
	"token=",
}

// credentialValueEnd reports whether b terminates a credential value. Values
// run until whitespace or a punctuation/structural delimiter so adjacent
// non-secret diagnostics ("invalid", "expired", "retry") survive redaction.
func credentialValueEnd(b byte) bool {
	switch b {
	case ' ', '\t', '\n', '\r', '"', '\'', ',', ';', '}', ']', ')', '&', '<', '>':
		return true
	}
	return false
}

// redactCredentialValues replaces every occurrence of pat plus the value that
// follows it with a single [REDACTED] marker.
func redactCredentialValues(msg, pat string) string {
	if pat == "" {
		return msg
	}
	var sb strings.Builder
	rest := msg
	for {
		idx := strings.Index(rest, pat)
		if idx < 0 {
			sb.WriteString(rest)
			return sb.String()
		}
		sb.WriteString(rest[:idx])
		sb.WriteString("[REDACTED]")
		rest = rest[idx+len(pat):]
		end := 0
		for end < len(rest) && !credentialValueEnd(rest[end]) {
			end++
		}
		rest = rest[end:]
	}
}

// sanitizeErrorString strips credential-like substrings (pattern AND value)
// from an error message to prevent accidental leakage of API keys, cookies,
// or tokens. Messages without credential patterns pass through unchanged.
func sanitizeErrorString(msg string) string {
	sanitized := msg
	for _, pat := range credentialPatterns {
		sanitized = redactCredentialValues(sanitized, pat)
	}
	return sanitized
}

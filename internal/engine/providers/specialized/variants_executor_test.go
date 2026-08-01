package specialized

// variants_executor_test.go — deep behavioural tests for the OpenAI-variant
// executor family (GitHub, Azure, Qwen, CommandCode, Xiaomi, Mimo, CodeBuddy)
// and the Codex Responses executor's peek/[DONE]/model/error/header branches.
//
// Contract source: upstream 9router commit 79918c7830695bbca4a45c9fea4a42c3e9fd73d1
// (open-sse/executors/github.js, open-sse/executors/codex.js,
// open-sse/executors/default.js, open-sse/providers/registry/*).
//
// Defect probes (kept RED on purpose; see defect report):
//   - TestGitHubExecutor_Execute_400FallsBackToResponses / ..._Stream_... :
//     the 400 -> /responses escalation is dead code because handleResponse
//     converts the 400 into an error before the fallback branch is reached.
//   - TestCodexExecutor_ExecuteStream_MultiLineFirstEvent* : the peek discard
//     arithmetic (bytes.Index + len(data) + 2) corrupts multi-line first
//     events, losing or mislabelling the remaining data lines.
//
// No production code is modified by this file.

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"gorouter/internal/domain/engine"
	"gorouter/internal/domain/engine/stream"
	"gorouter/internal/domain/provider"
	"gorouter/internal/shared"
)

// ---------------------------------------------------------------------------
// shared test helpers (unique to this package; no name collisions with the
// existing helpers newTestAccount / newTestRequest / codexTestAccount /
// codexTestRequest)
// ---------------------------------------------------------------------------

// variantsJSONBody marshals a map into a JSON request body for tests.
func variantsJSONBody(t *testing.T, v map[string]interface{}) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal body: %v", err)
	}
	return b
}

// collectStreamChunks drains a stream until it is closed (or a final chunk
// arrives) and returns every chunk received. It fails the test on timeout.
func collectStreamChunks(t *testing.T, st *stream.Stream) []stream.Chunk {
	t.Helper()
	var chunks []stream.Chunk
	deadline := time.After(5 * time.Second)
	for {
		select {
		case chunk, ok := <-st.Chunks():
			if !ok {
				return chunks
			}
			chunks = append(chunks, chunk)
			if chunk.IsFinal {
				return chunks
			}
		case <-deadline:
			t.Fatalf("timeout waiting for stream to finish; got %d chunks so far", len(chunks))
		}
	}
}

// dataChunks filters collected chunks down to those carrying payload data.
func dataChunks(chunks []stream.Chunk) []stream.Chunk {
	var out []stream.Chunk
	for _, c := range chunks {
		if len(c.Data) > 0 {
			out = append(out, c)
		}
	}
	return out
}

// chunkPayloads returns the payload strings of all data chunks.
func chunkPayloads(chunks []stream.Chunk) []string {
	var out []string
	for _, c := range dataChunks(chunks) {
		out = append(out, string(c.Data))
	}
	return out
}

// assertAppError verifies that err is an *shared.AppError with the expected
// error code and HTTP status.
func assertAppError(t *testing.T, err error, code shared.ErrorCode, status int) {
	t.Helper()
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	appErr, ok := err.(*shared.AppError)
	if !ok {
		t.Fatalf("expected *shared.AppError, got %T: %v", err, err)
	}
	if appErr.Code != code {
		t.Errorf("error code = %q, want %q (msg: %s)", appErr.Code, code, appErr.Message)
	}
	if appErr.HTTPStatus != status {
		t.Errorf("error HTTP status = %d, want %d", appErr.HTTPStatus, status)
	}
}

// standardChatResponse is a canonical OpenAI chat-completion JSON body used by
// non-streaming response fixtures.
const standardChatResponse = `{"id":"chatcmpl-test","object":"chat.completion","created":1715000000,` +
	`"model":"gpt-4","choices":[{"index":0,"finish_reason":"stop","message":{"role":"assistant","content":"Hello"}}],` +
	`"usage":{"prompt_tokens":10,"completion_tokens":20,"total_tokens":30}}`

// sseChatBody is a two-chunk streaming body terminated by [DONE].
const sseChatBody = "data: {\"choices\":[{\"delta\":{\"content\":\"Hello\"}}]}\n\n" +
	"data: {\"choices\":[{\"delta\":{\"content\":\" world\"}}]}\n\n" +
	"data: [DONE]\n\n"

// ---------------------------------------------------------------------------
// GitHubExecutor — copilot headers
// ---------------------------------------------------------------------------

// TestGitHubExecutor_CopilotHeaders pins the exact Copilot identity headers
// emitted by copilotHeaders for both streaming and non-streaming requests
// (upstream github.js buildHeaders, executors/github.js lines 36-54).
func TestGitHubExecutor_CopilotHeaders(t *testing.T) {
	e := NewGitHubExecutor(http.DefaultTransport)
	acct := newTestAccount("gh-copilot-token")

	h := e.copilotHeaders(acct, false)
	want := map[string]string{
		"Authorization":          "Bearer gh-copilot-token",
		"Content-Type":           "application/json",
		"copilot-integration-id": "vscode-chat",
		"editor-version":         "vscode/1.96.0",
		"editor-plugin-version":  "copilot-chat/0.24.0",
		"User-Agent":             "GitHubCopilot/1.96.0",
		"openai-intent":          "conversation-panel",
		"x-github-api-version":   "2025-01-01",
		"X-Initiator":            "user",
		"anthropic-version":      "2023-06-01",
		"Accept":                 "application/json",
	}
	for k, wantV := range want {
		if got := h[k]; got != wantV {
			t.Errorf("header %q = %q, want %q", k, got, wantV)
		}
	}

	hStream := e.copilotHeaders(acct, true)
	if got := hStream["Accept"]; got != "text/event-stream" {
		t.Errorf("stream Accept = %q, want text/event-stream", got)
	}
	if got := hStream["Authorization"]; got != "Bearer gh-copilot-token" {
		t.Errorf("stream Authorization = %q", got)
	}
}

// TestGitHubExecutor_CopilotHeaders_NoAccount verifies the Bearer token is
// empty when no account is supplied (headers are still emitted).
func TestGitHubExecutor_CopilotHeaders_NoAccount(t *testing.T) {
	e := NewGitHubExecutor(http.DefaultTransport)
	h := e.copilotHeaders(nil, false)
	if got := h["Authorization"]; got != "Bearer " {
		t.Errorf("Authorization = %q, want %q", got, "Bearer ")
	}
}

// ---------------------------------------------------------------------------
// GitHubExecutor — execute (non-streaming)
// ---------------------------------------------------------------------------

// TestGitHubExecutor_Execute_ChatCompletions exercises the default
// /chat/completions route for a non-claude model: exact path, full copilot
// header set, stream:false injection and raw body passthrough.
func TestGitHubExecutor_Execute_ChatCompletions(t *testing.T) {
	var gotPath, gotAccept, gotAuth, gotIntegration, gotIntent string
	var gotBody map[string]interface{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAccept = r.Header.Get("Accept")
		gotAuth = r.Header.Get("Authorization")
		gotIntegration = r.Header.Get("copilot-integration-id")
		gotIntent = r.Header.Get("openai-intent")
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, standardChatResponse)
	}))
	defer srv.Close()

	e := NewGitHubExecutor(srv.Client().Transport)
	req := newTestRequest(engine.FormatOpenAIChat, "gpt-5", variantsJSONBody(t, map[string]interface{}{
		"model":    "gpt-5",
		"messages": []map[string]interface{}{{"role": "user", "content": "hi"}},
	}))
	req.Headers = map[string]string{"X-Base-URL": srv.URL}

	resp, err := e.Execute(context.Background(), req, newTestAccount("copilot-token"))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if gotPath != "/chat/completions" {
		t.Errorf("path = %q, want /chat/completions", gotPath)
	}
	if gotAccept != "application/json" {
		t.Errorf("Accept = %q, want application/json", gotAccept)
	}
	if gotAuth != "Bearer copilot-token" {
		t.Errorf("Authorization = %q", gotAuth)
	}
	if gotIntegration != "vscode-chat" {
		t.Errorf("copilot-integration-id = %q", gotIntegration)
	}
	if gotIntent != "conversation-panel" {
		t.Errorf("openai-intent = %q", gotIntent)
	}
	if gotBody["stream"] != false {
		t.Errorf("body stream = %v, want false", gotBody["stream"])
	}
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d", resp.StatusCode)
	}
	if !strings.Contains(string(resp.Body), "chatcmpl-test") {
		t.Errorf("body = %q", resp.Body)
	}
	if resp.Model != "gpt-5" {
		t.Errorf("model = %q", resp.Model)
	}
}

// TestGitHubExecutor_Execute_ClaudeMessages verifies claude-* models route to
// the Anthropic-native /v1/messages shim with the copilot headers and no
// stream injection (upstream github.js execute(), lines 130-133).
func TestGitHubExecutor_Execute_ClaudeMessages(t *testing.T) {
	var gotPath, gotAccept, gotAnthropicVersion string
	var gotBody map[string]interface{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAccept = r.Header.Get("Accept")
		gotAnthropicVersion = r.Header.Get("anthropic-version")
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		_, _ = io.WriteString(w, `{"id":"msg_1","type":"message"}`)
	}))
	defer srv.Close()

	e := NewGitHubExecutor(srv.Client().Transport)
	req := newTestRequest(engine.FormatOpenAIChat, "claude-sonnet-4-20250514", variantsJSONBody(t, map[string]interface{}{
		"model":    "claude-sonnet-4-20250514",
		"messages": []map[string]interface{}{{"role": "user", "content": "hi"}},
	}))
	req.Headers = map[string]string{"X-Base-URL": srv.URL}

	resp, err := e.Execute(context.Background(), req, newTestAccount("copilot-token"))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if gotPath != "/v1/messages" {
		t.Errorf("path = %q, want /v1/messages", gotPath)
	}
	if gotAccept != "application/json" {
		t.Errorf("Accept = %q, want application/json", gotAccept)
	}
	if gotAnthropicVersion != "2023-06-01" {
		t.Errorf("anthropic-version = %q", gotAnthropicVersion)
	}
	if _, present := gotBody["stream"]; present {
		t.Error("stream must not be injected on the non-streaming /v1/messages route")
	}
	if !strings.Contains(string(resp.Body), "msg_1") {
		t.Errorf("body = %q", resp.Body)
	}
}

// TestGitHubExecutor_Execute_XBaseURLOverride verifies the X-Base-URL header
// is honored for the chat route.
func TestGitHubExecutor_Execute_XBaseURLOverride(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		_, _ = io.WriteString(w, `{"id":"ok"}`)
	}))
	defer srv.Close()

	e := NewGitHubExecutor(srv.Client().Transport)
	req := newTestRequest(engine.FormatOpenAIChat, "gpt-5", variantsJSONBody(t, map[string]interface{}{"model": "gpt-5"}))
	req.Headers = map[string]string{"X-Base-URL": srv.URL}

	if _, err := e.Execute(context.Background(), req, newTestAccount("t")); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if gotPath != "/chat/completions" {
		t.Errorf("path = %q, want /chat/completions on override base", gotPath)
	}
}

// TestGitHubExecutor_Execute_Error401 maps a 401 to an AppError with
// ErrUnauthorized and status 401.
func TestGitHubExecutor_Execute_Error401(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = io.WriteString(w, `{"error":{"message":"bad token"}}`)
	}))
	defer srv.Close()

	e := NewGitHubExecutor(srv.Client().Transport)
	req := newTestRequest(engine.FormatOpenAIChat, "gpt-5", variantsJSONBody(t, map[string]interface{}{"model": "gpt-5"}))
	req.Headers = map[string]string{"X-Base-URL": srv.URL}

	_, err := e.Execute(context.Background(), req, newTestAccount("bad"))
	assertAppError(t, err, shared.ErrUnauthorized, http.StatusUnauthorized)
}

// TestGitHubExecutor_Execute_Error429 maps a 429 to ErrRateLimited.
func TestGitHubExecutor_Execute_Error429(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = io.WriteString(w, `{"error":{"message":"rate limited"}}`)
	}))
	defer srv.Close()

	e := NewGitHubExecutor(srv.Client().Transport)
	req := newTestRequest(engine.FormatOpenAIChat, "gpt-5", variantsJSONBody(t, map[string]interface{}{"model": "gpt-5"}))
	req.Headers = map[string]string{"X-Base-URL": srv.URL}

	_, err := e.Execute(context.Background(), req, newTestAccount("t"))
	assertAppError(t, err, shared.ErrRateLimited, http.StatusTooManyRequests)
}

// TestGitHubExecutor_Execute_Error500 maps a 500 to ErrInternal.
func TestGitHubExecutor_Execute_Error500(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = io.WriteString(w, `{"error":"boom"}`)
	}))
	defer srv.Close()

	e := NewGitHubExecutor(srv.Client().Transport)
	req := newTestRequest(engine.FormatOpenAIChat, "gpt-5", variantsJSONBody(t, map[string]interface{}{"model": "gpt-5"}))
	req.Headers = map[string]string{"X-Base-URL": srv.URL}

	_, err := e.Execute(context.Background(), req, newTestAccount("t"))
	assertAppError(t, err, shared.ErrInternal, http.StatusInternalServerError)
}

// ---------------------------------------------------------------------------
// GitHubExecutor — execute 400 -> /responses fallback (defect probes)
// ---------------------------------------------------------------------------

// TestGitHubExecutor_Execute_400FallsBackToResponses is a DEFECT PROBE.
// Upstream (github.js execute(), lines 154-161) escalates a 400 whose body
// contains "not accessible via the /chat/completions endpoint" to the
// /responses endpoint for models that support it. The current Go
// implementation converts the 400 into an error inside handleResponse before
// the fallback branch in Execute can observe resp.StatusCode, so the fallback
// is dead code. This test asserts the upstream contract and is expected to
// FAIL (RED) until the production code is fixed.
func TestGitHubExecutor_Execute_400FallsBackToResponses(t *testing.T) {
	var chatHits, responsesHits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/chat/completions":
			chatHits.Add(1)
			w.WriteHeader(http.StatusBadRequest)
			_, _ = io.WriteString(w, `{"error":{"message":"This model is not accessible via the /chat/completions endpoint. Please use the Responses API."}}`)
		case "/responses":
			responsesHits.Add(1)
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"id":"resp_1","object":"response","model":"gpt-5-codex"}`)
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
		}
	}))
	defer srv.Close()

	e := NewGitHubExecutor(srv.Client().Transport)
	req := newTestRequest(engine.FormatOpenAIChat, "gpt-5-codex", variantsJSONBody(t, map[string]interface{}{
		"model":    "gpt-5-codex",
		"messages": []map[string]interface{}{{"role": "user", "content": "hi"}},
	}))
	req.Headers = map[string]string{"X-Base-URL": srv.URL}

	resp, err := e.Execute(context.Background(), req, newTestAccount("copilot-token"))
	if err != nil {
		t.Fatalf("DEFECT: expected /responses fallback after marker 400, got error: %v", err)
	}
	if chatHits.Load() != 1 {
		t.Errorf("chat hits = %d, want 1", chatHits.Load())
	}
	if responsesHits.Load() != 1 {
		t.Errorf("DEFECT: /responses hits = %d, want 1 (fallback never executed)", responsesHits.Load())
	}
	if !strings.Contains(string(resp.Body), "resp_1") {
		t.Errorf("expected /responses body, got %q", resp.Body)
	}
}

// TestGitHubExecutor_Execute_400RequestedModelNotSupported is a DEFECT PROBE
// for the second upstream marker string "The requested model is not
// supported" (github.js line 157).
func TestGitHubExecutor_Execute_400RequestedModelNotSupported(t *testing.T) {
	var responsesHits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/chat/completions":
			w.WriteHeader(http.StatusBadRequest)
			_, _ = io.WriteString(w, `{"error":"The requested model is not supported by the /chat/completions endpoint"}`)
		case "/responses":
			responsesHits.Add(1)
			_, _ = io.WriteString(w, `{"id":"resp_2"}`)
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
		}
	}))
	defer srv.Close()

	e := NewGitHubExecutor(srv.Client().Transport)
	req := newTestRequest(engine.FormatOpenAIChat, "gpt-5-codex", variantsJSONBody(t, map[string]interface{}{"model": "gpt-5-codex"}))
	req.Headers = map[string]string{"X-Base-URL": srv.URL}

	_, err := e.Execute(context.Background(), req, newTestAccount("copilot-token"))
	if err != nil {
		t.Fatalf("DEFECT: expected /responses fallback, got error: %v", err)
	}
	if responsesHits.Load() != 1 {
		t.Errorf("DEFECT: /responses hits = %d, want 1", responsesHits.Load())
	}
}

// TestGitHubExecutor_Execute_400GenericNoFallback verifies a 400 WITHOUT the
// escalation markers is surfaced as an error and never retried against
// /responses.
func TestGitHubExecutor_Execute_400GenericNoFallback(t *testing.T) {
	var responsesHits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/chat/completions":
			w.WriteHeader(http.StatusBadRequest)
			_, _ = io.WriteString(w, `{"error":{"message":"some unrelated validation error"}}`)
		case "/responses":
			responsesHits.Add(1)
			_, _ = io.WriteString(w, `{"id":"resp_x"}`)
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
		}
	}))
	defer srv.Close()

	e := NewGitHubExecutor(srv.Client().Transport)
	req := newTestRequest(engine.FormatOpenAIChat, "gpt-5", variantsJSONBody(t, map[string]interface{}{"model": "gpt-5"}))
	req.Headers = map[string]string{"X-Base-URL": srv.URL}

	_, err := e.Execute(context.Background(), req, newTestAccount("copilot-token"))
	assertAppError(t, err, shared.ErrInternal, http.StatusBadRequest)
	if responsesHits.Load() != 0 {
		t.Errorf("responses hits = %d, want 0", responsesHits.Load())
	}
}

// TestGitHubExecutor_Execute_400GeminiNoFallback verifies gemini models are
// never escalated to /responses even on a 400 (supportsResponsesEndpoint
// excludes gemini/claude, upstream fix #1062).
func TestGitHubExecutor_Execute_400GeminiNoFallback(t *testing.T) {
	var responsesHits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/chat/completions":
			w.WriteHeader(http.StatusBadRequest)
			_, _ = io.WriteString(w, `{"error":{"message":"This model is not accessible via the /chat/completions endpoint."}}`)
		case "/responses":
			responsesHits.Add(1)
			_, _ = io.WriteString(w, `{"id":"resp_x"}`)
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
		}
	}))
	defer srv.Close()

	e := NewGitHubExecutor(srv.Client().Transport)
	req := newTestRequest(engine.FormatOpenAIChat, "gemini-2.0-flash", variantsJSONBody(t, map[string]interface{}{"model": "gemini-2.0-flash"}))
	req.Headers = map[string]string{"X-Base-URL": srv.URL}

	_, err := e.Execute(context.Background(), req, newTestAccount("copilot-token"))
	assertAppError(t, err, shared.ErrInternal, http.StatusBadRequest)
	if responsesHits.Load() != 0 {
		t.Errorf("responses hits = %d, want 0 for gemini model", responsesHits.Load())
	}
}

// ---------------------------------------------------------------------------
// GitHubExecutor — execute stream
// ---------------------------------------------------------------------------

// TestGitHubExecutor_ExecuteStream_ChatCompletions verifies the streaming
// /chat/completions path: Accept: text/event-stream, stream:true injection,
// and an exact 2-data-chunk + final sequence.
func TestGitHubExecutor_ExecuteStream_ChatCompletions(t *testing.T) {
	var gotAccept string
	var gotBody map[string]interface{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAccept = r.Header.Get("Accept")
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, sseChatBody)
	}))
	defer srv.Close()

	e := NewGitHubExecutor(srv.Client().Transport)
	req := newTestRequest(engine.FormatOpenAIChat, "gpt-5", variantsJSONBody(t, map[string]interface{}{"model": "gpt-5"}))
	req.Headers = map[string]string{"X-Base-URL": srv.URL}

	resp, err := e.ExecuteStream(context.Background(), req, newTestAccount("copilot-token"))
	if err != nil {
		t.Fatalf("ExecuteStream: %v", err)
	}
	if gotAccept != "text/event-stream" {
		t.Errorf("Accept = %q, want text/event-stream", gotAccept)
	}
	if gotBody["stream"] != true {
		t.Errorf("body stream = %v, want true", gotBody["stream"])
	}
	if resp.Stream == nil {
		t.Fatal("nil stream")
	}
	st := resp.Stream.(*stream.Stream)
	chunks := collectStreamChunks(t, st)
	payloads := chunkPayloads(chunks)
	want := []string{
		`{"choices":[{"delta":{"content":"Hello"}}]}`,
		`{"choices":[{"delta":{"content":" world"}}]}`,
	}
	if len(payloads) != len(want) {
		t.Fatalf("chunk payloads = %d, want %d: %v", len(payloads), len(want), payloads)
	}
	for i := range want {
		if payloads[i] != want[i] {
			t.Errorf("chunk[%d] = %q, want %q", i, payloads[i], want[i])
		}
	}
	if len(chunks) != 3 {
		t.Errorf("total chunks = %d, want 3 (2 data + 1 final)", len(chunks))
	}
	if !chunks[len(chunks)-1].IsFinal {
		t.Error("last chunk must be IsFinal")
	}
}

// TestGitHubExecutor_ExecuteStream_ClaudeMessages verifies the claude
// streaming route sends stream:true to /v1/messages and relays SSE chunks.
func TestGitHubExecutor_ExecuteStream_ClaudeMessages(t *testing.T) {
	var gotPath string
	var gotBody map[string]interface{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, sseChatBody)
	}))
	defer srv.Close()

	e := NewGitHubExecutor(srv.Client().Transport)
	req := newTestRequest(engine.FormatOpenAIChat, "claude-sonnet-4-20250514", variantsJSONBody(t, map[string]interface{}{
		"model":    "claude-sonnet-4-20250514",
		"messages": []map[string]interface{}{{"role": "user", "content": "hi"}},
	}))
	req.Headers = map[string]string{"X-Base-URL": srv.URL}

	resp, err := e.ExecuteStream(context.Background(), req, newTestAccount("copilot-token"))
	if err != nil {
		t.Fatalf("ExecuteStream: %v", err)
	}
	if gotPath != "/v1/messages" {
		t.Errorf("path = %q, want /v1/messages", gotPath)
	}
	if gotBody["stream"] != true {
		t.Errorf("body stream = %v, want true", gotBody["stream"])
	}
	st := resp.Stream.(*stream.Stream)
	payloads := chunkPayloads(collectStreamChunks(t, st))
	if len(payloads) != 2 {
		t.Fatalf("chunk payloads = %d, want 2: %v", len(payloads), payloads)
	}
}

// TestGitHubExecutor_ExecuteStream_Error401 surfaces stream auth errors before
// any stream is handed back.
func TestGitHubExecutor_ExecuteStream_Error401(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = io.WriteString(w, `{"error":"unauthorized"}`)
	}))
	defer srv.Close()

	e := NewGitHubExecutor(srv.Client().Transport)
	req := newTestRequest(engine.FormatOpenAIChat, "gpt-5", variantsJSONBody(t, map[string]interface{}{"model": "gpt-5"}))
	req.Headers = map[string]string{"X-Base-URL": srv.URL}

	_, err := e.ExecuteStream(context.Background(), req, newTestAccount("bad"))
	assertAppError(t, err, shared.ErrUnauthorized, http.StatusUnauthorized)
}

// TestGitHubExecutor_ExecuteStream_400FallsBackToResponses is a DEFECT PROBE
// mirroring the non-streaming fallback probe on the streaming path: a 400
// marker from /chat/completions must be retried against /responses with
// streaming enabled. The current implementation returns the 400 error
// instead, so this test is expected to FAIL (RED).
func TestGitHubExecutor_ExecuteStream_400FallsBackToResponses(t *testing.T) {
	var chatHits, responsesHits atomic.Int32
	var responsesAccept string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/chat/completions":
			chatHits.Add(1)
			w.WriteHeader(http.StatusBadRequest)
			_, _ = io.WriteString(w, `{"error":{"message":"This model is not accessible via the /chat/completions endpoint. Please use the Responses API."}}`)
		case "/responses":
			responsesHits.Add(1)
			responsesAccept = r.Header.Get("Accept")
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = io.WriteString(w, sseChatBody)
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
		}
	}))
	defer srv.Close()

	e := NewGitHubExecutor(srv.Client().Transport)
	req := newTestRequest(engine.FormatOpenAIChat, "gpt-5-codex", variantsJSONBody(t, map[string]interface{}{
		"model":    "gpt-5-codex",
		"messages": []map[string]interface{}{{"role": "user", "content": "hi"}},
	}))
	req.Headers = map[string]string{"X-Base-URL": srv.URL}

	resp, err := e.ExecuteStream(context.Background(), req, newTestAccount("copilot-token"))
	if err != nil {
		t.Fatalf("DEFECT: expected /responses stream fallback, got error: %v", err)
	}
	if chatHits.Load() != 1 || responsesHits.Load() != 1 {
		t.Errorf("DEFECT: chat hits = %d, responses hits = %d, want 1/1", chatHits.Load(), responsesHits.Load())
	}
	if responsesAccept != "text/event-stream" {
		t.Errorf("responses Accept = %q, want text/event-stream", responsesAccept)
	}
	if resp.Stream == nil {
		t.Fatal("nil stream")
	}
	payloads := chunkPayloads(collectStreamChunks(t, resp.Stream.(*stream.Stream)))
	if len(payloads) != 2 {
		t.Errorf("chunk payloads = %d, want 2", len(payloads))
	}
}

// ---------------------------------------------------------------------------
// AzureExecutor
// ---------------------------------------------------------------------------

// TestAzureExecutor_Execute_ExactURLAndHeaders pins the Azure deployment URL
// shape, api-key auth, organization header, the absence of the Authorization
// header, and the pass-through of non-reserved X-Azure-* request headers.
func TestAzureExecutor_Execute_ExactURLAndHeaders(t *testing.T) {
	var gotPath, gotQuery, gotAPIKey, gotOrg, gotAuth, gotContentType, gotExtra string
	var gotBody map[string]interface{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotQuery = r.URL.RawQuery
		gotAPIKey = r.Header.Get("api-key")
		gotOrg = r.Header.Get("OpenAI-Organization")
		gotAuth = r.Header.Get("Authorization")
		gotContentType = r.Header.Get("Content-Type")
		gotExtra = r.Header.Get("X-Azure-Skip-Me")
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		_, _ = io.WriteString(w, standardChatResponse)
	}))
	defer srv.Close()

	e := NewAzureExecutor(srv.Client().Transport)
	req := newTestRequest(engine.FormatOpenAIChat, "gpt-4", variantsJSONBody(t, map[string]interface{}{
		"messages": []map[string]interface{}{{"role": "user", "content": "hi"}},
	}))
	req.Headers = map[string]string{
		"X-Azure-Endpoint":     srv.URL,
		"X-Azure-Api-Version":  "2025-01-01",
		"X-Azure-Organization": "acme-corp",
		"X-Azure-Skip-Me":      "forwarded-value",
	}

	resp, err := e.Execute(context.Background(), req, newTestAccount("azure-key"))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if gotPath != "/openai/deployments/gpt-4/chat/completions" {
		t.Errorf("path = %q, want /openai/deployments/gpt-4/chat/completions", gotPath)
	}
	if gotQuery != "api-version=2025-01-01" {
		t.Errorf("query = %q, want api-version=2025-01-01", gotQuery)
	}
	if gotAPIKey != "azure-key" {
		t.Errorf("api-key = %q, want azure-key", gotAPIKey)
	}
	if gotOrg != "acme-corp" {
		t.Errorf("OpenAI-Organization = %q", gotOrg)
	}
	if gotAuth != "" {
		t.Errorf("Authorization must be empty for Azure api-key auth, got %q", gotAuth)
	}
	if gotExtra != "forwarded-value" {
		t.Errorf("non-reserved X-Azure-* header = %q, want forwarded-value", gotExtra)
	}
	if gotContentType != "application/json" {
		t.Errorf("Content-Type = %q", gotContentType)
	}
	if gotBody["stream"] != false {
		t.Errorf("body stream = %v, want false", gotBody["stream"])
	}
	if gotBody["model"] != "gpt-4" {
		t.Errorf("injected model = %v, want gpt-4", gotBody["model"])
	}
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d", resp.StatusCode)
	}
	if resp.Usage == nil || resp.Usage.TotalTokens != 30 {
		t.Errorf("usage = %+v, want total 30", resp.Usage)
	}
	if resp.Model != "gpt-4" {
		t.Errorf("model = %q", resp.Model)
	}
}

// TestAzureExecutor_Execute_PayloadModelWins verifies an explicit model in the
// payload is preserved (no deployment override).
func TestAzureExecutor_Execute_PayloadModelWins(t *testing.T) {
	var gotBody map[string]interface{}
	var gotURL string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotURL = r.URL.String()
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		_, _ = io.WriteString(w, standardChatResponse)
	}))
	defer srv.Close()

	e := NewAzureExecutor(srv.Client().Transport)
	req := newTestRequest(engine.FormatOpenAIChat, "gpt-4", variantsJSONBody(t, map[string]interface{}{
		"model":    "gpt-4-32k",
		"messages": []map[string]interface{}{},
	}))
	req.Headers = map[string]string{"X-Azure-Endpoint": srv.URL}

	if _, err := e.Execute(context.Background(), req, newTestAccount("k")); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if gotBody["model"] != "gpt-4-32k" {
		t.Errorf("payload model = %v, want gpt-4-32k", gotBody["model"])
	}
	if !strings.Contains(gotURL, "/deployments/gpt-4/chat/completions") {
		t.Errorf("URL = %q, want gpt-4 deployment (req.Model)", gotURL)
	}
}

// TestAzureExecutor_ExecuteStream_AcceptAndChunks verifies streaming headers
// and an exact chunk sequence for Azure.
func TestAzureExecutor_ExecuteStream_AcceptAndChunks(t *testing.T) {
	var gotAccept string
	var gotBody map[string]interface{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAccept = r.Header.Get("Accept")
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, sseChatBody)
	}))
	defer srv.Close()

	e := NewAzureExecutor(srv.Client().Transport)
	req := newTestRequest(engine.FormatOpenAIChat, "gpt-4", variantsJSONBody(t, map[string]interface{}{
		"model":    "gpt-4",
		"messages": []map[string]interface{}{},
	}))
	req.Headers = map[string]string{"X-Azure-Endpoint": srv.URL}

	resp, err := e.ExecuteStream(context.Background(), req, newTestAccount("azure-key"))
	if err != nil {
		t.Fatalf("ExecuteStream: %v", err)
	}
	if gotAccept != "text/event-stream" {
		t.Errorf("Accept = %q, want text/event-stream", gotAccept)
	}
	if gotBody["stream"] != true {
		t.Errorf("body stream = %v, want true", gotBody["stream"])
	}
	if gotBody["model"] != "gpt-4" {
		t.Errorf("body model = %v, want gpt-4", gotBody["model"])
	}
	st := resp.Stream.(*stream.Stream)
	payloads := chunkPayloads(collectStreamChunks(t, st))
	if len(payloads) != 2 {
		t.Fatalf("chunk payloads = %d, want 2: %v", len(payloads), payloads)
	}
	if payloads[0] != `{"choices":[{"delta":{"content":"Hello"}}]}` {
		t.Errorf("chunk[0] = %q", payloads[0])
	}
}

// TestAzureExecutor_Execute_ErrorMapping covers 401/429/500 -> AppError codes.
func TestAzureExecutor_Execute_ErrorMapping(t *testing.T) {
	tests := []struct {
		name     string
		status   int
		wantCode shared.ErrorCode
	}{
		{"unauthorized", http.StatusUnauthorized, shared.ErrUnauthorized},
		{"forbidden", http.StatusForbidden, shared.ErrUnauthorized},
		{"rate_limited", http.StatusTooManyRequests, shared.ErrRateLimited},
		{"server_error", http.StatusInternalServerError, shared.ErrInternal},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tt.status)
				_, _ = io.WriteString(w, `{"error":"boom"}`)
			}))
			defer srv.Close()

			e := NewAzureExecutor(srv.Client().Transport)
			req := newTestRequest(engine.FormatOpenAIChat, "gpt-4", variantsJSONBody(t, map[string]interface{}{"model": "gpt-4"}))
			req.Headers = map[string]string{"X-Azure-Endpoint": srv.URL}

			_, err := e.Execute(context.Background(), req, newTestAccount("k"))
			assertAppError(t, err, tt.wantCode, tt.status)
		})
	}
}

// ---------------------------------------------------------------------------
// QwenExecutor
// ---------------------------------------------------------------------------

// TestQwenExecutor_Execute_ExactURLAndAuth pins the DashScope compatible
// endpoint and Bearer auth.
func TestQwenExecutor_Execute_ExactURLAndAuth(t *testing.T) {
	var gotPath, gotAuth, gotContentType string
	var gotBody map[string]interface{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		gotContentType = r.Header.Get("Content-Type")
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		_, _ = io.WriteString(w, standardChatResponse)
	}))
	defer srv.Close()

	e := NewQwenExecutor(srv.Client().Transport)
	req := newTestRequest(engine.FormatGemini, "qwen-max", variantsJSONBody(t, map[string]interface{}{
		"messages": []map[string]interface{}{{"role": "user", "content": "hi"}},
	}))
	req.Headers = map[string]string{"X-Base-URL": srv.URL}

	resp, err := e.Execute(context.Background(), req, newTestAccount("qwen-key"))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if gotPath != "/compatible-endpoint/v1/chat/completions" {
		t.Errorf("path = %q, want /compatible-endpoint/v1/chat/completions", gotPath)
	}
	if gotAuth != "Bearer qwen-key" {
		t.Errorf("Authorization = %q", gotAuth)
	}
	if gotContentType != "application/json" {
		t.Errorf("Content-Type = %q", gotContentType)
	}
	if gotBody["stream"] != false {
		t.Errorf("body stream = %v, want false", gotBody["stream"])
	}
	if gotBody["model"] != "qwen-max" {
		t.Errorf("body model = %v, want qwen-max", gotBody["model"])
	}
	if resp.Usage == nil || resp.Usage.PromptTokens != 10 {
		t.Errorf("usage = %+v", resp.Usage)
	}
}

// TestQwenExecutor_Execute_DefaultURL verifies the upstream default base URL.
func TestQwenExecutor_Execute_DefaultURL(t *testing.T) {
	e := NewQwenExecutor(roundTripperFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.String() != "https://dashscope.aliyuncs.com/compatible-endpoint/v1/chat/completions" {
			t.Errorf("URL = %q", r.URL.String())
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{},
			Body:       io.NopCloser(strings.NewReader(standardChatResponse)),
			Request:    r,
		}, nil
	}))

	req := newTestRequest(engine.FormatGemini, "qwen-max", variantsJSONBody(t, map[string]interface{}{"model": "qwen-max"}))
	if _, err := e.Execute(context.Background(), req, newTestAccount("k")); err != nil {
		t.Fatalf("Execute: %v", err)
	}
}

// TestQwenExecutor_ExecuteStream_Chunks verifies Qwen streaming relays the
// exact SSE chunk sequence.
func TestQwenExecutor_ExecuteStream_Chunks(t *testing.T) {
	var gotAccept string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAccept = r.Header.Get("Accept")
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, sseChatBody)
	}))
	defer srv.Close()

	e := NewQwenExecutor(srv.Client().Transport)
	req := newTestRequest(engine.FormatGemini, "qwen-max", variantsJSONBody(t, map[string]interface{}{"model": "qwen-max"}))
	req.Headers = map[string]string{"X-Base-URL": srv.URL}

	resp, err := e.ExecuteStream(context.Background(), req, newTestAccount("k"))
	if err != nil {
		t.Fatalf("ExecuteStream: %v", err)
	}
	if gotAccept != "text/event-stream" {
		t.Errorf("Accept = %q, want text/event-stream", gotAccept)
	}
	payloads := chunkPayloads(collectStreamChunks(t, resp.Stream.(*stream.Stream)))
	if len(payloads) != 2 {
		t.Fatalf("chunk payloads = %d, want 2: %v", len(payloads), payloads)
	}
}

// TestQwenExecutor_Execute_Error401 verifies error mapping.
func TestQwenExecutor_Execute_Error401(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = io.WriteString(w, `{"error":"bad key"}`)
	}))
	defer srv.Close()

	e := NewQwenExecutor(srv.Client().Transport)
	req := newTestRequest(engine.FormatGemini, "qwen-max", variantsJSONBody(t, map[string]interface{}{"model": "qwen-max"}))
	req.Headers = map[string]string{"X-Base-URL": srv.URL}

	_, err := e.Execute(context.Background(), req, newTestAccount("bad"))
	assertAppError(t, err, shared.ErrUnauthorized, http.StatusUnauthorized)
}

// ---------------------------------------------------------------------------
// CommandCode / Xiaomi / Mimo / CodeBuddy — table-driven OpenAI-variant tests
// ---------------------------------------------------------------------------

// variantCase describes one OpenAI-variant executor's wire contract.
type variantCase struct {
	name         string
	newExecutor  func(transport http.RoundTripper) engine.Executor
	defaultURL   string
	withAuth     bool
	supports     []engine.RequestFormat
	providerType provider.ProviderType
	respModel    string
	parsesUsage  bool
}

func openAIVariantCases() []variantCase {
	return []variantCase{
		{
			name:         "commandcode",
			newExecutor:  func(t http.RoundTripper) engine.Executor { return NewCommandCodeExecutor(t) },
			defaultURL:   "https://api.commandcode.ai/v1/chat/completions",
			withAuth:     true,
			supports:     []engine.RequestFormat{engine.FormatGemini},
			providerType: provider.ProviderCommandcode,
			respModel:    "gpt-4",
			parsesUsage:  true,
		},
		{
			name:         "xiaomi",
			newExecutor:  func(t http.RoundTripper) engine.Executor { return NewXiaomiTokenplanExecutor(t) },
			defaultURL:   "https://api.minimaxi.com/v1/chat/completions",
			withAuth:     true,
			supports:     []engine.RequestFormat{engine.FormatGemini},
			providerType: provider.ProviderXiaomiTokenplan,
			respModel:    "gpt-4",
			parsesUsage:  true,
		},
		{
			name:         "mimo",
			newExecutor:  func(t http.RoundTripper) engine.Executor { return NewMimoFreeExecutor(t) },
			defaultURL:   "https://api.mimo.ai/v1/chat/completions",
			withAuth:     false,
			supports:     []engine.RequestFormat{engine.FormatGemini},
			providerType: provider.ProviderMimoFree,
			respModel:    "gpt-4",
			parsesUsage:  true,
		},
		{
			name:         "codebuddy",
			newExecutor:  func(t http.RoundTripper) engine.Executor { return NewCodeBuddyExecutor(t) },
			defaultURL:   "https://api.codebuddy.ai/v1/chat/completions",
			withAuth:     true,
			supports:     []engine.RequestFormat{engine.FormatGemini},
			providerType: provider.ProviderCodebuddyCn,
			respModel:    "gpt-4",
			parsesUsage:  true,
		},
	}
}

// TestOpenAIVariantExecutor_Execute exercises the exact URL path, auth header,
// stream:false body injection, model injection, response parse and usage
// extraction for each OpenAI-variant executor.
func TestOpenAIVariantExecutor_Execute(t *testing.T) {
	for _, tc := range openAIVariantCases() {
		t.Run(tc.name, func(t *testing.T) {
			var gotPath, gotAuth string
			var gotBody map[string]interface{}
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				gotPath = r.URL.Path
				gotAuth = r.Header.Get("Authorization")
				_ = json.NewDecoder(r.Body).Decode(&gotBody)
				_, _ = io.WriteString(w, standardChatResponse)
			}))
			defer srv.Close()

			exec := tc.newExecutor(srv.Client().Transport)
			req := newTestRequest(engine.FormatOpenAICompat, "model-x", variantsJSONBody(t, map[string]interface{}{
				"messages": []map[string]interface{}{{"role": "user", "content": "hi"}},
			}))
			req.Headers = map[string]string{"X-Base-URL": srv.URL}

			resp, err := exec.Execute(context.Background(), req, newTestAccount("variant-key"))
			if err != nil {
				t.Fatalf("Execute: %v", err)
			}
			if gotPath != "/v1/chat/completions" {
				t.Errorf("path = %q, want /v1/chat/completions", gotPath)
			}
			if tc.withAuth {
				if gotAuth != "Bearer variant-key" {
					t.Errorf("Authorization = %q, want Bearer variant-key", gotAuth)
				}
			} else if gotAuth != "" {
				t.Errorf("Authorization = %q, want empty for auth-less variant", gotAuth)
			}
			if gotBody["stream"] != false {
				t.Errorf("body stream = %v, want false", gotBody["stream"])
			}
			if gotBody["model"] != "model-x" {
				t.Errorf("body model = %v, want model-x", gotBody["model"])
			}
			if resp.StatusCode != http.StatusOK {
				t.Errorf("status = %d", resp.StatusCode)
			}
			if tc.parsesUsage {
				if resp.Usage == nil || resp.Usage.TotalTokens != 30 {
					t.Errorf("usage = %+v, want total 30", resp.Usage)
				}
			} else if resp.Usage != nil {
				t.Errorf("usage = %+v, want nil for manual response building", resp.Usage)
			}
			if resp.Model != tc.respModel {
				t.Errorf("model = %q, want %q", resp.Model, tc.respModel)
			}
		})
	}
}

// TestOpenAIVariantExecutor_Execute_PayloadModelWins verifies an explicit
// payload model is never overridden.
func TestOpenAIVariantExecutor_Execute_PayloadModelWins(t *testing.T) {
	for _, tc := range openAIVariantCases() {
		t.Run(tc.name, func(t *testing.T) {
			var gotBody map[string]interface{}
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_ = json.NewDecoder(r.Body).Decode(&gotBody)
				_, _ = io.WriteString(w, standardChatResponse)
			}))
			defer srv.Close()

			exec := tc.newExecutor(srv.Client().Transport)
			req := newTestRequest(engine.FormatOpenAIChat, "req-model", variantsJSONBody(t, map[string]interface{}{
				"model":    "payload-model",
				"messages": []map[string]interface{}{},
			}))
			req.Headers = map[string]string{"X-Base-URL": srv.URL}

			if _, err := exec.Execute(context.Background(), req, newTestAccount("k")); err != nil {
				t.Fatalf("Execute: %v", err)
			}
			if gotBody["model"] != "payload-model" {
				t.Errorf("body model = %v, want payload-model", gotBody["model"])
			}
		})
	}
}

// TestOpenAIVariantExecutor_DefaultURL verifies each variant's default base
// URL when no X-Base-URL is supplied.
func TestOpenAIVariantExecutor_DefaultURL(t *testing.T) {
	for _, tc := range openAIVariantCases() {
		t.Run(tc.name, func(t *testing.T) {
			exec := tc.newExecutor(roundTripperFunc(func(r *http.Request) (*http.Response, error) {
				if r.URL.String() != tc.defaultURL {
					t.Errorf("URL = %q, want %q", r.URL.String(), tc.defaultURL)
				}
				return &http.Response{
					StatusCode: http.StatusOK,
					Header:     http.Header{},
					Body:       io.NopCloser(strings.NewReader(standardChatResponse)),
					Request:    r,
				}, nil
			}))
			req := newTestRequest(engine.FormatOpenAICompat, "model-x", variantsJSONBody(t, map[string]interface{}{"model": "model-x"}))
			if _, err := exec.Execute(context.Background(), req, newTestAccount("k")); err != nil {
				t.Fatalf("Execute: %v", err)
			}
		})
	}
}

// TestOpenAIVariantExecutor_ExecuteStream exercises the streaming path: Accept
// header, stream:true injection and exact chunk sequence.
func TestOpenAIVariantExecutor_ExecuteStream(t *testing.T) {
	for _, tc := range openAIVariantCases() {
		t.Run(tc.name, func(t *testing.T) {
			var gotAccept string
			var gotBody map[string]interface{}
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				gotAccept = r.Header.Get("Accept")
				_ = json.NewDecoder(r.Body).Decode(&gotBody)
				w.Header().Set("Content-Type", "text/event-stream")
				_, _ = io.WriteString(w, sseChatBody)
			}))
			defer srv.Close()

			exec := tc.newExecutor(srv.Client().Transport)
			req := newTestRequest(engine.FormatOpenAICompat, "model-x", variantsJSONBody(t, map[string]interface{}{
				"model":    "model-x",
				"messages": []map[string]interface{}{},
			}))
			req.Headers = map[string]string{"X-Base-URL": srv.URL}

			resp, err := exec.ExecuteStream(context.Background(), req, newTestAccount("k"))
			if err != nil {
				t.Fatalf("ExecuteStream: %v", err)
			}
			if gotAccept != "text/event-stream" {
				t.Errorf("Accept = %q, want text/event-stream", gotAccept)
			}
			if gotBody["stream"] != true {
				t.Errorf("body stream = %v, want true", gotBody["stream"])
			}
			st := resp.Stream.(*stream.Stream)
			payloads := chunkPayloads(collectStreamChunks(t, st))
			if len(payloads) != 2 {
				t.Fatalf("chunk payloads = %d, want 2: %v", len(payloads), payloads)
			}
			if payloads[0] != `{"choices":[{"delta":{"content":"Hello"}}]}` {
				t.Errorf("chunk[0] = %q", payloads[0])
			}
		})
	}
}

// TestOpenAIVariantExecutor_ErrorMapping covers 401/429/500 for each variant.
func TestOpenAIVariantExecutor_ErrorMapping(t *testing.T) {
	statuses := []struct {
		status   int
		wantCode shared.ErrorCode
	}{
		{http.StatusUnauthorized, shared.ErrUnauthorized},
		{http.StatusForbidden, shared.ErrUnauthorized},
		{http.StatusTooManyRequests, shared.ErrRateLimited},
		{http.StatusInternalServerError, shared.ErrInternal},
	}
	for _, tc := range openAIVariantCases() {
		for _, s := range statuses {
			t.Run(tc.name+"_"+string(s.wantCode), func(t *testing.T) {
				srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.WriteHeader(s.status)
					_, _ = io.WriteString(w, `{"error":"boom"}`)
				}))
				defer srv.Close()

				exec := tc.newExecutor(srv.Client().Transport)
				req := newTestRequest(engine.FormatOpenAIChat, "model-x", variantsJSONBody(t, map[string]interface{}{"model": "model-x"}))
				req.Headers = map[string]string{"X-Base-URL": srv.URL}

				_, err := exec.Execute(context.Background(), req, newTestAccount("k"))
				assertAppError(t, err, s.wantCode, s.status)
			})
		}
	}
}

// TestOpenAIVariantExecutor_SupportsFormat pins the supported format set.
func TestOpenAIVariantExecutor_SupportsFormat(t *testing.T) {
	allFormats := []engine.RequestFormat{
		engine.FormatOpenAIChat, engine.FormatOpenAICompat,
		engine.FormatCodexResponses, engine.FormatAnthropic, engine.FormatGemini,
	}
	for _, tc := range openAIVariantCases() {
		t.Run(tc.name, func(t *testing.T) {
			exec := tc.newExecutor(http.DefaultTransport)
			for _, f := range allFormats {
				got := exec.SupportsFormat(f)
				want := containsFormat(tc.supports, f)
				if got != want {
					t.Errorf("SupportsFormat(%s) = %v, want %v", f, got, want)
				}
			}
			if exec.ProviderType() != tc.providerType {
				t.Errorf("ProviderType = %s, want %s", exec.ProviderType(), tc.providerType)
			}
		})
	}
}

func containsFormat(list []engine.RequestFormat, f engine.RequestFormat) bool {
	for _, x := range list {
		if x == f {
			return true
		}
	}
	return false
}

// roundTripperFunc adapts a func to http.RoundTripper.
type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}

// ---------------------------------------------------------------------------
// CodexExecutor — peek / [DONE] / model / error / header branches
// ---------------------------------------------------------------------------

// codexStreamSSE is a two-event Codex Responses SSE stream.
const codexStreamSSE = "event: response.output_text.delta\n" +
	"data: {\"type\":\"response.output_text.delta\",\"delta\":\"hello\"}\n\n" +
	"event: response.completed\n" +
	"data: {\"type\":\"response.completed\"}\n\n"

// codexSSEServer starts a server that always answers /v1/responses with the
// given SSE body.
func codexSSEServer(t *testing.T, body string) (*httptest.Server, func() map[string]interface{}) {
	t.Helper()
	var lastBody map[string]interface{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&lastBody)
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(srv.Close)
	return srv, func() map[string]interface{} { return lastBody }
}

// TestCodexExecutor_ExecuteStream_SingleLineEventsExactChunks is the GREEN
// baseline for the peek path: single-data-line events must surface as exact
// chunks with their event types, and nothing may be duplicated or dropped.
func TestCodexExecutor_ExecuteStream_SingleLineEventsExactChunks(t *testing.T) {
	srv, _ := codexSSEServer(t, codexStreamSSE)

	e := NewCodexExecutor(srv.Client().Transport)
	e.SetBaseURL(srv.URL)

	resp, err := e.ExecuteStream(context.Background(), codexTestRequest(), codexTestAccount())
	if err != nil {
		t.Fatalf("ExecuteStream: %v", err)
	}
	st := resp.Stream.(*stream.Stream)
	chunks := collectStreamChunks(t, st)

	if len(chunks) != 3 {
		t.Fatalf("chunks = %d, want 3 (2 data + 1 final): %+v", len(chunks), chunks)
	}
	if got := string(chunks[0].Data); got != `{"type":"response.output_text.delta","delta":"hello"}` {
		t.Errorf("chunk[0] data = %q", got)
	}
	if chunks[0].Event != "response.output_text.delta" {
		t.Errorf("chunk[0] event = %q", chunks[0].Event)
	}
	if got := string(chunks[1].Data); got != `{"type":"response.completed"}` {
		t.Errorf("chunk[1] data = %q", got)
	}
	if chunks[1].Event != "response.completed" {
		t.Errorf("chunk[1] event = %q", chunks[1].Event)
	}
	if !chunks[2].IsFinal {
		t.Error("chunk[2] must be IsFinal")
	}
}

// TestCodexExecutor_ExecuteStream_MultiLineFirstEventPreserved is a DEFECT
// PROBE for the peek buffer arithmetic. An SSE event that spans several
// data: lines must arrive as one chunk per data line, each labelled with the
// first event's type — no line may be dropped, mangled, or duplicated.
//
// The current peekFirstEventFromReader computes the discard offset with
// bytes.Index + len(data) + 2, which consumes the trailing bytes of the first
// data line's newline plus one extra byte, corrupting the remaining lines of
// the same event (a "data: ..." continuation becomes "ata: ..." and is
// silently dropped by streamSSE). This test is expected to FAIL (RED).
func TestCodexExecutor_ExecuteStream_MultiLineFirstEventPreserved(t *testing.T) {
	body := "event: response.output_text.delta\n" +
		"data: {\"type\":\"response.output_text.delta\",\"delta\":\"Hello\"}\n" +
		"data: {\"type\":\"response.output_text.delta\",\"delta\":\" world\"}\n\n" +
		"event: response.completed\n" +
		"data: {\"type\":\"response.completed\"}\n\n"
	srv, _ := codexSSEServer(t, body)

	e := NewCodexExecutor(srv.Client().Transport)
	e.SetBaseURL(srv.URL)

	resp, err := e.ExecuteStream(context.Background(), codexTestRequest(), codexTestAccount())
	if err != nil {
		t.Fatalf("ExecuteStream: %v", err)
	}
	st := resp.Stream.(*stream.Stream)
	chunks := collectStreamChunks(t, st)

	// Contract: 3 data chunks (two for the first multi-line event, one for the
	// completed event) plus the final marker.
	if len(chunks) != 4 {
		t.Fatalf("DEFECT: chunks = %d, want 4 (3 data + 1 final); got %+v", len(chunks), chunks)
	}
	if got := string(chunks[0].Data); got != `{"type":"response.output_text.delta","delta":"Hello"}` {
		t.Errorf("chunk[0] data = %q", got)
	}
	if got := string(chunks[1].Data); got != `{"type":"response.output_text.delta","delta":" world"}` {
		t.Errorf("DEFECT: chunk[1] data = %q, want the second data line of the first event", got)
	}
	if chunks[1].Event != "response.output_text.delta" {
		t.Errorf("DEFECT: chunk[1] event = %q, want response.output_text.delta (event type lost after peek discard)", chunks[1].Event)
	}
	if got := string(chunks[2].Data); got != `{"type":"response.completed"}` {
		t.Errorf("chunk[2] data = %q", got)
	}
	if !chunks[3].IsFinal {
		t.Error("chunk[3] must be IsFinal")
	}
}

// TestCodexExecutor_ExecuteStream_NoDuplicateFirstEventData is a DEFECT PROBE
// for duplication: when the first event carries two identical data lines,
// each must surface exactly once. The current discard arithmetic mangles the
// second line so the payload is emitted once instead of twice — the chunk
// count and payload list prove the loss. Expected RED.
func TestCodexExecutor_ExecuteStream_NoDuplicateFirstEventData(t *testing.T) {
	payload := `{"type":"response.output_text.delta","delta":"hello"}`
	body := "event: response.output_text.delta\n" +
		"data: " + payload + "\n" +
		"data: " + payload + "\n\n" +
		"data: [DONE]\n\n"
	srv, _ := codexSSEServer(t, body)

	e := NewCodexExecutor(srv.Client().Transport)
	e.SetBaseURL(srv.URL)

	resp, err := e.ExecuteStream(context.Background(), codexTestRequest(), codexTestAccount())
	if err != nil {
		t.Fatalf("ExecuteStream: %v", err)
	}
	st := resp.Stream.(*stream.Stream)
	chunks := collectStreamChunks(t, st)

	got := chunkPayloads(chunks)
	if len(got) != 2 {
		t.Fatalf("DEFECT: data payloads = %d, want exactly 2 (one per data line); got %v", len(got), got)
	}
	for i, p := range got {
		if p != payload {
			t.Errorf("DEFECT: payload[%d] = %q, want %q", i, p, payload)
		}
	}
	for _, c := range dataChunks(chunks) {
		if c.Event != "response.output_text.delta" {
			t.Errorf("DEFECT: chunk event = %q, want response.output_text.delta", c.Event)
		}
	}
}

// TestCodexExecutor_ExecuteStream_FirstEventDone verifies the [DONE] branch:
// a stream whose first event is [DONE] is closed immediately with zero data
// chunks and no error.
func TestCodexExecutor_ExecuteStream_FirstEventDone(t *testing.T) {
	srv, _ := codexSSEServer(t, "data: [DONE]\n\n")

	e := NewCodexExecutor(srv.Client().Transport)
	e.SetBaseURL(srv.URL)

	resp, err := e.ExecuteStream(context.Background(), codexTestRequest(), codexTestAccount())
	if err != nil {
		t.Fatalf("ExecuteStream: %v", err)
	}
	st := resp.Stream.(*stream.Stream)
	if st.State() != stream.StateDone {
		t.Errorf("stream state = %v, want StateDone", st.State())
	}
	chunks := collectStreamChunks(t, st)
	if len(chunks) != 0 {
		t.Errorf("chunks = %d, want 0 for [DONE]-first stream: %+v", len(chunks), chunks)
	}
}

// TestCodexExecutor_ExecuteStream_DoneLaterInStream verifies [DONE] appearing
// after data events terminates the stream with an IsFinal chunk.
func TestCodexExecutor_ExecuteStream_DoneLaterInStream(t *testing.T) {
	body := "data: {\"type\":\"response.output_text.delta\",\"delta\":\"a\"}\n\n" +
		"data: [DONE]\n\n"
	srv, _ := codexSSEServer(t, body)

	e := NewCodexExecutor(srv.Client().Transport)
	e.SetBaseURL(srv.URL)

	resp, err := e.ExecuteStream(context.Background(), codexTestRequest(), codexTestAccount())
	if err != nil {
		t.Fatalf("ExecuteStream: %v", err)
	}
	chunks := collectStreamChunks(t, resp.Stream.(*stream.Stream))
	payloads := chunkPayloads(chunks)
	if len(payloads) != 1 || payloads[0] != `{"type":"response.output_text.delta","delta":"a"}` {
		t.Errorf("payloads = %v", payloads)
	}
	if len(chunks) != 2 || !chunks[1].IsFinal {
		t.Errorf("chunks = %+v, want 1 data + 1 IsFinal", chunks)
	}
}

// TestCodexExecutor_ExecuteStream_SSEErrorPatterns verifies each upstream SSE
// error pattern surfaces as an error before any stream is returned.
func TestCodexExecutor_ExecuteStream_SSEErrorPatterns(t *testing.T) {
	tests := []struct {
		name        string
		event       string
		wantMessage string
	}{
		{"server_is_overloaded", `{"error":{"type":"server_is_overloaded","message":"Server is overloaded"}}`, "Server is overloaded"},
		{"service_unavailable_error", `{"error":{"type":"service_unavailable_error","message":"Service temporarily unavailable"}}`, "Service temporarily unavailable"},
		{"model_at_capacity", `{"error":{"message":"model_at_capacity"}}`, "model_at_capacity"},
		{"capacity_text", `{"error":{"message":"Selected model is at capacity. Please try a different model."}}`, "Selected model is at capacity. Please try a different model."},
		{"uppercase_pattern", `{"error":{"type":"SERVER_IS_OVERLOADED","message":"down"}}`, "down"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv, _ := codexSSEServer(t, "event: error\ndata: "+tt.event+"\n\n")
			e := NewCodexExecutor(srv.Client().Transport)
			e.SetBaseURL(srv.URL)

			_, err := e.ExecuteStream(context.Background(), codexTestRequest(), codexTestAccount())
			if err == nil {
				t.Fatal("expected SSE error, got nil")
			}
			if !strings.Contains(err.Error(), tt.wantMessage) {
				t.Errorf("error = %q, want it to contain %q", err.Error(), tt.wantMessage)
			}
		})
	}
}

// TestCodexExecutor_ExecuteStream_NonMatchingFirstEventNoError verifies a
// benign first event (e.g. response.created) does not trigger the SSE error
// branch and the stream is delivered.
func TestCodexExecutor_ExecuteStream_NonMatchingFirstEventNoError(t *testing.T) {
	body := "event: response.created\n" +
		"data: {\"type\":\"response.created\",\"response\":{\"id\":\"resp_1\"}}\n\n" +
		"data: [DONE]\n\n"
	srv, _ := codexSSEServer(t, body)

	e := NewCodexExecutor(srv.Client().Transport)
	e.SetBaseURL(srv.URL)

	resp, err := e.ExecuteStream(context.Background(), codexTestRequest(), codexTestAccount())
	if err != nil {
		t.Fatalf("ExecuteStream: %v", err)
	}
	payloads := chunkPayloads(collectStreamChunks(t, resp.Stream.(*stream.Stream)))
	if len(payloads) != 1 {
		t.Errorf("payloads = %v, want 1", payloads)
	}
}

// TestCodexExecutor_ExecuteStream_HTTPErrors maps HTTP error statuses to
// AppError codes without creating a stream.
func TestCodexExecutor_ExecuteStream_HTTPErrors(t *testing.T) {
	tests := []struct {
		name     string
		status   int
		wantCode shared.ErrorCode
	}{
		{"unauthorized", http.StatusUnauthorized, shared.ErrUnauthorized},
		{"forbidden", http.StatusForbidden, shared.ErrUnauthorized},
		{"rate_limited", http.StatusTooManyRequests, shared.ErrRateLimited},
		{"server_error", http.StatusInternalServerError, shared.ErrInternal},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tt.status)
				_, _ = io.WriteString(w, `{"error":{"message":"boom"}}`)
			}))
			defer srv.Close()

			e := NewCodexExecutor(srv.Client().Transport)
			e.SetBaseURL(srv.URL)

			resp, err := e.ExecuteStream(context.Background(), codexTestRequest(), codexTestAccount())
			if resp != nil && resp.Stream != nil {
				t.Error("no stream must be returned on HTTP error")
			}
			assertAppError(t, err, tt.wantCode, tt.status)
		})
	}
}

// TestCodexExecutor_ExecuteStream_EmptyBody verifies an empty SSE body yields
// a stream with zero data chunks that terminates in an error state rather than
// silently returning data.
func TestCodexExecutor_ExecuteStream_EmptyBody(t *testing.T) {
	srv, _ := codexSSEServer(t, "")

	e := NewCodexExecutor(srv.Client().Transport)
	e.SetBaseURL(srv.URL)

	resp, err := e.ExecuteStream(context.Background(), codexTestRequest(), codexTestAccount())
	if err != nil {
		t.Fatalf("ExecuteStream: %v", err)
	}
	st := resp.Stream.(*stream.Stream)
	chunks := collectStreamChunks(t, st)
	if got := dataChunks(chunks); len(got) != 0 {
		t.Errorf("data chunks = %d, want 0 for empty SSE body: %+v", len(got), got)
	}
	if st.State() != stream.StateCancelled && st.State() != stream.StateErrored && st.State() != stream.StateDone {
		t.Errorf("stream state = %v, want a terminal state for empty body", st.State())
	}
}

// TestCodexExecutor_ExecuteStream_HeaderBranches pins every outgoing header:
// Authorization, session_id from the account, originator, Accept, and the
// request-header passthrough with the reserved-key skip list.
func TestCodexExecutor_ExecuteStream_HeaderBranches(t *testing.T) {
	var got map[string]string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = map[string]string{
			"Authorization": r.Header.Get("Authorization"),
			"Content-Type":  r.Header.Get("Content-Type"),
			"Accept":        r.Header.Get("Accept"),
			"session_id":    r.Header.Get("session_id"),
			"originator":    r.Header.Get("originator"),
			"X-Custom":      r.Header.Get("X-Custom"),
			"X-Override":    r.Header.Get("X-Override"),
			"X-Base-URL":    r.Header.Get("X-Base-URL"),
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: {\"ok\":true}\n\ndata: [DONE]\n\n")
	}))
	defer srv.Close()

	e := NewCodexExecutor(srv.Client().Transport)
	e.SetBaseURL(srv.URL)

	req := codexTestRequest()
	req.Headers = map[string]string{
		"X-Custom":   "custom-value",
		"X-Override": "should-not-win",
		// Decision #376: client-supplied base URLs are never honored; the value
		// must be forwarded as a literal header only.
		"X-Base-URL":    "https://attacker.example.com",
		"Authorization": "Bearer attacker-key",
		"session_id":    "attacker-session",
		"originator":    "attacker-origin",
	}

	if _, err := e.ExecuteStream(context.Background(), req, codexTestAccount()); err != nil {
		t.Fatalf("ExecuteStream: %v", err)
	}
	if got["Authorization"] != "Bearer test-key" {
		t.Errorf("Authorization = %q, want Bearer test-key (reserved key not overridable)", got["Authorization"])
	}
	if got["session_id"] != codexTestAccount().ID.String() {
		t.Errorf("session_id = %q, want account ID", got["session_id"])
	}
	if got["originator"] != "gorouter" {
		t.Errorf("originator = %q, want gorouter", got["originator"])
	}
	if got["Accept"] != "text/event-stream" {
		t.Errorf("Accept = %q, want text/event-stream", got["Accept"])
	}
	if got["Content-Type"] != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", got["Content-Type"])
	}
	if got["X-Custom"] != "custom-value" {
		t.Errorf("X-Custom = %q, want custom-value", got["X-Custom"])
	}
	if got["X-Override"] != "should-not-win" {
		t.Errorf("X-Override = %q, want should-not-win (non-reserved key passed through)", got["X-Override"])
	}
	if got["X-Base-URL"] != "https://attacker.example.com" {
		t.Errorf("X-Base-URL forwarded as literal header = %q", got["X-Base-URL"])
	}
}

// TestCodexExecutor_ExecuteStream_HeaderBranches_NoAccount verifies default
// session_id and empty Authorization when no account is present.
func TestCodexExecutor_ExecuteStream_HeaderBranches_NoAccount(t *testing.T) {
	var gotSession, gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotSession = r.Header.Get("session_id")
		gotAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: [DONE]\n\n")
	}))
	defer srv.Close()

	e := NewCodexExecutor(srv.Client().Transport)
	e.SetBaseURL(srv.URL)

	if _, err := e.ExecuteStream(context.Background(), codexTestRequest(), nil); err != nil {
		t.Fatalf("ExecuteStream: %v", err)
	}
	if gotSession != "default" {
		t.Errorf("session_id = %q, want default", gotSession)
	}
	// The Go HTTP transport trims trailing whitespace from header values, so a
	// nil account yields exactly "Bearer".
	if gotAuth != "Bearer" {
		t.Errorf("Authorization = %q, want Bearer", gotAuth)
	}
}

// TestCodexExecutor_Execute_ModelResolution covers the model branches: default
// gpt-4o maps to gpt-4o-codex, payload models are preserved, and response
// Model reflects the resolution.
func TestCodexExecutor_Execute_ModelResolution(t *testing.T) {
	tests := []struct {
		name        string
		model       string
		payload     map[string]interface{}
		wantPayload string
		wantRespMod string
	}{
		{"default_gpt4o_maps_to_codex", "gpt-4o", map[string]interface{}{"input": "hi"}, "gpt-4o-codex", "gpt-4o-codex"},
		{"payload_model_preserved", "gpt-5-codex", map[string]interface{}{"model": "gpt-5.1-codex", "input": "hi"}, "gpt-5.1-codex", "gpt-5-codex"},
		// DEFECT PROBE: transformRequest never consults req.Model — when the
		// payload omits "model" the outgoing model falls back to the executor's
		// empty model field ("gpt-4o-codex") instead of req.Model ("gpt-5-codex").
		// Upstream codex.js uses body.model || model. Expected RED.
		{"req_model_injected_when_missing", "gpt-5-codex", map[string]interface{}{"input": "hi"}, "gpt-5-codex", "gpt-5-codex"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var gotModel string
			var gotBody map[string]interface{}
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_ = json.NewDecoder(r.Body).Decode(&gotBody)
				gotModel, _ = gotBody["model"].(string)
				resp := fmt.Sprintf(`{"id":"resp_1","object":"response","model":%q}`, gotModel)
				w.Header().Set("Content-Type", "application/json")
				_, _ = io.WriteString(w, resp)
			}))
			defer srv.Close()

			e := NewCodexExecutor(srv.Client().Transport)
			e.SetBaseURL(srv.URL)

			req := &engine.Request{
				ID:         codexTestRequest().ID,
				Model:      tt.model,
				Format:     engine.FormatCodexResponses,
				RawBody:    variantsJSONBody(t, tt.payload),
				MappedBody: variantsJSONBody(t, tt.payload),
			}
			resp, err := e.Execute(context.Background(), req, codexTestAccount())
			if err != nil {
				t.Fatalf("Execute: %v", err)
			}
			if gotModel != tt.wantPayload {
				t.Errorf("outgoing model = %q, want %q", gotModel, tt.wantPayload)
			}
			if resp.Model != tt.wantRespMod {
				t.Errorf("response model = %q, want %q", resp.Model, tt.wantRespMod)
			}
			if resp.Usage != nil {
				t.Errorf("usage = %+v, want nil for non-usage response", resp.Usage)
			}
		})
	}
}

// TestCodexExecutor_Execute_RequestBodyBranches verifies stream/store
// injection and default instructions.
func TestCodexExecutor_Execute_RequestBodyBranches(t *testing.T) {
	var gotBody map[string]interface{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"id":"resp_1","model":"gpt-5-codex"}`)
	}))
	defer srv.Close()

	e := NewCodexExecutor(srv.Client().Transport)
	e.SetBaseURL(srv.URL)

	req := codexTestRequest()
	if _, err := e.Execute(context.Background(), req, codexTestAccount()); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if gotBody["stream"] != false {
		t.Errorf("stream = %v, want false", gotBody["stream"])
	}
	if gotBody["store"] != false {
		t.Errorf("store = %v, want false", gotBody["store"])
	}
	if gotBody["instructions"] != "You are a helpful assistant." {
		t.Errorf("instructions = %v", gotBody["instructions"])
	}
	if _, ok := gotBody["previous_response_id"]; ok {
		t.Error("previous_response_id must be stripped")
	}
}

// TestCodexExecutor_peekFirstEventFromReader unit-tests the peek branch on
// single-line events, [DONE] and events without an event: label, verifying
// the reader is left positioned for streamSSE to continue.
func TestCodexExecutor_peekFirstEventFromReader(t *testing.T) {
	e := NewCodexExecutor(nil)
	tests := []struct {
		name      string
		input     string
		wantEvent string
		wantData  string
	}{
		{"single_event", "event: a\ndata: X\n\n", "a", "X"},
		{"done_only", "data: [DONE]\n\n", "", "[DONE]"},
		{"no_event_label", "data: X\n\ndata: Y\n\n", "", "X"},
		{"trailing_without_blank", "event: a\ndata: X\n", "a", "X"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := bufio.NewReaderSize(strings.NewReader(tt.input), 64*1024)
			ev, data, err := e.peekFirstEventFromReader(r)
			if err != nil {
				t.Fatalf("peekFirstEventFromReader: %v", err)
			}
			if ev != tt.wantEvent {
				t.Errorf("event = %q, want %q", ev, tt.wantEvent)
			}
			if data != tt.wantData {
				t.Errorf("data = %q, want %q", data, tt.wantData)
			}
		})
	}
}

// TestIsCodexSSEError pins the error-pattern classifier (case-insensitive).
func TestIsCodexSSEError(t *testing.T) {
	tests := []struct {
		data string
		want bool
	}{
		{`{"error":{"type":"server_is_overloaded"}}`, true},
		{`{"error":{"type":"service_unavailable_error"}}`, true},
		{`{"error":{"message":"Selected model is at capacity"}}`, true},
		{`{"error":{"message":"model_at_capacity"}}`, true},
		{`{"error":{"type":"SERVER_IS_OVERLOADED"}}`, true},
		{`{"type":"response.output_text.delta"}`, false},
		{`{"type":"response.completed"}`, false},
		{"", false},
	}
	for _, tt := range tests {
		if got := isCodexSSEError(tt.data); got != tt.want {
			t.Errorf("isCodexSSEError(%q) = %v, want %v", tt.data, got, tt.want)
		}
	}
}

// TestExtractCodexErrorMessage pins message extraction and the fallback text.
func TestExtractCodexErrorMessage(t *testing.T) {
	if got := extractCodexErrorMessage(`{"error":{"message":"Selected model is at capacity. Please try a different model."}}`); got != "Selected model is at capacity. Please try a different model." {
		t.Errorf("extract = %q", got)
	}
	if got := extractCodexErrorMessage(`{"error":{"type":"server_is_overloaded"}}`); got != "upstream SSE error" {
		t.Errorf("fallback extract = %q", got)
	}
	if got := extractCodexErrorMessage("not json"); got != "upstream SSE error" {
		t.Errorf("non-json extract = %q", got)
	}
}

// TestCodexExecutor_ExecuteStream_StreamStateTerminal verifies a completed
// stream ends in StateDone after all chunks are consumed.
func TestCodexExecutor_ExecuteStream_StreamStateTerminal(t *testing.T) {
	srv, _ := codexSSEServer(t, codexStreamSSE)

	e := NewCodexExecutor(srv.Client().Transport)
	e.SetBaseURL(srv.URL)

	resp, err := e.ExecuteStream(context.Background(), codexTestRequest(), codexTestAccount())
	if err != nil {
		t.Fatalf("ExecuteStream: %v", err)
	}
	st := resp.Stream.(*stream.Stream)
	_ = collectStreamChunks(t, st)
	if st.State() != stream.StateDone {
		t.Errorf("stream state = %v, want StateDone", st.State())
	}
}

// blockingSSEBody serves up to its whole buffer, then blocks until Close.
// It models a live SSE connection that stalls mid-stream.
type blockingSSEBody struct {
	mu     sync.Mutex
	data   []byte
	done   chan struct{}
	closed bool
}

func newBlockingSSEBody(data []byte) *blockingSSEBody {
	return &blockingSSEBody{data: data, done: make(chan struct{})}
}

func (b *blockingSSEBody) Read(p []byte) (int, error) {
	b.mu.Lock()
	if len(b.data) > 0 {
		n := copy(p, b.data)
		b.data = b.data[n:]
		b.mu.Unlock()
		return n, nil
	}
	b.mu.Unlock()
	<-b.done
	return 0, io.EOF
}

func (b *blockingSSEBody) Close() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if !b.closed {
		b.closed = true
		close(b.done)
	}
	return nil
}

// TestCodexExecutor_ExecuteStream_ContextCancellation verifies cancelling the
// parent context mid-stream terminates the stream without hanging the caller.
func TestCodexExecutor_ExecuteStream_ContextCancellation(t *testing.T) {
	// The peek reads up to the 64KiB bufio buffer before returning
	// (Peek with a request larger than the buffer returns ErrBufferFull with
	// the buffered bytes), so the body must carry at least that much data
	// before the underlying read blocks.
	payload1 := `{"type":"response.output_text.delta","delta":"` + strings.Repeat("a", 20*1024) + `"}`
	payload2 := `{"type":"response.output_text.delta","delta":"` + strings.Repeat("b", 20*1024) + `"}`
	payload3 := `{"type":"response.output_text.delta","delta":"` + strings.Repeat("c", 30*1024) + `"}`
	body := "event: response.output_text.delta\ndata: " + payload1 + "\n\n" +
		"event: response.output_text.delta\ndata: " + payload2 + "\n\n" +
		"event: response.output_text.delta\ndata: " + payload3 + "\n\n"

	b := newBlockingSSEBody([]byte(body))
	transport := roundTripperFunc(func(r *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
			Body:       b,
			Request:    r,
		}, nil
	})

	e := NewCodexExecutor(transport)
	e.SetBaseURL("http://codex.invalid")

	ctx, cancel := context.WithCancel(context.Background())
	resp, err := e.ExecuteStream(ctx, codexTestRequest(), codexTestAccount())
	if err != nil {
		t.Fatalf("ExecuteStream: %v", err)
	}
	st := resp.Stream.(*stream.Stream)

	// Wait for the first chunk to confirm delivery, then cancel.
	select {
	case <-st.Chunks():
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for first chunk")
	}
	cancel()
	// Closing the body unblocks the stalled upstream read; the stream must
	// reach a terminal state after cancellation.
	select {
	case <-st.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("stream did not terminate after context cancellation")
	}
	if st.State() != stream.StateCancelled && st.State() != stream.StateDone {
		t.Errorf("stream state = %v, want cancelled or done", st.State())
	}
}

// TestCodexExecutor_ExecuteStream_CancelledContext verifies a pre-cancelled
// context fails fast at request time instead of leaking a stream.
func TestCodexExecutor_ExecuteStream_CancelledContext(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: [DONE]\n\n")
	}))
	defer srv.Close()

	e := NewCodexExecutor(srv.Client().Transport)
	e.SetBaseURL(srv.URL)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := e.ExecuteStream(ctx, codexTestRequest(), codexTestAccount())
	if err == nil {
		t.Fatal("expected error for pre-cancelled context")
	}
	if !strings.Contains(err.Error(), "canceled") {
		t.Errorf("error = %q, want context-canceled error", err.Error())
	}
}

// TestCodexExecutor_SupportsFormat_NoOthers ensures FormatCodexResponses is
// the only supported format (already covered for other formats by
// codex_test.go, pinned here for the branch set).
func TestCodexExecutor_SupportsFormat_NoOthers(t *testing.T) {
	e := NewCodexExecutor(nil)
	for _, f := range []engine.RequestFormat{
		engine.FormatOpenAIChat, engine.FormatOpenAICompat,
		engine.FormatAnthropic, engine.FormatGemini,
	} {
		if e.SupportsFormat(f) {
			t.Errorf("unexpected support for %s", f)
		}
	}
	if !e.SupportsFormat(engine.FormatCodexResponses) {
		t.Error("expected FormatCodexResponses support")
	}
}

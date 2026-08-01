package specialized

// missing_executors_test.go — tests for the "missing executor" family:
// DeepSeek, Fireworks, Groq, Mistral, Perplexity, Together and XAI. In the
// upstream 9router these providers have no dedicated executor (they fall back
// to DefaultExecutor); the Go port gives each a specialized OpenAI-compatible
// executor (see internal/engine/providers/specialized/missing_executors.go
// and docs/implementation/provider-matrix.yaml rows specialized/{deepseek,
// fireworks, groq, mistral, perplexity, together, xai}).
//
// Contract source: upstream 9router commit 79918c7830695bbca4a45c9fea4a42c3e9fd73d1
// (open-sse/executors/default.js buildHeaders: `if (stream) headers["Accept"]
// = "text/event-stream"`, line 210).
//
// Defect probe (kept RED on purpose; see defect report):
//   - TestDeepSeekExecutor_ExecuteStream_AcceptHeaderProbe : DeepSeek's
//     buildRequest hard-codes Accept: application/json even for streaming, so
//     ExecuteStream advertises a non-streaming Accept on a stream request.
//
// No production code is modified by this file.

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"gorouter/internal/domain/engine"
	"gorouter/internal/domain/engine/stream"
	"gorouter/internal/domain/provider"
	"gorouter/internal/shared"
)

// missingExecutorCases lists the missing-executor family with the wire
// contract taken from docs/implementation/provider-matrix.yaml.
func missingExecutorCases() []variantCase {
	return []variantCase{
		{
			name:         "deepseek",
			newExecutor:  func(t http.RoundTripper) engine.Executor { return NewDeepSeekExecutor(t) },
			defaultURL:   "https://api.deepseek.com/v1/chat/completions",
			withAuth:     true,
			supports:     []engine.RequestFormat{engine.FormatOpenAIChat, engine.FormatOpenAICompat},
			providerType: provider.ProviderDeepseek,
			respModel:    "gpt-4",
			parsesUsage:  true,
		},
		{
			name:         "fireworks",
			newExecutor:  func(t http.RoundTripper) engine.Executor { return NewFireworksExecutor(t) },
			defaultURL:   "https://api.fireworks.ai/v1/chat/completions",
			withAuth:     true,
			supports:     []engine.RequestFormat{engine.FormatOpenAIChat, engine.FormatOpenAICompat},
			providerType: provider.ProviderFireworks,
			respModel:    "gpt-4",
			parsesUsage:  true,
		},
		{
			name:         "groq",
			newExecutor:  func(t http.RoundTripper) engine.Executor { return NewGroqExecutor(t) },
			defaultURL:   "https://api.groq.com/v1/chat/completions",
			withAuth:     true,
			supports:     []engine.RequestFormat{engine.FormatOpenAIChat, engine.FormatOpenAICompat},
			providerType: provider.ProviderGroq,
			respModel:    "gpt-4",
			parsesUsage:  true,
		},
		{
			name:         "mistral",
			newExecutor:  func(t http.RoundTripper) engine.Executor { return NewMistralExecutor(t) },
			defaultURL:   "https://api.mistral.ai/v1/chat/completions",
			withAuth:     true,
			supports:     []engine.RequestFormat{engine.FormatOpenAIChat, engine.FormatOpenAICompat},
			providerType: provider.ProviderMistral,
			respModel:    "gpt-4",
			parsesUsage:  true,
		},
		{
			name:         "perplexity",
			newExecutor:  func(t http.RoundTripper) engine.Executor { return NewPerplexityExecutor(t) },
			defaultURL:   "https://api.perplexity.ai/v1/chat/completions",
			withAuth:     true,
			supports:     []engine.RequestFormat{engine.FormatOpenAICompat},
			providerType: provider.ProviderPerplexity,
			respModel:    "model-x",
		},
		{
			name:         "together",
			newExecutor:  func(t http.RoundTripper) engine.Executor { return NewTogetherExecutor(t) },
			defaultURL:   "https://api.together.xyz/v1/chat/completions",
			withAuth:     true,
			supports:     []engine.RequestFormat{engine.FormatOpenAIChat, engine.FormatOpenAICompat},
			providerType: provider.ProviderTogether,
			respModel:    "gpt-4",
			parsesUsage:  true,
		},
		{
			name:         "xai",
			newExecutor:  func(t http.RoundTripper) engine.Executor { return NewXAIExecutor(t) },
			defaultURL:   "https://api.x.ai/v1/chat/completions",
			withAuth:     true,
			supports:     []engine.RequestFormat{engine.FormatOpenAICompat},
			providerType: provider.ProviderXai,
			respModel:    "gpt-4",
			parsesUsage:  true,
		},
	}
}

// ---------------------------------------------------------------------------
// GetExecutor mapping for the missing-executor family
// ---------------------------------------------------------------------------

// TestGetExecutor_MissingFamilyMapping verifies every missing-executor
// provider resolves to its dedicated executor via GetExecutor.
func TestGetExecutor_MissingFamilyMapping(t *testing.T) {
	for _, tc := range missingExecutorCases() {
		t.Run(tc.name, func(t *testing.T) {
			exec, ok := GetExecutor(tc.providerType, http.DefaultTransport)
			if !ok {
				t.Fatalf("GetExecutor(%s) ok = false", tc.providerType)
			}
			if exec == nil {
				t.Fatal("GetExecutor returned nil executor with ok=true")
			}
			wantTyp := "*specialized." + expectedExecutorTypeName(tc.name)
			if typ := fmt.Sprintf("%T", exec); typ != wantTyp {
				t.Errorf("executor type = %s, want %s", typ, wantTyp)
			}
		})
	}
}

// expectedExecutorTypeName maps the case name to the Go executor type name.
func expectedExecutorTypeName(name string) string {
	switch name {
	case "deepseek":
		return "DeepSeekExecutor"
	case "fireworks":
		return "FireworksExecutor"
	case "groq":
		return "GroqExecutor"
	case "mistral":
		return "MistralExecutor"
	case "perplexity":
		return "PerplexityExecutor"
	case "together":
		return "TogetherExecutor"
	case "xai":
		return "XAIExecutor"
	default:
		return ""
	}
}

// TestGetExecutor_MissingFamilyAliases verifies the alias mappings
// (mmf -> MimoFreeExecutor, perplexity_agent -> PerplexityExecutor).
func TestGetExecutor_MissingFamilyAliases(t *testing.T) {
	exec, ok := GetExecutor(provider.ProviderMmf, http.DefaultTransport)
	if !ok {
		t.Fatal("GetExecutor(mmf) ok = false")
	}
	if _, isMimo := exec.(*MimoFreeExecutor); !isMimo {
		t.Errorf("mmf executor type = %T, want *MimoFreeExecutor", exec)
	}

	exec, ok = GetExecutor(provider.ProviderPerplexityAgent, http.DefaultTransport)
	if !ok {
		t.Fatal("GetExecutor(perplexity_agent) ok = false")
	}
	if _, isPplx := exec.(*PerplexityExecutor); !isPplx {
		t.Errorf("perplexity_agent executor type = %T, want *PerplexityExecutor", exec)
	}
}

// TestGetExecutor_UnknownProviderNoFallback verifies an unregistered provider
// type resolves to no executor (the missing-executor contract: no silent
// generic fallback at the specialized layer).
func TestGetExecutor_UnknownProviderNoFallback(t *testing.T) {
	for _, ptype := range []provider.ProviderType{
		"unknown",
		"anthropic",
		"openai",
		"totally-made-up-provider",
	} {
		exec, ok := GetExecutor(ptype, http.DefaultTransport)
		if ok {
			t.Errorf("GetExecutor(%s) ok = true, want false (executor %T)", ptype, exec)
		}
		if exec != nil {
			t.Errorf("GetExecutor(%s) = %T, want nil", ptype, exec)
		}
	}
}

// TestErrExecutorNotFoundContract pins the missing-executor sentinel error.
func TestErrExecutorNotFoundContract(t *testing.T) {
	if errExecutorNotFound.Code != shared.ErrNotFound {
		t.Errorf("code = %q, want %q", errExecutorNotFound.Code, shared.ErrNotFound)
	}
	if !strings.Contains(errExecutorNotFound.Message, "no specialized executor") {
		t.Errorf("message = %q", errExecutorNotFound.Message)
	}
}

// ---------------------------------------------------------------------------
// Missing family — non-streaming execute
// ---------------------------------------------------------------------------

// TestMissingExecutor_Execute exercises the exact URL path, Bearer auth,
// stream:false body injection, model injection, response parse and usage
// extraction for each member of the family.
func TestMissingExecutor_Execute(t *testing.T) {
	for _, tc := range missingExecutorCases() {
		t.Run(tc.name, func(t *testing.T) {
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

			exec := tc.newExecutor(srv.Client().Transport)
			req := newTestRequest(engine.FormatOpenAICompat, "model-x", variantsJSONBody(t, map[string]interface{}{
				"messages": []map[string]interface{}{{"role": "user", "content": "hi"}},
			}))
			req.Headers = map[string]string{"X-Base-URL": srv.URL}

			resp, err := exec.Execute(context.Background(), req, newTestAccount("family-key"))
			if err != nil {
				t.Fatalf("Execute: %v", err)
			}
			if gotPath != "/v1/chat/completions" {
				t.Errorf("path = %q, want /v1/chat/completions", gotPath)
			}
			if tc.withAuth {
				if gotAuth != "Bearer family-key" {
					t.Errorf("Authorization = %q, want Bearer family-key", gotAuth)
				}
			}
			if gotContentType != "application/json" {
				t.Errorf("Content-Type = %q", gotContentType)
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

// TestMissingExecutor_Execute_PayloadModelWins verifies an explicit payload
// model is preserved.
func TestMissingExecutor_Execute_PayloadModelWins(t *testing.T) {
	for _, tc := range missingExecutorCases() {
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

// TestMissingExecutor_DefaultURL verifies each member's default base URL.
func TestMissingExecutor_DefaultURL(t *testing.T) {
	for _, tc := range missingExecutorCases() {
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

// TestMissingExecutor_ErrorMapping covers 401/403/429/500 for each member.
func TestMissingExecutor_ErrorMapping(t *testing.T) {
	statuses := []struct {
		status   int
		wantCode shared.ErrorCode
	}{
		{http.StatusUnauthorized, shared.ErrUnauthorized},
		{http.StatusForbidden, shared.ErrUnauthorized},
		{http.StatusTooManyRequests, shared.ErrRateLimited},
		{http.StatusInternalServerError, shared.ErrInternal},
	}
	for _, tc := range missingExecutorCases() {
		for _, s := range statuses {
			t.Run(tc.name+"_"+string(s.wantCode), func(t *testing.T) {
				srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.WriteHeader(s.status)
					_, _ = io.WriteString(w, `{"error":"boom"}`)
				}))
				defer srv.Close()

				exec := tc.newExecutor(srv.Client().Transport)
				req := newTestRequest(engine.FormatOpenAICompat, "model-x", variantsJSONBody(t, map[string]interface{}{"model": "model-x"}))
				req.Headers = map[string]string{"X-Base-URL": srv.URL}

				_, err := exec.Execute(context.Background(), req, newTestAccount("k"))
				assertAppError(t, err, s.wantCode, s.status)
			})
		}
	}
}

// TestMissingExecutor_SupportsFormat pins the supported format set per the
// provider matrix.
func TestMissingExecutor_SupportsFormat(t *testing.T) {
	allFormats := []engine.RequestFormat{
		engine.FormatOpenAIChat, engine.FormatOpenAICompat,
		engine.FormatCodexResponses, engine.FormatAnthropic, engine.FormatGemini,
	}
	for _, tc := range missingExecutorCases() {
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

// ---------------------------------------------------------------------------
// Missing family — streaming
// ---------------------------------------------------------------------------

// TestMissingExecutor_ExecuteStream_Chunks verifies stream:true injection and
// the exact 2-data-chunk + final sequence for every member.
func TestMissingExecutor_ExecuteStream_Chunks(t *testing.T) {
	for _, tc := range missingExecutorCases() {
		t.Run(tc.name, func(t *testing.T) {
			var gotBody map[string]interface{}
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
			if gotBody["stream"] != true {
				t.Errorf("body stream = %v, want true", gotBody["stream"])
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
			if len(chunks) != 3 || !chunks[2].IsFinal {
				t.Errorf("chunks = %d, want 3 with final IsFinal", len(chunks))
			}
		})
	}
}

// TestMissingExecutor_ExecuteStream_AcceptHeader verifies streaming requests
// advertise text/event-stream for every member except deepseek (covered by the
// dedicated probe) — upstream default.js buildHeaders sets Accept to
// text/event-stream whenever stream=true.
func TestMissingExecutor_ExecuteStream_AcceptHeader(t *testing.T) {
	for _, tc := range missingExecutorCases() {
		if tc.name == "deepseek" {
			continue
		}
		t.Run(tc.name, func(t *testing.T) {
			var gotAccept string
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				gotAccept = r.Header.Get("Accept")
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

			if _, err := exec.ExecuteStream(context.Background(), req, newTestAccount("k")); err != nil {
				t.Fatalf("ExecuteStream: %v", err)
			}
			if gotAccept != "text/event-stream" {
				t.Errorf("Accept = %q, want text/event-stream", gotAccept)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// DeepSeek — dedicated probes
// ---------------------------------------------------------------------------

// TestDeepSeekExecutor_Execute_ExactRequest pins the DeepSeek non-streaming
// contract: URL path, Bearer auth, Content-Type, Accept: application/json and
// stream:false.
func TestDeepSeekExecutor_Execute_ExactRequest(t *testing.T) {
	var gotPath, gotAuth, gotAccept, gotContentType string
	var gotBody map[string]interface{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		gotAccept = r.Header.Get("Accept")
		gotContentType = r.Header.Get("Content-Type")
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		_, _ = io.WriteString(w, standardChatResponse)
	}))
	defer srv.Close()

	e := NewDeepSeekExecutor(srv.Client().Transport)
	req := newTestRequest(engine.FormatOpenAIChat, "deepseek-chat", variantsJSONBody(t, map[string]interface{}{
		"messages": []map[string]interface{}{{"role": "user", "content": "hi"}},
	}))
	req.Headers = map[string]string{"X-Base-URL": srv.URL}

	resp, err := e.Execute(context.Background(), req, newTestAccount("ds-key"))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if gotPath != "/v1/chat/completions" {
		t.Errorf("path = %q, want /v1/chat/completions", gotPath)
	}
	if gotAuth != "Bearer ds-key" {
		t.Errorf("Authorization = %q", gotAuth)
	}
	if gotAccept != "application/json" {
		t.Errorf("Accept = %q, want application/json for non-stream", gotAccept)
	}
	if gotContentType != "application/json" {
		t.Errorf("Content-Type = %q", gotContentType)
	}
	if gotBody["stream"] != false {
		t.Errorf("body stream = %v, want false", gotBody["stream"])
	}
	if resp.Usage == nil || resp.Usage.TotalTokens != 30 {
		t.Errorf("usage = %+v", resp.Usage)
	}
}

// TestDeepSeekExecutor_ExecuteStream_AcceptHeaderProbe is a DEFECT PROBE.
// DeepSeek's buildRequest hard-codes Accept: application/json for both
// streaming and non-streaming requests, so ExecuteStream advertises a
// non-streaming Accept on a streaming request. Upstream default.js sends
// Accept: text/event-stream whenever stream=true (line 210). This test
// asserts the correct contract and is expected to FAIL (RED) until the
// production code is fixed.
func TestDeepSeekExecutor_ExecuteStream_AcceptHeaderProbe(t *testing.T) {
	var gotAccept string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAccept = r.Header.Get("Accept")
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, sseChatBody)
	}))
	defer srv.Close()

	e := NewDeepSeekExecutor(srv.Client().Transport)
	req := newTestRequest(engine.FormatOpenAIChat, "deepseek-chat", variantsJSONBody(t, map[string]interface{}{
		"model":    "deepseek-chat",
		"messages": []map[string]interface{}{},
	}))
	req.Headers = map[string]string{"X-Base-URL": srv.URL}

	resp, err := e.ExecuteStream(context.Background(), req, newTestAccount("ds-key"))
	if err != nil {
		t.Fatalf("ExecuteStream: %v", err)
	}
	if gotAccept != "text/event-stream" {
		t.Errorf("DEFECT: stream Accept = %q, want text/event-stream (upstream default.js sends text/event-stream when stream=true)", gotAccept)
	}
	if resp.Stream == nil {
		t.Fatal("nil stream")
	}
	payloads := chunkPayloads(collectStreamChunks(t, resp.Stream.(*stream.Stream)))
	if len(payloads) != 2 {
		t.Errorf("chunk payloads = %d, want 2", len(payloads))
	}
}

// TestDeepSeekExecutor_ExecuteStream_Error401 verifies stream errors surface
// as AppError before any stream is returned.
func TestDeepSeekExecutor_ExecuteStream_Error401(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = io.WriteString(w, `{"error":"bad key"}`)
	}))
	defer srv.Close()

	e := NewDeepSeekExecutor(srv.Client().Transport)
	req := newTestRequest(engine.FormatOpenAIChat, "deepseek-chat", variantsJSONBody(t, map[string]interface{}{"model": "deepseek-chat"}))
	req.Headers = map[string]string{"X-Base-URL": srv.URL}

	resp, err := e.ExecuteStream(context.Background(), req, newTestAccount("bad"))
	if resp != nil && resp.Stream != nil {
		t.Error("no stream must be returned on HTTP error")
	}
	assertAppError(t, err, shared.ErrUnauthorized, http.StatusUnauthorized)
}

// TestMissingExecutor_PerplexityExecuteNoStreamInjection verifies Perplexity's
// dedicated Execute path (stream:false, no Accept header on non-stream,
// matching upstream default.js which only sets Accept when streaming).
func TestMissingExecutor_PerplexityExecuteNoStreamInjection(t *testing.T) {
	var gotAccept, gotAuth string
	var gotBody map[string]interface{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAccept = r.Header.Get("Accept")
		gotAuth = r.Header.Get("Authorization")
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		_, _ = io.WriteString(w, standardChatResponse)
	}))
	defer srv.Close()

	e := NewPerplexityExecutor(srv.Client().Transport)
	req := newTestRequest(engine.FormatOpenAICompat, "sonar", variantsJSONBody(t, map[string]interface{}{
		"model":    "sonar",
		"messages": []map[string]interface{}{},
	}))
	req.Headers = map[string]string{"X-Base-URL": srv.URL}

	if _, err := e.Execute(context.Background(), req, newTestAccount("pplx-key")); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if gotAccept != "" {
		t.Errorf("Accept = %q, want unset on non-streaming execute", gotAccept)
	}
	if gotAuth != "Bearer pplx-key" {
		t.Errorf("Authorization = %q", gotAuth)
	}
	if gotBody["stream"] != false {
		t.Errorf("body stream = %v, want false", gotBody["stream"])
	}
}

package specialized

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"gorouter/internal/domain/engine"
	"gorouter/internal/domain/provider"

	"github.com/google/uuid"
)

func newTestAccount(credentialRef string) *provider.Account {
	return &provider.Account{
		ID:            uuid.New(),
		CredentialRef: credentialRef,
		AuthType:      "api_key",
		IsEnabled:     true,
	}
}

func newTestRequest(format engine.RequestFormat, model string, body []byte) *engine.Request {
	return &engine.Request{
		ID:         uuid.New(),
		Format:     format,
		Model:      model,
		RawBody:    body,
		MappedBody: body,
		Stream:     false,
	}
}

func TestProviderTypeConstants(t *testing.T) {
	tests := []struct {
		ptype    provider.ProviderType
		expected string
	}{
		{provider.ProviderAzure, "azure"},
		{provider.ProviderGithubModels, "github_models"},
		{provider.ProviderGeminiCli, "gemini_cli"},
		{provider.ProviderGemini, "gemini"},
		{provider.ProviderIflow, "iflow"},
		{provider.ProviderQoder, "qoder"},
		{provider.ProviderKiro, "kiro"},
		{provider.ProviderKimchi, "kimchi"},
		{provider.ProviderCursor, "cursor"},
		{provider.ProviderVertex, "vertex"},
		{provider.ProviderQwen, "qwen"},
		{provider.ProviderAntigravity, "antigravity"},
		{provider.ProviderGrokWeb, "grok_web"},
		{provider.ProviderGrokCli, "grok_cli"},
		{provider.ProviderPerplexityWeb, "perplexity_web"},
		{provider.ProviderOllamaLocal, "ollama_local"},
		{provider.ProviderCommandcode, "commandcode"},
		{provider.ProviderXiaomiTokenplan, "xiaomi-tokenplan"},
		{provider.ProviderMimoFree, "mimo_free"},
		{provider.ProviderCodebuddyCn, "codebuddy_cn"},
	}
	for _, tt := range tests {
		if string(tt.ptype) != tt.expected {
			t.Errorf("expected %q, got %q", tt.expected, string(tt.ptype))
		}
	}
}

func TestGetExecutor(t *testing.T) {
	transport := http.DefaultTransport
	tests := []struct {
		name     string
		ptype    provider.ProviderType
		wantOK   bool
		wantType string
	}{
		{"azure", provider.ProviderAzure, true, "*specialized.AzureExecutor"},
		{"github_models", provider.ProviderGithubModels, true, "*specialized.GitHubExecutor"},
		{"gemini_cli", provider.ProviderGeminiCli, true, "*specialized.GeminiCLIExecutor"},
		{"gemini", provider.ProviderGemini, true, "*specialized.GeminiExecutor"},
		{"iflow", provider.ProviderIflow, true, "*specialized.IFlowExecutor"},
		{"qoder", provider.ProviderQoder, true, "*specialized.QoderExecutor"},
		{"kiro", provider.ProviderKiro, true, "*specialized.KiroExecutor"},
		{"kimchi", provider.ProviderKimchi, true, "*specialized.KimchiExecutor"},
		{"cursor", provider.ProviderCursor, true, "*specialized.CursorExecutor"},
		{"vertex", provider.ProviderVertex, true, "*specialized.VertexExecutor"},
		{"qwen", provider.ProviderQwen, true, "*specialized.QwenExecutor"},
		{"antigravity", provider.ProviderAntigravity, true, "*specialized.AntigravityExecutor"},
		{"grok_web", provider.ProviderGrokWeb, true, "*specialized.GrokWebExecutor"},
		{"grok_cli", provider.ProviderGrokCli, true, "*specialized.GrokCliExecutor"},
		{"perplexity_web", provider.ProviderPerplexityWeb, true, "*specialized.PerplexityWebExecutor"},
		{"ollama_local", provider.ProviderOllamaLocal, true, "*specialized.OllamaLocalExecutor"},
		{"commandcode", provider.ProviderCommandcode, true, "*specialized.CommandCodeExecutor"},
		{"xiaomi-tokenplan", provider.ProviderXiaomiTokenplan, true, "*specialized.XiaomiTokenplanExecutor"},
		{"mimo_free", provider.ProviderMimoFree, true, "*specialized.MimoFreeExecutor"},
		{"codebuddy_cn", provider.ProviderCodebuddyCn, true, "*specialized.CodeBuddyExecutor"},
		{"vertex_partner", provider.ProviderVertexPartner, true, "*specialized.VertexExecutor"},
		{"deepseek", provider.ProviderDeepseek, true, "*specialized.DeepSeekExecutor"},
		{"fireworks", provider.ProviderFireworks, true, "*specialized.FireworksExecutor"},
		{"groq", provider.ProviderGroq, true, "*specialized.GroqExecutor"},
		{"mistral", provider.ProviderMistral, true, "*specialized.MistralExecutor"},
		{"perplexity", provider.ProviderPerplexity, true, "*specialized.PerplexityExecutor"},
		{"perplexity_agent", provider.ProviderPerplexityAgent, true, "*specialized.PerplexityExecutor"},
		{"together", provider.ProviderTogether, true, "*specialized.TogetherExecutor"},
		{"xai", provider.ProviderXai, true, "*specialized.XAIExecutor"},
		{"mmf", provider.ProviderMmf, true, "*specialized.MimoFreeExecutor"},
		{"unknown", "unknown", false, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			exec, ok := GetExecutor(tt.ptype, transport)
			if ok != tt.wantOK {
				t.Errorf("GetExecutor(%q) ok = %v, want %v", tt.ptype, ok, tt.wantOK)
			}
			if tt.wantOK && exec == nil {
				t.Fatal("expected non-nil executor when ok=true")
			}
			if tt.wantOK {
				gotTyp := fmt.Sprintf("%T", exec)
				if gotTyp != tt.wantType {
					t.Errorf("expected type %s, got %s", tt.wantType, gotTyp)
				}
			}
		})
	}
}

func TestAzureExecutor_SupportsFormat(t *testing.T) {
	e := NewAzureExecutor(http.DefaultTransport)
	if !e.SupportsFormat(engine.FormatOpenAIChat) {
		t.Error("expected to support openai_chat")
	}
	if !e.SupportsFormat(engine.FormatOpenAICompat) {
		t.Error("expected to support openai_compat")
	}
	if e.SupportsFormat(engine.FormatAnthropic) {
		t.Error("expected NOT to support anthropic")
	}
}

func TestAzureExecutor_ProviderType(t *testing.T) {
	e := NewAzureExecutor(http.DefaultTransport)
	if e.ProviderType() != provider.ProviderAzure {
		t.Errorf("expected %s, got %s", provider.ProviderAzure, e.ProviderType())
	}
}

func TestAzureExecutor_Execute(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, "/chat/completions") {
			t.Errorf("expected /chat/completions in path, got %s", r.URL.Path)
		}
		if r.Header.Get("api-key") != "test-key" {
			t.Errorf("expected api-key header, got %s", r.Header.Get("api-key"))
		}

		resp := map[string]interface{}{
			"id":      "chatcmpl-test",
			"object":  "chat.completion",
			"created": 1715000000,
			"model":   "gpt-4",
			"choices": []map[string]interface{}{
				{
					"index":         0,
					"finish_reason": "stop",
					"message": map[string]interface{}{
						"role":    "assistant",
						"content": "Hello from Azure",
					},
				},
			},
			"usage": map[string]interface{}{
				"prompt_tokens":     10,
				"completion_tokens": 20,
				"total_tokens":      30,
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	body, _ := json.Marshal(map[string]interface{}{
		"model":    "gpt-4",
		"messages": []map[string]interface{}{{"role": "user", "content": "Hi"}},
	})

	e := NewAzureExecutor(http.DefaultTransport)
	req := newTestRequest(engine.FormatOpenAIChat, "gpt-4", body)
	req.Headers = map[string]string{
		"X-Base-URL":       srv.URL,
		"X-Azure-Endpoint": srv.URL,
	}

	resp, err := e.Execute(context.Background(), req, newTestAccount("test-key"))
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if resp.StatusCode != 200 {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
	if len(resp.Body) == 0 {
		t.Error("expected non-empty response body")
	}
}

func TestAzureExecutor_ExecuteStream(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"Hello\"}}]}\n\ndata: [DONE]\n\n")
	}))
	defer srv.Close()

	body, _ := json.Marshal(map[string]interface{}{
		"model":    "gpt-4",
		"messages": []map[string]interface{}{{"role": "user", "content": "Hi"}},
	})

	e := NewAzureExecutor(http.DefaultTransport)
	req := newTestRequest(engine.FormatOpenAIChat, "gpt-4", body)
	req.Headers = map[string]string{"X-Azure-Endpoint": srv.URL}

	resp, err := e.ExecuteStream(context.Background(), req, newTestAccount("key"))
	if err != nil {
		t.Fatalf("ExecuteStream failed: %v", err)
	}
	if resp.Stream == nil {
		t.Fatal("expected non-nil Stream")
	}
}

func TestGitHubExecutor_SupportsFormat(t *testing.T) {
	e := NewGitHubExecutor(http.DefaultTransport)
	if !e.SupportsFormat(engine.FormatOpenAIChat) {
		t.Error("expected to support openai_chat")
	}
}

func TestGitHubExecutor_ProviderType(t *testing.T) {
	e := NewGitHubExecutor(http.DefaultTransport)
	if e.ProviderType() != provider.ProviderGithubModels {
		t.Errorf("expected %s, got %s", provider.ProviderGithubModels, e.ProviderType())
	}
}

func TestGeminiCLIExecutor_SupportsFormat(t *testing.T) {
	e := NewGeminiCLIExecutor(http.DefaultTransport)
	if !e.SupportsFormat(engine.FormatGemini) {
		t.Error("expected to support gemini format")
	}
	if e.SupportsFormat(engine.FormatOpenAIChat) {
		t.Error("expected NOT to support openai_chat")
	}
}

func TestGeminiCLIExecutor_ProviderType(t *testing.T) {
	e := NewGeminiCLIExecutor(http.DefaultTransport)
	if e.ProviderType() != provider.ProviderGeminiCli {
		t.Errorf("expected %s, got %s", provider.ProviderGeminiCli, e.ProviderType())
	}
}

func TestIFlowExecutor_SupportsFormat(t *testing.T) {
	e := NewIFlowExecutor(http.DefaultTransport)
	if !e.SupportsFormat(engine.FormatGemini) {
		t.Error("expected to support gemini format")
	}
}

func TestIFlowExecutor_ProviderType(t *testing.T) {
	e := NewIFlowExecutor(http.DefaultTransport)
	if e.ProviderType() != provider.ProviderIflow {
		t.Errorf("expected %s, got %s", provider.ProviderIflow, e.ProviderType())
	}
}

func TestIFlowExecutor_Signature(t *testing.T) {
	e := NewIFlowExecutor(http.DefaultTransport)
	sig := e.sign("test:payload:12345", "secret-key")
	if sig == "" {
		t.Fatal("expected non-empty signature")
	}
	if len(sig) != 64 {
		t.Errorf("expected 64 hex chars, got %d", len(sig))
	}
}

func TestVertexExecutor_ProviderType(t *testing.T) {
	e := NewVertexExecutor(http.DefaultTransport, false)
	if e.ProviderType() != provider.ProviderVertex {
		t.Errorf("expected %s, got %s", provider.ProviderVertex, e.ProviderType())
	}

	e2 := NewVertexExecutor(http.DefaultTransport, true)
	if e2.ProviderType() != provider.ProviderVertexPartner {
		t.Errorf("expected %s, got %s", provider.ProviderVertexPartner, e2.ProviderType())
	}
}

func TestVertexExecutor_SupportsFormat(t *testing.T) {
	e := NewVertexExecutor(http.DefaultTransport, false)
	if !e.SupportsFormat(engine.FormatGemini) {
		t.Error("expected to support gemini format")
	}
}

func TestOllamaLocalExecutor_ProviderType(t *testing.T) {
	e := NewOllamaLocalExecutor(http.DefaultTransport)
	if e.ProviderType() != provider.ProviderOllamaLocal {
		t.Errorf("expected %s, got %s", provider.ProviderOllamaLocal, e.ProviderType())
	}
}

func TestOllamaLocalExecutor_SupportsFormat(t *testing.T) {
	e := NewOllamaLocalExecutor(http.DefaultTransport)
	if !e.SupportsFormat(engine.FormatOpenAIChat) {
		t.Error("expected to support openai_chat")
	}
}

func TestCommandCodeExecutor_ProviderType(t *testing.T) {
	e := NewCommandCodeExecutor(http.DefaultTransport)
	if e.ProviderType() != provider.ProviderCommandcode {
		t.Errorf("expected %s, got %s", provider.ProviderCommandcode, e.ProviderType())
	}
}

func TestXiaomiTokenplanExecutor_ProviderType(t *testing.T) {
	e := NewXiaomiTokenplanExecutor(http.DefaultTransport)
	if e.ProviderType() != provider.ProviderXiaomiTokenplan {
		t.Errorf("expected %s, got %s", provider.ProviderXiaomiTokenplan, e.ProviderType())
	}
}

func TestMimoFreeExecutor_ProviderType(t *testing.T) {
	e := NewMimoFreeExecutor(http.DefaultTransport)
	if e.ProviderType() != provider.ProviderMimoFree {
		t.Errorf("expected %s, got %s", provider.ProviderMimoFree, e.ProviderType())
	}
}

func TestCodeBuddyExecutor_ProviderType(t *testing.T) {
	e := NewCodeBuddyExecutor(http.DefaultTransport)
	if e.ProviderType() != provider.ProviderCodebuddyCn {
		t.Errorf("expected %s, got %s", provider.ProviderCodebuddyCn, e.ProviderType())
	}
}

func TestGrokWebExecutor_ProviderType(t *testing.T) {
	e := NewGrokWebExecutor(http.DefaultTransport)
	if e.ProviderType() != provider.ProviderGrokWeb {
		t.Errorf("expected %s, got %s", provider.ProviderGrokWeb, e.ProviderType())
	}
}

func TestGrokCliExecutor_ProviderType(t *testing.T) {
	e := NewGrokCliExecutor(http.DefaultTransport)
	if e.ProviderType() != provider.ProviderGrokCli {
		t.Errorf("expected %s, got %s", provider.ProviderGrokCli, e.ProviderType())
	}
}

func TestPerplexityWebExecutor_ProviderType(t *testing.T) {
	e := NewPerplexityWebExecutor(http.DefaultTransport)
	if e.ProviderType() != provider.ProviderPerplexityWeb {
		t.Errorf("expected %s, got %s", provider.ProviderPerplexityWeb, e.ProviderType())
	}
}

func TestQwenExecutor_ProviderType(t *testing.T) {
	e := NewQwenExecutor(http.DefaultTransport)
	if e.ProviderType() != provider.ProviderQwen {
		t.Errorf("expected %s, got %s", provider.ProviderQwen, e.ProviderType())
	}
}

func TestCursorExecutor_ProviderType(t *testing.T) {
	e := NewCursorExecutor(http.DefaultTransport)
	if e.ProviderType() != provider.ProviderCursor {
		t.Errorf("expected %s, got %s", provider.ProviderCursor, e.ProviderType())
	}
}

func TestKiroExecutor_ProviderType(t *testing.T) {
	e := NewKiroExecutor(http.DefaultTransport)
	if e.ProviderType() != provider.ProviderKiro {
		t.Errorf("expected %s, got %s", provider.ProviderKiro, e.ProviderType())
	}
}

func TestQoderExecutor_ProviderType(t *testing.T) {
	e := NewQoderExecutor(http.DefaultTransport)
	if e.ProviderType() != provider.ProviderQoder {
		t.Errorf("expected %s, got %s", provider.ProviderQoder, e.ProviderType())
	}
}

func TestKimchiExecutor_ProviderType(t *testing.T) {
	e := NewKimchiExecutor(http.DefaultTransport)
	if e.ProviderType() != provider.ProviderKimchi {
		t.Errorf("expected %s, got %s", provider.ProviderKimchi, e.ProviderType())
	}
}

func TestAntigravityExecutor_ProviderType(t *testing.T) {
	e := NewAntigravityExecutor(http.DefaultTransport)
	if e.ProviderType() != provider.ProviderAntigravity {
		t.Errorf("expected %s, got %s", provider.ProviderAntigravity, e.ProviderType())
	}
}

func TestAntigravityExecutor_SupportsFormat(t *testing.T) {
	e := NewAntigravityExecutor(http.DefaultTransport)
	if !e.SupportsFormat(engine.FormatGemini) {
		t.Error("expected to support gemini format")
	}
}

func TestGrokModelMap(t *testing.T) {
	if _, ok := grokModelMap["grok-4.1-fast"]; !ok {
		t.Error("expected grok-4.1-fast in model map")
	}
	if _, ok := grokModelMap["grok-3"]; !ok {
		t.Error("expected grok-3 in model map")
	}
	mi := grokModelMap["grok-4.1-fast"]
	if mi.grokModel != "grok-4-1-thinking-1129" {
		t.Errorf("expected grok-4-1-thinking-1129, got %s", mi.grokModel)
	}
}

func TestPplxModelMap(t *testing.T) {
	if _, ok := pplxModelMap["pplx-auto"]; !ok {
		t.Error("expected pplx-auto in model map")
	}
	if _, ok := pplxModelMap["pplx-sonar"]; !ok {
		t.Error("expected pplx-sonar in model map")
	}
}

func TestIsClaudeModel(t *testing.T) {
	if !isClaudeModel("claude-sonnet-4-20250514") {
		t.Error("expected true for claude model")
	}
	if !isClaudeModel("CLAUDE-3-5-sonnet") {
		t.Error("expected true for uppercase claude")
	}
	if isClaudeModel("gpt-4") {
		t.Error("expected false for non-claude model")
	}
}

func TestSupportsResponsesEndpoint(t *testing.T) {
	if !supportsResponsesEndpoint("gpt-5-codex") {
		t.Error("expected true for gpt-5-codex")
	}
	if !supportsResponsesEndpoint("gpt-5-codex") {
		t.Error("expected true for codex model")
	}
}

func TestParseGrokMessages(t *testing.T) {
	messages := []struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	}{
		{Role: "system", Content: "You are helpful"},
		{Role: "user", Content: "Hello"},
	}
	result := parseGrokMessages(messages)
	if !strings.Contains(result, "system: You are helpful") {
		t.Error("expected system message in output")
	}
	if !strings.Contains(result, "user: Hello") {
		t.Error("expected user message in output")
	}
}

func TestFlattenHeaders(t *testing.T) {
	h := http.Header{}
	h.Set("Content-Type", "application/json")
	h.Set("X-Custom", "value1")

	f := flattenHeaders(h)
	if f["Content-Type"] != "application/json" {
		t.Errorf("expected application/json, got %s", f["Content-Type"])
	}
	if f["X-Custom"] != "value1" {
		t.Errorf("expected value1, got %s", f["X-Custom"])
	}
}

func TestSelectBody(t *testing.T) {
	req := &engine.Request{
		RawBody:    []byte("raw"),
		MappedBody: []byte("mapped"),
	}
	if string(selectBody(req)) != "mapped" {
		t.Error("expected MappedBody to take priority")
	}

	req.MappedBody = nil
	if string(selectBody(req)) != "raw" {
		t.Error("expected RawBody fallback")
	}
}

func TestResolveModel(t *testing.T) {
	if resolveModel("override", nil) != "override" {
		t.Error("expected override to win")
	}
	req := &engine.Request{Model: "req-model"}
	if resolveModel("", req) != "req-model" {
		t.Error("expected req.Model fallback")
	}
	if resolveModel("", nil) != "gpt-4o" {
		t.Error("expected default fallback")
	}
}

func TestTruncateString(t *testing.T) {
	if truncateString("hello", 3) != "hel..." {
		t.Errorf("expected hel..., got %s", truncateString("hello", 3))
	}
	if truncateString("hi", 10) != "hi" {
		t.Errorf("expected hi, got %s", truncateString("hi", 10))
	}
}

func TestStreamingConfigDefaults(t *testing.T) {
	cfg := DefaultStreamingConfig()
	if cfg.FirstChunkTimeout != 200e9 {
		t.Errorf("expected 200s, got %v", cfg.FirstChunkTimeout)
	}
	if cfg.StallTimeout != 360e9 {
		t.Errorf("expected 6m, got %v", cfg.StallTimeout)
	}
}

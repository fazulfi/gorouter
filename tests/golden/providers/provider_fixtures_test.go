package providers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"gorouter/internal/domain/engine"
	"gorouter/internal/domain/provider"
	"gorouter/internal/engine/providers/specialized"

	"github.com/google/uuid"
)

func newAccount(ref string) *provider.Account {
	return &provider.Account{
		ID:            uuid.New(),
		CredentialRef: ref,
		AuthType:      "api_key",
		IsEnabled:     true,
	}
}

func newReq(model string, body []byte) *engine.Request {
	return &engine.Request{
		ID:         uuid.New(),
		Format:     engine.FormatOpenAIChat,
		Model:      model,
		RawBody:    body,
		MappedBody: body,
	}
}

// TestGolden_ProviderFixtures runs golden request/response tests for each
// specialized executor against mock upstream servers.
func TestGolden_ProviderFixtures(t *testing.T) {
	tests := []struct {
		name     string
		ptype    provider.ProviderType
		newExec  func(rt http.RoundTripper) engine.Executor
		setupSrv func() *httptest.Server
		model    string
		headers  map[string]string
	}{
		{
			name:  "azure-chat-completion",
			ptype: "azure",
			newExec: func(rt http.RoundTripper) engine.Executor {
				return specialized.NewAzureExecutor(rt)
			},
			setupSrv: func() *httptest.Server {
				return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if r.Header.Get("api-key") == "" {
						t.Error("missing api-key header")
					}
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusOK)
					json.NewEncoder(w).Encode(map[string]interface{}{
						"id": "cmpl-test", "object": "chat.completion",
						"created": 1715000000, "model": "gpt-4",
						"choices": []map[string]interface{}{
							{"index": 0, "finish_reason": "stop",
								"message": map[string]interface{}{"role": "assistant", "content": "Hello"}},
						},
						"usage": map[string]interface{}{"prompt_tokens": 10, "completion_tokens": 20, "total_tokens": 30},
					})
				}))
			},
			model: "gpt-4",
			headers: map[string]string{
				"X-Azure-Endpoint": "USE_SERVER_URL",
			},
		},
		{
			name:  "iflow-hmac-signature",
			ptype: "iflow",
			newExec: func(rt http.RoundTripper) engine.Executor {
				return specialized.NewIFlowExecutor(rt)
			},
			setupSrv: func() *httptest.Server {
				return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if r.Header.Get("x-iflow-signature") == "" {
						t.Error("missing HMAC signature header")
					}
					if r.Header.Get("session-id") == "" {
						t.Error("missing session-id header")
					}
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusOK)
					json.NewEncoder(w).Encode(map[string]interface{}{
						"id": "cmpl-iflow", "object": "chat.completion",
						"created": 1715000000, "model": "gpt-4",
						"choices": []map[string]interface{}{
							{"index": 0, "finish_reason": "stop",
								"message": map[string]interface{}{"role": "assistant", "content": "iFlow OK"}},
						},
					})
				}))
			},
			model: "gpt-4",
		},
		{
			name:  "vertex-gemini",
			ptype: "vertex",
			newExec: func(rt http.RoundTripper) engine.Executor {
				return specialized.NewVertexExecutor(rt, false)
			},
			setupSrv: func() *httptest.Server {
				return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusOK)
					json.NewEncoder(w).Encode(map[string]interface{}{
						"candidates": []map[string]interface{}{
							{"content": map[string]interface{}{
								"parts": []map[string]interface{}{{"text": "Vertex response"}},
								"role":  "model",
							}},
						},
					})
				}))
			},
			model: "gemini-2.0-flash",
		},
		{
			name:  "ollama-local",
			ptype: "ollama-local",
			newExec: func(rt http.RoundTripper) engine.Executor {
				return specialized.NewOllamaLocalExecutor(rt)
			},
			setupSrv: func() *httptest.Server {
				return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusOK)
					json.NewEncoder(w).Encode(map[string]interface{}{
						"id": "ollama-test", "object": "chat.completion",
						"created": 1715000000, "model": "llama3",
						"choices": []map[string]interface{}{
							{"index": 0, "finish_reason": "stop",
								"message": map[string]interface{}{"role": "assistant", "content": "Ollama OK"}},
						},
					})
				}))
			},
			model: "llama3",
		},
		{
			name:  "qwen-chat",
			ptype: "qwen",
			newExec: func(rt http.RoundTripper) engine.Executor {
				return specialized.NewQwenExecutor(rt)
			},
			setupSrv: func() *httptest.Server {
				return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusOK)
					json.NewEncoder(w).Encode(map[string]interface{}{
						"id": "qwen-test", "object": "chat.completion",
						"created": 1715000000, "model": "qwen-max",
						"choices": []map[string]interface{}{
							{"index": 0, "finish_reason": "stop",
								"message": map[string]interface{}{"role": "assistant", "content": "Qwen OK"}},
						},
					})
				}))
			},
			model: "qwen-max",
		},
		{
			name:  "cursor-stream",
			ptype: "cursor",
			newExec: func(rt http.RoundTripper) engine.Executor {
				return specialized.NewCursorExecutor(rt)
			},
			setupSrv: func() *httptest.Server {
				return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.Header().Set("Content-Type", "text/event-stream")
					w.WriteHeader(http.StatusOK)
					w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"Cursor\"}}]}\n\ndata: [DONE]\n\n"))
				}))
			},
			model: "cursor-fast",
		},
		{
			name:  "vertex-gemini",
			ptype: "vertex",
			newExec: func(rt http.RoundTripper) engine.Executor {
				return specialized.NewVertexExecutor(rt, false)
			},
			setupSrv: func() *httptest.Server {
				return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if !strings.Contains(r.URL.Path, "googleapis.com") && !strings.Contains(r.URL.Path, "generateContent") {
						// Allow test server URLs
					}
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusOK)
					json.NewEncoder(w).Encode(map[string]interface{}{
						"candidates": []map[string]interface{}{
							{"content": map[string]interface{}{
								"parts": []map[string]interface{}{{"text": "Vertex response"}},
								"role":  "model",
							}},
						},
					})
				}))
			},
			model: "gemini-2.0-flash",
		},
		{
			name:  "deepseek-chat",
			ptype: "deepseek",
			newExec: func(rt http.RoundTripper) engine.Executor {
				return specialized.NewDeepSeekExecutor(rt)
			},
			setupSrv: func() *httptest.Server {
				return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusOK)
					json.NewEncoder(w).Encode(map[string]interface{}{
						"id": "ds-test", "object": "chat.completion",
						"created": 1715000000, "model": "deepseek-chat",
						"choices": []map[string]interface{}{
							{"index": 0, "finish_reason": "stop",
								"message": map[string]interface{}{"role": "assistant", "content": "DeepSeek OK"}},
						},
					})
				}))
			},
			model: "deepseek-chat",
		},
		{
			name:  "grok-cli",
			ptype: "grok-cli",
			newExec: func(rt http.RoundTripper) engine.Executor {
				return specialized.NewGrokCliExecutor(rt)
			},
			setupSrv: func() *httptest.Server {
				return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusOK)
					json.NewEncoder(w).Encode(map[string]interface{}{
						"id": "grok-test", "object": "chat.completion",
						"created": 1715000000, "model": "grok-4",
						"choices": []map[string]interface{}{
							{"index": 0, "finish_reason": "stop",
								"message": map[string]interface{}{"role": "assistant", "content": "Grok CLI OK"}},
						},
					})
				}))
			},
			model: "grok-4",
		},
		{
			name:  "xiaomi-tokenplan",
			ptype: "xiaomi-tokenplan",
			newExec: func(rt http.RoundTripper) engine.Executor {
				return specialized.NewXiaomiTokenplanExecutor(rt)
			},
			setupSrv: func() *httptest.Server {
				return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusOK)
					json.NewEncoder(w).Encode(map[string]interface{}{
						"id": "tp-test", "object": "chat.completion",
						"created": 1715000000, "model": "test-model",
						"choices": []map[string]interface{}{
							{"index": 0, "finish_reason": "stop",
								"message": map[string]interface{}{"role": "assistant", "content": "Xiaomi OK"}},
						},
					})
				}))
			},
			model: "test-model",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := tt.setupSrv()
			defer srv.Close()

			exec := tt.newExec(http.DefaultTransport)

			body, _ := json.Marshal(map[string]interface{}{
				"model":    tt.model,
				"messages": []map[string]interface{}{{"role": "user", "content": "Hi"}},
			})

			req := newReq(tt.model, body)
			req.Headers = map[string]string{"X-Base-URL": srv.URL}
			if tt.headers != nil {
				for k, v := range tt.headers {
					if v == "USE_SERVER_URL" {
						req.Headers[k] = srv.URL
					} else {
						req.Headers[k] = v
					}
				}
			}

			// Test non-streaming execution
			resp, err := exec.Execute(context.Background(), req, newAccount("test-key"))
			if err != nil {
				t.Fatalf("Execute failed: %v", err)
			}
			if resp.StatusCode != 200 {
				t.Errorf("expected 200, got %d", resp.StatusCode)
			}
			if len(resp.Body) == 0 {
				t.Error("expected non-empty response body")
			}
		})
	}
}

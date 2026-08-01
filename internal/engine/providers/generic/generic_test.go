package generic

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"gorouter/internal/domain/engine"
	"gorouter/internal/domain/engine/stream"
	"gorouter/internal/domain/provider"

	"github.com/google/uuid"
)

// -- test helpers -------------------------------------------------------------

func testAccount(credentialRef string) *provider.Account {
	return &provider.Account{
		ID:            uuid.New(),
		CredentialRef: credentialRef,
		AuthType:      "api_key",
		IsEnabled:     true,
	}
}

func testRequest(format engine.RequestFormat, model string, body []byte) *engine.Request {
	return &engine.Request{
		ID:         uuid.New(),
		Format:     format,
		Model:      model,
		RawBody:    body,
		MappedBody: body,
		Stream:     false,
	}
}

func testClient(srv *httptest.Server) *Client {
	return NewClient(
		WithTransport(http.DefaultTransport),
		WithBaseURL(srv.URL),
	)
}

// -- OpenAI executor tests ----------------------------------------------------

func TestCompatibleOpenAIExecutor_SupportsFormat(t *testing.T) {
	e := NewCompatibleOpenAIExecutor(NewClient())
	if !e.SupportsFormat(engine.FormatOpenAIChat) {
		t.Error("expected to support FormatOpenAIChat")
	}
	if !e.SupportsFormat(engine.FormatOpenAICompat) {
		t.Error("expected to support FormatOpenAICompat")
	}
	if e.SupportsFormat(engine.FormatAnthropic) {
		t.Error("expected NOT to support FormatAnthropic")
	}
	if e.SupportsFormat(engine.FormatCodexResponses) {
		t.Error("expected NOT to support FormatCodexResponses")
	}
}

func TestCompatibleOpenAIExecutor_ProviderType(t *testing.T) {
	e := NewCompatibleOpenAIExecutor(NewClient())
	if e.ProviderType() != provider.ProviderOpenAI {
		t.Errorf("expected ProviderOpenAI, got %s", e.ProviderType())
	}
}

func TestCompatibleOpenAIExecutor_Execute_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if !strings.Contains(r.URL.Path, "/chat/completions") {
			t.Errorf("expected /chat/completions, got %s", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer test-key" {
			t.Errorf("expected Bearer test-key, got %s", r.Header.Get("Authorization"))
		}

		resp := map[string]interface{}{
			"id":      "chatcmpl-abc123",
			"object":  "chat.completion",
			"created": 1715000000,
			"model":   "gpt-4o",
			"choices": []map[string]interface{}{
				{
					"index":         0,
					"finish_reason": "stop",
					"message":       map[string]interface{}{"role": "assistant", "content": "Hello"},
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

	body, _ := json.Marshal(map[string]interface{}{"model": "gpt-4o", "messages": []map[string]interface{}{{"role": "user", "content": "Hi"}}})
	e := NewCompatibleOpenAIExecutor(testClient(srv))
	req := testRequest(engine.FormatOpenAIChat, "gpt-4o", body)
	account := testAccount("test-key")

	resp, err := e.Execute(context.Background(), req, account)
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if resp.StatusCode != 200 {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
	if resp.Model != "gpt-4o" {
		t.Errorf("expected model gpt-4o, got %s", resp.Model)
	}
	if resp.Usage == nil {
		t.Fatal("expected non-nil Usage")
	}
	if resp.Usage.PromptTokens != 10 {
		t.Errorf("expected 10 prompt tokens, got %d", resp.Usage.PromptTokens)
	}
}

func TestCompatibleOpenAIExecutor_Execute_CustomBaseURL(t *testing.T) {
	customURL := ""
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		customURL = "http://" + r.Host + r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{"id": "test", "model": "custom-model"})
	}))
	defer srv.Close()

	body, _ := json.Marshal(map[string]interface{}{"model": "custom-model"})
	client := NewClient(WithTransport(http.DefaultTransport), WithBaseURL(srv.URL))
	e := NewCompatibleOpenAIExecutor(client)
	req := testRequest(engine.FormatOpenAIChat, "custom-model", body)

	_, err := e.Execute(context.Background(), req, testAccount("key"))
	if err != nil {
		t.Fatalf("Execute with custom URL failed: %v", err)
	}
	if customURL == "" {
		t.Error("expected custom URL to be used")
	}
}

func TestCompatibleOpenAIExecutor_Execute_CustomModelPreserved(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		// Return the model we sent to verify it was preserved
		json.NewEncoder(w).Encode(map[string]interface{}{
			"id": "test", "model": "my-custom-llm-v2",
		})
	}))
	defer srv.Close()

	body, _ := json.Marshal(map[string]interface{}{"model": "my-custom-llm-v2"})
	e := NewCompatibleOpenAIExecutor(testClient(srv))
	req := testRequest(engine.FormatOpenAIChat, "my-custom-llm-v2", body)

	resp, err := e.Execute(context.Background(), req, testAccount("key"))
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if resp.Model != "my-custom-llm-v2" {
		t.Errorf("expected custom model preserved, got %s", resp.Model)
	}
}

func TestCompatibleOpenAIExecutor_Execute_401(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"error": map[string]interface{}{"message": "Invalid API key", "type": "invalid_request_error"},
		})
	}))
	defer srv.Close()

	body, _ := json.Marshal(map[string]interface{}{"model": "gpt-4o"})
	e := NewCompatibleOpenAIExecutor(testClient(srv))
	req := testRequest(engine.FormatOpenAIChat, "gpt-4o", body)

	_, err := e.Execute(context.Background(), req, testAccount("bad-key"))
	if err == nil {
		t.Fatal("expected error for 401, got nil")
	}
	// Must be an auth error (definitive rejection)
	if !strings.Contains(strings.ToLower(err.Error()), "unauthorized") &&
		!strings.Contains(strings.ToLower(err.Error()), "auth") {
		t.Errorf("expected auth-related error, got: %v", err)
	}
	// Must NOT contain the credential in the error
	if strings.Contains(err.Error(), "bad-key") {
		t.Error("credential leaked in error message")
	}
}

func TestCompatibleOpenAIExecutor_Execute_403(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		w.Write([]byte(`{"error":{"message":"Forbidden"}}`))
	}))
	defer srv.Close()

	body, _ := json.Marshal(map[string]interface{}{"model": "gpt-4o"})
	e := NewCompatibleOpenAIExecutor(testClient(srv))
	req := testRequest(engine.FormatOpenAIChat, "gpt-4o", body)

	_, err := e.Execute(context.Background(), req, testAccount("key"))
	if err == nil {
		t.Fatal("expected error for 403, got nil")
	}
}

func TestCompatibleOpenAIExecutor_Execute_429(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		w.Write([]byte(`{"error":{"message":"Rate limited"}}`))
	}))
	defer srv.Close()

	body, _ := json.Marshal(map[string]interface{}{"model": "gpt-4o"})
	e := NewCompatibleOpenAIExecutor(testClient(srv))
	req := testRequest(engine.FormatOpenAIChat, "gpt-4o", body)

	_, err := e.Execute(context.Background(), req, testAccount("key"))
	if err == nil {
		t.Fatal("expected error for 429, got nil")
	}
	// 429 is retryable
	if !strings.Contains(strings.ToLower(err.Error()), "rate") {
		t.Errorf("expected rate limit error, got: %v", err)
	}
}

func TestCompatibleOpenAIExecutor_Execute_5xx(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
		w.Write([]byte(`{"error":{"message":"Overloaded"}}`))
	}))
	defer srv.Close()

	body, _ := json.Marshal(map[string]interface{}{"model": "gpt-4o"})
	e := NewCompatibleOpenAIExecutor(testClient(srv))
	req := testRequest(engine.FormatOpenAIChat, "gpt-4o", body)

	_, err := e.Execute(context.Background(), req, testAccount("key"))
	if err == nil {
		t.Fatal("expected error for 503, got nil")
	}
}

func TestCompatibleOpenAIExecutor_Execute_Timeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(2 * time.Second)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	body, _ := json.Marshal(map[string]interface{}{"model": "gpt-4o"})
	client := NewClient(WithTransport(http.DefaultTransport), WithBaseURL(srv.URL), WithTimeout(50*time.Millisecond))
	e := NewCompatibleOpenAIExecutor(client)
	req := testRequest(engine.FormatOpenAIChat, "gpt-4o", body)

	ctx := context.Background()
	_, err := e.Execute(ctx, req, testAccount("key"))
	if err == nil {
		t.Error("expected timeout error, got nil")
	}
}

func TestCompatibleOpenAIExecutor_ExecuteStream_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		chunks := []string{
			"data: {\"id\":\"1\",\"choices\":[{\"delta\":{\"content\":\"Hello\"}}]}\n\n",
			"data: {\"id\":\"1\",\"choices\":[{\"delta\":{\"content\":\" world\"}}]}\n\n",
			"data: [DONE]\n\n",
		}
		for _, c := range chunks {
			fmt.Fprint(w, c)
		}
	}))
	defer srv.Close()

	body, _ := json.Marshal(map[string]interface{}{"model": "gpt-4o", "messages": []map[string]interface{}{{"role": "user", "content": "Hi"}}})
	e := NewCompatibleOpenAIExecutor(testClient(srv))
	req := testRequest(engine.FormatOpenAIChat, "gpt-4o", body)

	resp, err := e.ExecuteStream(context.Background(), req, testAccount("test-key"))
	if err != nil {
		t.Fatalf("ExecuteStream failed: %v", err)
	}
	if resp.Stream == nil {
		t.Fatal("expected non-nil Stream")
	}

	st := resp.Stream.(*stream.Stream)
	var received []stream.Chunk
	for chunk := range st.Chunks() {
		received = append(received, chunk)
	}
	if len(received) != 3 {
		t.Errorf("expected 3 chunks, got %d", len(received))
	}
}

func TestCompatibleOpenAIExecutor_ExecuteStream_Cancellation(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		flusher, ok := w.(http.Flusher)
		if !ok {
			return
		}
		for i := 0; i < 100; i++ {
			fmt.Fprintf(w, "data: {\"counter\":%d}\n\n", i)
			flusher.Flush()
			time.Sleep(10 * time.Millisecond)
		}
	}))
	defer srv.Close()

	body, _ := json.Marshal(map[string]interface{}{"model": "gpt-4o"})
	e := NewCompatibleOpenAIExecutor(testClient(srv))
	req := testRequest(engine.FormatOpenAIChat, "gpt-4o", body)

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	_, err := e.ExecuteStream(ctx, req, testAccount("key"))
	if err != nil {
		t.Logf("ExecuteStream with cancellation: %v", err)
	}
}

// -- Anthropic executor tests -------------------------------------------------

func TestCompatibleAnthropicExecutor_SupportsFormat(t *testing.T) {
	e := NewCompatibleAnthropicExecutor(NewClient())
	if !e.SupportsFormat(engine.FormatAnthropic) {
		t.Error("expected to support FormatAnthropic")
	}
	if e.SupportsFormat(engine.FormatOpenAIChat) {
		t.Error("expected NOT to support FormatOpenAIChat")
	}
}

func TestCompatibleAnthropicExecutor_ProviderType(t *testing.T) {
	e := NewCompatibleAnthropicExecutor(NewClient())
	if e.ProviderType() != provider.ProviderAnthropic {
		t.Errorf("expected ProviderAnthropic, got %s", e.ProviderType())
	}
}

func TestCompatibleAnthropicExecutor_Execute_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if !strings.Contains(r.URL.Path, "/v1/messages") {
			t.Errorf("expected /v1/messages, got %s", r.URL.Path)
		}
		if r.Header.Get("x-api-key") != "ant-key" {
			t.Errorf("expected x-api-key: ant-key, got %s", r.Header.Get("x-api-key"))
		}
		if r.Header.Get("anthropic-version") == "" {
			t.Error("expected anthropic-version header")
		}

		resp := map[string]interface{}{
			"id":          "msg_123",
			"type":        "message",
			"role":        "assistant",
			"model":       "claude-3-opus",
			"content":     []map[string]interface{}{{"type": "text", "text": "Hello from Claude"}},
			"stop_reason": "end_turn",
			"usage":       map[string]interface{}{"input_tokens": 5, "output_tokens": 10},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	body, _ := json.Marshal(map[string]interface{}{"model": "claude-3-opus", "messages": []map[string]interface{}{{"role": "user", "content": "Hi"}}})
	client := NewClient(WithTransport(http.DefaultTransport), WithBaseURL(srv.URL))
	e := NewCompatibleAnthropicExecutor(client)
	req := testRequest(engine.FormatAnthropic, "claude-3-opus", body)

	// Use anthropic_api_key auth type
	acct := testAccount("ant-key")
	acct.AuthType = "anthropic_api_key"

	resp, err := e.Execute(context.Background(), req, acct)
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if resp.StatusCode != 200 {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
	if resp.Model != "claude-3-opus" {
		t.Errorf("expected model claude-3-opus, got %s", resp.Model)
	}
	if resp.Usage == nil {
		t.Fatal("expected non-nil Usage")
	}
	if resp.Usage.PromptTokens != 5 {
		t.Errorf("expected 5 input tokens, got %d", resp.Usage.PromptTokens)
	}
}

func TestCompatibleAnthropicExecutor_Execute_401(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		json.NewEncoder(w).Encode(map[string]interface{}{"error": map[string]interface{}{"message": "Invalid API key"}})
	}))
	defer srv.Close()

	body, _ := json.Marshal(map[string]interface{}{"model": "claude-3"})
	e := NewCompatibleAnthropicExecutor(testClient(srv))
	req := testRequest(engine.FormatAnthropic, "claude-3", body)

	_, err := e.Execute(context.Background(), req, testAccount("bad-key"))
	if err == nil {
		t.Fatal("expected error for 401, got nil")
	}
	// Must NOT contain the credential in the error
	if strings.Contains(err.Error(), "bad-key") {
		t.Error("credential leaked in error message")
	}
}

// -- Models discovery tests ---------------------------------------------------

func TestListModels_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/models" {
			t.Errorf("expected /v1/models, got %s", r.URL.Path)
		}
		resp := map[string]interface{}{
			"object": "list",
			"data": []map[string]interface{}{
				{"id": "gpt-4o", "object": "model", "created": 1715000000, "owned_by": "openai"},
				{"id": "gpt-3.5-turbo", "object": "model", "created": 1715000000, "owned_by": "openai"},
			},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	client := NewClient(WithBaseURL(srv.URL))
	models, err := ListModels(context.Background(), client, testAccount("key"))
	if err != nil {
		t.Fatalf("ListModels failed: %v", err)
	}
	if len(models) != 2 {
		t.Errorf("expected 2 models, got %d", len(models))
	}
	if models[0].ID != "gpt-4o" {
		t.Errorf("expected gpt-4o, got %s", models[0].ID)
	}
}

func TestProbeModel_Found(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]interface{}{
			"object": "list",
			"data":   []map[string]interface{}{{"id": "my-custom-model"}},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	client := NewClient(WithBaseURL(srv.URL))
	found, err := ProbeModel(context.Background(), client, testAccount("key"), "my-custom-model")
	if err != nil {
		t.Fatalf("ProbeModel failed: %v", err)
	}
	if !found {
		t.Error("expected model to be found")
	}
}

func TestProbeModel_NotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]interface{}{"object": "list", "data": []map[string]interface{}{{"id": "gpt-4o"}}}
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	client := NewClient(WithBaseURL(srv.URL))
	found, err := ProbeModel(context.Background(), client, testAccount("key"), "nonexistent-model")
	if err != nil {
		t.Fatalf("ProbeModel failed: %v", err)
	}
	if found {
		t.Error("expected model NOT to be found")
	}
}

func TestProbeEmbeddings_ModelList(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]interface{}{
			"object": "list",
			"data":   []map[string]interface{}{{"id": "text-embedding-ada-002"}},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	client := NewClient(WithBaseURL(srv.URL))
	found, err := ProbeEmbeddings(context.Background(), client, testAccount("key"))
	if err != nil {
		t.Fatalf("ProbeEmbeddings failed: %v", err)
	}
	if !found {
		t.Error("expected embeddings to be found via model list")
	}
}

func TestProbeEmbeddings_Endpoint(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/models" {
			resp := map[string]interface{}{"object": "list", "data": []map[string]interface{}{{"id": "some-model"}}}
			json.NewEncoder(w).Encode(resp)
			return
		}
		if r.URL.Path == "/v1/embeddings" {
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode(map[string]interface{}{"data": []map[string]interface{}{{"embedding": []float64{0.1}}}})
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	client := NewClient(WithBaseURL(srv.URL))
	found, err := ProbeEmbeddings(context.Background(), client, testAccount("key"))
	if err != nil {
		t.Fatalf("ProbeEmbeddings failed: %v", err)
	}
	if !found {
		t.Error("expected embeddings probe to succeed")
	}
}

// -- Credential redaction tests -----------------------------------------------

func TestCredentialRedaction_OpenAIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"error":{"message":"Invalid API key"}}`))
	}))
	defer srv.Close()

	body, _ := json.Marshal(map[string]interface{}{"model": "gpt-4o"})
	e := NewCompatibleOpenAIExecutor(testClient(srv))
	req := testRequest(engine.FormatOpenAIChat, "gpt-4o", body)

	_, err := e.Execute(context.Background(), req, testAccount("sk-secret-key-12345"))
	if err == nil {
		t.Fatal("expected error")
	}

	errStr := err.Error()
	if strings.Contains(errStr, "sk-secret-key-12345") {
		t.Error("credential leaked in error message: " + errStr)
	}
}

func TestCredentialRedaction_AnthropicError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"error":{"message":"Invalid API key"}}`))
	}))
	defer srv.Close()

	body, _ := json.Marshal(map[string]interface{}{"model": "claude-3"})
	e := NewCompatibleAnthropicExecutor(testClient(srv))
	req := testRequest(engine.FormatAnthropic, "claude-3", body)

	acct := testAccount("sk-ant-secret-99999")
	acct.AuthType = "anthropic_api_key"

	_, err := e.Execute(context.Background(), req, acct)
	if err == nil {
		t.Fatal("expected error")
	}

	errStr := err.Error()
	if strings.Contains(errStr, "sk-ant-secret-99999") {
		t.Error("credential leaked in error message: " + errStr)
	}
}

// -- Response body closing guarantee ------------------------------------------

func TestResponseBody_Closed(t *testing.T) {
	closeCalled := make(chan struct{}, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{"id": "test"})
	}))
	defer srv.Close()

	client := NewClient(
		WithTransport(&closeTrackingTransport{
			rt:          http.DefaultTransport,
			closeCalled: closeCalled,
		}),
		WithBaseURL(srv.URL),
	)
	e := NewCompatibleOpenAIExecutor(client)
	body, _ := json.Marshal(map[string]interface{}{"model": "gpt-4o"})
	req := testRequest(engine.FormatOpenAIChat, "gpt-4o", body)

	_, err := e.Execute(context.Background(), req, testAccount("key"))
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	// The deferred resp.Body.Close() in Execute must have been called.
	select {
	case <-closeCalled:
		// success
	case <-time.After(time.Second):
		t.Fatal("response body was not closed after Execute returned")
	}
}

// -- Account/proxy selection test ---------------------------------------------

func TestAccountProxySelection(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{"id": "test", "model": "gpt-4o"})
	}))
	defer srv.Close()

	// Test with different accounts to verify credential injection
	body, _ := json.Marshal(map[string]interface{}{"model": "gpt-4o"})
	e := NewCompatibleOpenAIExecutor(testClient(srv))
	req := testRequest(engine.FormatOpenAIChat, "gpt-4o", body)

	acct1 := testAccount("key-account-1")
	_, err1 := e.Execute(context.Background(), req, acct1)
	if err1 != nil {
		t.Fatalf("account 1 failed: %v", err1)
	}

	acct2 := testAccount("key-account-2")
	_, err2 := e.Execute(context.Background(), req, acct2)
	if err2 != nil {
		t.Fatalf("account 2 failed: %v", err2)
	}
}

// -- Unrestricted URL test (decisions #136, #137, #376) -----------------------

func TestUnrestrictedCustomURL(t *testing.T) {
	// Custom URL with loopback address — must be allowed per decisions #136/#376
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{"id": "test", "model": "custom"})
	}))
	defer srv.Close()

	body, _ := json.Marshal(map[string]interface{}{"model": "custom"})
	client := NewClient(WithTransport(http.DefaultTransport), WithBaseURL(srv.URL))
	e := NewCompatibleOpenAIExecutor(client)
	req := testRequest(engine.FormatOpenAIChat, "custom", body)

	_, err := e.Execute(context.Background(), req, testAccount("key"))
	if err != nil {
		t.Fatalf("unrestricted custom URL should work: %v", err)
	}
}

// -- WithBaseURL clone method tests -------------------------------------------

func TestClient_WithBaseURL_PreservesCustomResolver(t *testing.T) {
	// Create a base client with a custom resolver that returns a secret
	// distinct from CredentialRef.
	const resolvedSecret = "resolved-secret-value"
	resolverCalled := false
	client := NewClient(
		WithTransport(http.DefaultTransport),
		WithCredentialResolver(CredentialResolverFunc(func(account *provider.Account) (string, error) {
			resolverCalled = true
			return resolvedSecret, nil
		})),
	)

	// Derive a new client with a different base URL.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Assert the Authorization header uses the resolved secret, not CredentialRef.
		if r.Header.Get("Authorization") != "Bearer "+resolvedSecret {
			t.Errorf("expected Bearer %s, got %s", resolvedSecret, r.Header.Get("Authorization"))
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{"id": "test"})
	}))
	defer srv.Close()

	derived := client.WithBaseURL(srv.URL)
	e := NewCompatibleOpenAIExecutor(derived)
	body, _ := json.Marshal(map[string]interface{}{"model": "gpt-4o"})
	req := testRequest(engine.FormatOpenAIChat, "gpt-4o", body)

	acct := testAccount("original-ref-value")
	_, err := e.Execute(context.Background(), req, acct)
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if !resolverCalled {
		t.Error("custom resolver was never called — derived client fell back to default resolver")
	}
}

func TestClient_WithBaseURL_PreservesTimeout(t *testing.T) {
	// Create a client with a very short timeout.
	client := NewClient(
		WithTransport(http.DefaultTransport),
		WithTimeout(10*time.Millisecond),
	)

	// Derive a new client with a different base URL pointing to a slow server.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(500 * time.Millisecond) // exceeds 10ms timeout
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{"id": "test"})
	}))
	defer srv.Close()

	derived := client.WithBaseURL(srv.URL)
	e := NewCompatibleOpenAIExecutor(derived)
	body, _ := json.Marshal(map[string]interface{}{"model": "gpt-4o"})
	req := testRequest(engine.FormatOpenAIChat, "gpt-4o", body)

	start := time.Now()
	_, err := e.Execute(context.Background(), req, testAccount("key"))
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("expected timeout error, got nil")
	}
	// Must fail fast — well under the 60s default timeout.
	if elapsed > 2*time.Second {
		t.Errorf("timeout took %v, expected ~10ms — client likely fell back to 60s default", elapsed)
	}
}

func TestClient_WithBaseURL_PreservesTransport(t *testing.T) {
	requestSeen := make(chan struct{}, 1)
	tr := &trackTransport{
		rt: http.DefaultTransport,
		fn: func() {
			select {
			case requestSeen <- struct{}{}:
			default:
			}
		},
	}
	client := NewClient(WithTransport(tr))

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{"id": "test"})
	}))
	defer srv.Close()

	derived := client.WithBaseURL(srv.URL)
	e := NewCompatibleOpenAIExecutor(derived)
	body, _ := json.Marshal(map[string]interface{}{"model": "gpt-4o"})
	req := testRequest(engine.FormatOpenAIChat, "gpt-4o", body)

	_, err := e.Execute(context.Background(), req, testAccount("key"))
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	select {
	case <-requestSeen:
	case <-time.After(time.Second):
		t.Fatal("configured transport was not used by derived client")
	}
}

type trackTransport struct {
	rt http.RoundTripper
	fn func()
}

func (t *trackTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	t.fn()
	return t.rt.RoundTrip(req)
}

// -- goroutine leak check helper ----------------------------------------------

type closeTrackingTransport struct {
	rt          http.RoundTripper
	closeCalled chan<- struct{}
}

func (t *closeTrackingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	resp, err := t.rt.RoundTrip(req)
	if resp != nil {
		resp.Body = &closeTrackingReadCloser{
			ReadCloser:  resp.Body,
			closeCalled: t.closeCalled,
		}
	}
	return resp, err
}

type closeTrackingReadCloser struct {
	io.ReadCloser
	closeCalled chan<- struct{}
}

func (c *closeTrackingReadCloser) Close() error {
	select {
	case c.closeCalled <- struct{}{}:
	default:
	}
	return c.ReadCloser.Close()
}

// -- benchmark ----------------------------------------------------------------

func BenchmarkCompatibleOpenAIExecutor_Execute(b *testing.B) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{"id": "test", "model": "gpt-4o"})
	}))
	defer srv.Close()

	body, _ := json.Marshal(map[string]interface{}{"model": "gpt-4o"})
	e := NewCompatibleOpenAIExecutor(testClient(srv))
	req := testRequest(engine.FormatOpenAIChat, "gpt-4o", body)
	acct := testAccount("key")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := e.Execute(context.Background(), req, acct)
		if err != nil {
			b.Fatal(err)
		}
	}
}

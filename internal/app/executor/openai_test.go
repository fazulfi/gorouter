package executor

import (
	"context"
	"encoding/json"
	"fmt"
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

func TestOpenAIChatExecutor_SupportsFormat(t *testing.T) {
	e := NewOpenAIChatExecutor(http.DefaultTransport)
	if !e.SupportsFormat(engine.FormatOpenAIChat) {
		t.Error("expected to support FormatOpenAIChat")
	}
	if !e.SupportsFormat(engine.FormatOpenAICompat) {
		t.Error("expected to support FormatOpenAICompat")
	}
	if e.SupportsFormat(engine.FormatCodexResponses) {
		t.Error("expected NOT to support FormatCodexResponses")
	}
}

func TestOpenAIChatExecutor_ProviderType(t *testing.T) {
	e := NewOpenAIChatExecutor(http.DefaultTransport)
	if e.ProviderType() != provider.ProviderOpenAI {
		t.Errorf("expected ProviderOpenAI, got %s", e.ProviderType())
	}
}

func TestOpenAIChatExecutor_Execute_Success(t *testing.T) {
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
		if r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("expected application/json, got %s", r.Header.Get("Content-Type"))
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
					"message": map[string]interface{}{
						"role":    "assistant",
						"content": "Hello, world!",
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
		"model":    "gpt-4o",
		"messages": []map[string]interface{}{{"role": "user", "content": "Hi"}},
	})

	e := NewOpenAIChatExecutor(http.DefaultTransport)
	req := newTestRequest(engine.FormatOpenAIChat, "gpt-4o", body)
	e.SetBaseURL(srv.URL)

	account := newTestAccount("test-key")
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
	if resp.Usage.CompletionTokens != 20 {
		t.Errorf("expected 20 completion tokens, got %d", resp.Usage.CompletionTokens)
	}
	if resp.Usage.TotalTokens != 30 {
		t.Errorf("expected 30 total tokens, got %d", resp.Usage.TotalTokens)
	}
	if len(resp.Body) == 0 {
		t.Error("expected non-empty response body")
	}
}

func TestOpenAIChatExecutor_Execute_Error4xx(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"error": map[string]interface{}{
				"message": "Invalid API key",
				"type":    "invalid_request_error",
			},
		})
	}))
	defer srv.Close()

	body, _ := json.Marshal(map[string]interface{}{"model": "gpt-4o"})
	e := NewOpenAIChatExecutor(http.DefaultTransport)
	req := newTestRequest(engine.FormatOpenAIChat, "gpt-4o", body)
	e.SetBaseURL(srv.URL)

	_, err := e.Execute(context.Background(), req, newTestAccount("bad-key"))
	if err == nil {
		t.Fatal("expected error for 401, got nil")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "auth") &&
		!strings.Contains(strings.ToLower(err.Error()), "unauthorized") &&
		!strings.Contains(strings.ToLower(err.Error()), "401") {
		t.Errorf("expected auth-related error, got: %v", err)
	}
}

func TestOpenAIChatExecutor_Execute_Error5xx(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
		w.Write([]byte(`{"error":{"message":"Overloaded"}}`))
	}))
	defer srv.Close()

	body, _ := json.Marshal(map[string]interface{}{"model": "gpt-4o"})
	e := NewOpenAIChatExecutor(http.DefaultTransport)
	req := newTestRequest(engine.FormatOpenAIChat, "gpt-4o", body)
	e.SetBaseURL(srv.URL)

	_, err := e.Execute(context.Background(), req, newTestAccount("key"))
	if err == nil {
		t.Fatal("expected error for 503, got nil")
	}
}

func TestOpenAIChatExecutor_ExecuteStream_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		chunks := []string{
			"event: chat.completion.chunk\ndata: {\"id\":\"1\",\"choices\":[{\"delta\":{\"content\":\"Hello\"}}]}\n\n",
			"event: chat.completion.chunk\ndata: {\"id\":\"1\",\"choices\":[{\"delta\":{\"content\":\" world\"}}]}\n\n",
			"data: [DONE]\n\n",
		}
		for _, c := range chunks {
			fmt.Fprint(w, c)
		}
	}))
	defer srv.Close()

	body, _ := json.Marshal(map[string]interface{}{
		"model":    "gpt-4o",
		"messages": []map[string]interface{}{{"role": "user", "content": "Hi"}},
	})

	e := NewOpenAIChatExecutor(http.DefaultTransport)
	req := newTestRequest(engine.FormatOpenAIChat, "gpt-4o", body)
	e.SetBaseURL(srv.URL)

	account := newTestAccount("test-key")
	resp, err := e.ExecuteStream(context.Background(), req, account)
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
	if string(received[0].Data) != `{"id":"1","choices":[{"delta":{"content":"Hello"}}]}` {
		t.Errorf("unexpected first chunk data: %s", string(received[0].Data))
	}
}

func TestOpenAIChatExecutor_ExecuteStream_EmptyBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer srv.Close()

	body, _ := json.Marshal(map[string]interface{}{"model": "gpt-4o"})
	e := NewOpenAIChatExecutor(http.DefaultTransport)
	req := newTestRequest(engine.FormatOpenAIChat, "gpt-4o", body)
	e.SetBaseURL(srv.URL)

	account := newTestAccount("key")
	resp, err := e.ExecuteStream(context.Background(), req, account)
	if err != nil {
		t.Fatalf("ExecuteStream failed: %v", err)
	}

	st := resp.Stream.(*stream.Stream)
	var count int
	for range st.Chunks() {
		count++
	}
	if count != 1 {
		t.Errorf("expected 1 chunk for [DONE] only stream, got %d", count)
	}
}

func TestOpenAIChatExecutor_ExecuteStream_Cancellation(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		for i := 0; i < 100; i++ {
			fmt.Fprintf(w, "data: {\"counter\":%d}\n\n", i)
			w.(http.Flusher).Flush()
			time.Sleep(10 * time.Millisecond)
		}
	}))
	defer srv.Close()

	body, _ := json.Marshal(map[string]interface{}{"model": "gpt-4o"})
	e := NewOpenAIChatExecutor(http.DefaultTransport)
	req := newTestRequest(engine.FormatOpenAIChat, "gpt-4o", body)
	e.SetBaseURL(srv.URL)

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	_, err := e.ExecuteStream(ctx, req, newTestAccount("key"))
	if err != nil {
		t.Logf("ExecuteStream with cancellation: %v", err)
	}
}

func TestOpenAIChatExecutor_SupportsFormat_Table(t *testing.T) {
	tests := []struct {
		format engine.RequestFormat
		want   bool
	}{
		{engine.FormatOpenAIChat, true},
		{engine.FormatOpenAICompat, true},
		{engine.FormatCodexResponses, false},
		{engine.FormatAnthropic, false},
		{engine.FormatGemini, false},
	}

	e := NewOpenAIChatExecutor(http.DefaultTransport)
	for _, tt := range tests {
		t.Run(string(tt.format), func(t *testing.T) {
			got := e.SupportsFormat(tt.format)
			if got != tt.want {
				t.Errorf("SupportsFormat(%s) = %v, want %v", tt.format, got, tt.want)
			}
		})
	}
}

func TestOpenAIChatExecutor_ExecuteStream_NoDuplicateChunks(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		data := "data: {\"chunk\":1}\n\ndata: {\"chunk\":2}\n\ndata: [DONE]\n\n"
		w.Write([]byte(data))
	}))
	defer srv.Close()

	body, _ := json.Marshal(map[string]interface{}{"model": "gpt-4o"})
	e := NewOpenAIChatExecutor(http.DefaultTransport)
	req := newTestRequest(engine.FormatOpenAIChat, "gpt-4o", body)
	e.SetBaseURL(srv.URL)

	resp, err := e.ExecuteStream(context.Background(), req, newTestAccount("key"))
	if err != nil {
		t.Fatalf("ExecuteStream failed: %v", err)
	}

	st := resp.Stream.(*stream.Stream)
	var received []stream.Chunk
	for ch := range st.Chunks() {
		received = append(received, ch)
	}

	if len(received) != 3 {
		t.Errorf("expected 3 chunks, got %d", len(received))
	}
}

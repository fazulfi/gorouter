package executor

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"gorouter/internal/domain/engine"
	"gorouter/internal/domain/engine/stream"
	"gorouter/internal/domain/provider"

	"github.com/google/uuid"
)

func TestCodexExecutor_SupportsFormat(t *testing.T) {
	e := NewCodexExecutor(http.DefaultTransport)
	if !e.SupportsFormat(engine.FormatCodexResponses) {
		t.Error("expected to support FormatCodexResponses")
	}
	if e.SupportsFormat(engine.FormatOpenAIChat) {
		t.Error("expected NOT to support FormatOpenAIChat")
	}
	if e.SupportsFormat(engine.FormatAnthropic) {
		t.Error("expected NOT to support FormatAnthropic")
	}
}

func TestCodexExecutor_ProviderType(t *testing.T) {
	e := NewCodexExecutor(http.DefaultTransport)
	if e.ProviderType() != ProviderCodex {
		t.Errorf("expected ProviderCodex, got %s", e.ProviderType())
	}
}

func TestCodexExecutor_Execute_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if !strings.Contains(r.URL.Path, "/responses") {
			t.Errorf("expected /responses, got %s", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer codex-key" {
			t.Errorf("expected Bearer codex-key, got %s", r.Header.Get("Authorization"))
		}
		if r.Header.Get("session_id") == "" {
			t.Error("expected session_id header")
		}
		if r.Header.Get("originator") != "gorouter" {
			t.Errorf("expected originator gorouter, got %s", r.Header.Get("originator"))
		}

		resp := map[string]interface{}{
			"id":      "resp_abc123",
			"object":  "response",
			"created": 1715000000,
			"status":  "completed",
			"model":   "gpt-4o-codex",
			"output":  []interface{}{},
			"usage": map[string]interface{}{
				"input_tokens":  10,
				"output_tokens": 20,
				"total_tokens":  30,
			},
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	body, _ := json.Marshal(map[string]interface{}{
		"model":    "gpt-4o-codex",
		"input":    "Hello",
		"stream":   false,
	})

	e := NewCodexExecutor(http.DefaultTransport)
	req := &engine.Request{
		ID:         uuid.New(),
		Format:     engine.FormatCodexResponses,
		Model:      "gpt-4o-codex",
		RawBody:    body,
		MappedBody: body,
		Headers:    map[string]string{"X-Base-URL": srv.URL},
	}

	account := newTestAccount("codex-key")
	resp, err := e.Execute(context.Background(), req, account)
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

func TestCodexExecutor_ExecuteStream_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)

		events := []string{
			"event: response.created\ndata: {\"type\":\"response.created\",\"response\":{\"id\":\"resp_1\",\"status\":\"in_progress\"}}\n\n",
			"event: response.output_item.added\ndata: {\"type\":\"response.output_item.added\",\"output_index\":0,\"item\":{\"id\":\"msg_1\",\"type\":\"message\",\"role\":\"assistant\"}}\n\n",
			"event: response.output_text.delta\ndata: {\"type\":\"response.output_text.delta\",\"delta\":\"Hello from Codex\"}\n\n",
			"event: response.completed\ndata: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_1\",\"status\":\"completed\"}}\n\n",
			"data: [DONE]\n\n",
		}
		for _, ev := range events {
			fmt.Fprint(w, ev)
		}
	}))
	defer srv.Close()

	body, _ := json.Marshal(map[string]interface{}{
		"model":  "gpt-4o-codex",
		"input":  "Hello",
		"stream": true,
	})

	e := NewCodexExecutor(http.DefaultTransport)
	req := &engine.Request{
		ID:         uuid.New(),
		Format:     engine.FormatCodexResponses,
		Model:      "gpt-4o-codex",
		RawBody:    body,
		MappedBody: body,
		Headers:    map[string]string{"X-Base-URL": srv.URL},
	}

	account := newTestAccount("codex-key")
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

	if len(received) < 3 {
		t.Errorf("expected at least 3 chunks, got %d", len(received))
	}
}

func TestCodexExecutor_ExecuteStream_PeekError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		data := "event: error\ndata: {\"error\":{\"message\":\"server_is_overloaded\"}}\n\n"
		w.Write([]byte(data))
	}))
	defer srv.Close()

	body, _ := json.Marshal(map[string]interface{}{
		"model":  "gpt-4o-codex",
		"input":  "Hello",
		"stream": true,
	})

	e := NewCodexExecutor(http.DefaultTransport)
	req := &engine.Request{
		ID:         uuid.New(),
		Format:     engine.FormatCodexResponses,
		Model:      "gpt-4o-codex",
		RawBody:    body,
		MappedBody: body,
		Headers:    map[string]string{"X-Base-URL": srv.URL},
	}

	_, err := e.ExecuteStream(context.Background(), req, newTestAccount("key"))
	if err == nil {
		t.Fatal("expected error due to peek failure for overloaded SSE, got nil")
	}
	t.Logf("Peek error correctly surfaced: %v", err)
}

func TestCodexExecutor_Execute_Error4xx(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"error": map[string]interface{}{
				"message": "Invalid authentication",
				"type":    "invalid_request_error",
			},
		})
	}))
	defer srv.Close()

	body, _ := json.Marshal(map[string]interface{}{"model": "gpt-4o-codex"})
	e := NewCodexExecutor(http.DefaultTransport)
	req := &engine.Request{
		ID:         uuid.New(),
		Format:     engine.FormatCodexResponses,
		Model:      "gpt-4o-codex",
		RawBody:    body,
		MappedBody: body,
		Headers:    map[string]string{"X-Base-URL": srv.URL},
	}

	_, err := e.Execute(context.Background(), req, newTestAccount("bad-key"))
	if err == nil {
		t.Fatal("expected error for 401, got nil")
	}
}

func TestCodexExecutor_Execute_Error5xx(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
		w.Write([]byte(`{"error":{"message":"Service unavailable"}}`))
	}))
	defer srv.Close()

	body, _ := json.Marshal(map[string]interface{}{"model": "gpt-4o-codex"})
	e := NewCodexExecutor(http.DefaultTransport)
	req := &engine.Request{
		ID:         uuid.New(),
		Format:     engine.FormatCodexResponses,
		Model:      "gpt-4o-codex",
		RawBody:    body,
		MappedBody: body,
		Headers:    map[string]string{"X-Base-URL": srv.URL},
	}

	_, err := e.Execute(context.Background(), req, newTestAccount("key"))
	if err == nil {
		t.Fatal("expected error for 503, got nil")
	}
}

func TestCodexExecutor_transformRequest(t *testing.T) {
	e := NewCodexExecutor(http.DefaultTransport)

	body, _ := json.Marshal(map[string]interface{}{
		"model":            "gpt-4o-codex",
		"input":            "Hello",
		"temperature":      0.7,
		"max_tokens":       100,
		"unsupported_field": "should be stripped",
	})

	payload := e.transformRequest(body, true)

	if payload["model"] != "gpt-4o-codex" {
		t.Errorf("expected model gpt-4o-codex, got %v", payload["model"])
	}
	if payload["stream"] != true {
		t.Errorf("expected stream=true, got %v", payload["stream"])
	}
	if payload["store"] != false {
		t.Errorf("expected store=false, got %v", payload["store"])
	}
	if _, ok := payload["temperature"]; ok {
		t.Error("expected temperature to be stripped")
	}
	if _, ok := payload["max_tokens"]; ok {
		t.Error("expected max_tokens to be stripped")
	}
	if _, ok := payload["unsupported_field"]; ok {
		t.Error("expected unsupported_field to be stripped")
	}
	if _, ok := payload["input"]; !ok {
		t.Fatal("expected input to be present")
	}
	if instr, ok := payload["instructions"].(string); !ok || instr == "" {
		t.Error("expected instructions to be set")
	}
}

func TestCodexExecutor_normaliseInput_String(t *testing.T) {
	e := NewCodexExecutor(http.DefaultTransport)
	payload := map[string]interface{}{
		"input": "Hello",
	}
	e.normaliseInput(payload)

	input, ok := payload["input"].([]interface{})
	if !ok {
		t.Fatalf("expected input to be []interface{}, got %T", payload["input"])
	}
	if len(input) != 1 {
		t.Fatalf("expected 1 item, got %d", len(input))
	}
}

func TestCodexExecutor_normaliseInput_Empty(t *testing.T) {
	e := NewCodexExecutor(http.DefaultTransport)
	payload := map[string]interface{}{}
	e.normaliseInput(payload)

	input, ok := payload["input"].([]interface{})
	if !ok {
		t.Fatalf("expected input to be []interface{}, got %T", payload["input"])
	}
	if len(input) != 1 {
		t.Fatalf("expected 1 item, got %d", len(input))
	}
}

func TestCodexExecutor_normaliseInput_Array(t *testing.T) {
	e := NewCodexExecutor(http.DefaultTransport)
	payload := map[string]interface{}{
		"input": []interface{}{
			map[string]interface{}{"type": "message", "role": "user", "content": "Hi"},
		},
	}
	e.normaliseInput(payload)

	input, ok := payload["input"].([]interface{})
	if !ok {
		t.Fatalf("expected input to be []interface{}, got %T", payload["input"])
	}
	if len(input) != 1 {
		t.Fatalf("expected 1 item, got %d", len(input))
	}
}

func TestCodexExecutor_ExecuteStream_EmptyBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer srv.Close()

	body, _ := json.Marshal(map[string]interface{}{"model": "gpt-4o-codex"})
	e := NewCodexExecutor(http.DefaultTransport)
	req := &engine.Request{
		ID:         uuid.New(),
		Format:     engine.FormatCodexResponses,
		Model:      "gpt-4o-codex",
		RawBody:    body,
		MappedBody: body,
		Headers:    map[string]string{"X-Base-URL": srv.URL},
	}

	resp, err := e.ExecuteStream(context.Background(), req, newTestAccount("key"))
	if err != nil {
		t.Fatalf("ExecuteStream failed: %v", err)
	}

	st := resp.Stream.(*stream.Stream)
	var count int
	for range st.Chunks() {
		count++
	}
	if count != 0 {
		t.Errorf("expected 0 chunks for [DONE] only stream, got %d", count)
	}
}

func TestCodexExecutor_SupportsFormat_Table(t *testing.T) {
	tests := []struct {
		format engine.RequestFormat
		want   bool
	}{
		{engine.FormatCodexResponses, true},
		{engine.FormatOpenAIChat, false},
		{engine.FormatOpenAICompat, false},
		{engine.FormatAnthropic, false},
		{engine.FormatGemini, false},
	}

	e := NewCodexExecutor(http.DefaultTransport)
	for _, tt := range tests {
		t.Run(string(tt.format), func(t *testing.T) {
			got := e.SupportsFormat(tt.format)
			if got != tt.want {
				t.Errorf("SupportsFormat(%s) = %v, want %v", tt.format, got, tt.want)
			}
		})
	}
}

func TestFactory_GetExecutor(t *testing.T) {
	f := NewFactory(http.DefaultTransport)

	tests := []struct {
		name    string
		ptype   string
		format  engine.RequestFormat
		wantOK  bool
		wantTyp string
	}{
		{
			name:    "openai chat format uses OpenAIChatExecutor",
			ptype:   "openai",
			format:  engine.FormatOpenAIChat,
			wantOK:  true,
			wantTyp: "*executor.OpenAIChatExecutor",
		},
		{
			name:    "openai compat format uses OpenAIChatExecutor",
			ptype:   "openai",
			format:  engine.FormatOpenAICompat,
			wantOK:  true,
			wantTyp: "*executor.OpenAIChatExecutor",
		},
		{
			name:    "codex responses format uses CodexExecutor",
			ptype:   "openai",
			format:  engine.FormatCodexResponses,
			wantOK:  true,
			wantTyp: "*executor.CodexExecutor",
		},
		{
			name:    "custom provider with no format match",
			ptype:   "custom",
			format:  engine.FormatAnthropic,
			wantOK:  false,
			wantTyp: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			exec, ok := f.GetExecutor(provider.ProviderType(tt.ptype), tt.format)
			if ok != tt.wantOK {
				t.Errorf("GetExecutor() ok = %v, want %v", ok, tt.wantOK)
			}
			if tt.wantOK && exec == nil {
				t.Fatal("expected non-nil executor when ok=true")
			}
			if tt.wantOK {
				gotTyp := fmt.Sprintf("%T", exec)
				if gotTyp != tt.wantTyp {
					t.Errorf("expected type %s, got %s", tt.wantTyp, gotTyp)
				}
			}
		})
	}
}

func TestFactory_Supports(t *testing.T) {
	f := NewFactory(http.DefaultTransport)

	if !f.Supports(provider.ProviderOpenAI, engine.FormatOpenAIChat) {
		t.Error("expected Supports=true for openai + chat format")
	}
	if !f.Supports(ProviderCodex, engine.FormatCodexResponses) {
		t.Error("expected Supports=true for codex + responses format")
	}
	if f.Supports("unknown", engine.FormatAnthropic) {
		t.Error("expected Supports=false for unknown provider + anthropic format")
	}
}

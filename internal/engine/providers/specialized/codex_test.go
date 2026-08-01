package specialized

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"gorouter/internal/domain/engine"
	"gorouter/internal/domain/engine/stream"
	"gorouter/internal/domain/provider"
)

func codexTestAccount() *provider.Account {
	return &provider.Account{
		ID:            uuid.MustParse("20000000-0000-0000-0000-000000000001"),
		CredentialRef: "test-key",
		AuthType:      "oauth",
	}
}

func codexTestRequest() *engine.Request {
	return &engine.Request{
		ID:     uuid.MustParse("30000000-0000-0000-0000-000000000001"),
		Model:  "gpt-5-codex",
		Format: engine.FormatCodexResponses,
		RawBody: []byte(`{
			"model": "gpt-5-codex",
			"input": "hello",
			"instructions": "You are a helpful assistant.",
			"store": true
		}`),
	}
}

func TestCodexExecutor_SupportsFormat(t *testing.T) {
	e := NewCodexExecutor(nil)
	if !e.SupportsFormat(engine.FormatCodexResponses) {
		t.Error("expected FormatCodexResponses support")
	}
	for _, f := range []engine.RequestFormat{
		engine.FormatOpenAIChat, engine.FormatOpenAICompat,
		engine.FormatAnthropic, engine.FormatGemini,
	} {
		if e.SupportsFormat(f) {
			t.Errorf("unexpected support for %s", f)
		}
	}
}

func TestCodexExecutor_ProviderType(t *testing.T) {
	e := NewCodexExecutor(nil)
	if e.ProviderType() != provider.ProviderCodex {
		t.Errorf("provider type = %s", e.ProviderType())
	}
}

func TestCodexExecutor_Execute_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/responses" {
			t.Errorf("path = %s", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer test-key" {
			t.Errorf("auth = %q", got)
		}
		if got := r.Header.Get("session_id"); got != codexTestAccount().ID.String() {
			t.Errorf("session_id = %q", got)
		}
		var payload map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode payload: %v", err)
		}
		if payload["store"] != false {
			t.Errorf("store = %v, want false", payload["store"])
		}
		if payload["stream"] != false {
			t.Errorf("stream = %v, want false", payload["stream"])
		}
		if _, ok := payload["previous_response_id"]; ok {
			t.Error("unexpected previous_response_id")
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `{"id":"resp_1","output":[]}`)
	}))
	defer server.Close()

	e := NewCodexExecutor(server.Client().Transport)
	e.SetBaseURL(server.URL)

	resp, err := e.Execute(context.Background(), codexTestRequest(), codexTestAccount())
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d", resp.StatusCode)
	}
	if !strings.Contains(string(resp.Body), "resp_1") {
		t.Errorf("body = %s", resp.Body)
	}
}

func TestCodexExecutor_Execute_Error4xx(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = io.WriteString(w, `{"error":{"message":"bad key"}}`)
	}))
	defer server.Close()

	e := NewCodexExecutor(server.Client().Transport)
	e.SetBaseURL(server.URL)

	_, err := e.Execute(context.Background(), codexTestRequest(), codexTestAccount())
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestCodexExecutor_ExecuteStream_Success(t *testing.T) {
	events := "event: response.output_text.delta\ndata: {\"type\":\"output_text_delta\",\"delta\":\"hello\"}\n\nevent: response.completed\ndata: {\"type\":\"response.completed\"}\n\n"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, events)
	}))
	defer server.Close()

	e := NewCodexExecutor(server.Client().Transport)
	e.SetBaseURL(server.URL)

	resp, err := e.ExecuteStream(context.Background(), codexTestRequest(), codexTestAccount())
	if err != nil {
		t.Fatalf("ExecuteStream: %v", err)
	}
	if resp.Stream == nil {
		t.Fatal("nil stream")
	}
	st, ok := resp.Stream.(*stream.Stream)
	if !ok {
		t.Fatalf("stream is %T", resp.Stream)
	}
	select {
	case chunk := <-st.Chunks():
		if string(chunk.Data) != "{\"type\":\"output_text_delta\",\"delta\":\"hello\"}" {
			t.Errorf("first chunk = %q", chunk.Data)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for first chunk")
	}
}

func TestCodexExecutor_ExecuteStream_SSEError(t *testing.T) {
	events := "event: error\ndata: {\"error\":{\"message\":\"selected model is at capacity\"}}\n\n"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, events)
	}))
	defer server.Close()

	e := NewCodexExecutor(server.Client().Transport)
	e.SetBaseURL(server.URL)

	_, err := e.ExecuteStream(context.Background(), codexTestRequest(), codexTestAccount())
	if err == nil || !strings.Contains(err.Error(), "at capacity") {
		t.Fatalf("expected SSE capacity error, got %v", err)
	}
}

func TestCodexExecutor_transformRequest(t *testing.T) {
	e := NewCodexExecutor(nil)
	payload := e.transformRequest([]byte(`{
		"model": "",
		"input": "hi",
		"previous_response_id": "resp_old",
		"stream": true
	}`), false)

	if payload["model"] != "gpt-4o-codex" {
		t.Errorf("model = %v", payload["model"])
	}
	if payload["stream"] != false {
		t.Errorf("stream = %v", payload["stream"])
	}
	if payload["store"] != false {
		t.Errorf("store = %v", payload["store"])
	}
	if _, ok := payload["previous_response_id"]; ok {
		t.Error("previous_response_id not stripped")
	}
	if payload["instructions"] != "You are a helpful assistant." {
		t.Errorf("instructions = %v", payload["instructions"])
	}
}

func TestCodexExecutor_normaliseInput(t *testing.T) {
	e := NewCodexExecutor(nil)

	payload := map[string]interface{}{"input": "hi"}
	e.normaliseInput(payload)
	input, ok := payload["input"].([]interface{})
	if !ok || len(input) != 1 {
		t.Fatalf("input = %v", payload["input"])
	}

	payload = map[string]interface{}{"input": []interface{}{}}
	e.normaliseInput(payload)
	input, ok = payload["input"].([]interface{})
	if !ok || len(input) != 1 {
		t.Fatalf("empty input not normalised: %v", payload["input"])
	}

	payload = map[string]interface{}{}
	e.normaliseInput(payload)
	input, ok = payload["input"].([]interface{})
	if !ok || len(input) != 1 {
		t.Fatalf("missing input not normalised: %v", payload["input"])
	}
}

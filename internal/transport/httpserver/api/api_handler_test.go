// Package api_test provides black-box tests for the HTTP API handlers.
//
// All tests use httptest.NewRecorder and chi test routers — no real network
// connections are established.
package api_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/rs/zerolog"

	"gorouter/internal/domain/auth"
	"gorouter/internal/domain/engine"
	"gorouter/internal/transport/httpserver/api"
)

// ---------------------------------------------------------------------------
// Mock types
// ---------------------------------------------------------------------------

// mockOrchestrator implements engine.Orchestrator for handler tests.
type mockOrchestrator struct {
	executeFn func(ctx context.Context, req *engine.Request) (*engine.Response, error)
	cancelFn  func(ctx context.Context, requestID uuid.UUID) error
}

func (m *mockOrchestrator) ExecuteRequest(ctx context.Context, req *engine.Request) (*engine.Response, error) {
	if m.executeFn != nil {
		return m.executeFn(ctx, req)
	}
	return &engine.Response{StatusCode: http.StatusOK, Body: []byte(`{}`)}, nil
}

func (m *mockOrchestrator) CancelStream(ctx context.Context, requestID uuid.UUID) error {
	if m.cancelFn != nil {
		return m.cancelFn(ctx, requestID)
	}
	return nil
}

func noopLogger() zerolog.Logger {
	return zerolog.Nop()
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

func TestHandleChatCompletions_NonStreaming(t *testing.T) {
	orch := &mockOrchestrator{
		executeFn: func(ctx context.Context, req *engine.Request) (*engine.Response, error) {
			return &engine.Response{
				RequestID:  uuid.New(),
				Model:      "gpt-4",
				Body:       []byte(`{"choices":[{"message":{"content":"hello"}}]}`),
				StatusCode: http.StatusOK,
			}, nil
		},
	}
	handler := api.New(api.DefaultConfig(), orch, noopLogger())

	r := chi.NewRouter()
	handler.RegisterRoutes(r)

	body := `{"model":"gpt-4","messages":[{"role":"user","content":"hello"}]}`
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), "hello") {
		t.Error("expected response body to contain 'hello'")
	}
}

func TestHandleChatCompletions_InvalidBody(t *testing.T) {
	orch := &mockOrchestrator{}
	handler := api.New(api.DefaultConfig(), orch, noopLogger())

	r := chi.NewRouter()
	handler.RegisterRoutes(r)

	body := `{invalid json`
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestHandleChatCompletions_EmptyBody(t *testing.T) {
	orch := &mockOrchestrator{}
	handler := api.New(api.DefaultConfig(), orch, noopLogger())

	r := chi.NewRouter()
	handler.RegisterRoutes(r)

	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", http.NoBody)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestHandleChatCompletions_OrchestratorError(t *testing.T) {
	orch := &mockOrchestrator{
		executeFn: func(ctx context.Context, req *engine.Request) (*engine.Response, error) {
			return nil, errors.New("provider unavailable")
		},
	}
	handler := api.New(api.DefaultConfig(), orch, noopLogger())

	r := chi.NewRouter()
	handler.RegisterRoutes(r)

	body := `{"model":"gpt-4","messages":[{"role":"user","content":"hello"}]}`
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected 500, got %d", w.Code)
	}
}

func TestHandleChatCompletions_WithActorContext(t *testing.T) {
	userID := uuid.New()
	var capturedUserID *uuid.UUID

	orch := &mockOrchestrator{
		executeFn: func(ctx context.Context, req *engine.Request) (*engine.Response, error) {
			capturedUserID = req.UserID
			return &engine.Response{
				RequestID:  uuid.New(),
				Model:      "gpt-4",
				Body:       []byte(`{"choices":[{"message":{"content":"ok"}}]}`),
				StatusCode: http.StatusOK,
			}, nil
		},
	}
	handler := api.New(api.DefaultConfig(), orch, noopLogger())

	r := chi.NewRouter()
	handler.RegisterRoutes(r)

	body := `{"model":"gpt-4","messages":[{"role":"user","content":"hello"}]}`
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	actor := &auth.Actor{UserID: userID, SessionID: uuid.New()}
	ctx := auth.ContextWithActor(req.Context(), actor)
	req = req.WithContext(ctx)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
	if capturedUserID == nil {
		t.Fatal("expected UserID to be set from context")
	}
	if *capturedUserID != userID {
		t.Errorf("expected UserID %s, got %s", userID.String(), capturedUserID.String())
	}
}

func TestHandleModels(t *testing.T) {
	orch := &mockOrchestrator{}
	handler := api.New(api.DefaultConfig(), orch, noopLogger())

	r := chi.NewRouter()
	handler.RegisterRoutes(r)

	req := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}

	var resp map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to parse response JSON: %v", err)
	}
	if resp["object"] != "list" {
		t.Errorf("expected object=list, got %v", resp["object"])
	}
	models, ok := resp["data"].([]interface{})
	if !ok {
		t.Fatal("expected data to be an array")
	}
	if len(models) == 0 {
		t.Fatal("expected at least 1 model")
	}
	first, ok := models[0].(map[string]interface{})
	if !ok {
		t.Fatal("expected model entry to be an object")
	}
	if first["id"] == "" {
		t.Error("expected model id to be non-empty")
	}
}

func TestHandleCodexResponses_NonStreaming(t *testing.T) {
	orch := &mockOrchestrator{
		executeFn: func(ctx context.Context, req *engine.Request) (*engine.Response, error) {
			return &engine.Response{
				RequestID:  uuid.New(),
				Model:      "gpt-4",
				Body:       []byte(`{"output":[{"content":"hello from codex"}]}`),
				StatusCode: http.StatusOK,
			}, nil
		},
	}
	handler := api.New(api.DefaultConfig(), orch, noopLogger())

	r := chi.NewRouter()
	handler.RegisterRoutes(r)

	body := `{"model":"gpt-4","input":"hello codex"}`
	req := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), "hello from codex") {
		t.Error("expected response body to contain 'hello from codex'")
	}
}

func TestHandleCodexResponses_InvalidBody(t *testing.T) {
	orch := &mockOrchestrator{}
	handler := api.New(api.DefaultConfig(), orch, noopLogger())

	r := chi.NewRouter()
	handler.RegisterRoutes(r)

	body := `{not valid json`
	req := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestHandleCodexResponses_EmptyBody(t *testing.T) {
	orch := &mockOrchestrator{}
	handler := api.New(api.DefaultConfig(), orch, noopLogger())

	r := chi.NewRouter()
	handler.RegisterRoutes(r)

	req := httptest.NewRequest(http.MethodPost, "/v1/responses", http.NoBody)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestHandleCodexResponses_OrchestratorError(t *testing.T) {
	orch := &mockOrchestrator{
		executeFn: func(ctx context.Context, req *engine.Request) (*engine.Response, error) {
			return nil, errors.New("upstream error")
		},
	}
	handler := api.New(api.DefaultConfig(), orch, noopLogger())

	r := chi.NewRouter()
	handler.RegisterRoutes(r)

	body := `{"model":"gpt-4","input":"hello"}`
	req := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected 500, got %d", w.Code)
	}
}

func TestHandler_DefaultConfig(t *testing.T) {
	cfg := api.DefaultConfig()
	if cfg.DefaultModel != "gpt-4" {
		t.Errorf("expected DefaultModel=gpt-4, got %s", cfg.DefaultModel)
	}
	if cfg.StreamKeepalive <= 0 {
		t.Error("expected positive StreamKeepalive")
	}
}

func TestHandler_CustomConfig(t *testing.T) {
	cfg := api.Config{
		DefaultModel:    "claude-3-opus",
		StreamKeepalive: 30,
	}
	orch := &mockOrchestrator{
		executeFn: func(ctx context.Context, req *engine.Request) (*engine.Response, error) {
			return &engine.Response{
				RequestID:  uuid.New(),
				Body:       []byte(`{"choices":[{"message":{"content":"ok"}}]}`),
				StatusCode: http.StatusOK,
			}, nil
		},
	}
	handler := api.New(cfg, orch, noopLogger())

	r := chi.NewRouter()
	handler.RegisterRoutes(r)

	body := `{"model":"claude-3-opus","messages":[{"role":"user","content":"hi"}]}`
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
}

func TestRegisterRoutes(t *testing.T) {
	orch := &mockOrchestrator{}
	handler := api.New(api.DefaultConfig(), orch, noopLogger())

	r := chi.NewRouter()
	handler.RegisterRoutes(r)

	tests := []struct {
		method string
		path   string
	}{
		{http.MethodPost, "/v1/chat/completions"},
		{http.MethodPost, "/v1/responses"},
		{http.MethodGet, "/v1/models"},
	}

	for _, tt := range tests {
		t.Run(tt.method+" "+tt.path, func(t *testing.T) {
			var req *http.Request
			if tt.method == http.MethodGet {
				req = httptest.NewRequest(tt.method, tt.path, nil)
			} else {
				req = httptest.NewRequest(tt.method, tt.path, strings.NewReader(`{}`))
			}
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)

			if w.Code == http.StatusNotFound {
				t.Errorf("expected route %s %s to be registered, got 404", tt.method, tt.path)
			}
		})
	}
}

func TestHandleChatCompletions_MissingModel(t *testing.T) {
	orch := &mockOrchestrator{}
	handler := api.New(api.DefaultConfig(), orch, noopLogger())

	r := chi.NewRouter()
	handler.RegisterRoutes(r)

	body := `{"messages":[{"role":"user","content":"hi"}]}`
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for missing model, got %d", w.Code)
	}
}

func TestHandleChatCompletions_MissingMessages(t *testing.T) {
	orch := &mockOrchestrator{}
	handler := api.New(api.DefaultConfig(), orch, noopLogger())

	r := chi.NewRouter()
	handler.RegisterRoutes(r)

	body := `{"model":"gpt-4"}`
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for missing messages, got %d", w.Code)
	}
}

func TestHandleCodexResponses_MissingModel(t *testing.T) {
	orch := &mockOrchestrator{}
	handler := api.New(api.DefaultConfig(), orch, noopLogger())

	r := chi.NewRouter()
	handler.RegisterRoutes(r)

	body := `{"input":"hello"}`
	req := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for missing model, got %d", w.Code)
	}
}

func TestHandleCodexResponses_MissingInput(t *testing.T) {
	orch := &mockOrchestrator{}
	handler := api.New(api.DefaultConfig(), orch, noopLogger())

	r := chi.NewRouter()
	handler.RegisterRoutes(r)

	body := `{"model":"gpt-4"}`
	req := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for missing input, got %d", w.Code)
	}
}

func TestHandleCodexResponses_WithActorContext(t *testing.T) {
	userID := uuid.New()
	var capturedUserID *uuid.UUID

	orch := &mockOrchestrator{
		executeFn: func(ctx context.Context, req *engine.Request) (*engine.Response, error) {
			capturedUserID = req.UserID
			return &engine.Response{
				RequestID:  uuid.New(),
				Model:      "gpt-4",
				Body:       []byte(`{"output":["ok"]}`),
				StatusCode: http.StatusOK,
			}, nil
		},
	}
	handler := api.New(api.DefaultConfig(), orch, noopLogger())

	r := chi.NewRouter()
	handler.RegisterRoutes(r)

	body := `{"model":"gpt-4","input":"hello"}`
	req := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	actor := &auth.Actor{UserID: userID, SessionID: uuid.New()}
	ctx := auth.ContextWithActor(req.Context(), actor)
	req = req.WithContext(ctx)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
	if capturedUserID == nil {
		t.Fatal("expected UserID to be set from context for codex handler")
	}
	if *capturedUserID != userID {
		t.Errorf("expected UserID %s, got %s", userID.String(), capturedUserID.String())
	}
}

func TestWriteJSONError(t *testing.T) {
	// writeJSONError is unexported but we can test it indirectly by
	// triggering a 400 due to invalid JSON.
	orch := &mockOrchestrator{}
	handler := api.New(api.DefaultConfig(), orch, noopLogger())

	r := chi.NewRouter()
	handler.RegisterRoutes(r)

	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{invalid}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
	if w.Header().Get("Content-Type") != "application/json" {
		t.Errorf("expected Content-Type application/json, got %s", w.Header().Get("Content-Type"))
	}

	var resp map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("response should be valid JSON: %v", err)
	}
	errObj, ok := resp["error"].(map[string]interface{})
	if !ok {
		t.Fatal("expected error object in response")
	}
	if errObj["message"] == "" {
		t.Error("expected non-empty error message")
	}
	if errObj["type"] != "invalid_request_error" {
		t.Errorf("expected type invalid_request_error, got %v", errObj["type"])
	}
}

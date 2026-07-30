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

	"gorouter/internal/app/translate"
	"gorouter/internal/domain/engine"
	"gorouter/internal/transport/httpserver/api"
	"gorouter/internal/transport/middleware"
)

type alwaysPassValidator struct{}

func (alwaysPassValidator) ValidateModelKey(_ context.Context, rawKey string) (uuid.UUID, error) {
	if rawKey == "" {
		return uuid.Nil, errors.New("empty key")
	}
	return uuid.MustParse("11111111-1111-1111-1111-111111111111"), nil
}

type alwaysFailValidator struct{}

func (alwaysFailValidator) ValidateModelKey(_ context.Context, rawKey string) (uuid.UUID, error) {
	return uuid.Nil, errors.New("invalid")
}

func authMiddleware(validator middleware.ModelKeyValidator) func(http.Handler) http.Handler {
	return middleware.ModelKeyAuth(validator)
}

func TestInt_LegacyDoubleV1(t *testing.T) {
	orch := &mockOrchestrator{executeFn: func(ctx context.Context, req *engine.Request) (*engine.Response, error) {
		return &engine.Response{StatusCode: http.StatusOK, Body: []byte("{}")}, nil
	}}
	svc := translate.NewService()
	handler := api.New(api.DefaultConfig(), orch, svc, noopLogger())
	r := chi.NewRouter()
	handler.RegisterRoutes(r)
	for _, p := range []string{"/v1/v1/chat/completions", "/v1/v1/responses"} {
		req := httptest.NewRequest(http.MethodPost, p, strings.NewReader("{}"))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code == http.StatusNotFound {
			t.Errorf("route %s not registered", p)
		}
	}
}

func TestInt_ResponsesAlias(t *testing.T) {
	orch := &mockOrchestrator{executeFn: func(ctx context.Context, req *engine.Request) (*engine.Response, error) {
		return &engine.Response{StatusCode: http.StatusOK, Body: []byte("{}")}, nil
	}}
	svc := translate.NewService()
	handler := api.New(api.DefaultConfig(), orch, svc, noopLogger())
	r := chi.NewRouter()
	handler.RegisterRoutes(r)
	req := httptest.NewRequest(http.MethodPost, "/responses", strings.NewReader("{}"))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code == http.StatusNotFound {
		t.Error("/responses not registered")
	}
}

func TestInt_CodexRoutes(t *testing.T) {
	orch := &mockOrchestrator{executeFn: func(ctx context.Context, req *engine.Request) (*engine.Response, error) {
		return &engine.Response{StatusCode: http.StatusOK, Body: []byte("{}")}, nil
	}}
	svc := translate.NewService()
	handler := api.New(api.DefaultConfig(), orch, svc, noopLogger())
	r := chi.NewRouter()
	handler.RegisterRoutes(r)
	tcases := []struct{ method, path string }{
		{http.MethodGet, "/codex/models"},
		{http.MethodPost, "/codex/chat/completions"},
		{http.MethodPost, "/codex/responses"},
	}
	for _, tt := range tcases {
		var req *http.Request
		if tt.method == http.MethodPost {
			req = httptest.NewRequest(tt.method, tt.path, strings.NewReader("{}"))
		} else {
			req = httptest.NewRequest(tt.method, tt.path, nil)
		}
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code == http.StatusNotFound {
			t.Errorf("codex route %s not registered", tt.path)
		}
	}
}

func TestInt_Auth_MissingKey(t *testing.T) {
	svc := translate.NewService()
	handler := api.New(api.DefaultConfig(), &mockOrchestrator{}, svc, noopLogger())
	r := chi.NewRouter()
	r.Group(func(r chi.Router) {
		r.Use(authMiddleware(alwaysPassValidator{}))
		handler.RegisterRoutes(r)
	})
	req := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", w.Code)
	}
}

func TestInt_Auth_InvalidKey(t *testing.T) {
	svc := translate.NewService()
	handler := api.New(api.DefaultConfig(), &mockOrchestrator{}, svc, noopLogger())
	r := chi.NewRouter()
	r.Group(func(r chi.Router) {
		r.Use(authMiddleware(alwaysFailValidator{}))
		handler.RegisterRoutes(r)
	})
	req := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	req.Header.Set("Authorization", "Bearer invalid-key")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", w.Code)
	}
}

func TestInt_Auth_ValidKey(t *testing.T) {
	orch := &mockOrchestrator{executeFn: func(ctx context.Context, req *engine.Request) (*engine.Response, error) {
		return &engine.Response{StatusCode: http.StatusOK, Body: []byte("{}")}, nil
	}}
	svc := translate.NewService()
	handler := api.New(api.DefaultConfig(), orch, svc, noopLogger())
	r := chi.NewRouter()
	r.Group(func(r chi.Router) {
		r.Use(authMiddleware(alwaysPassValidator{}))
		handler.RegisterRoutes(r)
	})
	req := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	req.Header.Set("Authorization", "Bearer valid-test-key")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
}

func TestInt_Auth_AllExtractions(t *testing.T) {
	orch := &mockOrchestrator{executeFn: func(ctx context.Context, req *engine.Request) (*engine.Response, error) {
		return &engine.Response{StatusCode: http.StatusOK, Body: []byte("ok")}, nil
	}}
	svc := translate.NewService()
	handler := api.New(api.DefaultConfig(), orch, svc, noopLogger())
	r := chi.NewRouter()
	r.Group(func(r chi.Router) {
		r.Use(authMiddleware(alwaysPassValidator{}))
		handler.RegisterRoutes(r)
	})
	rbody := `{"model":"gpt-4","messages":[{"role":"user","content":"hi"}]}`
	tcases := []struct{ name, header, value string }{
		{"Bearer", "Authorization", "Bearer valid-key"},
		{"x-api-key", "X-Api-Key", "valid-key"},
		{"x-goog-api-key", "X-Goog-Api-Key", "valid-key"},
	}
	for _, tt := range tcases {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(rbody))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set(tt.header, tt.value)
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)
			if w.Code != http.StatusOK {
				t.Errorf("expected 200, got %d", w.Code)
			}
		})
	}
	t.Run("query key", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions?key=valid-key", strings.NewReader(rbody))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Errorf("expected 200, got %d", w.Code)
		}
	})
	t.Run("no key", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(rbody))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != http.StatusUnauthorized {
			t.Errorf("expected 401, got %d", w.Code)
		}
	})
}

func TestInt_Auth_LegacyNoBypass(t *testing.T) {
	orch := &mockOrchestrator{executeFn: func(ctx context.Context, req *engine.Request) (*engine.Response, error) {
		return &engine.Response{StatusCode: http.StatusOK, Body: []byte("ok")}, nil
	}}
	svc := translate.NewService()
	handler := api.New(api.DefaultConfig(), orch, svc, noopLogger())
	r := chi.NewRouter()
	r.Group(func(r chi.Router) {
		r.Use(authMiddleware(alwaysPassValidator{}))
		handler.RegisterRoutes(r)
	})
	paths := []string{"/v1/v1/chat/completions", "/v1/v1/responses", "/responses", "/codex/models", "/codex/chat/completions", "/codex/responses"}
	for _, p := range paths {
		var req *http.Request
		if p == "/codex/models" {
			req = httptest.NewRequest(http.MethodGet, p, nil)
		} else {
			req = httptest.NewRequest(http.MethodPost, p, strings.NewReader("{}"))
		}
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != http.StatusUnauthorized {
			t.Errorf("expected 401 for %s, got %d", p, w.Code)
		}
	}
}

func TestInt_HealthPublic(t *testing.T) {
	svc := translate.NewService()
	handler := api.New(api.DefaultConfig(), &mockOrchestrator{}, svc, noopLogger())
	r := chi.NewRouter()
	handler.RegisterRoutes(r)
	r.Get("/health", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
}

func TestInt_ErrorShape(t *testing.T) {
	orch := &mockOrchestrator{executeFn: func(ctx context.Context, req *engine.Request) (*engine.Response, error) {
		return nil, errors.New("fail")
	}}
	svc := translate.NewService()
	handler := api.New(api.DefaultConfig(), orch, svc, noopLogger())
	r := chi.NewRouter()
	handler.RegisterRoutes(r)
	body := `{"model":"gpt-4","messages":[{"role":"user","content":"hi"}]}`
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Request-ID", "test-correlation-999")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	var resp map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	e, ok := resp["error"].(map[string]interface{})
	if !ok {
		t.Fatal("expected error")
	}
	if e["type"] != "internal_error" {
		t.Errorf("type=%v", e["type"])
	}
	if e["code"] != "INTERNAL_ERROR" {
		t.Errorf("code=%v", e["code"])
	}
	if e["request_id"] != "test-correlation-999" {
		t.Errorf("request_id=%v", e["request_id"])
	}
	if e["retryable"] != true {
		t.Errorf("retryable=%v", e["retryable"])
	}
}

func TestInt_NoCredentialLeak(t *testing.T) {
	orch := &mockOrchestrator{executeFn: func(ctx context.Context, req *engine.Request) (*engine.Response, error) {
		return nil, errors.New("secret-key-leaked")
	}}
	svc := translate.NewService()
	handler := api.New(api.DefaultConfig(), orch, svc, noopLogger())
	r := chi.NewRouter()
	handler.RegisterRoutes(r)
	body := `{"model":"gpt-4","messages":[{"role":"user","content":"hi"}]}`
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if strings.Contains(w.Body.String(), "secret-key-leaked") {
		t.Error("leak detected")
	}
}

func TestInt_XBaseURL_Stripped(t *testing.T) {
	orch := &mockOrchestrator{executeFn: func(ctx context.Context, req *engine.Request) (*engine.Response, error) {
		return &engine.Response{StatusCode: http.StatusOK, Body: []byte("{}")}, nil
	}}
	svc := translate.NewService()
	handler := api.New(api.DefaultConfig(), orch, svc, noopLogger())
	r := chi.NewRouter()
	handler.RegisterRoutes(r)
	body := `{"model":"gpt-4","messages":[{"role":"user","content":"hi"}]}`
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Base-URL", "http://malicious.example.com")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
}

func TestInt_OversizedBody(t *testing.T) {
	svc := translate.NewService()
	handler := api.New(api.DefaultConfig(), &mockOrchestrator{}, svc, noopLogger())
	r := chi.NewRouter()
	handler.RegisterRoutes(r)
	big := strings.Repeat("a", 2*1024*1024+1)
	body := `{"model":"gpt-4","messages":[{"role":"user","content":"` + big + `"}]}`
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

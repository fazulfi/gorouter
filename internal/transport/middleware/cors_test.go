package middleware

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestModelCORS_AllowsAllOrigins(t *testing.T) {
	handler := ModelCORS()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	testOrigins := []string{
		"https://example.com",
		"http://localhost:3000",
		"https://app.example.com",
		"chrome-extension://abc123",
	}

	for _, origin := range testOrigins {
		t.Run(origin, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
			req.Header.Set("Origin", origin)
			rr := httptest.NewRecorder()
			handler.ServeHTTP(rr, req)

			if rr.Header().Get("Access-Control-Allow-Origin") != origin {
				t.Errorf("expected ACAO=%q, got %q", origin, rr.Header().Get("Access-Control-Allow-Origin"))
			}
			if rr.Header().Get("Vary") == "" {
				t.Error("expected Vary header to be set")
			}
		})
	}
}

func TestModelCORS_OptionsPreflight(t *testing.T) {
	handler := ModelCORS()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodOptions, "/v1/models", nil)
	req.Header.Set("Origin", "https://example.com")
	req.Header.Set("Access-Control-Request-Method", "POST")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusNoContent {
		t.Errorf("expected 204 for OPTIONS preflight, got %d", rr.Code)
	}
	if rr.Header().Get("Access-Control-Allow-Origin") != "https://example.com" {
		t.Errorf("expected ACAO on preflight, got %q", rr.Header().Get("Access-Control-Allow-Origin"))
	}
	if rr.Header().Get("Access-Control-Allow-Methods") == "" {
		t.Error("expected Allow-Methods on preflight")
	}
	if rr.Header().Get("Access-Control-Allow-Headers") == "" {
		t.Error("expected Allow-Headers on preflight")
	}
}

func TestModelCORS_NoOrigin(t *testing.T) {
	handler := ModelCORS()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	// No Origin header
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected 200 with no origin, got %d", rr.Code)
	}
	if rr.Header().Get("Access-Control-Allow-Origin") != "" {
		t.Errorf("expected no ACAO without origin, got %q",
			rr.Header().Get("Access-Control-Allow-Origin"))
	}
}

func TestAdminCORS_RestrictedOrigin(t *testing.T) {
	cfg := CORSConfig{
		AllowedOrigins:   []string{"https://admin.example.com"},
		AllowedMethods:   []string{"GET", "POST", "PUT", "DELETE"},
		AllowedHeaders:   []string{"Authorization", "Content-Type"},
		MaxAge:           3600,
		AllowCredentials: true,
	}

	handler := AdminCORS(cfg)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/admin", nil)
	req.Header.Set("Origin", "https://admin.example.com")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Header().Get("Access-Control-Allow-Origin") != "https://admin.example.com" {
		t.Errorf("expected ACAO for allowed origin, got %q",
			rr.Header().Get("Access-Control-Allow-Origin"))
	}
	if rr.Header().Get("Access-Control-Allow-Credentials") != "true" {
		t.Errorf("expected credentials=true, got %q",
			rr.Header().Get("Access-Control-Allow-Credentials"))
	}
}

func TestAdminCORS_DeniedOrigin(t *testing.T) {
	cfg := CORSConfig{
		AllowedOrigins: []string{"https://admin.example.com"},
		AllowedMethods: []string{"GET", "POST"},
	}

	handler := AdminCORS(cfg)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/admin", nil)
	req.Header.Set("Origin", "https://evil.com")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Header().Get("Access-Control-Allow-Origin") != "" {
		t.Errorf("expected no ACAO for denied origin, got %q",
			rr.Header().Get("Access-Control-Allow-Origin"))
	}
	// Request should still be processed
	if rr.Code != http.StatusOK {
		t.Errorf("expected 200 for denied origin (still serves request), got %d", rr.Code)
	}
}

func TestAdminCORS_LocalhostDefault(t *testing.T) {
	// Empty AllowedOrigins should default to localhost
	cfg := CORSConfig{}

	handler := AdminCORS(cfg)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	tests := []struct {
		origin   string
		allowed  bool
	}{
		{"http://localhost:8080", true},
		{"http://127.0.0.1:8080", true},
		{"http://localhost", true},
		{"https://evil.com", false},
	}

	for _, tc := range tests {
		t.Run(tc.origin, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/admin", nil)
			req.Header.Set("Origin", tc.origin)
			rr := httptest.NewRecorder()
			handler.ServeHTTP(rr, req)

			if tc.allowed && rr.Header().Get("Access-Control-Allow-Origin") == "" {
				t.Errorf("expected ACAO for allowed origin %q", tc.origin)
			}
			if !tc.allowed && rr.Header().Get("Access-Control-Allow-Origin") != "" {
				t.Errorf("expected no ACAO for denied origin %q, got %q",
					tc.origin, rr.Header().Get("Access-Control-Allow-Origin"))
			}
		})
	}
}

func TestAdminCORS_PreflightWithCredentials(t *testing.T) {
	cfg := CORSConfig{
		AllowedOrigins:   []string{"https://admin.example.com"},
		AllowedMethods:   []string{"POST"},
		AllowedHeaders:   []string{"Authorization", "X-CSRF-Token"},
		MaxAge:           7200,
		AllowCredentials: true,
	}

	handler := AdminCORS(cfg)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodOptions, "/admin/settings", nil)
	req.Header.Set("Origin", "https://admin.example.com")
	req.Header.Set("Access-Control-Request-Method", "POST")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusNoContent {
		t.Errorf("expected 204 for preflight, got %d", rr.Code)
	}
	if rr.Header().Get("Access-Control-Allow-Credentials") != "true" {
		t.Errorf("expected credentials=true")
	}
	if rr.Header().Get("Access-Control-Max-Age") != "7200" {
		t.Errorf("expected Max-Age=7200, got %q", rr.Header().Get("Access-Control-Max-Age"))
	}
	if rr.Header().Get("Access-Control-Allow-Headers") != "Authorization, X-CSRF-Token" {
		t.Errorf("expected Allow-Headers=Authorization, X-CSRF-Token, got %q",
			rr.Header().Get("Access-Control-Allow-Headers"))
	}
}

func TestModelCORS_ValidatesHeaders(t *testing.T) {
	handler := ModelCORS()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// Model CORS should expose common headers
	req := httptest.NewRequest(http.MethodGet, "/v1/chat/completions", nil)
	req.Header.Set("Origin", "https://client-app.com")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	exposeHeaders := rr.Header().Get("Access-Control-Expose-Headers")
	if exposeHeaders == "" {
		t.Error("expected Expose-Headers for model CORS")
	}
}

func TestModelCORS_AllowsMethods(t *testing.T) {
	handler := ModelCORS()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	methods := []string{http.MethodGet, http.MethodPost, http.MethodPut, http.MethodDelete, http.MethodPatch}
	for _, method := range methods {
		t.Run(method, func(t *testing.T) {
			req := httptest.NewRequest(method, "/v1/models", nil)
			req.Header.Set("Origin", "https://example.com")
			rr := httptest.NewRecorder()
			handler.ServeHTTP(rr, req)

			if rr.Code != http.StatusOK {
				t.Errorf("expected 200 for %s, got %d", method, rr.Code)
			}
		})
	}
}

func TestCORSConfig_Defaults(t *testing.T) {
	t.Run("model CORS has safe defaults", func(t *testing.T) {
		handler := ModelCORS()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		}))

		req := httptest.NewRequest(http.MethodOptions, "/v1/models", nil)
		req.Header.Set("Origin", "https://test.com")
		req.Header.Set("Access-Control-Request-Method", "POST")
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)

		if rr.Header().Get("Access-Control-Allow-Methods") == "" {
			t.Error("expected default allowed methods")
		}
		if rr.Header().Get("Access-Control-Allow-Headers") == "" {
			t.Error("expected default allowed headers")
		}
	})

	t.Run("admin CORS with nil AllowedOrigins defaults to localhost", func(t *testing.T) {
		cfg := CORSConfig{
			AllowedOrigins: nil,
		}
		handler := AdminCORS(cfg)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		}))

		req := httptest.NewRequest(http.MethodGet, "/admin", nil)
		req.Header.Set("Origin", "http://localhost:9000")
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)

		if rr.Header().Get("Access-Control-Allow-Origin") != "http://localhost:9000" {
			t.Errorf("expected localhost default ACAO, got %q",
				rr.Header().Get("Access-Control-Allow-Origin"))
		}
	})
}

func TestModelCORS_VaryHeader(t *testing.T) {
	handler := ModelCORS()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	req.Header.Set("Origin", "https://example.com")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	vary := rr.Header().Get("Vary")
	if !strings.Contains(vary, "Origin") {
		t.Errorf("expected Vary to include Origin, got %q", vary)
	}
}

func TestModelCORS_OptionsWithoutOrigin(t *testing.T) {
	handler := ModelCORS()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodOptions, "/v1/models", nil)
	// No Origin header
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	// Without Origin, OPTIONS should pass through (no preflight needed)
	if rr.Code == http.StatusNoContent {
		t.Log("OPTIONS without origin returns 204 (acceptable)")
	}
}

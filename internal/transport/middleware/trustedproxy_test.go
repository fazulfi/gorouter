package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestTrustedProxy_TrustedIPv4(t *testing.T) {
	cfg := TrustedProxyConfig{
		TrustedProxies: []string{"10.0.0.0/8", "192.168.0.0/16"},
	}

	handler := TrustedProxy(cfg)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Trusted proxy should pass through X-Forwarded-For
		if fwd := r.Header.Get("X-Forwarded-For"); fwd != "10.0.0.1" {
			t.Errorf("expected X-Forwarded-For=10.0.0.1, got %q", fwd)
		}
		if realIP := r.Header.Get("X-Real-IP"); realIP != "10.0.0.1" {
			t.Errorf("expected X-Real-IP=10.0.0.1, got %q", realIP)
		}
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("X-Forwarded-For", "10.0.0.1")
	req.Header.Set("X-Real-IP", "10.0.0.1")
	req.RemoteAddr = "10.0.0.99:34567" // within 10.0.0.0/8

	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rr.Code)
	}
}

func TestTrustedProxy_UntrustedIPv4(t *testing.T) {
	cfg := TrustedProxyConfig{
		TrustedProxies: []string{"10.0.0.0/8", "192.168.0.0/16"},
	}

	handler := TrustedProxy(cfg)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Untrusted source: forwarded headers must be stripped
		if fwd := r.Header.Get("X-Forwarded-For"); fwd != "" {
			t.Errorf("expected empty X-Forwarded-For from untrusted source, got %q", fwd)
		}
		if realIP := r.Header.Get("X-Real-IP"); realIP != "" {
			t.Errorf("expected empty X-Real-IP from untrusted source, got %q", realIP)
		}
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("X-Forwarded-For", "1.2.3.4")
	req.Header.Set("X-Real-IP", "1.2.3.4")
	req.RemoteAddr = "203.0.113.50:54321" // outside trusted ranges

	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rr.Code)
	}
}

func TestTrustedProxy_SpoofedHeaderFromUntrusted(t *testing.T) {
	cfg := TrustedProxyConfig{
		TrustedProxies: []string{"10.0.0.0/8"},
	}

	handler := TrustedProxy(cfg)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Untrusted IP trying to spoof X-Forwarded-For with a trusted-looking value
		if fwd := r.Header.Get("X-Forwarded-For"); fwd != "" {
			t.Errorf("expected stripped X-Forwarded-For from untrusted source, got %q", fwd)
		}
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("X-Forwarded-For", "10.0.0.1") // spoofing attempt
	req.RemoteAddr = "203.0.113.50:54321"         // untrusted

	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rr.Code)
	}
}

func TestTrustedProxy_ChainedProxies(t *testing.T) {
	cfg := TrustedProxyConfig{
		TrustedProxies: []string{"10.0.0.0/8"},
	}

	handler := TrustedProxy(cfg)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Multiple proxies: the last hop (10.0.0.99) is trusted
		if fwd := r.Header.Get("X-Forwarded-For"); fwd != "1.2.3.4, 5.6.7.8" {
			t.Errorf("expected X-Forwarded-For chain, got %q", fwd)
		}
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("X-Forwarded-For", "1.2.3.4, 5.6.7.8") // external clients behind proxy
	req.RemoteAddr = "10.0.0.99:34567"                    // trusted proxy

	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rr.Code)
	}
}

func TestTrustedProxy_IPv6Trusted(t *testing.T) {
	cfg := TrustedProxyConfig{
		TrustedProxies: []string{"fd00::/8"},
	}

	handler := TrustedProxy(cfg)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if fwd := r.Header.Get("X-Forwarded-For"); fwd != "::1" {
			t.Errorf("expected X-Forwarded-For=::1, got %q", fwd)
		}
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("X-Forwarded-For", "::1")
	req.RemoteAddr = "[fd00::1]:12345" // within fd00::/8

	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rr.Code)
	}
}

func TestTrustedProxy_IPv6Untrusted(t *testing.T) {
	cfg := TrustedProxyConfig{
		TrustedProxies: []string{"fd00::/8"},
	}

	handler := TrustedProxy(cfg)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if fwd := r.Header.Get("X-Forwarded-For"); fwd != "" {
			t.Errorf("expected empty from untrusted IPv6, got %q", fwd)
		}
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("X-Forwarded-For", "2001:db8::1")
	req.RemoteAddr = "[2001:db8::1]:54321" // outside fd00::/8

	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rr.Code)
	}
}

func TestTrustedProxy_SpoofedXRealIP(t *testing.T) {
	cfg := TrustedProxyConfig{
		TrustedProxies: []string{"10.0.0.0/8"},
	}

	handler := TrustedProxy(cfg)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Untrusted IP trying to spoof X-Real-IP
		if realIP := r.Header.Get("X-Real-IP"); realIP != "" {
			t.Errorf("expected stripped X-Real-IP from untrusted source, got %q", realIP)
		}
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("X-Real-IP", "10.0.0.1") // spoof attempt
	req.RemoteAddr = "203.0.113.50:54321"   // untrusted

	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rr.Code)
	}
}

func TestTrustedProxy_EmptyConfig(t *testing.T) {
	// Empty trusted proxies = no IPs trusted, all headers stripped
	cfg := TrustedProxyConfig{}

	handler := TrustedProxy(cfg)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if fwd := r.Header.Get("X-Forwarded-For"); fwd != "" {
			t.Errorf("expected empty with no trusted proxies, got %q", fwd)
		}
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("X-Forwarded-For", "10.0.0.1")
	req.RemoteAddr = "10.0.0.99:34567"

	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rr.Code)
	}
}

func TestNewTrustedProxy_ValidConfig(t *testing.T) {
	mw, err := NewTrustedProxy(TrustedProxyConfig{
		TrustedProxies: []string{"10.0.0.0/8", "192.168.0.0/16"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if mw == nil {
		t.Fatal("expected non-nil middleware")
	}
}

func TestNewTrustedProxy_EmptyConfig(t *testing.T) {
	mw, err := NewTrustedProxy(TrustedProxyConfig{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if mw == nil {
		t.Fatal("expected non-nil middleware")
	}
}

func TestNewTrustedProxy_InvalidCIDR(t *testing.T) {
	cfg := TrustedProxyConfig{
		TrustedProxies: []string{"not-a-cidr"},
	}
	_, err := NewTrustedProxy(cfg)
	if err == nil {
		t.Error("expected error for invalid CIDR, got nil")
	}
}

func TestTrustedProxy_PreservesOtherHeaders(t *testing.T) {
	cfg := TrustedProxyConfig{
		TrustedProxies: []string{"10.0.0.0/8"},
	}

	handler := TrustedProxy(cfg)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Non-forwarded headers should be preserved regardless of trust
		if auth := r.Header.Get("Authorization"); auth != "Bearer token123" {
			t.Errorf("expected Authorization preserved, got %q", auth)
		}
		if ct := r.Header.Get("Content-Type"); ct != "application/json" {
			t.Errorf("expected Content-Type preserved, got %q", ct)
		}
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("X-Forwarded-For", "10.0.0.1")
	req.Header.Set("Authorization", "Bearer token123")
	req.Header.Set("Content-Type", "application/json")
	req.RemoteAddr = "203.0.113.50:54321" // untrusted - forwarded stripped, others kept

	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rr.Code)
	}
}

func TestTrustedProxy_TrustedProxiesConstructor(t *testing.T) {
	t.Run("nil config panics", func(t *testing.T) {
		defer func() {
			if r := recover(); r == nil {
				t.Error("expected panic for nil config")
			}
		}()
		TrustedProxy(nil) //nolint:staticcheck // testing nil panic
	})

	t.Run("valid config works", func(t *testing.T) {
		cfg := &TrustedProxyConfig{TrustedProxies: []string{"10.0.0.0/8"}}
		handler := TrustedProxy(cfg)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		}))
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.RemoteAddr = "10.0.0.1:12345"
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)
		if rr.Code != http.StatusOK {
			t.Errorf("expected 200, got %d", rr.Code)
		}
	})
}

func TestTrustedProxy_NoHeadersPresent(t *testing.T) {
	cfg := TrustedProxyConfig{
		TrustedProxies: []string{"10.0.0.0/8"},
	}

	handler := TrustedProxy(cfg)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// No forwarded headers at all - should still work
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "10.0.0.99:34567"

	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rr.Code)
	}
}

// Test TrustedProxy uses context with config value pattern
func TestTrustedProxy_WithContextKey(t *testing.T) {
	// Verify the RealIPFunc option works via config
	cfg := TrustedProxyConfig{
		TrustedProxies: []string{"0.0.0.0/0"},
		RealIPFunc: func(r *http.Request) string {
			return "custom-real-ip"
		},
	}

	handler := TrustedProxy(cfg)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctxIP := GetRealIP(r.Context())
		if ctxIP != "custom-real-ip" {
			t.Errorf("expected custom-real-ip, got %q", ctxIP)
		}
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "10.0.0.1:12345"
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rr.Code)
	}
}

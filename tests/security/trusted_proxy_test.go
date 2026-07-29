package security_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"gorouter/internal/transport/middleware"
)

// TestTrustedProxy_UntrustedSpoofing simulates an untrusted IP attempting
// to spoof X-Forwarded-For to bypass security controls.
func TestTrustedProxy_UntrustedSpoofing(t *testing.T) {
	cfg := middleware.TrustedProxyConfig{
		TrustedProxies: []string{"10.0.0.0/8", "192.168.0.0/16"},
	}

	handler := middleware.TrustedProxy(cfg)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Security check: untrusted IP must not affect forwarded headers
		if fwd := r.Header.Get("X-Forwarded-For"); fwd != "" {
			t.Errorf("SECURITY FAIL: untrusted IP spoofed X-Forwarded-For=%q", fwd)
		}
		if realIP := r.Header.Get("X-Real-IP"); realIP != "" {
			t.Errorf("SECURITY FAIL: untrusted IP spoofed X-Real-IP=%q", realIP)
		}
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/api/v1/models", nil)
	req.Header.Set("X-Forwarded-For", "10.0.0.1, 192.168.1.1")
	req.Header.Set("X-Real-IP", "10.0.0.1")
	req.RemoteAddr = "203.0.113.50:54321"

	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rr.Code)
	}
}

// TestTrustedProxy_TrustedChain verifies that forwarded headers from
// trusted proxies are properly preserved.
func TestTrustedProxy_TrustedChain(t *testing.T) {
	cfg := middleware.TrustedProxyConfig{
		TrustedProxies: []string{"10.0.0.0/8"},
	}

	handler := middleware.TrustedProxy(cfg)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fwd := r.Header.Get("X-Forwarded-For")
		if fwd != "1.2.3.4, 5.6.7.8" {
			t.Errorf("expected preserved chain, got %q", fwd)
		}
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/api/v1/models", nil)
	req.Header.Set("X-Forwarded-For", "1.2.3.4, 5.6.7.8")
	req.RemoteAddr = "10.0.0.99:34567"

	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rr.Code)
	}
}

// TestTrustedProxy_MultipleXForwardedFor verifies behavior when
// multiple X-Forwarded-For headers are present.
func TestTrustedProxy_MultipleXForwardedFor(t *testing.T) {
	cfg := middleware.TrustedProxyConfig{
		TrustedProxies: []string{"10.0.0.0/8"},
	}

	handler := middleware.TrustedProxy(cfg)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fwd := r.Header.Get("X-Forwarded-For")
		if fwd == "" {
			t.Error("expected non-empty X-Forwarded-For from trusted source")
		}
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Add("X-Forwarded-For", "1.2.3.4")
	req.Header.Add("X-Forwarded-For", "5.6.7.8")
	req.RemoteAddr = "10.0.0.1:12345"

	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rr.Code)
	}
}

// TestTrustedProxy_IPv6UntrustedSpoof verifies IPv6 spoofing is prevented.
func TestTrustedProxy_IPv6UntrustedSpoof(t *testing.T) {
	cfg := middleware.TrustedProxyConfig{
		TrustedProxies: []string{"fd00::/8"},
	}

	handler := middleware.TrustedProxy(cfg)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if fwd := r.Header.Get("X-Forwarded-For"); fwd != "" {
			t.Errorf("SECURITY FAIL: untrusted IPv6 spoofed X-Forwarded-For=%q", fwd)
		}
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("X-Forwarded-For", "fd00::1")
	req.RemoteAddr = "[2001:db8::1]:12345"

	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rr.Code)
	}
}

// TestTrustedProxy_SpoofedXRealIP verifies X-Real-IP spoofing is prevented.
func TestTrustedProxy_SpoofedXRealIP(t *testing.T) {
	cfg := middleware.TrustedProxyConfig{
		TrustedProxies: []string{"10.0.0.0/8"},
	}

	handler := middleware.TrustedProxy(cfg)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if realIP := r.Header.Get("X-Real-IP"); realIP != "" {
			t.Errorf("SECURITY FAIL: untrusted IP spoofed X-Real-IP=%q", realIP)
		}
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("X-Real-IP", "10.0.0.1")
	req.RemoteAddr = "203.0.113.50:54321"

	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rr.Code)
	}
}

// TestTrustedProxy_EmptyTrustedProxies verifies that when no proxies are
// trusted, all forwarded headers are stripped regardless of source IP.
func TestTrustedProxy_EmptyTrustedProxies(t *testing.T) {
	cfg := middleware.TrustedProxyConfig{}

	handler := middleware.TrustedProxy(cfg)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if fwd := r.Header.Get("X-Forwarded-For"); fwd != "" {
			t.Errorf("expected stripped with empty config, got %q", fwd)
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

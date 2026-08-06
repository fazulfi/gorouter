package middleware

import (
	"crypto/tls"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"gorouter/internal/domain/auth"
)

func TestSecurityHeadersUsesNonceAndNoUnsafeInline(t *testing.T) {
	h := SecurityHeaders()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte("ok"))
	}))
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/", nil))
	csp := rr.Header().Get("Content-Security-Policy")
	if strings.Contains(csp, "unsafe-inline") || !strings.Contains(csp, "nonce-") {
		t.Fatalf("bad CSP: %q", csp)
	}
	if rr.Header().Get("X-Frame-Options") != "DENY" || rr.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Fatal("missing security headers")
	}
	if v := rr.Header().Get("Strict-Transport-Security"); v != "" {
		t.Fatalf("HSTS must not be emitted for plain HTTP, got %q", v)
	}
}

func securityHeadersRequest(t *testing.T, mutate func(*http.Request)) *httptest.ResponseRecorder {
	t.Helper()
	h := SecurityHeaders()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	if mutate != nil {
		mutate(req)
	}
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	return rr
}

func TestSecurityHeadersHSTS(t *testing.T) {
	const hstsValue = "max-age=31536000; includeSubDomains"
	tests := []struct {
		name   string
		mutate func(*http.Request)
		want   string
	}{
		{
			name: "plain HTTP omits HSTS",
			mutate: func(r *http.Request) {
				r.TLS = nil
				r.Header.Del("X-Forwarded-Proto")
			},
			want: "",
		},
		{
			name: "TLS request emits HSTS",
			mutate: func(r *http.Request) {
				r.TLS = &tls.ConnectionState{}
			},
			want: hstsValue,
		},
		{
			name: "trusted proxy forwarded https emits HSTS",
			mutate: func(r *http.Request) {
				r.Header.Set("X-Forwarded-Proto", "https")
			},
			want: hstsValue,
		},
		{
			name: "forwarded proto is case-insensitive",
			mutate: func(r *http.Request) {
				r.Header.Set("X-Forwarded-Proto", "HTTPS")
			},
			want: hstsValue,
		},
		{
			name: "forwarded http omits HSTS",
			mutate: func(r *http.Request) {
				r.Header.Set("X-Forwarded-Proto", "http")
			},
			want: "",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rr := securityHeadersRequest(t, tc.mutate)
			if got := rr.Header().Get("Strict-Transport-Security"); got != tc.want {
				t.Fatalf("HSTS = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestSecurityHeadersNonceInContext(t *testing.T) {
	var ctxNonce string
	h := SecurityHeaders()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctxNonce = NonceFromContext(r.Context())
		w.WriteHeader(http.StatusOK)
	}))
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/", nil))
	csp := rr.Header().Get("Content-Security-Policy")
	if ctxNonce == "" {
		t.Fatal("nonce not propagated via request context")
	}
	if !strings.Contains(csp, "nonce-"+ctxNonce+"'") {
		t.Fatalf("context nonce %q not the one used in CSP %q", ctxNonce, csp)
	}
}

func TestHostCheckFailsClosed(t *testing.T) {
	h := HostCheck(HostConfig{Public: true, AllowedHosts: []string{"admin.example.com"}})(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
	}))
	req := httptest.NewRequest("GET", "http://evil.example/", nil)
	req.Host = "evil.example"
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusMisdirectedRequest {
		t.Fatalf("got %d", rr.Code)
	}
}

func TestStampActor(t *testing.T) {
	h := StampActor(auth.ActorKindPAT)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		a, ok := auth.FromContext(r.Context())
		if !ok || a.Kind != auth.ActorKindPAT || a.Origin != auth.ActorOriginRemote || a.UserAgent != "agent" {
			t.Fatal("actor not stamped")
		}
		w.WriteHeader(200)
	}))
	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("User-Agent", "agent")
	req.RemoteAddr = "192.0.2.1:1234"
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
}

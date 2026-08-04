package middleware

import (
 "net/http"
 "net/http/httptest"
 "strings"
 "testing"

 "gorouter/internal/domain/auth"
)

func TestSecurityHeadersUsesNonceAndNoUnsafeInline(t *testing.T) {
 h := SecurityHeaders()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Header().Set("Content-Type", "text/html"); _, _ = w.Write([]byte("ok")) }))
 rr := httptest.NewRecorder(); h.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/", nil))
 csp := rr.Header().Get("Content-Security-Policy")
 if strings.Contains(csp, "unsafe-inline") || !strings.Contains(csp, "nonce-") { t.Fatalf("bad CSP: %q", csp) }
 if rr.Header().Get("X-Frame-Options") != "DENY" || rr.Header().Get("X-Content-Type-Options") != "nosniff" { t.Fatal("missing security headers") }
}

func TestHostCheckFailsClosed(t *testing.T) {
 h := HostCheck(HostConfig{Public: true, AllowedHosts: []string{"admin.example.com"}})(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) }))
 req := httptest.NewRequest("GET", "http://evil.example/", nil); req.Host = "evil.example"
 rr:=httptest.NewRecorder(); h.ServeHTTP(rr, req)
 if rr.Code != http.StatusMisdirectedRequest { t.Fatalf("got %d", rr.Code) }
}

func TestStampActor(t *testing.T) {
 h := StampActor(auth.ActorKindPAT)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { a, ok:=auth.FromContext(r.Context()); if !ok || a.Kind != auth.ActorKindPAT || a.Origin != auth.ActorOriginRemote || a.UserAgent != "agent" { t.Fatal("actor not stamped") }; w.WriteHeader(200) }))
 req:=httptest.NewRequest("GET", "/", nil); req.Header.Set("User-Agent", "agent"); req.RemoteAddr="192.0.2.1:1234"
 rr:=httptest.NewRecorder(); h.ServeHTTP(rr, req)
}

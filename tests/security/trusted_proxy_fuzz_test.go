package security_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"gorouter/internal/transport/middleware"
)

func FuzzTrustedProxyHeaderHandling(f *testing.F) {
	f.Add("1.2.3.4", "10.0.0.1:1234")
	f.Add("", "203.0.113.9:443")
	f.Fuzz(func(t *testing.T, forwarded, remote string) {
		mw, err := middleware.NewTrustedProxy(middleware.TrustedProxyConfig{TrustedProxies: []string{"10.0.0.0/8"}})
		if err != nil {
			t.Fatal(err)
		}
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.Header.Set("X-Forwarded-For", forwarded)
		req.RemoteAddr = remote
		mw(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})).ServeHTTP(httptest.NewRecorder(), req)
	})
}

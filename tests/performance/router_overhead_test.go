package performance

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRouterOverheadIsMeasuredWithoutProvider(t *testing.T) {
	called := false
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { called = true; w.WriteHeader(http.StatusNoContent) })
	r := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	h.ServeHTTP(r, req)
	if !called || r.Code != http.StatusNoContent {
		t.Fatalf("router-only handler was not exercised: called=%v status=%d", called, r.Code)
	}
}

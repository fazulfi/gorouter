package middleware

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func newCSRFOrFail(t *testing.T, cfg CSRFConfig) func(http.Handler) http.Handler {
	t.Helper()
	return CSRF(cfg)
}

func TestCSRF_GetRequestSetsCookie(t *testing.T) {
	mw := newCSRFOrFail(t, CSRFConfig{})

	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/dashboard", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rr.Code)
	}

	cookies := rr.Result().Cookies()
	var csrfCookie *http.Cookie
	for _, c := range cookies {
		if c.Name == csrfCookieName {
			csrfCookie = c
			break
		}
	}
	if csrfCookie == nil {
		t.Fatal("expected CSRF cookie to be set on GET")
	}
	if csrfCookie.HttpOnly {
		t.Error("CSRF cookie should NOT be HttpOnly (JS must read it)")
	}
	if csrfCookie.Secure != false {
		t.Error("CSRF cookie should not be Secure in test (no HTTPS)")
	}
	if csrfCookie.Value == "" {
		t.Error("expected non-empty CSRF token value")
	}
}

func TestCSRF_SafeMethodsExempt(t *testing.T) {
	mw := newCSRFOrFail(t, CSRFConfig{})

	safeMethods := []string{http.MethodGet, http.MethodHead, http.MethodOptions, http.MethodTrace}
	for _, method := range safeMethods {
		t.Run(method, func(t *testing.T) {
			handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
			}))

			req := httptest.NewRequest(method, "/dashboard", nil)
			rr := httptest.NewRecorder()
			handler.ServeHTTP(rr, req)

			if rr.Code != http.StatusOK {
				t.Errorf("expected 200 for safe method %s, got %d", method, rr.Code)
			}
		})
	}
}

func TestCSRF_MutationWithoutToken(t *testing.T) {
	mw := newCSRFOrFail(t, CSRFConfig{})

	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	mutationMethods := []string{http.MethodPost, http.MethodPut, http.MethodDelete, http.MethodPatch}
	for _, method := range mutationMethods {
		t.Run(method, func(t *testing.T) {
			req := httptest.NewRequest(method, "/dashboard/settings", nil)
			req.Header.Set("Cookie", csrfCookieName+"=valid-token-value")
			rr := httptest.NewRecorder()
			handler.ServeHTTP(rr, req)

			if rr.Code != http.StatusForbidden {
				t.Errorf("expected 403 for %s without CSRF header, got %d", method, rr.Code)
			}
		})
	}
}

func TestCSRF_MutationWithValidToken(t *testing.T) {
	mw := newCSRFOrFail(t, CSRFConfig{})

	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	getReq := httptest.NewRequest(http.MethodGet, "/dashboard", nil)
	getRR := httptest.NewRecorder()
	handler.ServeHTTP(getRR, getReq)

	var token string
	for _, c := range getRR.Result().Cookies() {
		if c.Name == csrfCookieName {
			token = c.Value
			break
		}
	}
	if token == "" {
		t.Fatal("failed to get CSRF token from GET")
	}

	req := httptest.NewRequest(http.MethodPost, "/dashboard/settings", nil)
	req.Header.Set("Cookie", csrfCookieName+"="+token)
	req.Header.Set(csrfHeaderName, token)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected 200 with valid CSRF token, got %d", rr.Code)
	}
}

func TestCSRF_MutationWithWrongToken(t *testing.T) {
	mw := newCSRFOrFail(t, CSRFConfig{})

	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodPost, "/dashboard/settings", nil)
	req.Header.Set("Cookie", csrfCookieName+"=real-cookie-token")
	req.Header.Set(csrfHeaderName, "wrong-token-value")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Errorf("expected 403 for mismatched CSRF token, got %d", rr.Code)
	}
}

func TestCSRF_MutationWithEmptyToken(t *testing.T) {
	mw := newCSRFOrFail(t, CSRFConfig{})

	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodPost, "/dashboard/settings", nil)
	req.Header.Set("Cookie", csrfCookieName+"=real-cookie-token")
	req.Header.Set(csrfHeaderName, "")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Errorf("expected 403 for empty CSRF header, got %d", rr.Code)
	}
}

func TestCSRF_NoCookieOnMutation(t *testing.T) {
	mw := newCSRFOrFail(t, CSRFConfig{})

	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodPost, "/dashboard/settings", nil)
	req.Header.Set(csrfHeaderName, "some-token")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Errorf("expected 403 when no CSRF cookie present, got %d", rr.Code)
	}
}

func TestCSRF_TokenUniqueness(t *testing.T) {
	mw := newCSRFOrFail(t, CSRFConfig{})

	tokens := make(map[string]bool)
	for i := 0; i < 10; i++ {
		handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		}))

		req := httptest.NewRequest(http.MethodGet, "/dashboard", nil)
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)

		for _, c := range rr.Result().Cookies() {
			if c.Name == csrfCookieName {
				if tokens[c.Value] {
					t.Error("duplicate CSRF token generated")
				}
				tokens[c.Value] = true
			}
		}
	}
}

func TestCSRF_ErrorBodyOnRejection(t *testing.T) {
	mw := newCSRFOrFail(t, CSRFConfig{})

	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodPost, "/dashboard/settings", nil)
	req.Header.Set("Cookie", csrfCookieName+"=valid")
	req.Header.Set(csrfHeaderName, "wrong")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Errorf("expected 403, got %d", rr.Code)
	}

	body := strings.ToLower(rr.Body.String())
	if !strings.Contains(body, "csrf") {
		t.Errorf("expected error body to mention CSRF, got %q", rr.Body.String())
	}
}

package middleware_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"

	"gorouter/internal/domain/auth"
	"gorouter/internal/transport/middleware"
)

// ---------------------------------------------------------------------------
// Fake validator
// ---------------------------------------------------------------------------

type fakeKeyValidator struct {
	acceptKey string
	userID    uuid.UUID
	err       error
}

func (f *fakeKeyValidator) ValidateModelKey(_ context.Context, rawKey string) (uuid.UUID, error) {
	if f.err != nil {
		return uuid.Nil, f.err
	}
	if f.acceptKey == "" || rawKey == f.acceptKey {
		return f.userID, nil
	}
	return uuid.Nil, errors.New("invalid key")
}

func okValidator(userID uuid.UUID) middleware.ModelKeyValidator {
	return &fakeKeyValidator{acceptKey: "valid-key", userID: userID}
}

func failingValidator(err error) middleware.ModelKeyValidator {
	return &fakeKeyValidator{err: err}
}

// ---------------------------------------------------------------------------
// Test helpers
// ---------------------------------------------------------------------------

func newModelKeyRequest(method, path string, headers map[string]string, queryKey string) *http.Request {
	var req *http.Request
	if method == http.MethodPost {
		req = httptest.NewRequest(method, path, strings.NewReader(`{}`))
	} else {
		req = httptest.NewRequest(method, path, nil)
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	if queryKey != "" {
		req.URL.RawQuery = "key=" + queryKey
	}
	return req
}

func serveModelKeyMiddleware(t *testing.T, validator middleware.ModelKeyValidator, req *http.Request, targetCode int) *httptest.ResponseRecorder {
	t.Helper()
	mw := middleware.ModelKeyAuth(validator)
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify actor is present when request reaches handler
		if actor, ok := auth.FromContext(r.Context()); ok {
			w.Header().Set("X-Actor-UserID", actor.UserID.String())
		}
		w.WriteHeader(targetCode)
	}))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	return w
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

func TestModelKeyAuth_MissingKey_Returns401(t *testing.T) {
	req := newModelKeyRequest(http.MethodGet, "/v1/models", nil, "")
	w := serveModelKeyMiddleware(t, okValidator(uuid.New()), req, http.StatusOK)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", w.Code)
	}

	var resp map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	errObj, ok := resp["error"].(map[string]interface{})
	if !ok {
		t.Fatal("expected error object")
	}
	if errObj["code"] != "UNAUTHORIZED" {
		t.Errorf("expected UNAUTHORIZED code, got %v", errObj["code"])
	}
}

func TestModelKeyAuth_BearerToken_Valid(t *testing.T) {
	userID := uuid.New()
	req := newModelKeyRequest(http.MethodGet, "/v1/models", map[string]string{
		"Authorization": "Bearer valid-key",
	}, "")
	w := serveModelKeyMiddleware(t, okValidator(userID), req, http.StatusOK)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
	if w.Header().Get("X-Actor-UserID") != userID.String() {
		t.Errorf("expected UserID %s, got %s", userID.String(), w.Header().Get("X-Actor-UserID"))
	}
}

func TestModelKeyAuth_BearerToken_Invalid(t *testing.T) {
	req := newModelKeyRequest(http.MethodGet, "/v1/models", map[string]string{
		"Authorization": "Bearer invalid-token",
	}, "")
	w := serveModelKeyMiddleware(t, okValidator(uuid.New()), req, http.StatusOK)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", w.Code)
	}
}

func TestModelKeyAuth_XApiKey_Valid(t *testing.T) {
	userID := uuid.New()
	req := newModelKeyRequest(http.MethodGet, "/v1/models", map[string]string{
		"X-Api-Key": "valid-key",
	}, "")
	w := serveModelKeyMiddleware(t, okValidator(userID), req, http.StatusOK)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
	if w.Header().Get("X-Actor-UserID") != userID.String() {
		t.Errorf("expected UserID %s, got %s", userID.String(), w.Header().Get("X-Actor-UserID"))
	}
}

func TestModelKeyAuth_XGoogApiKey_Valid(t *testing.T) {
	userID := uuid.New()
	req := newModelKeyRequest(http.MethodGet, "/v1/models", map[string]string{
		"X-Goog-Api-Key": "valid-key",
	}, "")
	w := serveModelKeyMiddleware(t, okValidator(userID), req, http.StatusOK)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
	if w.Header().Get("X-Actor-UserID") != userID.String() {
		t.Errorf("expected UserID %s, got %s", userID.String(), w.Header().Get("X-Actor-UserID"))
	}
}

func TestModelKeyAuth_QueryKey_Valid(t *testing.T) {
	userID := uuid.New()
	req := newModelKeyRequest(http.MethodGet, "/v1/models", nil, "valid-key")
	w := serveModelKeyMiddleware(t, okValidator(userID), req, http.StatusOK)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
	if w.Header().Get("X-Actor-UserID") != userID.String() {
		t.Errorf("expected UserID %s, got %s", userID.String(), w.Header().Get("X-Actor-UserID"))
	}
}

func TestModelKeyAuth_BearerPriority(t *testing.T) {
	// Bearer should take priority over x-api-key and query parameter
	userID := uuid.New()
	req := newModelKeyRequest(http.MethodGet, "/v1/models", map[string]string{
		"Authorization": "Bearer valid-key",
		"X-Api-Key":     "wrong-key",
	}, "wrong-key")
	w := serveModelKeyMiddleware(t, okValidator(userID), req, http.StatusOK)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
	if w.Header().Get("X-Actor-UserID") != userID.String() {
		t.Errorf("expected UserID %s, got %s", userID.String(), w.Header().Get("X-Actor-UserID"))
	}
}

func TestModelKeyAuth_EmptyBearer_Rejected(t *testing.T) {
	req := newModelKeyRequest(http.MethodGet, "/v1/models", map[string]string{
		"Authorization": "Bearer ",
	}, "")
	w := serveModelKeyMiddleware(t, okValidator(uuid.New()), req, http.StatusOK)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 for empty bearer, got %d", w.Code)
	}
}

func TestModelKeyAuth_MalformedAuthorization_Rejected(t *testing.T) {
	req := newModelKeyRequest(http.MethodGet, "/v1/models", map[string]string{
		"Authorization": "Basic dXNlcjpwYXNz",
	}, "")
	w := serveModelKeyMiddleware(t, okValidator(uuid.New()), req, http.StatusOK)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 for non-Bearer auth, got %d", w.Code)
	}
}

func TestModelKeyAuth_ActorContextPopulated(t *testing.T) {
	userID := uuid.New()
	req := newModelKeyRequest(http.MethodPost, "/v1/chat/completions", map[string]string{
		"Authorization": "Bearer valid-key",
	}, "")

	mw := middleware.ModelKeyAuth(okValidator(userID))
	var capturedActor *auth.Actor
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var ok bool
		capturedActor, ok = auth.FromContext(r.Context())
		if !ok {
			t.Error("actor not found in context")
		}
		w.WriteHeader(http.StatusOK)
	}))

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
	if capturedActor == nil {
		t.Fatal("expected non-nil actor")
	}
	if capturedActor.UserID != userID {
		t.Errorf("expected UserID %s, got %s", userID.String(), capturedActor.UserID.String())
	}
}

func TestModelKeyAuth_ValidatorError_Returns401(t *testing.T) {
	req := newModelKeyRequest(http.MethodGet, "/v1/models", map[string]string{
		"Authorization": "Bearer some-key",
	}, "")
	w := serveModelKeyMiddleware(t, failingValidator(errors.New("db error")), req, http.StatusOK)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 on validator error, got %d", w.Code)
	}
}

func TestModelKeyAuth_CorrelationIDInError(t *testing.T) {
	req := newModelKeyRequest(http.MethodGet, "/v1/models", nil, "")
	req.Header.Set("X-Request-ID", "test-correlation-123")

	w := serveModelKeyMiddleware(t, okValidator(uuid.New()), req, http.StatusOK)

	var resp map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	errObj, ok := resp["error"].(map[string]interface{})
	if !ok {
		t.Fatal("expected error object")
	}
	if errObj["request_id"] != "test-correlation-123" {
		t.Errorf("expected request_id 'test-correlation-123', got %v", errObj["request_id"])
	}
}

func TestModelKeyAuth_ErrorResponseShape(t *testing.T) {
	req := newModelKeyRequest(http.MethodGet, "/v1/models", nil, "")
	w := serveModelKeyMiddleware(t, okValidator(uuid.New()), req, http.StatusOK)

	var resp map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	errObj, ok := resp["error"].(map[string]interface{})
	if !ok {
		t.Fatal("expected error object")
	}

	// Check safe error contract fields
	if _, ok := errObj["type"]; !ok {
		t.Error("expected 'type' in error")
	}
	if _, ok := errObj["code"]; !ok {
		t.Error("expected 'code' in error")
	}
	if _, ok := errObj["message"]; !ok {
		t.Error("expected 'message' in error")
	}
	if _, ok := errObj["request_id"]; !ok {
		t.Error("expected 'request_id' in error")
	}
	if _, ok := errObj["retryable"]; !ok {
		t.Error("expected 'retryable' in error")
	}

	// Must NOT leak internal details
	for _, key := range []string{"err", "stack", "trace", "credential", "key"} {
		if _, exists := errObj[key]; exists {
			t.Errorf("sensitive field %q should not be in error response", key)
		}
	}
}

func TestModelKeyAuth_NoCredentialLeak(t *testing.T) {
	// Ensure the actual key value is never reflected in the error
	req := newModelKeyRequest(http.MethodGet, "/v1/models", map[string]string{
		"Authorization": "Bearer sk-super-secret-key-12345",
	}, "")
	w := serveModelKeyMiddleware(t, okValidator(uuid.New()), req, http.StatusOK)

	body := w.Body.String()
	if strings.Contains(body, "sk-super-secret-key") {
		t.Error("credential leaked in error response")
	}
	if strings.Contains(body, "valid-key") {
		t.Error("test key leaked in error response")
	}
}

func TestModelKeyAuth_AllExtractionMechanisms(t *testing.T) {
	userID := uuid.New()
	validator := okValidator(userID)

	tests := []struct {
		name    string
		setup   func() *http.Request
		wantOK  bool
		wantErr string
	}{
		{
			name: "no key",
			setup: func() *http.Request {
				return newModelKeyRequest(http.MethodGet, "/v1/models", nil, "")
			},
			wantOK: false,
		},
		{
			name: "Bearer valid",
			setup: func() *http.Request {
				return newModelKeyRequest(http.MethodGet, "/v1/models", map[string]string{
					"Authorization": "Bearer valid-key",
				}, "")
			},
			wantOK: true,
		},
		{
			name: "x-api-key valid",
			setup: func() *http.Request {
				return newModelKeyRequest(http.MethodGet, "/v1/models", map[string]string{
					"X-Api-Key": "valid-key",
				}, "")
			},
			wantOK: true,
		},
		{
			name: "x-goog-api-key valid",
			setup: func() *http.Request {
				return newModelKeyRequest(http.MethodGet, "/v1/models", map[string]string{
					"X-Goog-Api-Key": "valid-key",
				}, "")
			},
			wantOK: true,
		},
		{
			name: "query key valid",
			setup: func() *http.Request {
				return newModelKeyRequest(http.MethodGet, "/v1/models", nil, "valid-key")
			},
			wantOK: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := tt.setup()
			w := serveModelKeyMiddleware(t, validator, req, http.StatusOK)
			if tt.wantOK && w.Code != http.StatusOK {
				t.Errorf("expected 200, got %d", w.Code)
			}
			if !tt.wantOK && w.Code != http.StatusUnauthorized {
				t.Errorf("expected 401, got %d", w.Code)
			}
		})
	}
}

func TestModelKeyAuth_PanicsOnNilValidator(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic on nil validator")
		}
	}()
	middleware.ModelKeyAuth(nil)
}

func TestModelKeyAuth_ValidatorFuncAdapter(t *testing.T) {
	userID := uuid.New()
	validatorFn := middleware.ModelKeyValidatorFunc(func(_ context.Context, rawKey string) (uuid.UUID, error) {
		if rawKey == "func-key" {
			return userID, nil
		}
		return uuid.Nil, errors.New("invalid")
	})

	req := newModelKeyRequest(http.MethodGet, "/v1/models", map[string]string{
		"X-Api-Key": "func-key",
	}, "")
	w := serveModelKeyMiddleware(t, validatorFn, req, http.StatusOK)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
	if w.Header().Get("X-Actor-UserID") != userID.String() {
		t.Errorf("expected UserID %s", userID.String())
	}
}

func TestModelKeyAuth_ContentTypeJSON(t *testing.T) {
	req := newModelKeyRequest(http.MethodGet, "/v1/models", nil, "")
	w := serveModelKeyMiddleware(t, okValidator(uuid.New()), req, http.StatusOK)

	ct := w.Header().Get("Content-Type")
	if !strings.HasPrefix(ct, "application/json") {
		t.Errorf("expected Content-Type application/json, got %s", ct)
	}
}

func TestModelKeyAuth_QueryKeyRedactedFromURL(t *testing.T) {
	userID := uuid.New()
	req := newModelKeyRequest(http.MethodGet, "/v1/models", nil, "valid-key")
	if !strings.Contains(req.URL.RawQuery, "key=") {
		t.Fatal("test setup: expected key= in RawQuery before middleware")
	}

	w := serveModelKeyMiddleware(t, okValidator(userID), req, http.StatusOK)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if w.Header().Get("X-Actor-UserID") != userID.String() {
		t.Errorf("expected UserID %s, got %s", userID.String(), w.Header().Get("X-Actor-UserID"))
	}
	if strings.Contains(req.URL.RawQuery, "key=") {
		t.Errorf("key= query param was NOT redacted from RawQuery: %q", req.URL.RawQuery)
	}
}

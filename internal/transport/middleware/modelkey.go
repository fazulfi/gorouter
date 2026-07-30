// Package middleware provides HTTP middleware for the gorouter HTTP server.
package middleware

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/google/uuid"

	"gorouter/internal/domain/auth"
	"gorouter/internal/shared"
)

// ---------------------------------------------------------------------------
// Model key validator interface
// ---------------------------------------------------------------------------

// ModelKeyValidator validates a raw model API key and returns the owning
// user ID on success. Implementations MUST NOT leak the key or its hash
// in error messages or logs.
type ModelKeyValidator interface {
	ValidateModelKey(ctx context.Context, rawKey string) (userID uuid.UUID, err error)
}

// ModelKeyValidatorFunc is an adapter that allows a plain function to serve
// as a ModelKeyValidator.
type ModelKeyValidatorFunc func(ctx context.Context, rawKey string) (uuid.UUID, error)

// ValidateModelKey implements ModelKeyValidator.
func (f ModelKeyValidatorFunc) ValidateModelKey(ctx context.Context, rawKey string) (uuid.UUID, error) {
	return f(ctx, rawKey)
}

// ---------------------------------------------------------------------------
// Sentinel errors
// ---------------------------------------------------------------------------

var (
	// ErrModelKeyMissing is returned when no model API credential can be
	// extracted from the request.
	ErrModelKeyMissing = shared.NewAppError(
		shared.ErrUnauthorized,
		"model API key required. Provide via Authorization Bearer header, x-api-key, x-goog-api-key, or key query parameter.",
		http.StatusUnauthorized,
		nil,
	)

	// ErrModelKeyInvalid is returned when the presented credential does not
	// match any active API key.
	ErrModelKeyInvalid = shared.NewAppError(
		shared.ErrUnauthorized,
		"model API key invalid or expired.",
		http.StatusUnauthorized,
		nil,
	)
)

// ---------------------------------------------------------------------------
// Key extraction
// ---------------------------------------------------------------------------

// modelKeyExtractions defines the ordered precedence for extracting a model
// API key from an HTTP request. Earlier sources take priority.
var modelKeyExtractions = []struct {
	name string
	fn   func(r *http.Request) (string, bool)
}{
	{name: "Authorization Bearer", fn: extractBearerToken},
	{name: "x-api-key", fn: extractHeader("X-Api-Key")},
	{name: "x-goog-api-key", fn: extractHeader("X-Goog-Api-Key")},
	{name: "key query", fn: extractQueryKey},
}

const bearerPrefix = "Bearer "

func extractBearerToken(r *http.Request) (string, bool) {
	auth := r.Header.Get("Authorization")
	if !strings.HasPrefix(auth, bearerPrefix) {
		return "", false
	}
	token := strings.TrimSpace(strings.TrimPrefix(auth, bearerPrefix))
	if token == "" {
		return "", false
	}
	return token, true
}

func extractHeader(header string) func(*http.Request) (string, bool) {
	return func(r *http.Request) (string, bool) {
		v := r.Header.Get(header)
		if v == "" {
			return "", false
		}
		return v, true
	}
}

func extractQueryKey(r *http.Request) (string, bool) {
	v := r.URL.Query().Get("key")
	if v == "" {
		return "", false
	}
	// Strip the key from RawQuery after extraction so it is not
	// forwarded to upstream handlers or logged.
	q := r.URL.Query()
	q.Del("key")
	r.URL.RawQuery = q.Encode()
	return v, true
}

// ---------------------------------------------------------------------------
// Context helpers
// ---------------------------------------------------------------------------

// GetModelKeyRequestID extracts the request/correlation ID stored by the
// middleware from context. Returns empty string when not present.
func GetModelKeyRequestID(ctx context.Context) string {
	v, _ := ctx.Value(modelKeyReqIDKey).(string)
	return v
}

type modelKeyReqIDType string

const modelKeyReqIDKey modelKeyReqIDType = "modelkey_req_id"

// ---------------------------------------------------------------------------
// Middleware
// ---------------------------------------------------------------------------

// ModelKeyAuth returns an HTTP middleware that enforces model API key
// authentication. It extracts the key from (in priority order):
//
//  1. Authorization: Bearer <key>
//  2. x-api-key header
//  3. x-goog-api-key header
//  4. key query parameter
//
// On success it stores an auth.Actor in the request context and calls the
// next handler. On failure it writes a structured JSON error response and
// does NOT call the next handler.
//
// The validator parameter must be non-nil. Pass a no-op implementation that
// always succeeds to disable enforcement (useful in tests).
func ModelKeyAuth(validator ModelKeyValidator) func(http.Handler) http.Handler {
	if validator == nil {
		panic("middleware.ModelKeyAuth: validator must not be nil")
	}
	return modelKeyAuth(validator)
}

func modelKeyAuth(validator ModelKeyValidator) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			rawKey, ok := extractKey(r)
			if !ok {
				writeModelKeyError(w, r, ErrModelKeyMissing)
				return
			}

			userID, err := validator.ValidateModelKey(r.Context(), rawKey)
			if err != nil {
				// Treat all validation errors as an invalid key. Never
				// distinguish between "not found", "expired", or "db error"
				// to avoid leaking credential state to the client.
				writeModelKeyError(w, r, ErrModelKeyInvalid)
				return
			}

			actor := &auth.Actor{
				UserID:  userID,
				Scopes:  []string{},
				IsAdmin: false,
			}
			ctx := auth.ContextWithActor(r.Context(), actor)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// extractKey attempts to extract a model API key using each extraction
// strategy in priority order. Returns the first key found.
func extractKey(r *http.Request) (string, bool) {
	for _, ext := range modelKeyExtractions {
		if key, ok := ext.fn(r); ok {
			return key, true
		}
	}
	return "", false
}

// writeModelKeyError writes a structured JSON error response and records the
// correlation ID. The error is sanitised and does not leak credential details.
func writeModelKeyError(w http.ResponseWriter, r *http.Request, appErr *shared.AppError) {
	reqID := GetRequestID(r.Context())
	if reqID == "" {
		reqID = r.Header.Get("X-Request-ID")
	}

	errToSend := shared.NewAppError(
		appErr.Code,
		appErr.Message,
		appErr.HTTPStatusCode(),
		nil,
		shared.WithRequestID(reqID),
	)

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(errToSend.HTTPStatusCode())

	// Ensure the JSON output follows the safe error contract: type, code,
	// message, request_id, retryable.
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"error": map[string]interface{}{
			"type":       errorTypeFromCode(errToSend.Code),
			"code":       string(errToSend.Code),
			"message":    errToSend.Message,
			"request_id": reqID,
			"retryable":  false,
		},
	})
}

func errorTypeFromCode(code shared.ErrorCode) string {
	switch code {
	case shared.ErrUnauthorized, shared.ErrForbidden:
		return "authentication_error"
	case shared.ErrValidation:
		return "invalid_request_error"
	case shared.ErrRateLimited:
		return "rate_limit_error"
	case shared.ErrTimeout:
		return "timeout_error"
	default:
		return "api_error"
	}
}

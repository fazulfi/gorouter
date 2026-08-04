// Package v1 implements the Admin API v1 resource-group handlers
// (P4-T01, API-05). Every handler is a mapping-only transport surface
// (ARCHITECTURE rule 4): it resolves the authenticated actor from the
// request context, delegates to the application service wired through
// Dependencies, and projects the response. No business logic is
// re-implemented in this package.
//
// Authorization is enforced by the design §6 matrix: the route registry
// (routes.go) declares the actor classes each route accepts (session, PAT,
// CLI; job actors never invoke HTTP routes), TestAuthzMatrix asserts the
// registry exactly matches the contract, and the admin router mounts every
// route behind the CSRF-aware session/PAT authenticator.
package v1

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"

	"gorouter/internal/domain/auth"
	comboerr "gorouter/internal/domain/combo"
	"gorouter/internal/transport/middleware"

	"github.com/go-chi/chi/v5"
)

// maxBodyBytes bounds request bodies read by the mapping handlers. Credential
// and configuration payloads never legitimately exceed this size; anything
// larger is rejected rather than buffered.
const maxBodyBytes = 1 << 20 // 1 MiB

// Auditor records a sanitized audit entry with actor, origin, ip, action,
// target and the correlation id. The production writer is provided by the
// backend lane; a nil auditor is a no-op (audit remains the responsibility of
// the wired application services, which receive the actor).
type Auditor interface {
	Audit(ctx context.Context, actor *auth.Actor, action, target string, before, after json.RawMessage) error
}

// actorFrom returns the authenticated actor stamped by the session or PAT
// middleware, or nil when no actor is present.
func actorFrom(r *http.Request) *auth.Actor {
	a, _ := auth.FromContext(r.Context())
	return a
}

// requestID returns the correlation/request id recorded by the Correlation
// middleware, or "" when absent.
func requestID(r *http.Request) string {
	return middleware.GetRequestID(r.Context())
}

// pathParam returns a chi URL path parameter value.
func pathParam(r *http.Request, name string) string {
	return chi.URLParam(r, name)
}

// clientIP returns the trusted-proxy-normalized client IP, falling back to
// the remote address when no real-IP was recorded.
func clientIP(r *http.Request) string {
	if v := middleware.GetRealIP(r.Context()); v != "" {
		return v
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// writeJSON writes v as a JSON response with the given status.
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// errorBody is the contract error shape (Error schema).
type errorBody struct {
	Code       string `json:"code"`
	Message    string `json:"message"`
	Details    any    `json:"details,omitempty"`
	RequestID  string `json:"request_id,omitempty"`
}

// statusFor maps a service error to an HTTP status. Unknown errors are
// internal errors (500); the mapping surface never leaks internals.
func statusFor(err error) int {
	switch {
	case err == nil:
		return http.StatusOK
	case errors.Is(err, comboerr.ErrNotFound),
		errors.Is(err, errNotFound):
		return http.StatusNotFound
	case errors.Is(err, comboerr.ErrInvalidConfig),
		errors.Is(err, comboerr.ErrInvalidMember),
		errors.Is(err, errInvalid):
		return http.StatusBadRequest
	case errors.Is(err, errConflict):
		return http.StatusConflict
	case errors.Is(err, errForbidden):
		return http.StatusForbidden
	case errors.Is(err, errUnauthorized):
		return http.StatusUnauthorized
	default:
		return http.StatusInternalServerError
	}
}

// local sentinel errors used by the mapping handlers themselves (the services
// may return their own; those are mapped through the domain packages above).
var (
	errNotFound    = errors.New("not found")
	errInvalid     = errors.New("invalid request")
	errConflict    = errors.New("conflict")
	errForbidden   = errors.New("forbidden")
	errUnauthorized = errors.New("unauthorized")
)

// writeError writes the contract error shape for err.
func writeError(w http.ResponseWriter, r *http.Request, err error) {
	code := "INTERNAL_ERROR"
	switch statusFor(err) {
	case http.StatusNotFound:
		code = "NOT_FOUND"
	case http.StatusBadRequest:
		code = "INVALID_REQUEST"
	case http.StatusConflict:
		code = "CONFLICT"
	case http.StatusForbidden:
		code = "FORBIDDEN"
	case http.StatusUnauthorized:
		code = "UNAUTHORIZED"
	}
	writeJSON(w, statusFor(err), errorBody{
		Code:      code,
		Message:   err.Error(),
		RequestID: requestID(r),
	})
}

// backendUnavailable is the response of a route whose application service is
// not wired yet. The route resolves (never 404) but honestly reports that the
// backend surface is unavailable; the wiring is the backend lane's
// integration step (design §15).
func backendUnavailable(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusServiceUnavailable, errorBody{
		Code:      "BACKEND_UNAVAILABLE",
		Message:   "this surface is not available yet",
		RequestID: requestID(r),
	})
}

// featureDisabled is the response of a host-operation route whose feature
// flag is closed (the default). It never enumerates capabilities.
func featureDisabled(w http.ResponseWriter, r *http.Request, feature string) {
	writeJSON(w, http.StatusForbidden, errorBody{
		Code:      "FEATURE_DISABLED",
		Message:   "this operation is disabled",
		RequestID: requestID(r),
	})
}

// decodeBody reads at most maxBodyBytes from the request body and decodes it
// into dst. An empty body decodes to a zero dst without error.
func decodeBody(r *http.Request, dst any) error {
	body, err := io.ReadAll(io.LimitReader(r.Body, maxBodyBytes))
	if err != nil {
		return errInvalid
	}
	r.Body = io.NopCloser(bytes.NewReader(body))
	if len(body) == 0 {
		return nil
	}
	if err := json.Unmarshal(body, dst); err != nil {
		return errInvalid
	}
	return nil
}

// confirmRequest is the prompt/confirmation contract for host operations
// (design §6, P0-1): the caller must echo confirm=true in the mutation body.
type confirmRequest struct {
	Confirm bool `json:"confirm"`
}

// confirmPresent reports whether the request body carries confirm=true. The
// body is re-wound so a downstream handler can decode it.
func confirmPresent(r *http.Request) bool {
	var c confirmRequest
	if err := decodeBody(r, &c); err != nil {
		return false
	}
	return c.Confirm
}

func sanitizeJSON(v any) json.RawMessage {
	if v == nil {
		return nil
	}
	raw, err := json.Marshal(v)
	if err != nil {
		return nil
	}
	return json.RawMessage(bytes.TrimSpace(raw))
}

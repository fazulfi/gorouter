package media

import (
	"errors"
	"fmt"
	"net/http"
	"regexp"

	"gorouter/internal/domain/provider"
	"gorouter/internal/shared"
)

const (
	// ErrCodePayloadTooLarge classifies 413-class media errors. HTTP status
	// 413 semantics follow http.StatusRequestEntityTooLarge.
	ErrCodePayloadTooLarge shared.ErrorCode = "PAYLOAD_TOO_LARGE"
	// ErrCodeCapabilityUnproven classifies fail-closed capability rejections.
	ErrCodeCapabilityUnproven shared.ErrorCode = "CAPABILITY_UNPROVEN"
)

var (
	// ErrMalformedRequest is wrapped by codec decode errors (mirrors
	// internal/engine/formats/errors.go ErrMalformedBody).
	ErrMalformedRequest = errors.New("malformed media request")
	// ErrCapabilityUnproven is wrapped by capability-check rejections when a
	// provider has no proven mapping for the requested modality.
	ErrCapabilityUnproven = errors.New("media modality capability not proven for provider")
)

// NewPayloadTooLargeError returns a typed 413-class error carrying the ceiling
// and its exact source authority.
func NewPayloadTooLargeError(limit int64, authority string) *shared.AppError {
	return &shared.AppError{
		Code:       ErrCodePayloadTooLarge,
		Message:    fmt.Sprintf("payload exceeds transport ceiling of %d bytes (authority: %s)", limit, authority),
		HTTPStatus: http.StatusRequestEntityTooLarge,
		Retryable:  false,
	}
}

// IsPayloadTooLarge reports whether err is a typed 413-class media error.
func IsPayloadTooLarge(err error) bool {
	var ae *shared.AppError
	return errors.As(err, &ae) && ae.Code == ErrCodePayloadTooLarge
}

// NewCapabilityUnprovenError returns the fail-closed rejection for a modality
// on a provider with no proven capability mapping.
func NewCapabilityUnprovenError(m Modality, pt provider.ProviderType, authority string) *shared.AppError {
	return &shared.AppError{
		Code:       ErrCodeCapabilityUnproven,
		Message:    fmt.Sprintf("modality %q not proven for provider %q (authority: %s)", m, pt, authority),
		HTTPStatus: http.StatusBadRequest,
		Retryable:  false,
	}
}

// IsCapabilityUnproven reports whether err is a fail-closed capability
// rejection.
func IsCapabilityUnproven(err error) bool {
	var ae *shared.AppError
	return errors.As(err, &ae) && ae.Code == ErrCodeCapabilityUnproven
}

// NewUpstreamError classifies a non-2xx upstream response. The body is
// truncated to 1024 bytes and placed in Details (mirrors
// internal/app/executor/errors.go NewUpstreamError).
func NewUpstreamError(statusCode int, body string) *shared.AppError {
	if len(body) > 1024 {
		body = body[:1024]
	}
	return &shared.AppError{
		Code:       shared.ErrInternal,
		Message:    "media upstream request failed",
		HTTPStatus: statusCode,
		Details:    map[string]any{"status_code": statusCode, "body": body},
		Retryable:  statusCode >= 500,
	}
}

// NewAuthError classifies definitive 401/403 upstream rejection (decision
// #336 semantics). Body truncated to 512 bytes.
func NewAuthError(statusCode int, body string) *shared.AppError {
	if len(body) > 512 {
		body = body[:512]
	}
	return &shared.AppError{
		Code:       shared.ErrUnauthorized,
		Message:    "media upstream authentication failed",
		HTTPStatus: statusCode,
		Details:    map[string]any{"status_code": statusCode, "body": body},
		Retryable:  false,
	}
}

// NewRateLimitError classifies 429 upstream rejection. Body truncated to 512
// bytes.
func NewRateLimitError(statusCode int, body string) *shared.AppError {
	if len(body) > 512 {
		body = body[:512]
	}
	return &shared.AppError{
		Code:       shared.ErrRateLimited,
		Message:    "media upstream rate limit exceeded",
		HTTPStatus: statusCode,
		Details:    map[string]any{"status_code": statusCode, "body": body},
		Retryable:  false,
	}
}

// redactions strips credential-like substrings from error messages. Token
// patterns mirror the upstream sanitizer (decolua/9router @
// 79918c7830695bbca4a45c9fea4a42c3e9fd73d1, open-sse/handlers/videoCore.js:23
// `Bearer\s+[A-Za-z0-9._~+/=-]{8,}`) extended to the credential families in
// the tree's own sanitizers (internal/engine/mutators/pipeline.go,
// internal/engine/providers/specialized/cookie_session.go).
var redactions = []struct {
	pattern *regexp.Regexp
	repl    string
}{
	{regexp.MustCompile(`Bearer [A-Za-z0-9._~+/=-]+`), "Bearer ***"},
	{regexp.MustCompile(`sk-proj-[A-Za-z0-9_-]+`), "sk-proj-***"},
	{regexp.MustCompile(`sk-ant-[A-Za-z0-9_-]+`), "sk-ant-***"},
	{regexp.MustCompile(`session-token=[^&\s;]+`), "session-token=[REDACTED]"},
	{regexp.MustCompile(`token=[^&\s;]+`), "token=[REDACTED]"},
	{regexp.MustCompile(`api-key=[^&\s;]+`), "api-key=[REDACTED]"},
	{regexp.MustCompile(`access_token=[^&\s;]+`), "access_token=[REDACTED]"},
	{regexp.MustCompile(`x-api-key[:\s]+[A-Za-z0-9._~+/=-]+`), "x-api-key***"},
}

// RedactErrorString strips credential-like substrings from an error message
// before it is surfaced to logs or callers (decisions #127, #145).
func RedactErrorString(msg string) string {
	for _, r := range redactions {
		msg = r.pattern.ReplaceAllString(msg, r.repl)
	}
	return msg
}

// RedactError returns a copy of err whose message has been credential-redacted.
func RedactError(err error) error {
	if err == nil {
		return nil
	}
	var ae *shared.AppError
	if errors.As(err, &ae) {
		clone := *ae
		clone.Message = RedactErrorString(clone.Message)
		return &clone
	}
	return fmt.Errorf("%s", RedactErrorString(err.Error()))
}

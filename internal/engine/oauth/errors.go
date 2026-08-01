package oauth

import (
	"errors"
	"fmt"
)

var (
	ErrFlowNotFound    = errors.New("oauth flow not found for provider")
	ErrInvalidCallback = errors.New("invalid OAuth callback")
	ErrStateMismatch   = errors.New("OAuth state parameter mismatch")
	ErrReplayDetected  = errors.New("replay attack detected on callback")
)

// RedactedError wraps an error with a public-safe message.
// Internal details and credentials are stripped from serialization.
type RedactedError struct {
	Public  string
	Wrapped error
}

func (e *RedactedError) Error() string {
	if e.Wrapped != nil {
		return e.Public + ": " + e.Wrapped.Error()
	}
	return e.Public
}

func (e *RedactedError) Unwrap() error { return e.Wrapped }

// NewRedactedError creates a new RedactedError.
func NewRedactedError(public string, wrapped error) *RedactedError {
	return &RedactedError{Public: public, Wrapped: wrapped}
}

// SafeErrorMessage returns a sanitized error message safe for API responses.
func SafeErrorMessage(err error) string {
	if err == nil {
		return ""
	}
	var re *RedactedError
	if errors.As(err, &re) {
		return re.Public
	}
	msg := err.Error()
	if len(msg) > 200 {
		msg = msg[:200] + "..."
	}
	return fmt.Sprintf("oauth error: %s", msg)
}

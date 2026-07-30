package executor

import (
	"errors"
	"strings"
	"testing"

	"gorouter/internal/shared"
)

func TestNewUpstreamError(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		body       string
	}{
		{"server error 500", 500, "internal server error"},
		{"bad gateway 502", 502, "bad gateway"},
		{"service unavailable 503", 503, "service unavailable"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := NewUpstreamError(tt.statusCode, tt.body)
			if err == nil {
				t.Fatal("expected non-nil error")
			}
			if err.Code != shared.ErrInternal {
				t.Errorf("expected code %s, got %s", shared.ErrInternal, err.Code)
			}
			if err.HTTPStatus != tt.statusCode {
				t.Errorf("expected HTTPStatus %d, got %d", tt.statusCode, err.HTTPStatus)
			}
			if err.Message != "upstream API returned error" {
				t.Errorf("unexpected message: %s", err.Message)
			}
			details, ok := err.Details.(map[string]interface{})
			if !ok {
				t.Fatal("expected Details to be a map")
			}
			if details["status_code"] != tt.statusCode {
				t.Errorf("expected status_code %d in details, got %v", tt.statusCode, details["status_code"])
			}
			if details["body"] != tt.body {
				t.Errorf("expected body %q in details, got %q", tt.body, details["body"])
			}
		})
	}
}

func TestNewUpstreamError_TruncatesBody(t *testing.T) {
	longBody := strings.Repeat("a", 2000)
	err := NewUpstreamError(500, longBody)
	details := err.Details.(map[string]interface{})
	body := details["body"].(string)
	if len(body) > 1024+3 { // 1024 + "..."
		t.Errorf("expected body to be truncated, got length %d", len(body))
	}
	if !strings.HasSuffix(body, "...") {
		t.Error("expected truncated body to end with '...'")
	}
}

func TestNewStreamFailedError(t *testing.T) {
	cause := errors.New("connection reset")
	err := NewStreamFailedError(cause)
	if err == nil {
		t.Fatal("expected non-nil error")
	}
	if err.Code != shared.ErrInternal {
		t.Errorf("expected code %s, got %s", shared.ErrInternal, err.Code)
	}
	if err.Message != "stream creation failed" {
		t.Errorf("unexpected message: %s", err.Message)
	}
	if !errors.Is(err, cause) {
		t.Errorf("expected error to wrap cause, got %v", err.Err)
	}
}

func TestNewStreamFailedError_NilCause(t *testing.T) {
	err := NewStreamFailedError(nil)
	if err == nil {
		t.Fatal("expected non-nil error")
	}
	if err.Err != nil {
		t.Errorf("expected nil wrapped error, got %v", err.Err)
	}
}

func TestNewExecutorTimeoutError(t *testing.T) {
	cause := errors.New("deadline exceeded")
	err := NewExecutorTimeoutError(cause)
	if err == nil {
		t.Fatal("expected non-nil error")
	}
	if err.Code != shared.ErrTimeout {
		t.Errorf("expected code %s, got %s", shared.ErrTimeout, err.Code)
	}
	if err.Message != "executor request timed out" {
		t.Errorf("unexpected message: %s", err.Message)
	}
	if !errors.Is(err, cause) {
		t.Errorf("expected error to wrap cause, got %v", err.Err)
	}
}

func TestNewExecutorTimeoutError_NilCause(t *testing.T) {
	err := NewExecutorTimeoutError(nil)
	if err == nil {
		t.Fatal("expected non-nil error")
	}
	if err.Err != nil {
		t.Errorf("expected nil wrapped error, got %v", err.Err)
	}
}

func TestNewAuthError(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		body       string
	}{
		{"unauthorized 401", 401, "invalid API key"},
		{"forbidden 403", 403, "insufficient permissions"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := NewAuthError(tt.statusCode, tt.body)
			if err == nil {
				t.Fatal("expected non-nil error")
			}
			if err.Code != shared.ErrUnauthorized {
				t.Errorf("expected code %s, got %s", shared.ErrUnauthorized, err.Code)
			}
			if err.Message != "upstream authentication failed" {
				t.Errorf("unexpected message: %s", err.Message)
			}
			if err.HTTPStatus != tt.statusCode {
				t.Errorf("expected HTTPStatus %d, got %d", tt.statusCode, err.HTTPStatus)
			}
			details, ok := err.Details.(map[string]interface{})
			if !ok {
				t.Fatal("expected Details to be a map")
			}
			if details["status_code"] != tt.statusCode {
				t.Errorf("expected status_code %d in details, got %v", tt.statusCode, details["status_code"])
			}
		})
	}
}

func TestNewAuthError_TruncatesBody(t *testing.T) {
	longBody := strings.Repeat("b", 1000)
	err := NewAuthError(401, longBody)
	details := err.Details.(map[string]interface{})
	body := details["body"].(string)
	if len(body) > 512+3 {
		t.Errorf("expected body to be truncated, got length %d", len(body))
	}
}

func TestNewRateLimitError(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		body       string
	}{
		{"rate limited 429", 429, "too many requests"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := NewRateLimitError(tt.statusCode, tt.body)
			if err == nil {
				t.Fatal("expected non-nil error")
			}
			if err.Code != shared.ErrRateLimited {
				t.Errorf("expected code %s, got %s", shared.ErrRateLimited, err.Code)
			}
			if err.Message != "upstream rate limit exceeded" {
				t.Errorf("unexpected message: %s", err.Message)
			}
			if err.HTTPStatus != tt.statusCode {
				t.Errorf("expected HTTPStatus %d, got %d", tt.statusCode, err.HTTPStatus)
			}
			details, ok := err.Details.(map[string]interface{})
			if !ok {
				t.Fatal("expected Details to be a map")
			}
			if details["status_code"] != tt.statusCode {
				t.Errorf("expected status_code %d in details, got %v", tt.statusCode, details["status_code"])
			}
		})
	}
}

func TestNewRateLimitError_TruncatesBody(t *testing.T) {
	longBody := strings.Repeat("c", 1000)
	err := NewRateLimitError(429, longBody)
	details := err.Details.(map[string]interface{})
	body := details["body"].(string)
	if len(body) > 512+3 {
		t.Errorf("expected body to be truncated, got length %d", len(body))
	}
}

func TestTruncateString(t *testing.T) {
	tests := []struct {
		name   string
		input  string
		maxLen int
		want   string
	}{
		{"short string", "hello", 10, "hello"},
		{"exact length", "hello", 5, "hello"},
		{"long string", "hello world", 5, "hello..."},
		{"empty string", "", 10, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := truncateString(tt.input, tt.maxLen)
			if got != tt.want {
				t.Errorf("truncateString(%q, %d) = %q, want %q", tt.input, tt.maxLen, got, tt.want)
			}
		})
	}
}

func TestExecutorSentinelErrors(t *testing.T) {
	if ErrExecutorNotFound == nil {
		t.Fatal("expected ErrExecutorNotFound to be non-nil")
	}
	if ErrExecutorNotFound.Code != shared.ErrNotFound {
		t.Errorf("expected code %s, got %s", shared.ErrNotFound, ErrExecutorNotFound.Code)
	}

	if ErrUnsupportedProviderType == nil {
		t.Fatal("expected ErrUnsupportedProviderType to be non-nil")
	}
	if ErrUnsupportedProviderType.Code != shared.ErrValidation {
		t.Errorf("expected code %s, got %s", shared.ErrValidation, ErrUnsupportedProviderType.Code)
	}
}

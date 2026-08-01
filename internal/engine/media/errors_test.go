package media

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"gorouter/internal/domain/provider"
	"gorouter/internal/shared"
)

func TestNewPayloadTooLargeError(t *testing.T) {
	err := NewPayloadTooLargeError(1024, "test-authority")
	var ae *shared.AppError
	if !errors.As(err, &ae) {
		t.Fatalf("expected *shared.AppError, got %T", err)
	}
	if ae.Code != ErrCodePayloadTooLarge {
		t.Errorf("Code = %q, want %q", ae.Code, ErrCodePayloadTooLarge)
	}
	if ae.HTTPStatus != http.StatusRequestEntityTooLarge {
		t.Errorf("HTTPStatus = %d, want 413", ae.HTTPStatus)
	}
	if ae.Retryable {
		t.Errorf("Retryable = true, want false")
	}
	if !strings.Contains(ae.Message, "1024") || !strings.Contains(ae.Message, "test-authority") {
		t.Errorf("Message = %q, want ceiling and authority", ae.Message)
	}
	if !IsPayloadTooLarge(err) {
		t.Errorf("IsPayloadTooLarge(err) = false, want true")
	}
	if IsPayloadTooLarge(errors.New("nope")) {
		t.Errorf("IsPayloadTooLarge(plain) = true, want false")
	}
	if IsPayloadTooLarge(NewCapabilityUnprovenError(ModalityTTS, "x", "a")) {
		t.Errorf("IsPayloadTooLarge(capability error) = true, want false")
	}
}

func TestNewCapabilityUnprovenError(t *testing.T) {
	err := NewCapabilityUnprovenError(ModalityVideoEdit, provider.ProviderType("openai"), "authority-x")
	var ae *shared.AppError
	if !errors.As(err, &ae) {
		t.Fatalf("expected *shared.AppError, got %T", err)
	}
	if ae.Code != ErrCodeCapabilityUnproven {
		t.Errorf("Code = %q, want %q", ae.Code, ErrCodeCapabilityUnproven)
	}
	if ae.HTTPStatus != http.StatusBadRequest {
		t.Errorf("HTTPStatus = %d, want 400", ae.HTTPStatus)
	}
	if !strings.Contains(ae.Message, "video_edit") || !strings.Contains(ae.Message, "authority-x") {
		t.Errorf("Message = %q, want modality and authority", ae.Message)
	}
	if !IsCapabilityUnproven(err) {
		t.Errorf("IsCapabilityUnproven(err) = false, want true")
	}
	if IsCapabilityUnproven(errors.New("nope")) {
		t.Errorf("IsCapabilityUnproven(plain) = true, want false")
	}
}

func TestUpstreamErrorClassification(t *testing.T) {
	long := strings.Repeat("x", 3000)

	err := NewUpstreamError(500, long)
	var ae *shared.AppError
	errors.As(err, &ae)
	if ae.Code != shared.ErrInternal || !ae.Retryable {
		t.Errorf("5xx: Code=%q Retryable=%v want ErrInternal,true", ae.Code, ae.Retryable)
	}
	if len(ae.Details.(map[string]any)["body"].(string)) != 1024 {
		t.Errorf("5xx body truncation = %d, want 1024", len(ae.Details.(map[string]any)["body"].(string)))
	}

	err = NewAuthError(401, long)
	errors.As(err, &ae)
	if ae.Code != shared.ErrUnauthorized || ae.Retryable {
		t.Errorf("401: Code=%q Retryable=%v want ErrUnauthorized,false", ae.Code, ae.Retryable)
	}
	if len(ae.Details.(map[string]any)["body"].(string)) != 512 {
		t.Errorf("401 body truncation = %d, want 512", len(ae.Details.(map[string]any)["body"].(string)))
	}

	err = NewRateLimitError(429, "slow down")
	errors.As(err, &ae)
	if ae.Code != shared.ErrRateLimited || ae.Retryable {
		t.Errorf("429: Code=%q Retryable=%v want ErrRateLimited,false", ae.Code, ae.Retryable)
	}
}

func TestRedactErrorString(t *testing.T) {
	cases := []struct{ in, want string }{
		{"sk-proj-abcdef", "sk-proj-***"},
		{"sk-ant-abcdef", "sk-ant-***"},
		{"Authorization: Bearer abc123", "Authorization: Bearer ***"},
		{"x-api-key: secret", "x-api-key***"},
		{"session-token=deadbeef", "session-token=[REDACTED]"},
		{"token=xyz", "token=[REDACTED]"},
		{"api-key=xyz", "api-key=[REDACTED]"},
		{"access_token=xyz", "access_token=[REDACTED]"},
		{"Bearer sk-proj-TOKEN with tail", "Bearer *** with tail"},
		{"clean message", "clean message"},
	}
	for _, tc := range cases {
		if got := RedactErrorString(tc.in); got != tc.want {
			t.Errorf("RedactErrorString(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestRedactError(t *testing.T) {
	inner := fmt.Errorf("upstream said: sk-proj-TOP-SECRET and token=leak")
	red := RedactError(inner)
	if strings.Contains(red.Error(), "sk-proj-TOP-SECRET") {
		t.Errorf("RedactError leaked credential: %q", red.Error())
	}
	if !strings.Contains(red.Error(), "upstream said") {
		t.Errorf("RedactError changed message: %q", red.Error())
	}

	app := NewUpstreamError(500, "sk-proj-LEAK")
	redApp := RedactError(app)
	var ae *shared.AppError
	if !errors.As(redApp, &ae) {
		t.Fatalf("RedactError(*AppError) = %T, want *AppError", redApp)
	}
	if strings.Contains(ae.Message, "sk-proj-LEAK") {
		t.Errorf("RedactError leaked credential in AppError message: %q", ae.Message)
	}
	if ae.HTTPStatus != 500 {
		t.Errorf("RedactError lost HTTPStatus: %d", ae.HTTPStatus)
	}
	if RedactError(nil) != nil {
		t.Errorf("RedactError(nil) != nil")
	}
}

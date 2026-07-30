package shared_test

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"testing"

	"gorouter/internal/shared"
)

func TestAppError_Error(t *testing.T) {
	err := shared.NewAppError(shared.ErrNotFound, "resource not found", 0, nil)
	want := "resource not found"
	if got := err.Error(); got != want {
		t.Errorf("Error() = %q, want %q", got, want)
	}
}

func TestAppError_ErrorWithWrapped(t *testing.T) {
	inner := errors.New("underlying issue")
	err := shared.NewAppError(shared.ErrInternal, "something broke", 0, inner)
	want := "something broke: underlying issue"
	if got := err.Error(); got != want {
		t.Errorf("Error() = %q, want %q", got, want)
	}
}

func TestAppError_Unwrap_ReturnsWrapped(t *testing.T) {
	inner := errors.New("inner error")
	err := shared.NewAppError(shared.ErrInternal, "msg", 0, inner)

	if !errors.Is(err, inner) {
		t.Error("errors.Is should find the wrapped error")
	}
}

func TestAppError_HTTPStatusCode_Explicit(t *testing.T) {
	err := shared.NewAppError(shared.ErrValidation, "bad input", http.StatusUnprocessableEntity, nil)
	if got := err.HTTPStatusCode(); got != http.StatusUnprocessableEntity {
		t.Errorf("HTTPStatusCode() = %d, want %d", got, http.StatusUnprocessableEntity)
	}
}

func TestAppError_HTTPStatusCode_Mapped(t *testing.T) {
	tests := []struct {
		code     shared.ErrorCode
		expected int
	}{
		{shared.ErrValidation, http.StatusBadRequest},
		{shared.ErrUnauthorized, http.StatusUnauthorized},
		{shared.ErrForbidden, http.StatusForbidden},
		{shared.ErrNotFound, http.StatusNotFound},
		{shared.ErrConflict, http.StatusConflict},
		{shared.ErrRateLimited, http.StatusTooManyRequests},
		{shared.ErrTimeout, http.StatusGatewayTimeout},
		{shared.ErrInternal, http.StatusInternalServerError},
	}

	for _, tt := range tests {
		t.Run(string(tt.code), func(t *testing.T) {
			err := shared.NewAppError(tt.code, "test", 0, nil)
			if got := err.HTTPStatusCode(); got != tt.expected {
				t.Errorf("HTTPStatusCode() = %d, want %d", got, tt.expected)
			}
		})
	}
}

func TestAppError_RedactDetails_CopyOnWrite(t *testing.T) {
	inner := errors.New("secret")
	err := shared.NewAppError(shared.ErrInternal, "msg", 0, inner)
	err.Details = map[string]string{"password": "supersecret"}
	err.RequestID = "req-123"

	redacted := err.RedactDetails()

	// Details are NOT cleared by RedactDetails - callers use `json:"-"` tags.
	if redacted.Details == nil {
		t.Fatal("redacted Details should be preserved (not cleared)")
	}
	if redacted.Err != nil {
		t.Error("redacted Err should be nil")
	}
	if redacted.RequestID != "" {
		t.Error("redacted RequestID should be empty")
	}

	if err.Details == nil {
		t.Fatal("original Details should remain intact")
	}
	origDetails, ok := err.Details.(map[string]string)
	if !ok || origDetails["password"] != "supersecret" {
		t.Error("original Details should retain sensitive data")
	}
	if err.RequestID != "req-123" {
		t.Error("original RequestID should be retained")
	}
}

func TestAppError_JSON_OmitsSensitiveFields(t *testing.T) {
	inner := errors.New("secret-info")
	err := shared.NewAppError(shared.ErrValidation, "bad input", 0, inner)
	err.RequestID = "req-456"

	b, jsonErr := json.Marshal(err)
	if jsonErr != nil {
		t.Fatal(jsonErr)
	}

	var result map[string]interface{}
	if jsonErr := json.Unmarshal(b, &result); jsonErr != nil {
		t.Fatal(jsonErr)
	}

	if result["code"] != "VALIDATION_ERROR" {
		t.Errorf("code = %v, want VALIDATION_ERROR", result["code"])
	}
	if result["message"] != "bad input" {
		t.Errorf("message = %v, want 'bad input'", result["message"])
	}

	for _, key := range []string{"Err", "HTTPStatus", "request_id"} {
		if _, exists := result[key]; exists {
			t.Errorf("sensitive field %q should not appear in JSON", key)
		}
	}
}

func TestAppError_JSON_IncludesDetails(t *testing.T) {
	err := shared.NewAppError(shared.ErrValidation, "bad input", 0, nil)
	err.Details = map[string]string{"field": "name", "reason": "required"}

	b, jsonErr := json.Marshal(err)
	if jsonErr != nil {
		t.Fatal(jsonErr)
	}

	var result map[string]interface{}
	if jsonErr := json.Unmarshal(b, &result); jsonErr != nil {
		t.Fatal(jsonErr)
	}

	if _, exists := result["details"]; !exists {
		t.Error("non-sensitive details should be included in JSON")
	}
}

func TestAsAppError_Direct(t *testing.T) {
	appErr := shared.NewAppError(shared.ErrNotFound, "not found", 0, nil)
	extracted, ok := shared.AsAppError(appErr)
	if !ok {
		t.Fatal("AsAppError should find direct AppError")
	}
	if extracted.Code != shared.ErrNotFound {
		t.Errorf("Code = %s, want %s", extracted.Code, shared.ErrNotFound)
	}
}

func TestAsAppError_FmtWrapped(t *testing.T) {
	inner := shared.NewAppError(shared.ErrNotFound, "not found", 0, nil)
	wrapped := fmt.Errorf("wrapped: %w", inner)

	extracted, ok := shared.AsAppError(wrapped)
	if !ok {
		t.Fatal("AsAppError should find AppError in fmt.Errorf wrapped chain")
	}
	if extracted.Code != shared.ErrNotFound {
		t.Errorf("Code = %s, want %s", extracted.Code, shared.ErrNotFound)
	}
}

func TestAsAppError_Nil(t *testing.T) {
	if _, ok := shared.AsAppError(nil); ok {
		t.Error("AsAppError(nil) should return false")
	}
}

func TestAsAppError_PlainError(t *testing.T) {
	stdErr := errors.New("standard error")
	if _, ok := shared.AsAppError(stdErr); ok {
		t.Error("AsAppError on plain error should return false")
	}
}

func TestIsAppError(t *testing.T) {
	if shared.IsAppError(nil) {
		t.Error("nil should not be an AppError")
	}
	if shared.IsAppError(errors.New("standard")) {
		t.Error("standard error should not be AppError")
	}
	if !shared.IsAppError(shared.NewAppError(shared.ErrConflict, "conflict", 0, nil)) {
		t.Error("AppError should be recognized")
	}
}

func TestMapHTTPStatus(t *testing.T) {
	if got := shared.MapHTTPStatus(shared.ErrNotFound); got != http.StatusNotFound {
		t.Errorf("MapHTTPStatus(ErrNotFound) = %d, want %d", got, http.StatusNotFound)
	}
	if got := shared.MapHTTPStatus(shared.ErrUnauthorized); got != http.StatusUnauthorized {
		t.Errorf("MapHTTPStatus(ErrUnauthorized) = %d, want %d", got, http.StatusUnauthorized)
	}
	if got := shared.MapHTTPStatus(shared.ErrInternal); got != http.StatusInternalServerError {
		t.Errorf("MapHTTPStatus(ErrInternal) = %d, want %d", got, http.StatusInternalServerError)
	}
}

func TestAppError_NilSafe(t *testing.T) {
	var err *shared.AppError

	if got := err.Error(); got != "" {
		t.Errorf("nil AppError.Error() = %q, want empty", got)
	}
	if got := err.HTTPStatusCode(); got != http.StatusInternalServerError {
		t.Errorf("nil AppError.HTTPStatusCode() = %d, want 500", got)
	}
	if got := err.RedactDetails(); got != nil {
		t.Error("nil AppError.RedactDetails() should return nil")
	}
}

func TestMapHTTPStatus_Unknown(t *testing.T) {
	if got := shared.MapHTTPStatus("UNKNOWN_CODE"); got != http.StatusInternalServerError {
		t.Errorf("unknown code should map to 500, got %d", got)
	}
}

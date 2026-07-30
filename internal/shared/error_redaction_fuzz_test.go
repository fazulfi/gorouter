package shared_test

import (
	"encoding/json"
	"testing"
	"unicode/utf8"

	"gorouter/internal/shared"
)

// FuzzAppErrorRedactDetails fuzzes the RedactDetails method with various
// combinations of ErrorCode, message, and underlying error to ensure the
// redacted copy never leaks internal fields.
func FuzzAppErrorRedactDetails(f *testing.F) {
	seeds := []struct {
		code    string
		message string
		err     string
		reqID   string
	}{
		{string(shared.ErrInternal), "internal error", "db connection failed", "req-001"},
		{string(shared.ErrValidation), "bad input", "", "req-002"},
		{string(shared.ErrNotFound), "not found", "sql: no rows", "req-003"},
		{string(shared.ErrUnauthorized), "unauthorized", "invalid token", ""},
		{string(shared.ErrForbidden), "forbidden", "permission denied", "req-004"},
		{string(shared.ErrConflict), "conflict", "duplicate key", "req-005"},
		{string(shared.ErrRateLimited), "rate limited", "too many", ""},
		{string(shared.ErrTimeout), "timeout", "dial tcp: i/o timeout", "req-006"},
		{"", "", "", ""},
		{"UNKNOWN_CODE", "weird", "some error", "req-999"},
		{"VALIdATION_ERROR", "Edge Cases!", "underlying\nwith newline", ""},
	}
	for _, s := range seeds {
		var inner error
		if s.err != "" {
			inner = &simpleErr{s.err}
		}
		appErr := shared.NewAppError(shared.ErrorCode(s.code), s.message, 0, inner)
		appErr.RequestID = s.reqID
		f.Add(s.code, s.message, s.err, s.reqID)
	}

	f.Fuzz(func(t *testing.T, code, message, errMsg, reqID string) {
		var inner error
		if errMsg != "" {
			inner = &simpleErr{errMsg}
		}
		appErr := shared.NewAppError(shared.ErrorCode(code), message, 0, inner)
		appErr.RequestID = reqID

		redacted := appErr.RedactDetails()

		// Invariant 1: redacted Err must always be nil
		if redacted.Err != nil {
			t.Errorf("redacted Err should be nil, got %v", redacted.Err)
		}

		// Invariant 2: redacted RequestID must always be empty
		if redacted.RequestID != "" {
			t.Errorf("redacted RequestID should be empty, got %q", redacted.RequestID)
		}

		// Invariant 3: redacted Code should match original
		if redacted.Code != appErr.Code {
			t.Errorf("redacted Code = %q, want %q", redacted.Code, appErr.Code)
		}

		// Invariant 4: redacted Message should match original
		if redacted.Message != appErr.Message {
			t.Errorf("redacted Message = %q, want %q", redacted.Message, appErr.Message)
		}

		// Invariant 5: JSON marshal must not contain Err, HTTPStatus, or RequestID fields
		data, jErr := json.Marshal(redacted)
		if jErr != nil {
			t.Fatalf("json.Marshal(redacted) error: %v", jErr)
		}
		var result map[string]interface{}
		if jErr := json.Unmarshal(data, &result); jErr != nil {
			t.Fatalf("json.Unmarshal error: %v", jErr)
		}
		for _, key := range []string{"Err", "HTTPStatus", "request_id"} {
			if _, exists := result[key]; exists {
				t.Errorf("sensitive field %q should not appear in JSON output", key)
			}
		}
	})
}

// FuzzAppErrorJSONSerialization fuzzes JSON marshalling/unmarshalling of
// AppError to ensure it never panics and maintains the error contract.
func FuzzAppErrorJSONSerialization(f *testing.F) {
	seeds := []struct {
		code    string
		message string
		details string
	}{
		{string(shared.ErrInternal), "oops", `{"key":"val"}`},
		{string(shared.ErrValidation), "", ""},
		{string(shared.ErrNotFound), "missing", `{"id":"123"}`},
	}
	for _, s := range seeds {
		f.Add(s.code, s.message, s.details)
	}

	f.Fuzz(func(t *testing.T, code, message, detailsJSON string) {
		var details interface{}
		if detailsJSON != "" {
			var m map[string]interface{}
			if err := json.Unmarshal([]byte(detailsJSON), &m); err == nil {
				details = m
			}
		}
		appErr := shared.NewAppError(shared.ErrorCode(code), message, 0, nil)
		appErr.Details = details

		// Marshal must never panic
		data, err := json.Marshal(appErr)
		if err != nil {
			// Only expected error is for cyclic structures, which we don't produce
			return
		}

		// Unmarshal must produce valid structure
		var restored shared.AppError
		if err := json.Unmarshal(data, &restored); err != nil {
			t.Fatalf("unmarshal failed: %v", err)
		}

		// Invariant: Code and Message survive round trip for valid UTF-8
		// (JSON encoding cannot preserve invalid UTF-8 byte sequences)
		if utf8.ValidString(code) && restored.Code != shared.ErrorCode(code) && code != "" {
			t.Errorf("Code round-trip: got %q, want %q", restored.Code, code)
		}
		if utf8.ValidString(message) && restored.Message != message && message != "" {
			t.Errorf("Message round-trip: got %q, want %q", restored.Message, message)
		}
	})
}

// FuzzMapHTTPStatus fuzzes the HTTP status mapping for all possible ErrorCode
// strings to ensure it never panics and always returns a valid status code.
func FuzzMapHTTPStatus(f *testing.F) {
	seeds := []string{
		string(shared.ErrInternal),
		string(shared.ErrValidation),
		string(shared.ErrNotFound),
		string(shared.ErrUnauthorized),
		string(shared.ErrForbidden),
		string(shared.ErrConflict),
		string(shared.ErrRateLimited),
		string(shared.ErrTimeout),
		"",
		"UNKNOWN",
		"INVALID_ERROR_CODE_WITH_VERY_LONG_NAME_1234567890",
	}
	for _, s := range seeds {
		f.Add(s)
	}

	f.Fuzz(func(t *testing.T, code string) {
		status := shared.MapHTTPStatus(shared.ErrorCode(code))
		// Must return a valid HTTP status code in the 1xx-5xx range
		if status < 100 || status > 599 {
			t.Errorf("MapHTTPStatus(%q) = %d, want valid HTTP status (100-599)", code, status)
		}
		// Unknown codes must fall back to 500 (not panic or return 0)
		known := map[shared.ErrorCode]bool{
			shared.ErrInternal:     true,
			shared.ErrValidation:   true,
			shared.ErrNotFound:     true,
			shared.ErrUnauthorized: true,
			shared.ErrForbidden:    true,
			shared.ErrConflict:     true,
			shared.ErrRateLimited:  true,
			shared.ErrTimeout:      true,
		}
		if !known[shared.ErrorCode(code)] && status != 500 {
			t.Errorf("MapHTTPStatus(%q) = %d, want 500 for unknown codes", code, status)
		}
	})
}

// simpleErr is a minimal error implementation used for fuzz testing.
type simpleErr struct {
	msg string
}

func (e *simpleErr) Error() string { return e.msg }

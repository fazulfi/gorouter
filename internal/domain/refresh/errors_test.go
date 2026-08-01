package refresh_test

import (
	"errors"
	"testing"

	"gorouter/internal/domain/refresh"
)

func TestDefinitiveError(t *testing.T) {
	t.Parallel()
	err := refresh.ErrDefinitive("reason")
	if !refresh.IsDefinitive(err) {
		t.Error("should be definitive")
	}
	if refresh.IsTransient(err) {
		t.Error("should not be transient")
	}
	var de *refresh.DefinitiveError
	if !errors.As(err, &de) {
		t.Error("should unwrap")
	}
	if de.Reason != "reason" {
		t.Errorf("Reason = %q", de.Reason)
	}
}

func TestTransientError(t *testing.T) {
	t.Parallel()
	cause := errors.New("network error")
	err := &refresh.TransientError{Reason: "timeout", Cause: cause}
	if !refresh.IsTransient(err) {
		t.Error("should be transient")
	}
	if refresh.IsDefinitive(err) {
		t.Error("should not be definitive")
	}
	if !errors.Is(err, cause) {
		t.Error("should wrap cause")
	}
}

func TestSentinelErrors(t *testing.T) {
	t.Parallel()
	if refresh.ErrUnsupported == nil {
		t.Error("ErrUnsupported should be defined")
	}
	if refresh.ErrBusy == nil {
		t.Error("ErrBusy should be defined")
	}
	if refresh.ErrTimedOut == nil {
		t.Error("ErrTimedOut should be defined")
	}
}

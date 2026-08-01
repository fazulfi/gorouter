package refresh

import (
	"errors"
	"fmt"
)

var (
	ErrUnsupported = errors.New("refresh not supported for this account")
	ErrBusy        = errors.New("global refresh concurrency limit reached")
	ErrTimedOut    = errors.New("refresh attempt timed out")
)

type DefinitiveError struct {
	Reason string
	Cause  error
}

func (e *DefinitiveError) Error() string {
	if e.Cause != nil {
		return fmt.Sprintf("definitive refresh failure: %s: %v", e.Reason, e.Cause)
	}
	return fmt.Sprintf("definitive refresh failure: %s", e.Reason)
}

func (e *DefinitiveError) Unwrap() error { return e.Cause }

func ErrDefinitive(reason string) error {
	return &DefinitiveError{Reason: reason}
}

type TransientError struct {
	Reason string
	Cause  error
}

func (e *TransientError) Error() string {
	if e.Cause != nil {
		return fmt.Sprintf("transient refresh failure: %s: %v", e.Reason, e.Cause)
	}
	return fmt.Sprintf("transient refresh failure: %s", e.Reason)
}

func (e *TransientError) Unwrap() error { return e.Cause }

func ErrTransient(reason string) error {
	return &TransientError{Reason: reason}
}

func IsDefinitive(err error) bool {
	if err == nil {
		return false
	}
	var de *DefinitiveError
	return errors.As(err, &de)
}

func IsTransient(err error) bool {
	if err == nil {
		return false
	}
	var te *TransientError
	return errors.As(err, &te)
}

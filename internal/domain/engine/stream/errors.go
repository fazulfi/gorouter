package stream

import "errors"

var (
	// ErrStreamClosed is returned when attempting to push to or peek from a
	// stream that has already been closed or cancelled.
	ErrStreamClosed = errors.New("stream already closed")

	// ErrStreamCancelled is returned when the stream was explicitly cancelled
	// by the caller before completion.
	ErrStreamCancelled = errors.New("stream cancelled by caller")

	// ErrPeekTimeout is returned when the first chunk peek operation times out
	// before any data arrives.
	ErrPeekTimeout = errors.New("first chunk peek timed out")

	// ErrStallTimeout is returned when the stream has been idle for longer than
	// the configured stall timeout without receiving any data.
	ErrStallTimeout = errors.New("stream stalled — no data received")
)

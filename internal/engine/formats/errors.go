package formats

import "errors"

var (
	// ErrUnknownFormat is returned when the endpoint and body do not match any
	// known wire format.
	ErrUnknownFormat = errors.New("unknown request format")

	// ErrMalformedBody is returned when the request body is syntactically invalid
	// JSON or is structurally incomplete for the detected format.
	ErrMalformedBody = errors.New("malformed request body")

	// ErrUnsupportedFormat is returned when a format is recognised but not
	// currently supported by the codec layer.
	ErrUnsupportedFormat = errors.New("format not supported")

	// ErrInvalidChunk is returned when a stream chunk cannot be decoded
	// for the given format.
	ErrInvalidChunk = errors.New("invalid stream chunk")
)

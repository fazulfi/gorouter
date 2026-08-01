// Package formats provides format detection and codec interfaces for wire
// protocol formats supported by the engine layer.
package formats

import "encoding/json"

// Chunk represents a single decoded data frame from a provider stream.
// Unlike stream.Chunk which holds raw bytes, this type represents the
// format-decoded payload that codec implementations produce and consume.
type Chunk struct {
	Event   string
	Data    json.RawMessage
	IsFinal bool
	Error   error
}

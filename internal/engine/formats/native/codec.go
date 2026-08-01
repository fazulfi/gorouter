package native

import (
	"encoding/json"
	"fmt"

	"gorouter/internal/domain/engine"
	"gorouter/internal/engine/formats"
)

// PassthroughCodec implements formats.Codec for native passthrough of
// protocol-specific binary, NDJSON, and event stream formats. It performs
// minimal validation and passes data through without transformation.
//
// This codec is used when the executor implements NativePassthroughExecutor
// and the source format does not require canonical translation.
type PassthroughCodec struct {
	format engine.RequestFormat
}

// NewPassthroughCodec creates a native passthrough codec for the given format.
func NewPassthroughCodec(format engine.RequestFormat) *PassthroughCodec {
	return &PassthroughCodec{format: format}
}

func (c *PassthroughCodec) Format() engine.RequestFormat {
	return c.format
}

func (c *PassthroughCodec) DecodeRequest(body json.RawMessage) (*engine.Request, error) {
	if len(body) == 0 {
		return nil, fmt.Errorf("native: %w", formats.ErrMalformedBody)
	}
	return &engine.Request{
		Format:     c.format,
		RawBody:    body,
		MappedBody: body,
	}, nil
}

func (c *PassthroughCodec) EncodeResponse(resp *engine.Response) (json.RawMessage, error) {
	if resp == nil || len(resp.Body) == 0 {
		return nil, fmt.Errorf("native: %w", formats.ErrMalformedBody)
	}
	return resp.Body, nil
}

func (c *PassthroughCodec) DecodeStreamChunk(eventType string, data []byte) (*formats.Chunk, error) {
	if len(data) == 0 {
		return nil, fmt.Errorf("native: %w", formats.ErrInvalidChunk)
	}
	// For native passthrough, we treat each data frame as opaque.
	// The executor handles protocol-specific semantics.
	chunk := &formats.Chunk{
		Event:   "passthrough",
		Data:    data,
		IsFinal: false,
	}

	// NDJSON terminal: {"done":true}
	if isNDJSONTerminal(data) {
		chunk.IsFinal = true
		chunk.Event = "ndjson.done"
	}

	return chunk, nil
}

func (c *PassthroughCodec) EncodeStreamChunk(chunk *formats.Chunk) ([]byte, error) {
	if chunk.Event == "ndjson.done" {
		return chunk.Data, nil
	}
	if chunk.Data != nil {
		return chunk.Data, nil
	}
	return []byte{}, nil
}

func (c *PassthroughCodec) TerminalEvent() []byte {
	// Native passthrough has no universal terminal event.
	// Terminal detection is format-specific and handled by the executor.
	return nil
}

// isNDJSONTerminal checks whether a raw data frame signals NDJSON completion
// by containing {"done":true} as a top-level key.
func isNDJSONTerminal(data []byte) bool {
	var obj struct {
		Done *bool `json:"done"`
	}
	if err := json.Unmarshal(data, &obj); err != nil {
		return false
	}
	return obj.Done != nil && *obj.Done
}

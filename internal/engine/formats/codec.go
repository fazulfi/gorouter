package formats

import (
	"encoding/json"

	"gorouter/internal/domain/engine"
)

// Codec handles format-specific marshal/unmarshal for request and response
// wire formats. Each supported format has a corresponding Codec implementation.
type Codec interface {
	// Format returns the domain format constant this codec handles.
	Format() engine.RequestFormat

	// DecodeRequest parses a wire-format JSON request body into an engine.Request.
	// The returned Request has RawBody set to the original body and MappedBody
	// set to the canonical representation.
	DecodeRequest(body json.RawMessage) (*engine.Request, error)

	// EncodeResponse serialises an engine.Response into the wire-format JSON body.
	EncodeResponse(resp *engine.Response) (json.RawMessage, error)

	// DecodeStreamChunk parses a single raw SSE/NDJSON data frame into a Chunk.
	// The eventType parameter carries the SSE event type (e.g. "message_stop")
	// from the wire, which some codecs (Claude) require for correct terminal
	// detection when the data payload alone is ambiguous. Codecs whose data
	// payloads contain type information (OpenAI, Gemini, Responses) may ignore
	// eventType.
	// Returns ErrInvalidChunk if the data cannot be decoded.
	DecodeStreamChunk(eventType string, data []byte) (*Chunk, error)

	// EncodeStreamChunk serialises a Chunk into a raw SSE/NDJSON data frame.
	EncodeStreamChunk(chunk *Chunk) ([]byte, error)

	// TerminalEvent returns the byte sequence that signals stream completion
	// for this format (e.g. "data: [DONE]\n\n" for OpenAI Chat).
	TerminalEvent() []byte
}

// Package engine provides core domain interfaces and types for the AI request execution
// pipeline, including request/response models, format constants, and a lightweight
// stream reference to decouple the engine root from the stream sub-package.
package engine

import (
	"github.com/google/uuid"
)

// RequestFormat identifies the wire protocol format of an incoming LLM inference request.
type RequestFormat string

const (
	FormatOpenAIChat     RequestFormat = "openai_chat"
	FormatOpenAICompat   RequestFormat = "openai_compat"
	FormatCodexResponses RequestFormat = "codex_responses"
	FormatAnthropic      RequestFormat = "anthropic"
	FormatGemini         RequestFormat = "gemini"
)

// TerminalState enumerates the final disposition of a completed request.
type TerminalState string

const (
	TerminalSuccess     TerminalState = "success"
	TerminalError       TerminalState = "error"
	TerminalCancelled   TerminalState = "cancelled"
	TerminalTimeout     TerminalState = "timeout"
	TerminalStreamAbort TerminalState = "stream_abort"
)

// Usage tracks token consumption reported by a provider for a completed request.
type Usage struct {
	PromptTokens     int
	CompletionTokens int
	TotalTokens      int
}

// Request represents an inbound LLM inference request after it has been parsed,
// validated, and mapped into the engine's canonical representation.
type Request struct {
	ID          uuid.UUID
	Format      RequestFormat
	Model       string
	RawBody     []byte
	MappedBody  []byte
	Headers     map[string]string
	Stream      bool
	MaxTokens   int
	Temperature *float64
	UserID      *uuid.UUID
}

// StreamRef is a lightweight reference to an active stream that avoids importing
// the stream sub-package from the domain engine root.
type StreamRef interface {
	StreamID() uuid.UUID
}

// Response represents the result of executing a request against a provider account.
// For streaming responses the Body field is typically empty and the caller should
// consume chunks via the Stream reference.
type Response struct {
	RequestID  uuid.UUID
	Body       []byte
	Stream     StreamRef
	Model      string
	Usage      *Usage
	StatusCode int
	Headers    map[string]string
}

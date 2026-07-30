package translate

import (
	"context"
	"encoding/json"

	"gorouter/internal/domain/engine"
)

// ---------------------------------------------------------------------------
// Format detection constants
// ---------------------------------------------------------------------------

const (
	RequestFormatOpenAIChat                          = engine.FormatOpenAIChat
	RequestFormatCodexResponses                      = engine.FormatCodexResponses
	RequestFormatUnknown        engine.RequestFormat = "unknown"
)

// ---------------------------------------------------------------------------
// Canonical message & tool types
// These types represent the internal canonical representation stored inside
// engine.Request.MappedBody as a CanonicalBody JSON payload.
// ---------------------------------------------------------------------------

// Message represents a single conversation message in the canonical format.
type Message struct {
	Role       string          `json:"role"`
	Content    json.RawMessage `json:"content"` // string or array of content parts
	ToolCalls  []ToolCall      `json:"tool_calls,omitempty"`
	ToolCallID string          `json:"tool_call_id,omitempty"`
}

// ToolCall represents an invocation of a tool during a conversation turn.
type ToolCall struct {
	ID       string   `json:"id"`
	Type     string   `json:"type"`
	Function Function `json:"function"`
}

// Tool is a function tool definition advertised by the model.
type Tool struct {
	Type     string   `json:"type"`
	Function Function `json:"function"`
}

// Function describes a callable function for tool-use.
type Function struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Parameters  json.RawMessage `json:"parameters,omitempty"`
}

// CanonicalBody is the internal representation stored in engine.Request.MappedBody
// after a wire-protocol body has been parsed. It holds fields that are not
// directly represented on the engine.Request struct.
type CanonicalBody struct {
	Messages         []Message       `json:"messages,omitempty"`
	Instructions     string          `json:"instructions,omitempty"`
	TopP             *float64        `json:"top_p,omitempty"`
	Stop             []string        `json:"stop,omitempty"`
	PresencePenalty  *float64        `json:"presence_penalty,omitempty"`
	FrequencyPenalty *float64        `json:"frequency_penalty,omitempty"`
	Tools            []Tool          `json:"tools,omitempty"`
	ToolChoice       json.RawMessage `json:"tool_choice,omitempty"`
	ResponseFormat   json.RawMessage `json:"response_format,omitempty"`
	Input            json.RawMessage `json:"input,omitempty"`
	Store            *bool           `json:"store,omitempty"`
	Metadata         json.RawMessage `json:"metadata,omitempty"`
	Reasoning        json.RawMessage `json:"reasoning,omitempty"`
}

// ---------------------------------------------------------------------------
// Service
// ---------------------------------------------------------------------------

// Service provides format detection and request body translation.
// It is stateless — all methods take a context and raw JSON body.
type Service struct{}

// NewService creates a new translate Service.
func NewService() *Service {
	return &Service{}
}

// DetectFormat examines the raw JSON request body and determines its wire
// protocol format. It returns the corresponding engine.RequestFormat constant
// or an error if the body does not match any known format.
func (s *Service) DetectFormat(_ context.Context, body json.RawMessage) (engine.RequestFormat, error) {
	if len(body) == 0 {
		return RequestFormatUnknown, ErrUnsupportedFormat
	}

	var raw map[string]json.RawMessage
	if err := json.Unmarshal(body, &raw); err != nil {
		return RequestFormatUnknown, ErrUnsupportedFormat
	}

	_, hasMessages := raw["messages"]
	_, hasInput := raw["input"]
	_, hasModel := raw["model"]

	if hasMessages && hasModel {
		return RequestFormatOpenAIChat, nil
	}

	if hasInput && hasModel {
		return RequestFormatCodexResponses, nil
	}

	return RequestFormatUnknown, ErrUnsupportedFormat
}

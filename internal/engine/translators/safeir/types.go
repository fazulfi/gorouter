package safeir

import (
	"encoding/json"
	"errors"
)

var ErrLossyTranslation = errors.New("lossy translation: content would be dropped")

type SafeIR struct {
	Messages    []Message       `json:"messages,omitempty"`
	System      string          `json:"system,omitempty"`
	MaxTokens   int             `json:"max_tokens,omitempty"`
	Temperature *float64        `json:"temperature,omitempty"`
	TopP        *float64        `json:"top_p,omitempty"`
	Stop        []string        `json:"stop,omitempty"`
	Tools       []Tool          `json:"tools,omitempty"`
	ToolChoice  interface{}     `json:"tool_choice,omitempty"`
	Model       string          `json:"model,omitempty"`
	Stream      bool            `json:"stream,omitempty"`
	Metadata    json.RawMessage `json:"metadata,omitempty"`
	RawBody     json.RawMessage `json:"-"`
}

type Message struct {
	Role       string            `json:"role"`
	Text       string            `json:"text,omitempty"`
	ToolCallID string            `json:"tool_call_id,omitempty"`
	ToolCalls  []ToolCall        `json:"tool_calls,omitempty"`
	ImageURLs  []string          `json:"image_urls,omitempty"`
	ImageMIMEs map[string]string `json:"-"`
	Thinking   string            `json:"thinking,omitempty"`
	RawParts   json.RawMessage   `json:"raw_parts,omitempty"`
}

type ToolCall struct {
	ID        string          `json:"id"`
	Type      string          `json:"type"`
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}

type Tool struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Parameters  json.RawMessage `json:"parameters,omitempty"`
}

package translate

import (
	"context"
	"encoding/json"
	"fmt"

	"gorouter/internal/domain/engine"
)

// TranslateChatToRequest parses an OpenAI Chat Completions request body into an
// engine.Request. The parsed messages and tool definitions are stored in the
// request's MappedBody as a canonical JSON payload.
func (s *Service) TranslateChatToRequest(ctx context.Context, body json.RawMessage) (*engine.Request, error) {
	if len(body) == 0 {
		return nil, fmt.Errorf("translate chat: %w", ErrInvalidRequest)
	}

	var raw struct {
		Model            string              `json:"model"`
		Messages         []json.RawMessage   `json:"messages"`
		Stream           bool                `json:"stream"`
		MaxTokens        int                 `json:"max_tokens"`
		Temperature      *float64            `json:"temperature"`
		TopP             *float64            `json:"top_p"`
		Stop             json.RawMessage     `json:"stop"`       // string or []string
		PresencePenalty  *float64            `json:"presence_penalty"`
		FrequencyPenalty *float64            `json:"frequency_penalty"`
		Tools            []json.RawMessage   `json:"tools"`
		ToolChoice       json.RawMessage     `json:"tool_choice"`
		ResponseFormat   json.RawMessage     `json:"response_format"`
	}

	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("translate chat: %w", ErrTranslationFailed)
	}

	if raw.Model == "" {
		return nil, fmt.Errorf("translate chat: %w", ErrInvalidRequest)
	}
	if len(raw.Messages) == 0 {
		return nil, fmt.Errorf("translate chat: %w", ErrInvalidRequest)
	}

	// Parse messages.
	messages := make([]Message, 0, len(raw.Messages))
	for _, m := range raw.Messages {
		msg, err := parseChatMessage(m)
		if err != nil {
			return nil, fmt.Errorf("translate chat: %w", err)
		}
		messages = append(messages, msg)
	}

	// Parse tools.
	tools, err := parseChatTools(raw.Tools)
	if err != nil {
		return nil, fmt.Errorf("translate chat: %w", err)
	}

	// Normalize stop: "stop" or ["stop"] → CanonicalBody uses []string.
	stop := parseStop(raw.Stop)

	canon := CanonicalBody{
		Messages:         messages,
		TopP:             raw.TopP,
		Stop:             stop,
		PresencePenalty:  raw.PresencePenalty,
		FrequencyPenalty: raw.FrequencyPenalty,
		Tools:            tools,
		ToolChoice:       raw.ToolChoice,
		ResponseFormat:   raw.ResponseFormat,
	}

	mapped, err := json.Marshal(canon)
	if err != nil {
		return nil, fmt.Errorf("translate chat: %w", ErrTranslationFailed)
	}

	return &engine.Request{
		Model:      raw.Model,
		RawBody:    body,
		MappedBody: mapped,
		Stream:     raw.Stream,
		MaxTokens:  raw.MaxTokens,
		Temperature: raw.Temperature,
	}, nil
}

// parseChatMessage unmarshals a single message from the OpenAI Chat format.
func parseChatMessage(raw json.RawMessage) (Message, error) {
	var base struct {
		Role    string          `json:"role"`
		Content json.RawMessage `json:"content"` // string or array
	}
	if err := json.Unmarshal(raw, &base); err != nil {
		return Message{}, ErrInvalidRequest
	}
	if base.Role == "" {
		return Message{}, ErrInvalidRequest
	}

	msg := Message{
		Role:    base.Role,
		Content: base.Content,
	}

	// Check for tool_calls and tool_call_id fields (assistant messages).
	var extra struct {
		ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
		ToolCallID string     `json:"tool_call_id,omitempty"`
	}
	// Best-effort parse; these fields are optional.
	if err := json.Unmarshal(raw, &extra); err == nil {
		msg.ToolCalls = extra.ToolCalls
		msg.ToolCallID = extra.ToolCallID
	}

	return msg, nil
}

// parseChatTools unmarshals the tools array from the OpenAI Chat format.
func parseChatTools(raw []json.RawMessage) ([]Tool, error) {
	if len(raw) == 0 {
		return nil, nil
	}

	tools := make([]Tool, 0, len(raw))
	for _, t := range raw {
		var tool Tool
		if err := json.Unmarshal(t, &tool); err != nil {
			return nil, fmt.Errorf("%w: invalid tool definition", ErrInvalidRequest)
		}
		if tool.Type == "" {
			return nil, fmt.Errorf("%w: tool type is required", ErrInvalidRequest)
		}
		if tool.Type != "function" {
			// Only function-type tools are supported; skip others.
			continue
		}
		if tool.Function.Name == "" {
			return nil, fmt.Errorf("%w: tool function name is required", ErrInvalidRequest)
		}
		tools = append(tools, tool)
	}
	return tools, nil
}

// parseStop normalizes the stop field which can be a string or []string.
func parseStop(raw json.RawMessage) []string {
	if len(raw) == 0 {
		return nil
	}

	// Try array first.
	var arr []string
	if err := json.Unmarshal(raw, &arr); err == nil {
		return arr
	}

	// Try single string.
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return []string{s}
	}

	return nil
}

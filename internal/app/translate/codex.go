package translate

import (
	"context"
	"encoding/json"
	"fmt"

	"gorouter/internal/domain/engine"
)

// TranslateCodexToRequest parses an OpenAI Codex Responses API request body
// into an engine.Request. The parsed messages and tool definitions are stored
// in the request's MappedBody as a canonical JSON payload.
func (s *Service) TranslateCodexToRequest(ctx context.Context, body json.RawMessage) (*engine.Request, error) {
	if len(body) == 0 {
		return nil, fmt.Errorf("translate codex: %w", ErrInvalidRequest)
	}

	var raw struct {
		Model          string            `json:"model"`
		Input          json.RawMessage   `json:"input"` // string or array
		Instructions   string            `json:"instructions"`
		MaxOutputTokens int              `json:"max_output_tokens"`
		Temperature    *float64          `json:"temperature"`
		TopP           *float64          `json:"top_p"`
		Tools          []json.RawMessage `json:"tools"`
		ToolChoice     json.RawMessage   `json:"tool_choice"`
		Store          *bool             `json:"store"`
		Metadata       json.RawMessage   `json:"metadata"`
		Reasoning      json.RawMessage   `json:"reasoning"`
	}

	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("translate codex: %w", ErrTranslationFailed)
	}

	if raw.Model == "" {
		return nil, fmt.Errorf("translate codex: %w", ErrInvalidRequest)
	}
	if len(raw.Input) == 0 {
		return nil, fmt.Errorf("translate codex: %w", ErrInvalidRequest)
	}

	// Parse input into messages.
	messages, err := parseCodexInput(raw.Input)
	if err != nil {
		return nil, fmt.Errorf("translate codex: %w", err)
	}

	// Parse tools (same structure as OpenAI Chat).
	tools, err := parseChatTools(raw.Tools)
	if err != nil {
		return nil, fmt.Errorf("translate codex: %w", err)
	}

	canon := CanonicalBody{
		Messages:     messages,
		Instructions: raw.Instructions,
		TopP:         raw.TopP,
		Tools:        tools,
		ToolChoice:   raw.ToolChoice,
		Input:        raw.Input,
		Store:        raw.Store,
		Metadata:     raw.Metadata,
		Reasoning:    raw.Reasoning,
	}

	mapped, err := json.Marshal(canon)
	if err != nil {
		return nil, fmt.Errorf("translate codex: %w", ErrTranslationFailed)
	}

	return &engine.Request{
		Model:      raw.Model,
		RawBody:    body,
		MappedBody: mapped,
		MaxTokens:  raw.MaxOutputTokens,
		Temperature: raw.Temperature,
	}, nil
}

// parseCodexInput converts the Codex Responses "input" field (string or array)
// into a slice of canonical Messages.
func parseCodexInput(input json.RawMessage) ([]Message, error) {
	if len(input) == 0 {
		return nil, nil
	}

	// Try string input first.
	var str string
	if err := json.Unmarshal(input, &str); err == nil {
		content, _ := json.Marshal(str)
		return []Message{
			{
				Role:    "user",
				Content: content,
			},
		}, nil
	}

	// Try array of messages. Each element can be a string or a message object.
	var arr []json.RawMessage
	if err := json.Unmarshal(input, &arr); err != nil {
		return nil, fmt.Errorf("%w: input must be a string or array", ErrInvalidRequest)
	}

	messages := make([]Message, 0, len(arr))
	for _, item := range arr {
		msg, err := parseCodexInputItem(item)
		if err != nil {
			return nil, err
		}
		messages = append(messages, msg)
	}
	return messages, nil
}

// parseCodexInputItem parses a single element from the Codex Responses input array.
// Input array elements can be strings (treated as user messages) or message objects
// with "role" and "content" fields.
func parseCodexInputItem(item json.RawMessage) (Message, error) {
	// Try string.
	var str string
	if err := json.Unmarshal(item, &str); err == nil {
		content, _ := json.Marshal(str)
		return Message{
			Role:    "user",
			Content: content,
		}, nil
	}

	// Try message object.
	var msg struct {
		Role       string          `json:"role"`
		Content    json.RawMessage `json:"content"`
		ToolCallID string          `json:"tool_call_id,omitempty"`
	}
	if err := json.Unmarshal(item, &msg); err != nil {
		return Message{}, fmt.Errorf("%w: invalid input element", ErrInvalidRequest)
	}
	if msg.Role == "" {
		return Message{}, fmt.Errorf("%w: message role is required", ErrInvalidRequest)
	}

	canon := Message{
		Role:       msg.Role,
		Content:    msg.Content,
		ToolCallID: msg.ToolCallID,
	}

	// Parse tool_calls if present on assistant messages.
	var extra struct {
		ToolCalls []ToolCall `json:"tool_calls,omitempty"`
	}
	if err := json.Unmarshal(item, &extra); err == nil {
		canon.ToolCalls = extra.ToolCalls
	}

	return canon, nil
}

// TranslateRequestToCodex converts a canonical engine.Request back into an
// OpenAI Codex Responses API JSON body (reverse translation).
func (s *Service) TranslateRequestToCodex(ctx context.Context, req *engine.Request) (json.RawMessage, error) {
	if req == nil {
		return nil, fmt.Errorf("translate to codex: %w", ErrInvalidRequest)
	}

	// Decode the canonical body from MappedBody.
	var canon CanonicalBody
	if len(req.MappedBody) > 0 {
		if err := json.Unmarshal(req.MappedBody, &canon); err != nil {
			return nil, fmt.Errorf("translate to codex: %w", ErrTranslationFailed)
		}
	}

	// If we have the original RawBody and it looks like Codex format, return
	// it directly as an optimization.
	if len(req.RawBody) > 0 {
		var check struct {
			Input json.RawMessage `json:"input"`
		}
		if json.Unmarshal(req.RawBody, &check) == nil && len(check.Input) > 0 {
			return req.RawBody, nil
		}
	}

	// Rebuild input from canonical messages.
	input := rebuildCodexInput(canon.Messages)

	out := map[string]interface{}{
		"model": req.Model,
		"input": input,
	}

	if req.MaxTokens > 0 {
		out["max_output_tokens"] = req.MaxTokens
	}
	if req.Temperature != nil {
		out["temperature"] = req.Temperature
	}
	if canon.TopP != nil {
		out["top_p"] = canon.TopP
	}
	if canon.Instructions != "" {
		out["instructions"] = canon.Instructions
	}
	if len(canon.Tools) > 0 {
		out["tools"] = canon.Tools
	}
	if len(canon.ToolChoice) > 0 {
		out["tool_choice"] = canon.ToolChoice
	}
	if canon.Store != nil {
		out["store"] = *canon.Store
	}
	if len(canon.Metadata) > 0 {
		out["metadata"] = canon.Metadata
	}
	if len(canon.Reasoning) > 0 {
		out["reasoning"] = canon.Reasoning
	}

	result, err := json.Marshal(out)
	if err != nil {
		return nil, fmt.Errorf("translate to codex: %w", ErrTranslationFailed)
	}
	return result, nil
}

// rebuildCodexInput converts canonical Messages back into the Codex Responses
// input format (string or array of message objects).
func rebuildCodexInput(messages []Message) interface{} {
	if len(messages) == 0 {
		return ""
	}

	// If there's a single user message with string content, return it as a plain string.
	if len(messages) == 1 && messages[0].Role == "user" {
		var contentStr string
		if err := json.Unmarshal(messages[0].Content, &contentStr); err == nil {
			return contentStr
		}
	}

	// Otherwise build an array of message objects.
	out := make([]map[string]interface{}, 0, len(messages))
	for _, msg := range messages {
		entry := map[string]interface{}{
			"role":    msg.Role,
			"content": msg.Content,
		}
		if msg.ToolCallID != "" {
			entry["tool_call_id"] = msg.ToolCallID
		}
		if len(msg.ToolCalls) > 0 {
			entry["tool_calls"] = msg.ToolCalls
		}
		out = append(out, entry)
	}
	return out
}

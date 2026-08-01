package translators

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"gorouter/internal/domain/engine"
)

// ParseChatToCanonical parses an OpenAI Chat Completions request body into an
// engine.Request with a canonical MappedBody. Migrated from the app-layer
// translate service (design §3.2): this is the single canonicalisation entry
// point for chat requests.
func ParseChatToCanonical(ctx context.Context, body json.RawMessage) (*engine.Request, error) {
	if len(body) == 0 {
		return nil, fmt.Errorf("parse chat: %w", ErrNilRequest)
	}

	var raw struct {
		Model            string            `json:"model"`
		Messages         []json.RawMessage `json:"messages"`
		Stream           bool              `json:"stream"`
		MaxTokens        int               `json:"max_tokens"`
		Temperature      *float64          `json:"temperature"`
		TopP             *float64          `json:"top_p"`
		Stop             json.RawMessage   `json:"stop"`
		PresencePenalty  *float64          `json:"presence_penalty"`
		FrequencyPenalty *float64          `json:"frequency_penalty"`
		Tools            []json.RawMessage `json:"tools"`
		ToolChoice       json.RawMessage   `json:"tool_choice"`
		ResponseFormat   json.RawMessage   `json:"response_format"`
	}

	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("parse chat: %w", err)
	}

	if raw.Model == "" {
		return nil, fmt.Errorf("parse chat: %w", errMissingModel)
	}
	if len(raw.Messages) == 0 {
		return nil, fmt.Errorf("parse chat: %w", errMissingMessages)
	}

	messages := make([]Message, 0, len(raw.Messages))
	for _, m := range raw.Messages {
		msg, err := parseChatMessage(m)
		if err != nil {
			return nil, fmt.Errorf("parse chat: %w", err)
		}
		messages = append(messages, msg)
	}

	tools, err := parseChatTools(raw.Tools)
	if err != nil {
		return nil, fmt.Errorf("parse chat: %w", err)
	}

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
		return nil, fmt.Errorf("parse chat: %w", err)
	}

	return &engine.Request{
		Model:       raw.Model,
		RawBody:     body,
		MappedBody:  mapped,
		Stream:      raw.Stream,
		MaxTokens:   raw.MaxTokens,
		Temperature: raw.Temperature,
	}, nil
}

// ParseCodexToCanonical parses an OpenAI Codex Responses API request body
// into an engine.Request with a canonical MappedBody. Migrated from the
// app-layer translate service (design §3.2).
func ParseCodexToCanonical(ctx context.Context, body json.RawMessage) (*engine.Request, error) {
	if len(body) == 0 {
		return nil, fmt.Errorf("parse codex: %w", ErrNilRequest)
	}

	var raw struct {
		Model           string            `json:"model"`
		Input           json.RawMessage   `json:"input"`
		Instructions    string            `json:"instructions"`
		MaxOutputTokens int               `json:"max_output_tokens"`
		Temperature     *float64          `json:"temperature"`
		TopP            *float64          `json:"top_p"`
		Tools           []json.RawMessage `json:"tools"`
		ToolChoice      json.RawMessage   `json:"tool_choice"`
		Store           *bool             `json:"store"`
		Metadata        json.RawMessage   `json:"metadata"`
		Reasoning       json.RawMessage   `json:"reasoning"`
	}

	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("parse codex: %w", err)
	}

	if raw.Model == "" {
		return nil, fmt.Errorf("parse codex: %w", errMissingModel)
	}
	if len(raw.Input) == 0 {
		return nil, fmt.Errorf("parse codex: %w", errMissingInput)
	}

	messages, err := parseCodexInput(raw.Input)
	if err != nil {
		return nil, fmt.Errorf("parse codex: %w", err)
	}

	tools, err := parseChatTools(raw.Tools)
	if err != nil {
		return nil, fmt.Errorf("parse codex: %w", err)
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
		return nil, fmt.Errorf("parse codex: %w", err)
	}

	return &engine.Request{
		Model:       raw.Model,
		RawBody:     body,
		MappedBody:  mapped,
		MaxTokens:   raw.MaxOutputTokens,
		Temperature: raw.Temperature,
	}, nil
}

func parseChatMessage(raw json.RawMessage) (Message, error) {
	var base struct {
		Role    string          `json:"role"`
		Content json.RawMessage `json:"content"`
	}
	if err := json.Unmarshal(raw, &base); err != nil {
		return Message{}, errInvalidBody
	}
	if base.Role == "" {
		return Message{}, errInvalidBody
	}

	msg := Message{Role: base.Role, Content: base.Content}

	var extra struct {
		ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
		ToolCallID string     `json:"tool_call_id,omitempty"`
	}
	if err := json.Unmarshal(raw, &extra); err == nil {
		msg.ToolCalls = extra.ToolCalls
		msg.ToolCallID = extra.ToolCallID
	}

	return msg, nil
}

func parseChatTools(raw []json.RawMessage) ([]Tool, error) {
	if len(raw) == 0 {
		return nil, nil
	}

	tools := make([]Tool, 0, len(raw))
	for _, t := range raw {
		var tool Tool
		if err := json.Unmarshal(t, &tool); err != nil {
			return nil, errUnmarshal
		}
		if tool.Type == "" {
			return nil, errMissingToolType
		}
		if tool.Type != "function" {
			continue
		}
		if tool.Function.Name == "" {
			return nil, errMissingFuncName
		}
		tools = append(tools, tool)
	}
	return tools, nil
}

func parseStop(raw json.RawMessage) []string {
	if len(raw) == 0 {
		return nil
	}

	var arr []string
	if err := json.Unmarshal(raw, &arr); err == nil {
		return arr
	}

	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return []string{s}
	}

	return nil
}

func parseCodexInput(input json.RawMessage) ([]Message, error) {
	if len(input) == 0 {
		return nil, nil
	}

	var str string
	if err := json.Unmarshal(input, &str); err == nil {
		content, _ := json.Marshal(str)
		return []Message{{Role: "user", Content: content}}, nil
	}

	var arr []json.RawMessage
	if err := json.Unmarshal(input, &arr); err != nil {
		return nil, fmt.Errorf("%w: input must be a string or array", errInvalidBody)
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

func parseCodexInputItem(item json.RawMessage) (Message, error) {
	var str string
	if err := json.Unmarshal(item, &str); err == nil {
		content, _ := json.Marshal(str)
		return Message{Role: "user", Content: content}, nil
	}

	var msg struct {
		Role       string          `json:"role"`
		Content    json.RawMessage `json:"content"`
		ToolCallID string          `json:"tool_call_id,omitempty"`
	}
	if err := json.Unmarshal(item, &msg); err != nil {
		return Message{}, fmt.Errorf("%w: invalid input element", errInvalidBody)
	}
	if msg.Role == "" {
		return Message{}, fmt.Errorf("%w: message role is required", errInvalidBody)
	}

	canon := Message{Role: msg.Role, Content: msg.Content, ToolCallID: msg.ToolCallID}

	var extra struct {
		ToolCalls []ToolCall `json:"tool_calls,omitempty"`
	}
	if err := json.Unmarshal(item, &extra); err == nil {
		canon.ToolCalls = extra.ToolCalls
	}

	return canon, nil
}

// parse errors used by the parse helpers. Callers receive plain fmt.Errorf
// chains wrapping these sentinels.
var (
	errInvalidBody     = errors.New("invalid request body")
	errMissingModel    = errors.New("missing model")
	errMissingMessages = errors.New("missing messages")
	errMissingInput    = errors.New("missing input")
	errUnmarshal       = errors.New("unmarshal failed")
	errMissingToolType = errors.New("missing tool type")
	errMissingFuncName = errors.New("missing function name")
)

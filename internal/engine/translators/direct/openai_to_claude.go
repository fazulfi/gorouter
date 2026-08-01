package direct

import (
	"context"
	"encoding/json"
	"fmt"

	"gorouter/internal/domain/engine"
	"gorouter/internal/engine/formats"
)

type OpenAIToClaude struct{}

func NewOpenAIToClaude() *OpenAIToClaude { return &OpenAIToClaude{} }

func (t *OpenAIToClaude) Source() engine.RequestFormat { return engine.FormatOpenAIChat }
func (t *OpenAIToClaude) Target() engine.RequestFormat { return engine.FormatAnthropic }

// TranslateRequest converts OpenAI Chat request → Claude Messages request.
func (t *OpenAIToClaude) TranslateRequest(_ context.Context, req *engine.Request) (*engine.Request, error) {
	if req == nil {
		return nil, errNilRequest
	}
	var openaiBody struct {
		Model       string            `json:"model"`
		Messages    []json.RawMessage `json:"messages"`
		MaxTokens   int               `json:"max_tokens"`
		Temperature *float64          `json:"temperature,omitempty"`
		TopP        *float64          `json:"top_p,omitempty"`
		Stream      bool              `json:"stream,omitempty"`
		Stop        json.RawMessage   `json:"stop,omitempty"`
		Tools       []json.RawMessage `json:"tools,omitempty"`
		ToolChoice  json.RawMessage   `json:"tool_choice,omitempty"`
	}
	if err := json.Unmarshal(req.RawBody, &openaiBody); err != nil {
		return nil, fmt.Errorf("openai→claude: unmarshal request: %w", err)
	}
	claudeMsgs := make([]json.RawMessage, 0, len(openaiBody.Messages))
	var systemPrompt string
	for _, rawMsg := range openaiBody.Messages {
		var base struct {
			Role    string          `json:"role"`
			Content json.RawMessage `json:"content"`
		}
		if err := json.Unmarshal(rawMsg, &base); err != nil {
			return nil, fmt.Errorf("openai→claude: unmarshal message: %w", err)
		}
		switch base.Role {
		case "system":
			var text string
			if json.Unmarshal(base.Content, &text) == nil {
				systemPrompt = mergeSystem(systemPrompt, text)
			}
		case "user":
			m, err := openaiUserToClaude(rawMsg)
			if err != nil {
				return nil, fmt.Errorf("openai→claude: user: %w", err)
			}
			claudeMsgs = append(claudeMsgs, m)
		case "assistant":
			m, err := openaiAssistantToClaude(rawMsg)
			if err != nil {
				return nil, fmt.Errorf("openai→claude: assistant: %w", err)
			}
			claudeMsgs = append(claudeMsgs, m)
		case "tool":
			m, err := openaiToolToClaude(rawMsg)
			if err != nil {
				return nil, fmt.Errorf("openai→claude: tool: %w", err)
			}
			claudeMsgs = append(claudeMsgs, m)
		}
	}
	claudeBody := map[string]interface{}{"model": openaiBody.Model, "max_tokens": openaiBody.MaxTokens, "messages": claudeMsgs}
	if systemPrompt != "" {
		claudeBody["system"] = systemPrompt
	}
	if openaiBody.Temperature != nil {
		claudeBody["temperature"] = *openaiBody.Temperature
	}
	if openaiBody.TopP != nil {
		claudeBody["top_p"] = *openaiBody.TopP
	}
	if openaiBody.Stream {
		claudeBody["stream"] = true
	}
	if openaiBody.Stop != nil {
		claudeBody["stop_sequences"] = parseStopSequences(openaiBody.Stop)
	}
	if len(openaiBody.Tools) > 0 {
		claudeBody["tools"] = openaiToolsToClaude(openaiBody.Tools)
	}
	if openaiBody.ToolChoice != nil {
		claudeBody["tool_choice"] = mapToolChoice(openaiBody.ToolChoice)
	}
	raw, err := json.Marshal(claudeBody)
	if err != nil {
		return nil, fmt.Errorf("openai→claude: marshal: %w", err)
	}
	return &engine.Request{
		ID: req.ID, Format: engine.FormatAnthropic, Model: openaiBody.Model,
		RawBody: raw, MappedBody: raw, Stream: openaiBody.Stream,
		MaxTokens: openaiBody.MaxTokens, Temperature: openaiBody.Temperature,
		Headers: req.Headers, UserID: req.UserID,
	}, nil
}

// TranslateResponse converts Claude response → OpenAI Chat response.
func (t *OpenAIToClaude) TranslateResponse(_ context.Context, resp *engine.Response) (*engine.Response, error) {
	if resp == nil {
		return nil, errNilResponse
	}
	var claudeResp struct {
		ID         string            `json:"id"`
		Type       string            `json:"type"`
		Role       string            `json:"role"`
		Content    []json.RawMessage `json:"content"`
		StopReason string            `json:"stop_reason,omitempty"`
		Usage      *struct {
			InputTokens  int `json:"input_tokens"`
			OutputTokens int `json:"output_tokens"`
		} `json:"usage,omitempty"`
		Model string `json:"model"`
	}
	if err := json.Unmarshal(resp.Body, &claudeResp); err != nil {
		return nil, fmt.Errorf("openai→claude: unmarshal response: %w", err)
	}
	content, toolCalls := claudeContentToOpenAI(claudeResp.Content)
	openaiResp := map[string]interface{}{
		"id":     "chatcmpl-" + claudeResp.ID,
		"object": "chat.completion",
		"model":  claudeResp.Model,
		"choices": []map[string]interface{}{
			{
				"index":         0,
				"message":       buildOpenAIMessage("assistant", content, toolCalls),
				"finish_reason": mapClaudeStop(claudeResp.StopReason),
			},
		},
	}
	if claudeResp.Usage != nil {
		openaiResp["usage"] = map[string]int{
			"prompt_tokens":     claudeResp.Usage.InputTokens,
			"completion_tokens": claudeResp.Usage.OutputTokens,
			"total_tokens":      claudeResp.Usage.InputTokens + claudeResp.Usage.OutputTokens,
		}
	}
	body, _ := json.Marshal(openaiResp)
	return &engine.Response{
		RequestID: resp.RequestID, Body: body, Model: resp.Model,
		StatusCode: resp.StatusCode, Headers: resp.Headers,
	}, nil
}

// TranslateStreamChunk converts Claude stream chunk → OpenAI Chat chunk.
func (t *OpenAIToClaude) TranslateStreamChunk(_ context.Context, chunk *formats.Chunk) (*formats.Chunk, error) {
	if chunk == nil {
		return nil, errNilChunk
	}
	if chunk.Error != nil || chunk.IsFinal {
		if chunk.Event == "message_stop" || chunk.IsFinal {
			return &formats.Chunk{Event: "done", Data: []byte("[DONE]"), IsFinal: true}, nil
		}
		return &formats.Chunk{Event: chunk.Event, Data: chunk.Data, IsFinal: chunk.IsFinal, Error: chunk.Error}, nil
	}
	var event struct {
		Type         string          `json:"type"`
		Index        *int            `json:"index,omitempty"`
		Delta        json.RawMessage `json:"delta,omitempty"`
		ContentBlock json.RawMessage `json:"content_block,omitempty"`
	}
	if err := json.Unmarshal(chunk.Data, &event); err != nil {
		return nil, fmt.Errorf("openai→claude: unmarshal chunk: %w", err)
	}
	switch event.Type {
	case "content_block_delta":
		var delta struct {
			Type string `json:"type"`
			Text string `json:"text,omitempty"`
		}
		if json.Unmarshal(event.Delta, &delta) == nil && delta.Type == "text_delta" {
			data, _ := json.Marshal(map[string]interface{}{
				"choices": []map[string]interface{}{
					{"index": 0, "delta": map[string]string{"content": delta.Text}},
				},
			})
			return &formats.Chunk{Event: "chunk", Data: data, IsFinal: false}, nil
		}
	case "content_block_start":
		var block struct {
			Type string `json:"type"`
			ID   string `json:"id,omitempty"`
			Name string `json:"name,omitempty"`
		}
		if json.Unmarshal(event.ContentBlock, &block) == nil && block.Type == "tool_use" {
			tc := []map[string]interface{}{
				{"id": block.ID, "type": "function", "function": map[string]string{"name": block.Name, "arguments": ""}},
			}
			data, _ := json.Marshal(map[string]interface{}{
				"choices": []map[string]interface{}{
					{"index": 0, "delta": map[string]interface{}{"role": "assistant", "content": nil, "tool_calls": tc}},
				},
			})
			return &formats.Chunk{Event: "chunk", Data: data, IsFinal: false}, nil
		}
	case "message_delta":
		var delta struct {
			StopReason *string `json:"stop_reason,omitempty"`
		}
		if json.Unmarshal(event.Delta, &delta) == nil && delta.StopReason != nil {
			stop := mapClaudeStop(*delta.StopReason)
			data, _ := json.Marshal(map[string]interface{}{
				"choices": []map[string]interface{}{
					{"index": 0, "delta": struct{}{}, "finish_reason": stop},
				},
			})
			return &formats.Chunk{Event: "chunk", Data: data, IsFinal: true}, nil
		}
	case "error":
		errMsg := "claude stream error"
		var errData struct {
			Error struct {
				Message string `json:"message"`
			} `json:"error"`
		}
		if json.Unmarshal(chunk.Data, &errData) == nil {
			errMsg = errData.Error.Message
		}
		return &formats.Chunk{Event: "error", Data: chunk.Data, IsFinal: true, Error: fmt.Errorf("%s", errMsg)}, nil
	}
	return chunk, nil
}

// ---- helpers ----

func claudeContentToOpenAI(blocks []json.RawMessage) (content interface{}, toolCalls []map[string]interface{}) {
	textParts := make([]string, 0)
	for _, b := range blocks {
		var bl struct {
			Type string `json:"type"`
		}
		if json.Unmarshal(b, &bl) != nil {
			continue
		}
		switch bl.Type {
		case "text":
			var tb struct {
				Text string `json:"text"`
			}
			if json.Unmarshal(b, &tb) == nil {
				textParts = append(textParts, tb.Text)
			}
		case "thinking":
			var tb struct {
				Thinking string `json:"thinking"`
			}
			if json.Unmarshal(b, &tb) == nil {
				textParts = append(textParts, "[thinking] "+tb.Thinking)
			}
		case "tool_use":
			var tb struct {
				ID    string          `json:"id"`
				Name  string          `json:"name"`
				Input json.RawMessage `json:"input"`
			}
			if json.Unmarshal(b, &tb) == nil {
				argsStr, _ := json.Marshal(tb.Input)
				toolCalls = append(toolCalls, map[string]interface{}{
					"id": tb.ID, "type": "function",
					"function": map[string]string{"name": tb.Name, "arguments": string(argsStr)},
				})
			}
		}
	}
	content = nil
	if len(textParts) > 0 {
		text := ""
		for _, p := range textParts {
			text += p
		}
		content = text
	}
	return
}

func buildOpenAIMessage(role string, content interface{}, toolCalls []map[string]interface{}) map[string]interface{} {
	m := map[string]interface{}{"role": role, "content": content}
	if len(toolCalls) > 0 {
		m["tool_calls"] = toolCalls
	}
	return m
}

func mapClaudeStop(reason string) string {
	switch reason {
	case "end_turn", "stop":
		return "stop"
	case "tool_use":
		return "tool_calls"
	case "max_tokens":
		return "length"
	default:
		return "stop"
	}
}

func mergeSystem(existing, add string) string {
	if existing == "" {
		return add
	}
	return existing + "\n" + add
}

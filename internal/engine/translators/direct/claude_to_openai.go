package direct

import (
	"context"
	"encoding/json"
	"fmt"

	"gorouter/internal/domain/engine"
	"gorouter/internal/engine/formats"
)

type ClaudeToOpenAI struct{}

func NewClaudeToOpenAI() *ClaudeToOpenAI { return &ClaudeToOpenAI{} }

func (t *ClaudeToOpenAI) Source() engine.RequestFormat { return engine.FormatAnthropic }
func (t *ClaudeToOpenAI) Target() engine.RequestFormat { return engine.FormatOpenAIChat }

// TranslateRequest converts Claude Messages request → OpenAI Chat request.
func (t *ClaudeToOpenAI) TranslateRequest(_ context.Context, req *engine.Request) (*engine.Request, error) {
	if req == nil {
		return nil, errNilRequest
	}
	var claudeBody struct {
		Model         string            `json:"model"`
		Messages      []json.RawMessage `json:"messages"`
		System        string            `json:"system,omitempty"`
		MaxTokens     int               `json:"max_tokens"`
		Temperature   *float64          `json:"temperature,omitempty"`
		TopP          *float64          `json:"top_p,omitempty"`
		Stream        bool              `json:"stream,omitempty"`
		StopSequences []string          `json:"stop_sequences,omitempty"`
		Tools         []json.RawMessage `json:"tools,omitempty"`
		ToolChoice    json.RawMessage   `json:"tool_choice,omitempty"`
	}
	if err := json.Unmarshal(req.RawBody, &claudeBody); err != nil {
		return nil, fmt.Errorf("claude→openai: unmarshal request: %w", err)
	}
	openaiMsgs := make([]json.RawMessage, 0, len(claudeBody.Messages)+1)
	if claudeBody.System != "" {
		sys, _ := json.Marshal(map[string]interface{}{"role": "system", "content": claudeBody.System})
		openaiMsgs = append(openaiMsgs, sys)
	}
	for _, rawMsg := range claudeBody.Messages {
		var base struct {
			Role    string          `json:"role"`
			Content json.RawMessage `json:"content"`
		}
		if err := json.Unmarshal(rawMsg, &base); err != nil {
			return nil, fmt.Errorf("claude→openai: unmarshal message: %w", err)
		}
		switch base.Role {
		case "user":
			m, err := claudeUserToOpenAI(rawMsg)
			if err != nil {
				return nil, fmt.Errorf("claude→openai: user: %w", err)
			}
			openaiMsgs = append(openaiMsgs, m)
		case "assistant":
			m, err := claudeAssistantToOpenAI(rawMsg)
			if err != nil {
				return nil, fmt.Errorf("claude→openai: assistant: %w", err)
			}
			openaiMsgs = append(openaiMsgs, m)
		}
	}
	openaiBody := map[string]interface{}{"model": claudeBody.Model, "messages": openaiMsgs}
	if claudeBody.MaxTokens > 0 {
		openaiBody["max_tokens"] = claudeBody.MaxTokens
	}
	if claudeBody.Temperature != nil {
		openaiBody["temperature"] = *claudeBody.Temperature
	}
	if claudeBody.TopP != nil {
		openaiBody["top_p"] = *claudeBody.TopP
	}
	if claudeBody.Stream {
		openaiBody["stream"] = true
	}
	if len(claudeBody.StopSequences) > 0 {
		openaiBody["stop"] = claudeBody.StopSequences
	}
	if len(claudeBody.Tools) > 0 {
		openaiBody["tools"] = claudeToolsToOpenAI(claudeBody.Tools)
	}
	if claudeBody.ToolChoice != nil {
		openaiBody["tool_choice"] = mapClaudeToolChoice(claudeBody.ToolChoice)
	}
	raw, err := json.Marshal(openaiBody)
	if err != nil {
		return nil, fmt.Errorf("claude→openai: marshal: %w", err)
	}
	return &engine.Request{
		ID: req.ID, Format: engine.FormatOpenAIChat, Model: claudeBody.Model,
		RawBody: raw, MappedBody: raw, Stream: claudeBody.Stream,
		MaxTokens: claudeBody.MaxTokens, Temperature: claudeBody.Temperature,
		Headers: req.Headers, UserID: req.UserID,
	}, nil
}

// TranslateResponse converts OpenAI response → Claude response.
func (t *ClaudeToOpenAI) TranslateResponse(_ context.Context, resp *engine.Response) (*engine.Response, error) {
	if resp == nil {
		return nil, errNilResponse
	}
	var openaiResp struct {
		ID      string `json:"id"`
		Object  string `json:"object"`
		Model   string `json:"model"`
		Choices []struct {
			Index        int             `json:"index"`
			Message      json.RawMessage `json:"message"`
			FinishReason *string         `json:"finish_reason,omitempty"`
		} `json:"choices"`
		Usage *struct {
			PromptTokens     int `json:"prompt_tokens"`
			CompletionTokens int `json:"completion_tokens"`
		} `json:"usage,omitempty"`
	}
	if err := json.Unmarshal(resp.Body, &openaiResp); err != nil {
		return nil, fmt.Errorf("claude→openai: unmarshal response: %w", err)
	}
	var contentBlocks []map[string]interface{}
	if len(openaiResp.Choices) > 0 {
		var msg struct {
			Role      string                   `json:"role"`
			Content   interface{}              `json:"content"`
			ToolCalls []map[string]interface{} `json:"tool_calls,omitempty"`
		}
		_ = json.Unmarshal(openaiResp.Choices[0].Message, &msg)
		if msg.Content != nil {
			if s, ok := msg.Content.(string); ok && s != "" {
				contentBlocks = append(contentBlocks, map[string]interface{}{"type": "text", "text": s})
			}
		}
		for _, tc := range msg.ToolCalls {
			var fn struct {
				Name      string `json:"name"`
				Arguments string `json:"arguments"`
			}
			if tc["function"] != nil {
				fnBytes, _ := json.Marshal(tc["function"])
				_ = json.Unmarshal(fnBytes, &fn)
			}
			var input json.RawMessage
			_ = json.Unmarshal([]byte(fn.Arguments), &input)
			id, _ := tc["id"].(string)
			if id == "" {
				id = fn.Name
			}
			contentBlocks = append(contentBlocks, map[string]interface{}{
				"type": "tool_use", "id": id, "name": fn.Name, "input": input,
			})
		}
	}
	claudeResp := map[string]interface{}{
		"id": openaiResp.ID, "type": "message", "role": "assistant",
		"content": contentBlocks, "model": openaiResp.Model,
	}
	if len(openaiResp.Choices) > 0 {
		claudeResp["stop_reason"] = mapOpenAIStop(openaiResp.Choices[0].FinishReason)
	}
	if openaiResp.Usage != nil {
		claudeResp["usage"] = map[string]int{
			"input_tokens":  openaiResp.Usage.PromptTokens,
			"output_tokens": openaiResp.Usage.CompletionTokens,
		}
	}
	body, _ := json.Marshal(claudeResp)
	return &engine.Response{
		RequestID: resp.RequestID, Body: body, Model: resp.Model,
		StatusCode: resp.StatusCode, Headers: resp.Headers,
	}, nil
}

// TranslateStreamChunk converts OpenAI stream chunk → Claude chunk.
func (t *ClaudeToOpenAI) TranslateStreamChunk(_ context.Context, chunk *formats.Chunk) (*formats.Chunk, error) {
	if chunk == nil {
		return nil, errNilChunk
	}
	if string(chunk.Data) == "[DONE]" || chunk.IsFinal {
		return &formats.Chunk{Event: "message_stop", Data: []byte("{}"), IsFinal: true}, nil
	}
	var openai struct {
		Choices []struct {
			Index        int             `json:"index"`
			Delta        json.RawMessage `json:"delta"`
			FinishReason *string         `json:"finish_reason,omitempty"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(chunk.Data, &openai); err != nil {
		return nil, fmt.Errorf("claude→openai: unmarshal chunk: %w", err)
	}
	if len(openai.Choices) == 0 {
		return chunk, nil
	}
	choice := openai.Choices[0]
	if choice.FinishReason != nil {
		delta, _ := json.Marshal(map[string]interface{}{
			"stop_reason": mapOpenAIStop(choice.FinishReason),
		})
		return &formats.Chunk{Event: "message_delta", Data: delta, IsFinal: true}, nil
	}
	var delta struct {
		Role      string                   `json:"role,omitempty"`
		Content   string                   `json:"content,omitempty"`
		ToolCalls []map[string]interface{} `json:"tool_calls,omitempty"`
	}
	_ = json.Unmarshal(choice.Delta, &delta)
	if delta.Content != "" {
		cb, _ := json.Marshal(map[string]interface{}{
			"type": "content_block_delta", "index": 0,
			"delta": map[string]string{"type": "text_delta", "text": delta.Content},
		})
		return &formats.Chunk{Event: "content_block_delta", Data: cb, IsFinal: false}, nil
	}
	if len(delta.ToolCalls) > 0 {
		tc := delta.ToolCalls[0]
		fn, _ := tc["function"].(map[string]interface{})
		name, _ := fn["name"].(string)
		id, _ := tc["id"].(string)
		cb, _ := json.Marshal(map[string]interface{}{
			"type": "content_block_start", "index": 0,
			"content_block": map[string]interface{}{
				"type": "tool_use", "id": id, "name": name, "input": struct{}{},
			},
		})
		return &formats.Chunk{Event: "content_block_start", Data: cb, IsFinal: false}, nil
	}
	if delta.Role != "" {
		return chunk, nil
	}
	return chunk, nil
}

// ---- helpers ----

func mapOpenAIStop(reason *string) string {
	if reason == nil {
		return "end_turn"
	}
	switch *reason {
	case "stop":
		return "end_turn"
	case "tool_calls":
		return "tool_use"
	case "length":
		return "max_tokens"
	default:
		return *reason
	}
}

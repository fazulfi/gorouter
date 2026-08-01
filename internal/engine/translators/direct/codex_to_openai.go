package direct

import (
	"context"
	"encoding/json"
	"fmt"

	"gorouter/internal/domain/engine"
	"gorouter/internal/engine/formats"
)

type CodexToOpenAI struct{}

func NewCodexToOpenAI() *CodexToOpenAI { return &CodexToOpenAI{} }

func (t *CodexToOpenAI) Source() engine.RequestFormat { return engine.FormatCodexResponses }
func (t *CodexToOpenAI) Target() engine.RequestFormat { return engine.FormatOpenAIChat }

// TranslateRequest converts Codex Responses request → OpenAI Chat request.
func (t *CodexToOpenAI) TranslateRequest(_ context.Context, req *engine.Request) (*engine.Request, error) {
	if req == nil {
		return nil, errNilRequest
	}
	var codexBody struct {
		Model           string            `json:"model"`
		Input           json.RawMessage   `json:"input"`
		Instructions    string            `json:"instructions,omitempty"`
		MaxOutputTokens int               `json:"max_output_tokens,omitempty"`
		Temperature     *float64          `json:"temperature,omitempty"`
		TopP            *float64          `json:"top_p,omitempty"`
		Tools           []json.RawMessage `json:"tools,omitempty"`
		ToolChoice      json.RawMessage   `json:"tool_choice,omitempty"`
	}
	if err := json.Unmarshal(req.RawBody, &codexBody); err != nil {
		return nil, fmt.Errorf("codex→openai: unmarshal: %w", err)
	}
	if len(codexBody.Input) == 0 {
		return nil, fmt.Errorf("codex→openai: empty input")
	}
	messages, _ := codexInputToMessages(codexBody.Input)
	if codexBody.Instructions != "" {
		s, _ := json.Marshal(map[string]interface{}{"role": "system", "content": codexBody.Instructions})
		messages = append([]json.RawMessage{s}, messages...)
	}
	openaiBody := map[string]interface{}{"model": codexBody.Model, "messages": messages}
	if codexBody.MaxOutputTokens > 0 {
		openaiBody["max_tokens"] = codexBody.MaxOutputTokens
	}
	if codexBody.Temperature != nil {
		openaiBody["temperature"] = *codexBody.Temperature
	}
	if codexBody.TopP != nil {
		openaiBody["top_p"] = *codexBody.TopP
	}
	if len(codexBody.Tools) > 0 {
		openaiBody["tools"] = codexBody.Tools
	}
	if len(codexBody.ToolChoice) > 0 {
		openaiBody["tool_choice"] = codexBody.ToolChoice
	}
	raw, err := json.Marshal(openaiBody)
	if err != nil {
		return nil, fmt.Errorf("codex→openai: marshal: %w", err)
	}
	return &engine.Request{
		ID: req.ID, Format: engine.FormatOpenAIChat, Model: codexBody.Model,
		RawBody: raw, MappedBody: raw, Stream: req.Stream,
		MaxTokens: codexBody.MaxOutputTokens, Temperature: codexBody.Temperature,
		Headers: req.Headers, UserID: req.UserID,
	}, nil
}

// TranslateResponse converts OpenAI response → Codex Responses format.
func (t *CodexToOpenAI) TranslateResponse(_ context.Context, resp *engine.Response) (*engine.Response, error) {
	if resp == nil {
		return nil, errNilResponse
	}
	var openaiResp struct {
		ID      string `json:"id"`
		Model   string `json:"model"`
		Choices []struct {
			Message struct {
				Content   interface{}              `json:"content"`
				ToolCalls []map[string]interface{} `json:"tool_calls,omitempty"`
			} `json:"message"`
		} `json:"choices"`
		Usage *struct {
			PromptTokens     int `json:"prompt_tokens"`
			CompletionTokens int `json:"completion_tokens"`
		} `json:"usage,omitempty"`
	}
	if err := json.Unmarshal(resp.Body, &openaiResp); err != nil {
		return nil, fmt.Errorf("codex→openai: unmarshal response: %w", err)
	}
	output := make([]map[string]interface{}, 0)
	if len(openaiResp.Choices) > 0 {
		msg := openaiResp.Choices[0].Message
		entry := map[string]interface{}{"role": "assistant", "content": msg.Content}
		tcs := make([]map[string]interface{}, 0)
		for _, tc := range msg.ToolCalls {
			fn, _ := tc["function"].(map[string]interface{})
			tcs = append(tcs, map[string]interface{}{
				"id": tc["id"], "type": "function",
				"function": map[string]interface{}{
					"name": fn["name"], "arguments": fn["arguments"],
				},
			})
		}
		if len(tcs) > 0 {
			entry["tool_calls"] = tcs
		}
		output = append(output, entry)
	}
	codexResp := map[string]interface{}{
		"id": openaiResp.ID, "object": "response", "model": openaiResp.Model,
		"output": output,
	}
	if openaiResp.Usage != nil {
		codexResp["usage"] = map[string]int{
			"input_tokens":  openaiResp.Usage.PromptTokens,
			"output_tokens": openaiResp.Usage.CompletionTokens,
		}
	}
	body, _ := json.Marshal(codexResp)
	return &engine.Response{
		RequestID: resp.RequestID, Body: body, Model: resp.Model,
		StatusCode: resp.StatusCode, Headers: resp.Headers,
	}, nil
}

// TranslateStreamChunk converts OpenAI chunk → Codex Responses chunk.
func (t *CodexToOpenAI) TranslateStreamChunk(_ context.Context, chunk *formats.Chunk) (*formats.Chunk, error) {
	if chunk == nil {
		return nil, errNilChunk
	}
	if string(chunk.Data) == "[DONE]" {
		return &formats.Chunk{Event: "response.completed", Data: []byte("{}"), IsFinal: true}, nil
	}
	var openai struct {
		Choices []struct {
			Delta struct {
				Content string `json:"content,omitempty"`
			} `json:"delta"`
			FinishReason *string `json:"finish_reason,omitempty"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(chunk.Data, &openai); err != nil {
		return nil, fmt.Errorf("codex→openai: unmarshal chunk: %w", err)
	}
	if len(openai.Choices) == 0 {
		return chunk, nil
	}
	c := openai.Choices[0]
	codexChunk := map[string]interface{}{
		"type":  "response.output_text.delta",
		"delta": c.Delta.Content,
	}
	isFinal := false
	if c.FinishReason != nil && *c.FinishReason != "" {
		codexChunk["type"] = "response.output_text.done"
		isFinal = true
	}
	data, _ := json.Marshal(codexChunk)
	return &formats.Chunk{Event: "chunk", Data: data, IsFinal: isFinal}, nil
}

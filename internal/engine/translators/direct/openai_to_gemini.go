package direct

import (
	"context"
	"encoding/json"
	"fmt"

	"gorouter/internal/domain/engine"
	"gorouter/internal/engine/formats"
)

type OpenAIToGemini struct{}

func NewOpenAIToGemini() *OpenAIToGemini { return &OpenAIToGemini{} }

func (t *OpenAIToGemini) Source() engine.RequestFormat { return engine.FormatOpenAIChat }
func (t *OpenAIToGemini) Target() engine.RequestFormat { return engine.FormatGemini }

// TranslateRequest converts OpenAI Chat request → Gemini generateContent request.
func (t *OpenAIToGemini) TranslateRequest(_ context.Context, req *engine.Request) (*engine.Request, error) {
	if req == nil {
		return nil, errNilRequest
	}
	var openaiBody struct {
		Model       string            `json:"model"`
		Messages    []json.RawMessage `json:"messages"`
		MaxTokens   int               `json:"max_tokens"`
		Temperature *float64          `json:"temperature,omitempty"`
		TopP        *float64          `json:"top_p,omitempty"`
		Stop        json.RawMessage   `json:"stop,omitempty"`
		Tools       []json.RawMessage `json:"tools,omitempty"`
	}
	if err := json.Unmarshal(req.RawBody, &openaiBody); err != nil {
		return nil, fmt.Errorf("openai→gemini: unmarshal: %w", err)
	}
	var systemInstruction string
	contents := make([]map[string]interface{}, 0)
	for _, rawMsg := range openaiBody.Messages {
		var base struct {
			Role    string          `json:"role"`
			Content json.RawMessage `json:"content"`
		}
		if err := json.Unmarshal(rawMsg, &base); err != nil {
			return nil, fmt.Errorf("openai→gemini: message: %w", err)
		}
		switch base.Role {
		case "system":
			_ = json.Unmarshal(base.Content, &systemInstruction)
		case "user":
			parts, _ := openaiContentToGeminiParts(base.Content)
			contents = append(contents, map[string]interface{}{"role": "user", "parts": parts})
		case "assistant":
			c, _ := openaiAsstToGeminiParts(rawMsg)
			contents = append(contents, c)
		case "tool":
			c, _ := openaiToolToGeminiParts(rawMsg)
			contents = append(contents, c)
		}
	}
	geminiBody := map[string]interface{}{"contents": contents}
	if systemInstruction != "" {
		geminiBody["systemInstruction"] = map[string]interface{}{
			"parts": []map[string]string{{"text": systemInstruction}},
		}
	}
	gc := map[string]interface{}{}
	if openaiBody.Temperature != nil {
		gc["temperature"] = *openaiBody.Temperature
	}
	if openaiBody.TopP != nil {
		gc["topP"] = *openaiBody.TopP
	}
	if openaiBody.MaxTokens > 0 {
		gc["maxOutputTokens"] = openaiBody.MaxTokens
	}
	if openaiBody.Stop != nil {
		gc["stopSequences"] = parseStopSequences(openaiBody.Stop)
	}
	if len(gc) > 0 {
		geminiBody["generationConfig"] = gc
	}
	if len(openaiBody.Tools) > 0 {
		geminiBody["tools"] = openaiToolsToGemini(openaiBody.Tools)
	}
	raw, err := json.Marshal(geminiBody)
	if err != nil {
		return nil, fmt.Errorf("openai→gemini: marshal: %w", err)
	}
	return &engine.Request{
		ID: req.ID, Format: engine.FormatGemini, Model: openaiBody.Model,
		RawBody: raw, MappedBody: raw, Stream: req.Stream,
		MaxTokens: openaiBody.MaxTokens, Temperature: openaiBody.Temperature,
		Headers: req.Headers, UserID: req.UserID,
	}, nil
}

// TranslateResponse converts Gemini response → OpenAI Chat response.
func (t *OpenAIToGemini) TranslateResponse(_ context.Context, resp *engine.Response) (*engine.Response, error) {
	if resp == nil {
		return nil, errNilResponse
	}
	var geminiResp struct {
		Candidates []struct {
			Content struct {
				Parts []struct {
					Text     string          `json:"text,omitempty"`
					FuncCall json.RawMessage `json:"functionCall,omitempty"`
				} `json:"parts"`
				Role string `json:"role"`
			} `json:"content"`
			FinishReason *string `json:"finishReason,omitempty"`
		} `json:"candidates"`
		UsageMetadata *struct {
			PromptTokenCount     int `json:"promptTokenCount"`
			CandidatesTokenCount int `json:"candidatesTokenCount"`
			TotalTokenCount      int `json:"totalTokenCount"`
		} `json:"usageMetadata,omitempty"`
	}
	if err := json.Unmarshal(resp.Body, &geminiResp); err != nil {
		return nil, fmt.Errorf("openai→gemini: unmarshal response: %w", err)
	}
	content := ""
	var toolCalls []map[string]interface{}
	if len(geminiResp.Candidates) > 0 {
		c := geminiResp.Candidates[0]
		for _, p := range c.Content.Parts {
			if p.Text != "" {
				content += p.Text
			}
			if p.FuncCall != nil {
				var fc struct {
					Name string                 `json:"name"`
					Args map[string]interface{} `json:"args"`
				}
				_ = json.Unmarshal(p.FuncCall, &fc)
				argsStr, _ := json.Marshal(fc.Args)
				toolCalls = append(toolCalls, map[string]interface{}{
					"id": fc.Name, "type": "function",
					"function": map[string]string{"name": fc.Name, "arguments": string(argsStr)},
				})
			}
		}
	}
	choices := []map[string]interface{}{
		{
			"index":         0,
			"message":       buildOpenAIMsg(content, toolCalls),
			"finish_reason": mapGeminiFinish(geminiResp.Candidates),
		},
	}
	openaiResp := map[string]interface{}{
		"id": "chatcmpl-gemini", "object": "chat.completion",
		"model": resp.Model, "choices": choices,
	}
	if geminiResp.UsageMetadata != nil {
		openaiResp["usage"] = map[string]int{
			"prompt_tokens":     geminiResp.UsageMetadata.PromptTokenCount,
			"completion_tokens": geminiResp.UsageMetadata.CandidatesTokenCount,
			"total_tokens":      geminiResp.UsageMetadata.TotalTokenCount,
		}
	}
	body, _ := json.Marshal(openaiResp)
	return &engine.Response{
		RequestID: resp.RequestID, Body: body, Model: resp.Model,
		StatusCode: resp.StatusCode, Headers: resp.Headers,
	}, nil
}

// TranslateStreamChunk converts Gemini stream chunk → OpenAI Chat chunk.
func (t *OpenAIToGemini) TranslateStreamChunk(_ context.Context, chunk *formats.Chunk) (*formats.Chunk, error) {
	if chunk == nil {
		return nil, errNilChunk
	}
	if chunk.Error != nil {
		return &formats.Chunk{Event: "error", Data: chunk.Data, IsFinal: true, Error: chunk.Error}, nil
	}
	var gemini struct {
		Candidates []struct {
			Content struct {
				Parts []struct {
					Text string `json:"text,omitempty"`
				} `json:"parts"`
			} `json:"content"`
			FinishReason *string `json:"finishReason,omitempty"`
		} `json:"candidates"`
		UsageMetadata *struct {
			PromptTokenCount int `json:"promptTokenCount"`
		} `json:"usageMetadata,omitempty"`
	}
	if err := json.Unmarshal(chunk.Data, &gemini); err != nil {
		return nil, fmt.Errorf("openai→gemini: unmarshal chunk: %w", err)
	}
	if len(gemini.Candidates) == 0 {
		return chunk, nil
	}
	c := gemini.Candidates[0]
	text := ""
	if len(c.Content.Parts) > 0 {
		text = c.Content.Parts[0].Text
	}
	isFinal := c.FinishReason != nil && *c.FinishReason != ""
	stop := ""
	if isFinal {
		stop = mapGeminiFinishReason(c.FinishReason)
	}
	data, _ := json.Marshal(map[string]interface{}{
		"choices": []map[string]interface{}{
			{"index": 0, "delta": map[string]string{"content": text}, "finish_reason": stop},
		},
	})
	return &formats.Chunk{Event: "chunk", Data: data, IsFinal: isFinal}, nil
}

// ---- helpers ----

func buildOpenAIMsg(content string, toolCalls []map[string]interface{}) map[string]interface{} {
	m := map[string]interface{}{"role": "assistant"}
	if content != "" {
		m["content"] = content
	} else {
		m["content"] = nil
	}
	if len(toolCalls) > 0 {
		m["tool_calls"] = toolCalls
	}
	return m
}

func mapGeminiFinish(candidates []struct {
	Content struct {
		Parts []struct {
			Text     string          `json:"text,omitempty"`
			FuncCall json.RawMessage `json:"functionCall,omitempty"`
		} `json:"parts"`
		Role string `json:"role"`
	} `json:"content"`
	FinishReason *string `json:"finishReason,omitempty"`
}) string {
	if len(candidates) == 0 || candidates[0].FinishReason == nil {
		return "stop"
	}
	return mapGeminiFinishReason(candidates[0].FinishReason)
}

func mapGeminiFinishReason(r *string) string {
	if r == nil {
		return "stop"
	}
	switch *r {
	case "STOP":
		return "stop"
	case "MAX_TOKENS":
		return "length"
	case "SAFETY", "BLOCKLIST", "PROHIBITED_CONTENT":
		return "content_filter"
	case "RECITATION":
		return "content_filter"
	case "TOOL_CALLS", "FUNCTION_CALL":
		return "tool_calls"
	default:
		return "stop"
	}
}

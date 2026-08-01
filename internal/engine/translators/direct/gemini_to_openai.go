package direct

import (
	"context"
	"encoding/json"
	"fmt"

	"gorouter/internal/domain/engine"
	"gorouter/internal/engine/formats"
)

type GeminiToOpenAI struct{}

func NewGeminiToOpenAI() *GeminiToOpenAI { return &GeminiToOpenAI{} }

func (t *GeminiToOpenAI) Source() engine.RequestFormat { return engine.FormatGemini }
func (t *GeminiToOpenAI) Target() engine.RequestFormat { return engine.FormatOpenAIChat }

// TranslateRequest converts Gemini generateContent request → OpenAI Chat request.
func (t *GeminiToOpenAI) TranslateRequest(_ context.Context, req *engine.Request) (*engine.Request, error) {
	if req == nil {
		return nil, errNilRequest
	}
	var geminiBody struct {
		Contents          []json.RawMessage `json:"contents"`
		SystemInstruction json.RawMessage   `json:"systemInstruction,omitempty"`
		GenerationConfig  json.RawMessage   `json:"generationConfig,omitempty"`
		Tools             []json.RawMessage `json:"tools,omitempty"`
	}
	if err := json.Unmarshal(req.RawBody, &geminiBody); err != nil {
		return nil, fmt.Errorf("gemini→openai: unmarshal: %w", err)
	}
	openaiMsgs := make([]json.RawMessage, 0)
	if len(geminiBody.SystemInstruction) > 0 {
		txt := extractGeminiSystemText(geminiBody.SystemInstruction)
		if txt != "" {
			s, _ := json.Marshal(map[string]interface{}{"role": "system", "content": txt})
			openaiMsgs = append(openaiMsgs, s)
		}
	}
	for _, raw := range geminiBody.Contents {
		var c struct {
			Role  string            `json:"role"`
			Parts []json.RawMessage `json:"parts"`
		}
		if json.Unmarshal(raw, &c) != nil {
			continue
		}
		role := mapGeminiRole(c.Role)
		text := ""
		for _, p := range c.Parts {
			var part struct {
				Text string `json:"text,omitempty"`
			}
			if json.Unmarshal(p, &part) == nil && part.Text != "" {
				text += part.Text
			}
		}
		if text != "" {
			m, _ := json.Marshal(map[string]interface{}{"role": role, "content": text})
			openaiMsgs = append(openaiMsgs, m)
		}
	}
	openaiBody := map[string]interface{}{"model": req.Model, "messages": openaiMsgs}
	if len(geminiBody.GenerationConfig) > 0 {
		var gc struct {
			Temperature  *float64 `json:"temperature,omitempty"`
			TopP         *float64 `json:"topP,omitempty"`
			MaxOutTokens int      `json:"maxOutputTokens,omitempty"`
		}
		if json.Unmarshal(geminiBody.GenerationConfig, &gc) == nil {
			if gc.Temperature != nil {
				openaiBody["temperature"] = *gc.Temperature
			}
			if gc.TopP != nil {
				openaiBody["top_p"] = *gc.TopP
			}
			if gc.MaxOutTokens > 0 {
				openaiBody["max_tokens"] = gc.MaxOutTokens
			}
		}
	}
	if len(geminiBody.Tools) > 0 {
		openaiBody["tools"] = geminiToolsToOpenAI(geminiBody.Tools)
	}
	raw, err := json.Marshal(openaiBody)
	if err != nil {
		return nil, fmt.Errorf("gemini→openai: marshal: %w", err)
	}
	return &engine.Request{
		ID: req.ID, Format: engine.FormatOpenAIChat, Model: req.Model,
		RawBody: raw, MappedBody: raw, Stream: req.Stream,
		Headers: req.Headers, UserID: req.UserID,
	}, nil
}

// TranslateResponse converts OpenAI response → Gemini response.
func (t *GeminiToOpenAI) TranslateResponse(_ context.Context, resp *engine.Response) (*engine.Response, error) {
	if resp == nil {
		return nil, errNilResponse
	}
	var openaiResp struct {
		Choices []struct {
			Message struct {
				Content   interface{}              `json:"content"`
				ToolCalls []map[string]interface{} `json:"tool_calls,omitempty"`
			} `json:"message"`
			FinishReason *string `json:"finish_reason,omitempty"`
		} `json:"choices"`
		Usage *struct {
			PromptTokens     int `json:"prompt_tokens"`
			CompletionTokens int `json:"completion_tokens"`
			TotalTokens      int `json:"total_tokens"`
		} `json:"usage,omitempty"`
	}
	if err := json.Unmarshal(resp.Body, &openaiResp); err != nil {
		return nil, fmt.Errorf("gemini→openai: unmarshal response: %w", err)
	}
	parts := make([]map[string]interface{}, 0)
	if len(openaiResp.Choices) > 0 {
		msg := openaiResp.Choices[0].Message
		if msg.Content != nil {
			if s, ok := msg.Content.(string); ok && s != "" {
				parts = append(parts, map[string]interface{}{"text": s})
			}
		}
		for _, tc := range msg.ToolCalls {
			var fn struct {
				Name      string `json:"name"`
				Arguments string `json:"arguments"`
			}
			if tc["function"] != nil {
				b, _ := json.Marshal(tc["function"])
				json.Unmarshal(b, &fn)
			}
			var args map[string]interface{}
			json.Unmarshal([]byte(fn.Arguments), &args)
			parts = append(parts, map[string]interface{}{
				"functionCall": map[string]interface{}{"name": fn.Name, "args": args},
			})
		}
	}
	geminiResp := map[string]interface{}{
		"candidates": []map[string]interface{}{
			{
				"content":      map[string]interface{}{"parts": parts, "role": "model"},
				"finishReason": mapOpenAIToGeminiFinish(openaiResp.Choices),
			},
		},
	}
	if openaiResp.Usage != nil {
		geminiResp["usageMetadata"] = map[string]int{
			"promptTokenCount":     openaiResp.Usage.PromptTokens,
			"candidatesTokenCount": openaiResp.Usage.CompletionTokens,
			"totalTokenCount":      openaiResp.Usage.TotalTokens,
		}
	}
	body, _ := json.Marshal(geminiResp)
	return &engine.Response{
		RequestID: resp.RequestID, Body: body, Model: resp.Model,
		StatusCode: resp.StatusCode, Headers: resp.Headers,
	}, nil
}

// TranslateStreamChunk converts OpenAI chunk → Gemini chunk.
func (t *GeminiToOpenAI) TranslateStreamChunk(_ context.Context, chunk *formats.Chunk) (*formats.Chunk, error) {
	if chunk == nil {
		return nil, errNilChunk
	}
	if string(chunk.Data) == "[DONE]" {
		return &formats.Chunk{Event: "candidate.finished", Data: []byte{}, IsFinal: true}, nil
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
		return nil, fmt.Errorf("gemini→openai: unmarshal chunk: %w", err)
	}
	if len(openai.Choices) == 0 {
		return chunk, nil
	}
	c := openai.Choices[0]
	candidate := map[string]interface{}{
		"candidates": []map[string]interface{}{
			{
				"content": map[string]interface{}{
					"parts": []map[string]string{{"text": c.Delta.Content}},
					"role":  "model",
				},
			},
		},
	}
	isFinal := false
	if c.FinishReason != nil && *c.FinishReason != "" {
		cf := mapOpenAIToGeminiFinishReason(*c.FinishReason)
		candidate["candidates"].([]map[string]interface{})[0]["finishReason"] = cf
		isFinal = true
	}
	data, _ := json.Marshal(candidate)
	return &formats.Chunk{Event: "chunk", Data: data, IsFinal: isFinal}, nil
}

// ---- helpers ----

func mapOpenAIToGeminiFinish(choices []struct {
	Message struct {
		Content   interface{}              `json:"content"`
		ToolCalls []map[string]interface{} `json:"tool_calls,omitempty"`
	} `json:"message"`
	FinishReason *string `json:"finish_reason,omitempty"`
}) string {
	if len(choices) == 0 || choices[0].FinishReason == nil {
		return "STOP"
	}
	return mapOpenAIToGeminiFinishReason(*choices[0].FinishReason)
}

func mapOpenAIToGeminiFinishReason(r string) string {
	switch r {
	case "stop":
		return "STOP"
	case "length":
		return "MAX_TOKENS"
	case "tool_calls":
		return "TOOL_CALLS"
	case "content_filter":
		return "SAFETY"
	default:
		return "STOP"
	}
}

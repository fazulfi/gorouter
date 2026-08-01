package openai

import (
	"encoding/json"
	"fmt"

	"gorouter/internal/domain/engine"
	"gorouter/internal/engine/formats"
)

// ChatCodec implements formats.Codec for the OpenAI Chat Completions wire
// format (FormatOpenAIChat).
type ChatCodec struct{}

func NewChatCodec() *ChatCodec {
	return &ChatCodec{}
}

func (c *ChatCodec) Format() engine.RequestFormat {
	return engine.FormatOpenAIChat
}

func (c *ChatCodec) DecodeRequest(body json.RawMessage) (*engine.Request, error) {
	if len(body) == 0 {
		return nil, fmt.Errorf("openai chat: %w", formats.ErrMalformedBody)
	}
	var raw struct {
		Model       string            `json:"model"`
		Messages    []json.RawMessage `json:"messages"`
		Stream      bool              `json:"stream"`
		MaxTokens   int               `json:"max_tokens"`
		Temperature *float64          `json:"temperature"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("openai chat: %w", formats.ErrMalformedBody)
	}
	if raw.Model == "" {
		return nil, fmt.Errorf("openai chat: %w", formats.ErrMalformedBody)
	}
	return &engine.Request{
		Format:      engine.FormatOpenAIChat,
		Model:       raw.Model,
		RawBody:     body,
		MappedBody:  body,
		Stream:      raw.Stream,
		MaxTokens:   raw.MaxTokens,
		Temperature: raw.Temperature,
	}, nil
}

func (c *ChatCodec) EncodeResponse(resp *engine.Response) (json.RawMessage, error) {
	if resp == nil || len(resp.Body) == 0 {
		return nil, fmt.Errorf("openai chat: %w", formats.ErrMalformedBody)
	}
	return resp.Body, nil
}

// openaiStreamChunk represents a single SSE data payload from the
// OpenAI Chat Completions streaming API.
type openaiStreamChunk struct {
	Choices []struct {
		Delta struct {
			Content string `json:"content,omitempty"`
		} `json:"delta,omitempty"`
		FinishReason *string `json:"finish_reason,omitempty"`
	} `json:"choices,omitempty"`
	Error *struct {
		Message string `json:"message"`
		Type    string `json:"type"`
	} `json:"error,omitempty"`
	Usage *struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
		TotalTokens      int `json:"total_tokens"`
	} `json:"usage,omitempty"`
}

func (c *ChatCodec) DecodeStreamChunk(eventType string, data []byte) (*formats.Chunk, error) {
	if len(data) == 0 {
		return nil, fmt.Errorf("openai chat: %w", formats.ErrInvalidChunk)
	}
	var raw openaiStreamChunk
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("openai chat: %w", formats.ErrInvalidChunk)
	}
	if raw.Error != nil {
		return &formats.Chunk{
			Event:   "error",
			Data:    data,
			IsFinal: true,
			Error:   fmt.Errorf("openai error: %s: %s", raw.Error.Type, raw.Error.Message),
		}, nil
	}
	chunk := &formats.Chunk{
		Event:   "chunk",
		Data:    data,
		IsFinal: false,
	}
	if len(raw.Choices) > 0 && raw.Choices[0].FinishReason != nil {
		chunk.IsFinal = true
	}
	if raw.Usage != nil {
		chunk.IsFinal = true
	}
	return chunk, nil
}

func (c *ChatCodec) EncodeStreamChunk(chunk *formats.Chunk) ([]byte, error) {
	if chunk.Error != nil {
		errData, _ := json.Marshal(map[string]interface{}{
			"error": map[string]string{
				"message": chunk.Error.Error(),
				"type":    "engine_error",
			},
		})
		return append([]byte("data: "), append(errData, '\n', '\n')...), nil
	}
	if chunk.Event == "done" || chunk.IsFinal {
		return c.TerminalEvent(), nil
	}
	return append([]byte("data: "), append(chunk.Data, '\n', '\n')...), nil
}

func (c *ChatCodec) TerminalEvent() []byte {
	return []byte("data: [DONE]\n\n")
}

// CompatCodec implements formats.Codec for OpenAI-compatible format
// (FormatOpenAICompat).
type CompatCodec struct{}

func NewCompatCodec() *CompatCodec {
	return &CompatCodec{}
}

func (c *CompatCodec) Format() engine.RequestFormat {
	return engine.FormatOpenAICompat
}

func (c *CompatCodec) DecodeRequest(body json.RawMessage) (*engine.Request, error) {
	return (&ChatCodec{}).DecodeRequest(body)
}

func (c *CompatCodec) EncodeResponse(resp *engine.Response) (json.RawMessage, error) {
	return (&ChatCodec{}).EncodeResponse(resp)
}

func (c *CompatCodec) DecodeStreamChunk(eventType string, data []byte) (*formats.Chunk, error) {
	return (&ChatCodec{}).DecodeStreamChunk(eventType, data)
}

func (c *CompatCodec) EncodeStreamChunk(chunk *formats.Chunk) ([]byte, error) {
	return (&ChatCodec{}).EncodeStreamChunk(chunk)
}

func (c *CompatCodec) TerminalEvent() []byte {
	return []byte("data: [DONE]\n\n")
}

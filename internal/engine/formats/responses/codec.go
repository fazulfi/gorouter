package responses

import (
	"encoding/json"
	"fmt"

	"gorouter/internal/domain/engine"
	"gorouter/internal/engine/formats"
)

// ResponsesCodec implements formats.Codec for the OpenAI Responses API wire
// format (FormatCodexResponses).
type ResponsesCodec struct{}

func NewResponsesCodec() *ResponsesCodec {
	return &ResponsesCodec{}
}

func (c *ResponsesCodec) Format() engine.RequestFormat {
	return engine.FormatCodexResponses
}

func (c *ResponsesCodec) DecodeRequest(body json.RawMessage) (*engine.Request, error) {
	if len(body) == 0 {
		return nil, fmt.Errorf("responses: %w", formats.ErrMalformedBody)
	}
	var raw struct {
		Model           string          `json:"model"`
		Input           json.RawMessage `json:"input"`
		Instructions    string          `json:"instructions,omitempty"`
		MaxOutputTokens int             `json:"max_output_tokens,omitempty"`
		Temperature     *float64        `json:"temperature,omitempty"`
		Stream          bool            `json:"stream,omitempty"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("responses: %w", formats.ErrMalformedBody)
	}
	if raw.Model == "" {
		return nil, fmt.Errorf("responses: %w", formats.ErrMalformedBody)
	}
	return &engine.Request{
		Format:      engine.FormatCodexResponses,
		Model:       raw.Model,
		RawBody:     body,
		MappedBody:  body,
		Stream:      raw.Stream,
		MaxTokens:   raw.MaxOutputTokens,
		Temperature: raw.Temperature,
	}, nil
}

func (c *ResponsesCodec) EncodeResponse(resp *engine.Response) (json.RawMessage, error) {
	if resp == nil || len(resp.Body) == 0 {
		return nil, fmt.Errorf("responses: %w", formats.ErrMalformedBody)
	}
	return resp.Body, nil
}

// responsesStreamEvent represents a single SSE event from the Responses API.
type responsesStreamEvent struct {
	Type  string          `json:"type"`
	Data  json.RawMessage `json:"data,omitempty"`
	Error *struct {
		Message string `json:"message"`
		Code    string `json:"code,omitempty"`
	} `json:"error,omitempty"`
}

func (c *ResponsesCodec) DecodeStreamChunk(eventType string, data []byte) (*formats.Chunk, error) {
	if len(data) == 0 {
		return nil, fmt.Errorf("responses: %w", formats.ErrInvalidChunk)
	}
	var raw json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("responses: %w", formats.ErrInvalidChunk)
	}

	var event responsesStreamEvent
	if err := json.Unmarshal(data, &event); err != nil {
		return nil, fmt.Errorf("responses: %w", formats.ErrInvalidChunk)
	}

	chunk := &formats.Chunk{
		Event:   event.Type,
		Data:    data,
		IsFinal: false,
	}

	switch event.Type {
	case "response.completed":
		chunk.IsFinal = true
	case "error":
		chunk.IsFinal = true
		if event.Error != nil {
			chunk.Error = fmt.Errorf("responses error: %s: %s", event.Error.Code, event.Error.Message)
		}
	case "response.failed":
		chunk.IsFinal = true
		if event.Error != nil {
			chunk.Error = fmt.Errorf("responses failed: %s: %s", event.Error.Code, event.Error.Message)
		}
	case "done":
		chunk.IsFinal = true
	}
	return chunk, nil
}

func (c *ResponsesCodec) EncodeStreamChunk(chunk *formats.Chunk) ([]byte, error) {
	if chunk.Error != nil {
		errData, _ := json.Marshal(map[string]interface{}{
			"type": "error",
			"error": map[string]string{
				"message": chunk.Error.Error(),
			},
		})
		return append([]byte("event: error\ndata: "), append(errData, '\n', '\n')...), nil
	}
	if chunk.Event == "done" || chunk.Event == "response.completed" {
		if chunk.Event == "done" {
			return []byte("event: done\ndata: [DONE]\n\n"), nil
		}
		return append([]byte("event: "), append([]byte(chunk.Event), append([]byte("\ndata: "), append(chunk.Data, '\n', '\n')...)...)...), nil
	}
	return append([]byte("event: "), append([]byte(chunk.Event), append([]byte("\ndata: "), append(chunk.Data, '\n', '\n')...)...)...), nil
}

func (c *ResponsesCodec) TerminalEvent() []byte {
	return []byte("event: done\ndata: [DONE]\n\n")
}

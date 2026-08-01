package claude

import (
	"encoding/json"
	"fmt"

	"gorouter/internal/domain/engine"
	"gorouter/internal/engine/formats"
)

// MessagesCodec implements formats.Codec for the Anthropic Claude Messages
// API wire format (FormatAnthropic).
type MessagesCodec struct{}

func NewMessagesCodec() *MessagesCodec {
	return &MessagesCodec{}
}

func (c *MessagesCodec) Format() engine.RequestFormat {
	return engine.FormatAnthropic
}

func (c *MessagesCodec) DecodeRequest(body json.RawMessage) (*engine.Request, error) {
	if len(body) == 0 {
		return nil, fmt.Errorf("claude: %w", formats.ErrMalformedBody)
	}
	var raw struct {
		Model       string            `json:"model"`
		Messages    []json.RawMessage `json:"messages"`
		MaxTokens   int               `json:"max_tokens"`
		Stream      bool              `json:"stream,omitempty"`
		Temperature *float64          `json:"temperature,omitempty"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("claude: %w", formats.ErrMalformedBody)
	}
	if raw.Model == "" {
		return nil, fmt.Errorf("claude: %w", formats.ErrMalformedBody)
	}
	return &engine.Request{
		Format:      engine.FormatAnthropic,
		Model:       raw.Model,
		RawBody:     body,
		MappedBody:  body,
		Stream:      raw.Stream,
		MaxTokens:   raw.MaxTokens,
		Temperature: raw.Temperature,
	}, nil
}

func (c *MessagesCodec) EncodeResponse(resp *engine.Response) (json.RawMessage, error) {
	if resp == nil || len(resp.Body) == 0 {
		return nil, fmt.Errorf("claude: %w", formats.ErrMalformedBody)
	}
	return resp.Body, nil
}

// claudeStreamEvent represents a single SSE event from the Claude Messages
// streaming API.
type claudeStreamEvent struct {
	Type string          `json:"type"`
	Data json.RawMessage `json:"data,omitempty"`
	// message_stop has no data payload.
	Error *struct {
		Type    string `json:"type"`
		Message string `json:"message"`
	} `json:"error,omitempty"`
	// delta events carry a stop_reason in the delta block.
	Delta *struct {
		StopReason   *string `json:"stop_reason,omitempty"`
		StopSequence *string `json:"stop_sequence,omitempty"`
		Text         string  `json:"text,omitempty"`
	} `json:"delta,omitempty"`
}

func (c *MessagesCodec) DecodeStreamChunk(eventType string, data []byte) (*formats.Chunk, error) {
	if len(data) == 0 && eventType == "" {
		return nil, fmt.Errorf("claude: %w", formats.ErrInvalidChunk)
	}

	// When the SSE event type is message_stop, the data payload is "{}" with no
	// type field. Detect via eventType before attempting JSON parse.
	if eventType == "message_stop" {
		return &formats.Chunk{
			Event:   "message_stop",
			Data:    data,
			IsFinal: true,
		}, nil
	}

	var event claudeStreamEvent
	if err := json.Unmarshal(data, &event); err != nil {
		return nil, fmt.Errorf("claude: %w", formats.ErrInvalidChunk)
	}

	chunk := &formats.Chunk{
		Event:   event.Type,
		Data:    data,
		IsFinal: false,
	}

	switch event.Type {
	case "message_stop":
		chunk.IsFinal = true
	case "message_delta":
		if event.Delta != nil && event.Delta.StopReason != nil {
			chunk.IsFinal = true
		}
	case "error":
		chunk.IsFinal = true
		if event.Error != nil {
			chunk.Error = fmt.Errorf("claude error: %s: %s", event.Error.Type, event.Error.Message)
		}
	}
	return chunk, nil
}

func (c *MessagesCodec) EncodeStreamChunk(chunk *formats.Chunk) ([]byte, error) {
	if chunk.Error != nil {
		errData, _ := json.Marshal(map[string]interface{}{
			"type": "error",
			"error": map[string]string{
				"type":    "engine_error",
				"message": chunk.Error.Error(),
			},
		})
		return append([]byte("event: error\ndata: "), append(errData, '\n', '\n')...), nil
	}
	if chunk.Event == "message_stop" {
		return []byte("event: message_stop\ndata: {}\n\n"), nil
	}
	return append([]byte("event: "), append([]byte(chunk.Event), append([]byte("\ndata: "), append(chunk.Data, '\n', '\n')...)...)...), nil
}

func (c *MessagesCodec) TerminalEvent() []byte {
	return []byte("event: message_stop\ndata: {}\n\n")
}

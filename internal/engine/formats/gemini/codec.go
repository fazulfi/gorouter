package gemini

import (
	"encoding/json"
	"fmt"

	"gorouter/internal/domain/engine"
	"gorouter/internal/engine/formats"
)

// GenerateCodec implements formats.Codec for the Google Gemini API wire
// format (FormatGemini).
type GenerateCodec struct{}

func NewGenerateCodec() *GenerateCodec {
	return &GenerateCodec{}
}

func (c *GenerateCodec) Format() engine.RequestFormat {
	return engine.FormatGemini
}

func (c *GenerateCodec) DecodeRequest(body json.RawMessage) (*engine.Request, error) {
	if len(body) == 0 {
		return nil, fmt.Errorf("gemini: %w", formats.ErrMalformedBody)
	}
	var raw struct {
		Contents    []json.RawMessage `json:"contents"`
		Model       string            `json:"model,omitempty"`
		Temperature *float64          `json:"temperature,omitempty"`
		MaxTokens   int               `json:"maxOutputTokens,omitempty"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("gemini: %w", formats.ErrMalformedBody)
	}
	// Model can be embedded in the URL path for Gemini, so it's optional in body.
	req := &engine.Request{
		Format:      engine.FormatGemini,
		Model:       raw.Model,
		RawBody:     body,
		MappedBody:  body,
		Temperature: raw.Temperature,
		MaxTokens:   raw.MaxTokens,
	}
	return req, nil
}

func (c *GenerateCodec) EncodeResponse(resp *engine.Response) (json.RawMessage, error) {
	if resp == nil || len(resp.Body) == 0 {
		return nil, fmt.Errorf("gemini: %w", formats.ErrMalformedBody)
	}
	return resp.Body, nil
}

// geminiStreamCandidate represents a candidate in a Gemini stream response.
type geminiStreamCandidate struct {
	Index   int `json:"index"`
	Content struct {
		Parts []struct {
			Text string `json:"text,omitempty"`
		} `json:"parts,omitempty"`
		Role string `json:"role,omitempty"`
	} `json:"content,omitempty"`
	FinishReason  *string `json:"finishReason,omitempty"`
	SafetyRatings []struct {
		Category    string `json:"category"`
		Probability string `json:"probability"`
	} `json:"safetyRatings,omitempty"`
}

// geminiStreamResponse is a single chunk from the Gemini streaming API.
// Gemini does not use a [DONE] sentinel; stream termination is indicated
// by the presence of finishReason in the final candidate.
type geminiStreamResponse struct {
	Candidates []geminiStreamCandidate `json:"candidates,omitempty"`
	Error      *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
		Status  string `json:"status"`
	} `json:"error,omitempty"`
	UsageMetadata *struct {
		PromptTokenCount     int `json:"promptTokenCount"`
		CandidatesTokenCount int `json:"candidatesTokenCount"`
		TotalTokenCount      int `json:"totalTokenCount"`
	} `json:"usageMetadata,omitempty"`
}

func (c *GenerateCodec) DecodeStreamChunk(eventType string, data []byte) (*formats.Chunk, error) {
	if len(data) == 0 {
		return nil, fmt.Errorf("gemini: %w", formats.ErrInvalidChunk)
	}
	var resp geminiStreamResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, fmt.Errorf("gemini: %w", formats.ErrInvalidChunk)
	}

	chunk := &formats.Chunk{
		Event:   "chunk",
		Data:    data,
		IsFinal: false,
	}

	if resp.Error != nil {
		chunk.IsFinal = true
		chunk.Event = "error"
		chunk.Error = fmt.Errorf("gemini error: %s: %s", resp.Error.Status, resp.Error.Message)
		return chunk, nil
	}

	for _, c := range resp.Candidates {
		if c.FinishReason != nil && *c.FinishReason != "" {
			chunk.IsFinal = true
			chunk.Event = "candidate.finished"
			break
		}
	}

	if resp.UsageMetadata != nil {
		chunk.IsFinal = true
	}

	return chunk, nil
}

func (c *GenerateCodec) EncodeStreamChunk(chunk *formats.Chunk) ([]byte, error) {
	if chunk.Error != nil {
		errData, _ := json.Marshal(map[string]interface{}{
			"error": map[string]interface{}{
				"code":    500,
				"message": chunk.Error.Error(),
				"status":  "ENGINE_ERROR",
			},
		})
		return append(errData, '\n'), nil
	}
	if chunk.IsFinal && chunk.Event == "candidate.finished" {
		return append(chunk.Data, '\n'), nil
	}
	if chunk.Data != nil {
		return append(chunk.Data, '\n'), nil
	}
	return []byte("\n"), nil
}

func (c *GenerateCodec) TerminalEvent() []byte {
	// Gemini has no [DONE] sentinel. Terminal detection is via finishReason
	// in the last candidate. Return nil to indicate absence of a sentinel.
	return nil
}

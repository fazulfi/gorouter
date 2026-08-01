// Package embeddings implements the OpenAI-compatible /v1/embeddings
// modality (audit/01-http-contracts.md:544, upstream
// open-sse/handlers/embeddingsCore.js; endpoint mapping
// internal/engine/formats/detect.go:32).
package embeddings

import (
	"encoding/json"
	"fmt"

	"gorouter/internal/domain/engine"
	"gorouter/internal/engine/media"
)

// Request is the OpenAI-compatible embeddings request body
// (audit/01-http-contracts.md:544 "JSON OpenAI embeddings").
type Request struct {
	Model          string          `json:"model"`
	Input          json.RawMessage `json:"input"`
	EncodingFormat string          `json:"encoding_format,omitempty"`
	Dimensions     *int            `json:"dimensions,omitempty"`
	User           string          `json:"user,omitempty"`
}

// Embedding is a single embedding vector in the response data array.
type Embedding struct {
	Object    string    `json:"object"`
	Index     int       `json:"index"`
	Embedding []float64 `json:"embedding"`
}

// Response is the OpenAI-compatible embeddings response body.
type Response struct {
	Object string        `json:"object"`
	Data   []Embedding   `json:"data"`
	Model  string        `json:"model"`
	Usage  *engine.Usage `json:"usage,omitempty"`
}

// DecodeRequest parses an embeddings request body. Empty or malformed JSON
// returns a wrapped media.ErrMalformedRequest.
func DecodeRequest(body []byte) (*Request, error) {
	if len(body) == 0 {
		return nil, fmt.Errorf("embeddings: %w", media.ErrMalformedRequest)
	}
	var req Request
	if err := json.Unmarshal(body, &req); err != nil {
		return nil, fmt.Errorf("embeddings: %w", media.ErrMalformedRequest)
	}
	if req.Model == "" {
		return nil, fmt.Errorf("embeddings: %w: missing model", media.ErrMalformedRequest)
	}
	if len(req.Input) == 0 || string(req.Input) == "null" {
		return nil, fmt.Errorf("embeddings: %w: missing input", media.ErrMalformedRequest)
	}
	return &req, nil
}

// EncodeRequest serializes an embeddings request.
func EncodeRequest(req *Request) ([]byte, error) {
	if req == nil {
		return nil, fmt.Errorf("embeddings: %w: nil request", media.ErrMalformedRequest)
	}
	return json.Marshal(req)
}

// usage is the wire-level usage payload. engine.Usage carries no JSON tags,
// so the wire shape is decoded here and converted.
type usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

// DecodeResponse parses an embeddings response body.
func DecodeResponse(body []byte) (*Response, error) {
	if len(body) == 0 {
		return nil, fmt.Errorf("embeddings: %w", media.ErrMalformedRequest)
	}
	var wire struct {
		Object string      `json:"object"`
		Data   []Embedding `json:"data"`
		Model  string      `json:"model"`
		Usage  *usage      `json:"usage,omitempty"`
	}
	if err := json.Unmarshal(body, &wire); err != nil {
		return nil, fmt.Errorf("embeddings: %w", media.ErrMalformedRequest)
	}
	resp := &Response{Object: wire.Object, Data: wire.Data, Model: wire.Model}
	if wire.Usage != nil {
		resp.Usage = &engine.Usage{
			PromptTokens:     wire.Usage.PromptTokens,
			CompletionTokens: wire.Usage.CompletionTokens,
			TotalTokens:      wire.Usage.TotalTokens,
		}
	}
	return resp, nil
}

// EncodeResponse serializes an embeddings response.
func EncodeResponse(resp *Response) ([]byte, error) {
	if resp == nil {
		return nil, fmt.Errorf("embeddings: %w: nil response", media.ErrMalformedRequest)
	}
	return json.Marshal(resp)
}

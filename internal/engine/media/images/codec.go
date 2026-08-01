// Package images implements the OpenAI-compatible image generation,
// image edit, and image-to-text modalities (audit/01-http-contracts.md:545,
// upstream src/sse/handlers/imageGeneration.js; endpoint mapping
// internal/engine/formats/detect.go:33; imageToText service kind
// audit/02-provider-matrix.md:58).
package images

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strings"

	"gorouter/internal/engine/media"
)

// GenerationRequest is the OpenAI-compatible image generation body
// (audit/01-http-contracts.md:545 "JSON OpenAI image gen").
type GenerationRequest struct {
	Model          string `json:"model"`
	Prompt         string `json:"prompt"`
	N              *int   `json:"n,omitempty"`
	Size           string `json:"size,omitempty"`
	ResponseFormat string `json:"response_format,omitempty"`
	Quality        string `json:"quality,omitempty"`
}

// Image is one generated image: URL or base64 payload
// (audit/01-http-contracts.md:545 "JSON with image URLs/b64").
type Image struct {
	URL           string `json:"url,omitempty"`
	B64JSON       string `json:"b64_json,omitempty"`
	RevisedPrompt string `json:"revised_prompt,omitempty"`
}

// GenerationResponse is the OpenAI-compatible image generation response.
type GenerationResponse struct {
	Created int64   `json:"created"`
	Data    []Image `json:"data"`
}

// DecodeGenerationRequest parses an image generation body.
func DecodeGenerationRequest(body []byte) (*GenerationRequest, error) {
	if len(body) == 0 {
		return nil, fmt.Errorf("images: %w", media.ErrMalformedRequest)
	}
	var req GenerationRequest
	if err := json.Unmarshal(body, &req); err != nil {
		return nil, fmt.Errorf("images: %w", media.ErrMalformedRequest)
	}
	if req.Prompt == "" {
		return nil, fmt.Errorf("images: %w: missing prompt", media.ErrMalformedRequest)
	}
	return &req, nil
}

// EncodeGenerationRequest serializes an image generation body.
func EncodeGenerationRequest(req *GenerationRequest) ([]byte, error) {
	if req == nil {
		return nil, fmt.Errorf("images: %w: nil request", media.ErrMalformedRequest)
	}
	return json.Marshal(req)
}

// DecodeGenerationResponse parses an image generation response.
func DecodeGenerationResponse(body []byte) (*GenerationResponse, error) {
	if len(body) == 0 {
		return nil, fmt.Errorf("images: %w", media.ErrMalformedRequest)
	}
	var resp GenerationResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("images: %w", media.ErrMalformedRequest)
	}
	return &resp, nil
}

// EditRequest is the OpenAI-compatible image edit shape. The image binary is
// uploaded separately from the JSON fields (multipart/form-data upstream).
// Upstream route inventory lists only image generation (audit/01-http-contracts.md:545);
// no edit route is cited, so every provider capability check fails closed
// until an authority proves edit support.
type EditRequest struct {
	Model          string `json:"model"`
	Image          []byte `json:"-"`
	ImageFileName  string `json:"-"`
	Prompt         string `json:"prompt"`
	Size           string `json:"size,omitempty"`
	ResponseFormat string `json:"response_format,omitempty"`
	N              *int   `json:"n,omitempty"`
}

// DecodeEditRequest parses the JSON portion of an image edit request.
func DecodeEditRequest(body []byte) (*EditRequest, error) {
	if len(body) == 0 {
		return nil, fmt.Errorf("images: %w", media.ErrMalformedRequest)
	}
	var req EditRequest
	if err := json.Unmarshal(body, &req); err != nil {
		return nil, fmt.Errorf("images: %w", media.ErrMalformedRequest)
	}
	if req.Prompt == "" {
		return nil, fmt.Errorf("images: %w: missing prompt", media.ErrMalformedRequest)
	}
	return &req, nil
}

// Part is an OpenAI-compatible chat message content part. Image content parts
// follow the shape translated by internal/engine/translators/direct/helpers.go
// (image_url with data:image/...;base64 or https URL).
type Part struct {
	Type     string    `json:"type"`
	Text     string    `json:"text,omitempty"`
	ImageURL *ImageURL `json:"image_url,omitempty"`
}

// ImageURL is the image_url content part.
type ImageURL struct {
	URL string `json:"url"`
}

// Message is an OpenAI-compatible chat message whose content may be a string
// or an array of parts (including image parts).
type Message struct {
	Role    string          `json:"role"`
	Content json.RawMessage `json:"content"`
}

// ImageToTextRequest is the image-to-text request: an OpenAI-compatible chat
// body containing at least one image content part (imageToText service kind,
// audit/02-provider-matrix.md:58).
type ImageToTextRequest struct {
	Model    string    `json:"model"`
	Messages []Message `json:"messages"`
}

// DecodeImageToTextRequest parses and validates an image-to-text request:
// every message role/content is required and at least one image_url part must
// be present with a data:image/<mime>;base64 or http(s) URL.
func DecodeImageToTextRequest(body []byte) (*ImageToTextRequest, error) {
	if len(body) == 0 {
		return nil, fmt.Errorf("images: %w", media.ErrMalformedRequest)
	}
	var req ImageToTextRequest
	if err := json.Unmarshal(body, &req); err != nil {
		return nil, fmt.Errorf("images: %w", media.ErrMalformedRequest)
	}
	if req.Model == "" {
		return nil, fmt.Errorf("images: %w: missing model", media.ErrMalformedRequest)
	}
	if len(req.Messages) == 0 {
		return nil, fmt.Errorf("images: %w: missing messages", media.ErrMalformedRequest)
	}
	foundImage := false
	for i, msg := range req.Messages {
		if msg.Role == "" || len(msg.Content) == 0 {
			return nil, fmt.Errorf("images: %w: message %d missing role/content", media.ErrMalformedRequest, i)
		}
		trimmed := strings.TrimSpace(string(msg.Content))
		if strings.HasPrefix(trimmed, "[") {
			var parts []Part
			if err := json.Unmarshal(msg.Content, &parts); err != nil {
				return nil, fmt.Errorf("images: %w: message %d bad content parts", media.ErrMalformedRequest, i)
			}
			for _, p := range parts {
				if p.Type == "image_url" && p.ImageURL != nil {
					if !validImageURL(p.ImageURL.URL) {
						return nil, fmt.Errorf("images: %w: message %d invalid image_url", media.ErrMalformedRequest, i)
					}
					foundImage = true
				}
			}
		}
	}
	if !foundImage {
		return nil, fmt.Errorf("images: %w: no image_url part", media.ErrMalformedRequest)
	}
	return &req, nil
}

// validImageURL accepts data:image/<mime>;base64,<payload> data URLs with a
// non-empty payload, or http(s) URLs (shape proven by the safeIR/direct image
// handling, internal/engine/translators/safeir/roundtrip.go image_url
// conversion).
func validImageURL(raw string) bool {
	if strings.HasPrefix(raw, "data:image/") {
		marker := ";base64,"
		idx := strings.Index(raw, marker)
		return idx > len("data:image/") && len(raw) > idx+len(marker)
	}
	u, err := url.Parse(raw)
	if err != nil {
		return false
	}
	return u.Scheme == "https" || u.Scheme == "http"
}

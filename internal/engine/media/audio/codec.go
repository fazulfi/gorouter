// Package audio implements the TTS, STT, and voices modalities
// (audit/01-http-contracts.md:546-548, upstream src/sse/handlers/tts.js and
// stt.js; OpenAI voice list format at :548).
package audio

import (
	"encoding/json"
	"fmt"
	"io"

	"gorouter/internal/engine/media"
)

// SpeechRequest is the OpenAI-compatible TTS request body
// (audit/01-http-contracts.md:546 "JSON TTS request").
type SpeechRequest struct {
	Model          string  `json:"model"`
	Input          string  `json:"input"`
	Voice          string  `json:"voice"`
	ResponseFormat string  `json:"response_format,omitempty"`
	Speed          float64 `json:"speed,omitempty"`
}

// SpeechResponse is the binary TTS audio stream
// (audit/01-http-contracts.md:546 "Binary audio stream").
type SpeechResponse struct {
	Audio     []byte
	MediaType string
}

// DecodeSpeechRequest parses a TTS request body.
func DecodeSpeechRequest(body []byte) (*SpeechRequest, error) {
	if len(body) == 0 {
		return nil, fmt.Errorf("audio: %w", media.ErrMalformedRequest)
	}
	var req SpeechRequest
	if err := json.Unmarshal(body, &req); err != nil {
		return nil, fmt.Errorf("audio: %w", media.ErrMalformedRequest)
	}
	if req.Model == "" {
		return nil, fmt.Errorf("audio: %w: missing model", media.ErrMalformedRequest)
	}
	if req.Input == "" {
		return nil, fmt.Errorf("audio: %w: missing input", media.ErrMalformedRequest)
	}
	if req.Voice == "" {
		return nil, fmt.Errorf("audio: %w: missing voice", media.ErrMalformedRequest)
	}
	return &req, nil
}

// EncodeSpeechRequest serializes a TTS request body.
func EncodeSpeechRequest(req *SpeechRequest) ([]byte, error) {
	if req == nil {
		return nil, fmt.Errorf("audio: %w: nil request", media.ErrMalformedRequest)
	}
	return json.Marshal(req)
}

// TranscribeRequest is the STT upload. The audio binary streams through the
// multipart body; the route ceiling is 300 s (maxDuration = 300,
// audit/01-http-contracts.md:547).
type TranscribeRequest struct {
	Model          string
	FileName       string
	Audio          io.Reader
	Language       string
	Prompt         string
	ResponseFormat string
	Temperature    *float64
}

// TranscribeResponse is the STT JSON text result
// (audit/01-http-contracts.md:547 "JSON with text").
type TranscribeResponse struct {
	Text string `json:"text"`
}

// DecodeTranscribeResponse parses an STT result body.
func DecodeTranscribeResponse(body []byte) (*TranscribeResponse, error) {
	if len(body) == 0 {
		return nil, fmt.Errorf("audio: %w", media.ErrMalformedRequest)
	}
	var resp TranscribeResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("audio: %w", media.ErrMalformedRequest)
	}
	return &resp, nil
}

// Voice is one entry of the OpenAI voice list format
// (audit/01-http-contracts.md:548 `{ object: "list", data: [...] }`).
type Voice struct {
	VoiceID  string `json:"voice_id"`
	Name     string `json:"name,omitempty"`
	Language string `json:"language,omitempty"`
}

// VoicesResponse is the OpenAI voice list format.
type VoicesResponse struct {
	Object string  `json:"object"`
	Data   []Voice `json:"data"`
}

// DecodeVoicesResponse parses a voices list body.
func DecodeVoicesResponse(body []byte) (*VoicesResponse, error) {
	if len(body) == 0 {
		return nil, fmt.Errorf("audio: %w", media.ErrMalformedRequest)
	}
	var resp VoicesResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("audio: %w", media.ErrMalformedRequest)
	}
	return &resp, nil
}

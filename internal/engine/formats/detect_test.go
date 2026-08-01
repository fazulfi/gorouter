package formats_test

import (
	"context"
	"encoding/json"
	"testing"

	"gorouter/internal/domain/engine"
	"gorouter/internal/engine/formats"
)

func TestDetect_EndpointPriority(t *testing.T) {
	d := formats.NewDetector()
	ctx := context.Background()

	tests := []struct {
		name     string
		endpoint string
		body     json.RawMessage
		want     engine.RequestFormat
		wantErr  bool
	}{
		{
			name:     "openai chat endpoint",
			endpoint: "/v1/chat/completions",
			body:     json.RawMessage(`{"model":"gpt-4","messages":[{"role":"user","content":"hi"}]}`),
			want:     engine.FormatOpenAIChat,
		},
		{
			name:     "openai chat with query string",
			endpoint: "/v1/chat/completions?foo=bar",
			body:     json.RawMessage(`{"messages":[{"role":"user","content":"hi"}]}`),
			want:     engine.FormatOpenAIChat,
		},
		{
			name:     "codex responses endpoint",
			endpoint: "/v1/responses",
			body:     json.RawMessage(`{"model":"gpt-4","input":"hello"}`),
			want:     engine.FormatCodexResponses,
		},
		{
			name:     "anthropic messages endpoint",
			endpoint: "/v1/messages",
			body:     json.RawMessage(`{"model":"claude-3","messages":[{"role":"user","content":"hi"}]}`),
			want:     engine.FormatAnthropic,
		},
		{
			name:     "gemini endpoint",
			endpoint: "/v1beta/models/gemini-pro:generateContent",
			body:     json.RawMessage(`{"contents":[{"role":"user","parts":[{"text":"hi"}]}]}`),
			want:     engine.FormatGemini,
		},
		{
			name:     "openai compat endpoint",
			endpoint: "/v1/completions",
			body:     json.RawMessage(`{"model":"text-davinci-003","prompt":"hello"}`),
			want:     engine.FormatOpenAICompat,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := d.Detect(ctx, tt.endpoint, tt.body)
			if (err != nil) != tt.wantErr {
				t.Errorf("Detect() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("Detect() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestDetect_BodyFallback(t *testing.T) {
	d := formats.NewDetector()
	ctx := context.Background()

	tests := []struct {
		name     string
		endpoint string
		body     json.RawMessage
		want     engine.RequestFormat
	}{
		{
			name:     "openai chat by body with messages+model",
			endpoint: "/api/generate",
			body:     json.RawMessage(`{"model":"gpt-4","messages":[{"role":"user","content":"hi"}]}`),
			want:     engine.FormatOpenAIChat,
		},
		{
			name:     "openai chat by body with model only",
			endpoint: "/api/generate",
			body:     json.RawMessage(`{"model":"gpt-4"}`),
			want:     engine.FormatOpenAIChat,
		},
		{
			name:     "codex responses by body with input",
			endpoint: "/api/generate",
			body:     json.RawMessage(`{"model":"gpt-4","input":"hello"}`),
			want:     engine.FormatCodexResponses,
		},
		{
			name:     "openai compat by body with messages but no model",
			endpoint: "/api/generate",
			body:     json.RawMessage(`{"messages":[{"role":"user","content":"hi"}]}`),
			want:     engine.FormatOpenAICompat,
		},
		{
			name:     "anthropic by body with messages and system discriminator",
			endpoint: "/api/generate",
			body:     json.RawMessage(`{"system":"You are helpful","messages":[{"role":"user","content":"hi"}]}`),
			want:     engine.FormatAnthropic,
		},
		{
			name:     "anthropic by body with anthropic_version",
			endpoint: "/api/generate",
			body:     json.RawMessage(`{"anthropic_version":"bedrock-2023-05-31","messages":[{"role":"user","content":"hi"}]}`),
			want:     engine.FormatAnthropic,
		},
		{
			name:     "gemini by body with contents",
			endpoint: "/api/generate",
			body:     json.RawMessage(`{"contents":[{"role":"user","parts":[{"text":"hi"}]}]}`),
			want:     engine.FormatGemini,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := d.Detect(ctx, tt.endpoint, tt.body)
			if err != nil {
				t.Errorf("Detect() unexpected error: %v", err)
				return
			}
			if got != tt.want {
				t.Errorf("Detect() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestDetect_UnknownFormat(t *testing.T) {
	d := formats.NewDetector()
	ctx := context.Background()

	tests := []struct {
		name     string
		endpoint string
		body     json.RawMessage
	}{
		{
			name:     "empty endpoint and body",
			endpoint: "",
			body:     nil,
		},
		{
			name:     "unknown endpoint, empty body",
			endpoint: "/v1/unknown",
			body:     nil,
		},
		{
			name:     "unknown body shape",
			endpoint: "/api/generate",
			body:     json.RawMessage(`{"foo":"bar"}`),
		},
		{
			name:     "invalid JSON body",
			endpoint: "/api/generate",
			body:     json.RawMessage(`{invalid}`),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := d.Detect(ctx, tt.endpoint, tt.body)
			if err == nil {
				t.Errorf("Detect() expected error, got %v", got)
			}
			if got != "" {
				t.Errorf("Detect() = %v, want empty on error", got)
			}
		})
	}
}

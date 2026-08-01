// Package golden_formats provides golden-file based testing for format
// detection and codec behavior across all supported wire protocols.
package golden_formats

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"gorouter/internal/domain/engine"
	"gorouter/internal/engine/formats"
	"gorouter/internal/engine/formats/claude"
	"gorouter/internal/engine/formats/gemini"
	"gorouter/internal/engine/formats/native"
	"gorouter/internal/engine/formats/openai"
	"gorouter/internal/engine/formats/responses"
)

// fixtureDir returns the directory containing golden test fixtures.
func fixtureDir() string {
	_, f, _, _ := runtime.Caller(0)
	return filepath.Dir(f)
}

func readGolden(t *testing.T, name string) []byte {
	t.Helper()
	path := filepath.Join(fixtureDir(), name)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden %s: %v", name, err)
	}
	return data
}

func TestDetectFormat_GoldenFixtures(t *testing.T) {
	det := formats.NewDetector()
	ctx := context.Background()

	tests := []struct {
		name     string
		golden   string
		endpoint string
		want     engine.RequestFormat
	}{
		{
			name:     "openai_chat_request",
			golden:   "openai_chat_request.json",
			endpoint: "/v1/chat/completions",
			want:     engine.FormatOpenAIChat,
		},
		{
			name:     "codex_responses_request",
			golden:   "codex_responses_request.json",
			endpoint: "/v1/responses",
			want:     engine.FormatCodexResponses,
		},
		{
			name:     "anthropic_messages_request",
			golden:   "anthropic_messages_request.json",
			endpoint: "/v1/messages",
			want:     engine.FormatAnthropic,
		},
		{
			name:     "gemini_generate_request",
			golden:   "gemini_generate_request.json",
			endpoint: "/v1beta/models/gemini-pro:generateContent",
			want:     engine.FormatGemini,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body := readGolden(t, tt.golden)
			got, err := det.Detect(ctx, tt.endpoint, body)
			if err != nil {
				t.Fatalf("Detect() error: %v", err)
			}
			if got != tt.want {
				t.Errorf("Detect() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestOpenAIChatCodec_GoldenDecode(t *testing.T) {
	body := readGolden(t, "openai_chat_request.json")
	c := openai.NewChatCodec()
	req, err := c.DecodeRequest(body)
	if err != nil {
		t.Fatalf("DecodeRequest error: %v", err)
	}
	if req.Model != "gpt-4" {
		t.Errorf("Model = %q, want gpt-4", req.Model)
	}
	if req.Format != engine.FormatOpenAIChat {
		t.Errorf("Format = %v", req.Format)
	}
}

func TestOpenAIChatCodec_GoldenStreamChunk(t *testing.T) {
	c := openai.NewChatCodec()
	data := readGolden(t, "openai_chat_stream_chunk.json")

	chunk, err := c.DecodeStreamChunk("", data)
	if err != nil {
		t.Fatalf("DecodeStreamChunk error: %v", err)
	}
	if chunk.IsFinal {
		t.Error("stream chunk should not be terminal")
	}
	if chunk.Data == nil {
		t.Error("chunk should have data")
	}
}

func TestOpenAIChatCodec_GoldenStreamTerminal(t *testing.T) {
	c := openai.NewChatCodec()
	if string(c.TerminalEvent()) != "data: [DONE]\n\n" {
		t.Errorf("TerminalEvent = %q", string(c.TerminalEvent()))
	}
}

func TestResponsesCodec_GoldenDecode(t *testing.T) {
	body := readGolden(t, "codex_responses_request.json")
	c := responses.NewResponsesCodec()
	req, err := c.DecodeRequest(body)
	if err != nil {
		t.Fatalf("DecodeRequest error: %v", err)
	}
	if req.Model != "gpt-4" {
		t.Errorf("Model = %q", req.Model)
	}
}

func TestResponsesCodec_GoldenTerminal(t *testing.T) {
	c := responses.NewResponsesCodec()
	expected := "event: done\ndata: [DONE]\n\n"
	if string(c.TerminalEvent()) != expected {
		t.Errorf("TerminalEvent = %q, want %q", string(c.TerminalEvent()), expected)
	}
}

func TestClaudeCodec_GoldenDecode(t *testing.T) {
	body := readGolden(t, "anthropic_messages_request.json")
	c := claude.NewMessagesCodec()
	req, err := c.DecodeRequest(body)
	if err != nil {
		t.Fatalf("DecodeRequest error: %v", err)
	}
	if req.Model != "claude-3-opus-20240229" {
		t.Errorf("Model = %q", req.Model)
	}
}

func TestClaudeCodec_GoldenTerminal(t *testing.T) {
	c := claude.NewMessagesCodec()
	expected := "event: message_stop\ndata: {}\n\n"
	if string(c.TerminalEvent()) != expected {
		t.Errorf("TerminalEvent = %q, want %q", string(c.TerminalEvent()), expected)
	}
}

func TestGeminiCodec_GoldenDecode(t *testing.T) {
	body := readGolden(t, "gemini_generate_request.json")
	c := gemini.NewGenerateCodec()
	req, err := c.DecodeRequest(body)
	if err != nil {
		t.Fatalf("DecodeRequest error: %v", err)
	}
	if req.Format != engine.FormatGemini {
		t.Errorf("Format = %v", req.Format)
	}
}

func TestGeminiCodec_NoTerminalSentinel(t *testing.T) {
	c := gemini.NewGenerateCodec()
	if term := c.TerminalEvent(); term != nil {
		t.Errorf("Expected nil terminal for Gemini, got %q", string(term))
	}
}

func TestNativeCodec_Wrap(t *testing.T) {
	body := readGolden(t, "native_passthrough.json")
	c := native.NewPassthroughCodec(engine.FormatOpenAICompat)
	req, err := c.DecodeRequest(body)
	if err != nil {
		t.Fatalf("DecodeRequest error: %v", err)
	}
	if req.Format != engine.FormatOpenAICompat {
		t.Errorf("Format = %v", req.Format)
	}
}

func TestDetectBodyFallback_Golden(t *testing.T) {
	det := formats.NewDetector()
	ctx := context.Background()

	body := readGolden(t, "openai_chat_request.json")
	got, err := det.Detect(ctx, "/unknown/endpoint", body)
	if err != nil {
		t.Fatalf("Detect() error: %v", err)
	}
	if got != engine.FormatOpenAIChat {
		t.Errorf("Detect() = %v, want FormatOpenAIChat", got)
	}

	body = readGolden(t, "anthropic_messages_request_with_version.json")
	got, err = det.Detect(ctx, "/unknown/endpoint", body)
	if err != nil {
		t.Fatalf("Detect() error: %v", err)
	}
	if got != engine.FormatAnthropic {
		t.Errorf("Detect() = %v, want FormatAnthropic", got)
	}
}

func TestOpenAIChatCodec_GoldenStreamFinal(t *testing.T) {
	c := openai.NewChatCodec()
	data := readGolden(t, "openai_chat_stream_final.json")

	chunk, err := c.DecodeStreamChunk("", data)
	if err != nil {
		t.Fatalf("DecodeStreamChunk error: %v", err)
	}
	if !chunk.IsFinal {
		t.Error("final stream chunk should be terminal")
	}
}

func assertJSON(t *testing.T, data []byte) {
	t.Helper()
	if !json.Valid(data) {
		t.Fatalf("invalid JSON: %s", string(data))
	}
}

func TestAllGoldens_ValidJSON(t *testing.T) {
	files := []string{
		"openai_chat_request.json",
		"codex_responses_request.json",
		"anthropic_messages_request.json",
		"anthropic_messages_request_with_version.json",
		"gemini_generate_request.json",
		"native_passthrough.json",
		"openai_chat_stream_chunk.json",
		"openai_chat_stream_final.json",
	}
	for _, f := range files {
		t.Run(f, func(t *testing.T) {
			data := readGolden(t, f)
			assertJSON(t, data)
		})
	}
}

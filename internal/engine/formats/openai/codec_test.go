package openai_test

import (
	"encoding/json"
	"testing"

	"gorouter/internal/domain/engine"
	"gorouter/internal/engine/formats"
	"gorouter/internal/engine/formats/openai"
)

func TestChatCodec_Format(t *testing.T) {
	c := openai.NewChatCodec()
	if got := c.Format(); got != engine.FormatOpenAIChat {
		t.Errorf("Format() = %v, want %v", got, engine.FormatOpenAIChat)
	}
}

func TestChatCodec_DecodeRequest(t *testing.T) {
	c := openai.NewChatCodec()

	t.Run("valid request", func(t *testing.T) {
		body := json.RawMessage(`{
			"model": "gpt-4",
			"messages": [{"role": "user", "content": "hello"}],
			"stream": true,
			"max_tokens": 100
		}`)
		req, err := c.DecodeRequest(body)
		if err != nil {
			t.Fatalf("DecodeRequest unexpected error: %v", err)
		}
		if req.Format != engine.FormatOpenAIChat {
			t.Errorf("Format = %v, want %v", req.Format, engine.FormatOpenAIChat)
		}
		if req.Model != "gpt-4" {
			t.Errorf("Model = %q, want %q", req.Model, "gpt-4")
		}
		if !req.Stream {
			t.Error("Stream should be true")
		}
		if req.MaxTokens != 100 {
			t.Errorf("MaxTokens = %d, want 100", req.MaxTokens)
		}
	})

	t.Run("empty body", func(t *testing.T) {
		_, err := c.DecodeRequest(nil)
		if err == nil {
			t.Fatal("expected error for empty body")
		}
	})

	t.Run("missing model", func(t *testing.T) {
		body := json.RawMessage(`{"messages": [{"role": "user", "content": "hi"}]}`)
		_, err := c.DecodeRequest(body)
		if err == nil {
			t.Fatal("expected error for missing model")
		}
	})

	t.Run("invalid JSON", func(t *testing.T) {
		body := json.RawMessage(`{invalid}`)
		_, err := c.DecodeRequest(body)
		if err == nil {
			t.Fatal("expected error for invalid JSON")
		}
	})
}

func TestChatCodec_TerminalEvent(t *testing.T) {
	c := openai.NewChatCodec()
	expected := []byte("data: [DONE]\n\n")
	if got := c.TerminalEvent(); string(got) != string(expected) {
		t.Errorf("TerminalEvent() = %q, want %q", string(got), string(expected))
	}
}

func TestChatCodec_StreamChunkRoundtrip(t *testing.T) {
	c := openai.NewChatCodec()

	t.Run("content chunk", func(t *testing.T) {
		input := json.RawMessage(`{"choices":[{"delta":{"content":"Hello"},"finish_reason":null}]}`)
		chunk, err := c.DecodeStreamChunk("", input)
		if err != nil {
			t.Fatalf("DecodeStreamChunk error: %v", err)
		}
		if chunk.IsFinal {
			t.Error("chunk should not be final")
		}
		encoded, err := c.EncodeStreamChunk(chunk)
		if err != nil {
			t.Fatalf("EncodeStreamChunk error: %v", err)
		}
		expected := "data: " + string(input) + "\n\n"
		if string(encoded) != expected {
			t.Errorf("roundtrip = %q, want %q", string(encoded), expected)
		}
	})

	t.Run("final chunk with finish_reason", func(t *testing.T) {
		reason := "stop"
		input, _ := json.Marshal(map[string]interface{}{
			"choices": []interface{}{
				map[string]interface{}{
					"delta":         map[string]string{},
					"finish_reason": &reason,
				},
			},
		})
		chunk, err := c.DecodeStreamChunk("", input)
		if err != nil {
			t.Fatalf("DecodeStreamChunk error: %v", err)
		}
		if !chunk.IsFinal {
			t.Error("chunk should be final")
		}
	})

	t.Run("error chunk", func(t *testing.T) {
		input := json.RawMessage(`{"error":{"message":"rate limit","type":"rate_limit_error"}}`)
		chunk, err := c.DecodeStreamChunk("", input)
		if err != nil {
			t.Fatalf("DecodeStreamChunk error: %v", err)
		}
		if chunk.Error == nil {
			t.Fatal("expected error in chunk")
		}
		if !chunk.IsFinal {
			t.Error("error chunk should be final")
		}
	})

	t.Run("empty data", func(t *testing.T) {
		_, err := c.DecodeStreamChunk("", []byte{})
		if err == nil {
			t.Fatal("expected error for empty data")
		}
	})

	t.Run("non-JSON data", func(t *testing.T) {
		_, err := c.DecodeStreamChunk("", []byte("not json"))
		if err == nil {
			t.Fatal("expected error for non-JSON data")
		}
	})
}

func TestChatCodec_EncodeStreamChunk(t *testing.T) {
	c := openai.NewChatCodec()

	t.Run("done event produces terminal", func(t *testing.T) {
		encoded, err := c.EncodeStreamChunk(&formats.Chunk{
			Event:   "done",
			IsFinal: true,
		})
		if err != nil {
			t.Fatalf("EncodeStreamChunk error: %v", err)
		}
		if string(encoded) != string(c.TerminalEvent()) {
			t.Errorf("got %q, want terminal %q", string(encoded), string(c.TerminalEvent()))
		}
	})

	t.Run("isFinal produces terminal", func(t *testing.T) {
		encoded, err := c.EncodeStreamChunk(&formats.Chunk{
			IsFinal: true,
		})
		if err != nil {
			t.Fatalf("EncodeStreamChunk error: %v", err)
		}
		if string(encoded) != string(c.TerminalEvent()) {
			t.Errorf("got %q, want terminal %q", string(encoded), string(c.TerminalEvent()))
		}
	})
}

func TestCompatCodec_Format(t *testing.T) {
	c := openai.NewCompatCodec()
	if got := c.Format(); got != engine.FormatOpenAICompat {
		t.Errorf("Format() = %v, want %v", got, engine.FormatOpenAICompat)
	}
}

func TestCompatCodec_DelegatesToChat(t *testing.T) {
	compat := openai.NewCompatCodec()
	chat := openai.NewChatCodec()

	body := json.RawMessage(`{"model":"gpt-4","messages":[{"role":"user","content":"hi"}]}`)
	reqCompat, err := compat.DecodeRequest(body)
	if err != nil {
		t.Fatalf("Compat DecodeRequest error: %v", err)
	}
	reqChat, _ := chat.DecodeRequest(body)
	if reqCompat.Model != reqChat.Model {
		t.Error("Compat codec should delegate to Chat codec")
	}

	if string(compat.TerminalEvent()) != string(chat.TerminalEvent()) {
		t.Error("Compat terminal should match Chat terminal")
	}
}

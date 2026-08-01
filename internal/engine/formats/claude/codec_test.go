package claude_test

import (
	"encoding/json"
	"testing"

	"gorouter/internal/domain/engine"
	"gorouter/internal/engine/formats"
	"gorouter/internal/engine/formats/claude"
)

func TestMessagesCodec_Format(t *testing.T) {
	c := claude.NewMessagesCodec()
	if got := c.Format(); got != engine.FormatAnthropic {
		t.Errorf("Format() = %v, want %v", got, engine.FormatAnthropic)
	}
}

func TestMessagesCodec_DecodeRequest(t *testing.T) {
	c := claude.NewMessagesCodec()

	t.Run("valid request", func(t *testing.T) {
		body := json.RawMessage(`{"model":"claude-3-opus","messages":[{"role":"user","content":"hi"}],"max_tokens":100}`)
		req, err := c.DecodeRequest(body)
		if err != nil {
			t.Fatalf("DecodeRequest error: %v", err)
		}
		if req.Format != engine.FormatAnthropic {
			t.Errorf("Format = %v", req.Format)
		}
		if req.Model != "claude-3-opus" {
			t.Errorf("Model = %q", req.Model)
		}
		if req.MaxTokens != 100 {
			t.Errorf("MaxTokens = %d", req.MaxTokens)
		}
	})

	t.Run("empty body", func(t *testing.T) {
		_, err := c.DecodeRequest(nil)
		if err == nil {
			t.Fatal("expected error for empty body")
		}
	})

	t.Run("missing model", func(t *testing.T) {
		body := json.RawMessage(`{"messages":[{"role":"user","content":"hi"}]}`)
		_, err := c.DecodeRequest(body)
		if err == nil {
			t.Fatal("expected error for missing model")
		}
	})
}

func TestMessagesCodec_TerminalEvent(t *testing.T) {
	c := claude.NewMessagesCodec()
	expected := []byte("event: message_stop\ndata: {}\n\n")
	if got := c.TerminalEvent(); string(got) != string(expected) {
		t.Errorf("TerminalEvent() = %q, want %q", string(got), string(expected))
	}
}

func TestMessagesCodec_StreamChunks(t *testing.T) {
	c := claude.NewMessagesCodec()

	t.Run("message_stop chunk with type in data", func(t *testing.T) {
		input := json.RawMessage(`{"type":"message_stop"}`)
		chunk, err := c.DecodeStreamChunk("", input)
		if err != nil {
			t.Fatalf("DecodeStreamChunk error: %v", err)
		}
		if !chunk.IsFinal {
			t.Error("message_stop should be final")
		}
	})

	t.Run("message_stop via eventType with empty data", func(t *testing.T) {
		// Real Anthropic SSE sends event: message_stop\ndata: {}
		chunk, err := c.DecodeStreamChunk("message_stop", json.RawMessage(`{}`))
		if err != nil {
			t.Fatalf("DecodeStreamChunk error: %v", err)
		}
		if !chunk.IsFinal {
			t.Error("message_stop via eventType should be final")
		}
		if chunk.Event != "message_stop" {
			t.Errorf("Event = %q, want message_stop", chunk.Event)
		}
	})

	t.Run("message_delta with stop_reason", func(t *testing.T) {
		input := json.RawMessage(`{"type":"message_delta","delta":{"stop_reason":"end_turn","text":""}}`)
		chunk, err := c.DecodeStreamChunk("", input)
		if err != nil {
			t.Fatalf("DecodeStreamChunk error: %v", err)
		}
		if !chunk.IsFinal {
			t.Error("message_delta with stop_reason should be final")
		}
	})

	t.Run("message_delta without stop_reason", func(t *testing.T) {
		input := json.RawMessage(`{"type":"message_delta","delta":{"text":"hello"}}`)
		chunk, err := c.DecodeStreamChunk("", input)
		if err != nil {
			t.Fatalf("DecodeStreamChunk error: %v", err)
		}
		if chunk.IsFinal {
			t.Error("message_delta without stop_reason should not be final")
		}
	})

	t.Run("content_block_delta intermediate", func(t *testing.T) {
		input := json.RawMessage(`{"type":"content_block_delta","index":0,"delta":{"text":"thinking"}}`)
		chunk, err := c.DecodeStreamChunk("", input)
		if err != nil {
			t.Fatalf("DecodeStreamChunk error: %v", err)
		}
		if chunk.IsFinal {
			t.Error("content_block_delta should not be final")
		}
	})

	t.Run("error chunk", func(t *testing.T) {
		input := json.RawMessage(`{"type":"error","error":{"type":"invalid_request_error","message":"bad request"}}`)
		chunk, err := c.DecodeStreamChunk("", input)
		if err != nil {
			t.Fatalf("DecodeStreamChunk error: %v", err)
		}
		if !chunk.IsFinal {
			t.Error("error should be final")
		}
		if chunk.Error == nil {
			t.Fatal("expected error detail")
		}
	})

	t.Run("empty data", func(t *testing.T) {
		_, err := c.DecodeStreamChunk("", []byte{})
		if err == nil {
			t.Fatal("expected error for empty data")
		}
	})
}

func TestMessagesCodec_EncodeStreamChunk(t *testing.T) {
	c := claude.NewMessagesCodec()

	t.Run("message_stop event", func(t *testing.T) {
		encoded, err := c.EncodeStreamChunk(&formats.Chunk{
			Event: "message_stop",
		})
		if err != nil {
			t.Fatalf("EncodeStreamChunk error: %v", err)
		}
		if string(encoded) != string(c.TerminalEvent()) {
			t.Errorf("got %q, want %q", string(encoded), string(c.TerminalEvent()))
		}
	})

	t.Run("content block event", func(t *testing.T) {
		data := json.RawMessage(`{"type":"content_block_delta","index":0,"delta":{"text":"hi"}}`)
		encoded, err := c.EncodeStreamChunk(&formats.Chunk{
			Event: "content_block_delta",
			Data:  data,
		})
		if err != nil {
			t.Fatalf("EncodeStreamChunk error: %v", err)
		}
		expected := "event: content_block_delta\ndata: " + string(data) + "\n\n"
		if string(encoded) != expected {
			t.Errorf("got %q, want %q", string(encoded), expected)
		}
	})
}

package native_test

import (
	"encoding/json"
	"testing"

	"gorouter/internal/domain/engine"
	"gorouter/internal/engine/formats"
	"gorouter/internal/engine/formats/native"
)

func TestPassthroughCodec_Format(t *testing.T) {
	c := native.NewPassthroughCodec(engine.FormatOpenAICompat)
	if got := c.Format(); got != engine.FormatOpenAICompat {
		t.Errorf("Format() = %v, want %v", got, engine.FormatOpenAICompat)
	}
}

func TestPassthroughCodec_DecodeRequest(t *testing.T) {
	c := native.NewPassthroughCodec(engine.FormatOpenAICompat)

	t.Run("valid body", func(t *testing.T) {
		body := json.RawMessage(`{"model":"test","input":"data"}`)
		req, err := c.DecodeRequest(body)
		if err != nil {
			t.Fatalf("DecodeRequest error: %v", err)
		}
		if req.Format != engine.FormatOpenAICompat {
			t.Errorf("Format = %v", req.Format)
		}
		if string(req.RawBody) != string(body) {
			t.Error("RawBody should match input")
		}
	})

	t.Run("empty body", func(t *testing.T) {
		_, err := c.DecodeRequest(nil)
		if err == nil {
			t.Fatal("expected error for empty body")
		}
	})
}

func TestPassthroughCodec_TerminalEvent(t *testing.T) {
	c := native.NewPassthroughCodec(engine.FormatOpenAIChat)
	if got := c.TerminalEvent(); got != nil {
		t.Errorf("TerminalEvent() = %v, want nil", got)
	}
}

func TestPassthroughCodec_StreamChunks(t *testing.T) {
	c := native.NewPassthroughCodec(engine.FormatOpenAICompat)

	t.Run("opaque passthrough chunk", func(t *testing.T) {
		data := []byte(`{"some":"binary payload"}`)
		chunk, err := c.DecodeStreamChunk("", data)
		if err != nil {
			t.Fatalf("DecodeStreamChunk error: %v", err)
		}
		if chunk.Event != "passthrough" {
			t.Errorf("Event = %q, want passthrough", chunk.Event)
		}
		if chunk.IsFinal {
			t.Error("opaque chunk should not be final")
		}
	})

	t.Run("NDJSON done:true terminal", func(t *testing.T) {
		data := []byte(`{"done":true}`)
		chunk, err := c.DecodeStreamChunk("", data)
		if err != nil {
			t.Fatalf("DecodeStreamChunk error: %v", err)
		}
		if !chunk.IsFinal {
			t.Error("done:true should be final")
		}
		if chunk.Event != "ndjson.done" {
			t.Errorf("Event = %q, want ndjson.done", chunk.Event)
		}
	})

	t.Run("NDJSON done:false not terminal", func(t *testing.T) {
		data := []byte(`{"done":false,"response":"partial"}`)
		chunk, err := c.DecodeStreamChunk("", data)
		if err != nil {
			t.Fatalf("DecodeStreamChunk error: %v", err)
		}
		if chunk.IsFinal {
			t.Error("done:false should not be final")
		}
	})

	t.Run("empty data", func(t *testing.T) {
		_, err := c.DecodeStreamChunk("", []byte{})
		if err == nil {
			t.Fatal("expected error for empty data")
		}
	})
}

func TestPassthroughCodec_EncodeStreamChunk(t *testing.T) {
	c := native.NewPassthroughCodec(engine.FormatOpenAICompat)

	t.Run("passthrough chunk", func(t *testing.T) {
		data := []byte(`{"response":"data"}`)
		encoded, err := c.EncodeStreamChunk(&formats.Chunk{
			Event: "passthrough",
			Data:  data,
		})
		if err != nil {
			t.Fatalf("EncodeStreamChunk error: %v", err)
		}
		if string(encoded) != string(data) {
			t.Errorf("got %q, want %q", string(encoded), string(data))
		}
	})

	t.Run("ndjson done", func(t *testing.T) {
		data := []byte(`{"done":true}`)
		encoded, err := c.EncodeStreamChunk(&formats.Chunk{
			Event:   "ndjson.done",
			Data:    data,
			IsFinal: true,
		})
		if err != nil {
			t.Fatalf("EncodeStreamChunk error: %v", err)
		}
		if string(encoded) != string(data) {
			t.Errorf("got %q, want %q", string(encoded), string(data))
		}
	})
}

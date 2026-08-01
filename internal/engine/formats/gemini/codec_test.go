package gemini_test

import (
	"encoding/json"
	"testing"

	"gorouter/internal/domain/engine"
	"gorouter/internal/engine/formats"
	"gorouter/internal/engine/formats/gemini"
)

func TestGenerateCodec_Format(t *testing.T) {
	c := gemini.NewGenerateCodec()
	if got := c.Format(); got != engine.FormatGemini {
		t.Errorf("Format() = %v, want %v", got, engine.FormatGemini)
	}
}

func TestGenerateCodec_DecodeRequest(t *testing.T) {
	c := gemini.NewGenerateCodec()

	t.Run("valid request", func(t *testing.T) {
		body := json.RawMessage(`{"contents":[{"role":"user","parts":[{"text":"hello"}]}],"temperature":0.7}`)
		req, err := c.DecodeRequest(body)
		if err != nil {
			t.Fatalf("DecodeRequest error: %v", err)
		}
		if req.Format != engine.FormatGemini {
			t.Errorf("Format = %v", req.Format)
		}
		if req.Temperature == nil || *req.Temperature != 0.7 {
			t.Errorf("Temperature = %v, want 0.7", req.Temperature)
		}
	})

	t.Run("empty body", func(t *testing.T) {
		_, err := c.DecodeRequest(nil)
		if err == nil {
			t.Fatal("expected error for empty body")
		}
	})

	t.Run("minimal body", func(t *testing.T) {
		body := json.RawMessage(`{"contents":[]}`)
		req, err := c.DecodeRequest(body)
		if err != nil {
			t.Fatalf("DecodeRequest error: %v", err)
		}
		if req.Format != engine.FormatGemini {
			t.Errorf("Format = %v", req.Format)
		}
	})
}

func TestGenerateCodec_TerminalEvent(t *testing.T) {
	c := gemini.NewGenerateCodec()
	if got := c.TerminalEvent(); got != nil {
		t.Errorf("TerminalEvent() = %v, want nil", got)
	}
}

func TestGenerateCodec_StreamChunks(t *testing.T) {
	c := gemini.NewGenerateCodec()

	t.Run("content chunk without finishReason", func(t *testing.T) {
		input := json.RawMessage(`{"candidates":[{"index":0,"content":{"parts":[{"text":"hello"}],"role":"model"}}]}`)
		chunk, err := c.DecodeStreamChunk("", input)
		if err != nil {
			t.Fatalf("DecodeStreamChunk error: %v", err)
		}
		if chunk.IsFinal {
			t.Error("chunk without finishReason should not be final")
		}
	})

	t.Run("final chunk with finishReason", func(t *testing.T) {
		reason := "STOP"
		input, _ := json.Marshal(map[string]interface{}{
			"candidates": []interface{}{
				map[string]interface{}{
					"index":        0,
					"finishReason": &reason,
					"content":      map[string]interface{}{"parts": []interface{}{}, "role": "model"},
				},
			},
		})
		chunk, err := c.DecodeStreamChunk("", input)
		if err != nil {
			t.Fatalf("DecodeStreamChunk error: %v", err)
		}
		if !chunk.IsFinal {
			t.Error("chunk with finishReason should be final")
		}
		if chunk.Event != "candidate.finished" {
			t.Errorf("Event = %q, want candidate.finished", chunk.Event)
		}
	})

	t.Run("error chunk", func(t *testing.T) {
		input := json.RawMessage(`{"error":{"code":400,"message":"invalid request","status":"INVALID_ARGUMENT"}}`)
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
		if chunk.Event != "error" {
			t.Errorf("Event = %q, want error", chunk.Event)
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

func TestGenerateCodec_EncodeStreamChunk(t *testing.T) {
	c := gemini.NewGenerateCodec()

	t.Run("content chunk", func(t *testing.T) {
		data := json.RawMessage(`{"candidates":[{"index":0,"content":{"parts":[{"text":"hi"}]}}]}`)
		encoded, err := c.EncodeStreamChunk(&formats.Chunk{
			Event: "chunk",
			Data:  data,
		})
		if err != nil {
			t.Fatalf("EncodeStreamChunk error: %v", err)
		}
		expected := string(data) + "\n"
		if string(encoded) != expected {
			t.Errorf("got %q, want %q", string(encoded), expected)
		}
	})

	t.Run("candidate.finished event", func(t *testing.T) {
		data := json.RawMessage(`{"candidates":[{"finishReason":"STOP"}]}`)
		encoded, err := c.EncodeStreamChunk(&formats.Chunk{
			Event:   "candidate.finished",
			Data:    data,
			IsFinal: true,
		})
		if err != nil {
			t.Fatalf("EncodeStreamChunk error: %v", err)
		}
		expected := string(data) + "\n"
		if string(encoded) != expected {
			t.Errorf("got %q, want %q", string(encoded), expected)
		}
	})
}

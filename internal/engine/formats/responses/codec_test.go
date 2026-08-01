package responses_test

import (
	"encoding/json"
	"testing"

	"gorouter/internal/domain/engine"
	"gorouter/internal/engine/formats"
	"gorouter/internal/engine/formats/responses"
)

func TestResponsesCodec_Format(t *testing.T) {
	c := responses.NewResponsesCodec()
	if got := c.Format(); got != engine.FormatCodexResponses {
		t.Errorf("Format() = %v, want %v", got, engine.FormatCodexResponses)
	}
}

func TestResponsesCodec_DecodeRequest(t *testing.T) {
	c := responses.NewResponsesCodec()

	t.Run("valid request", func(t *testing.T) {
		body := json.RawMessage(`{"model":"gpt-4","input":"hello","instructions":"be helpful"}`)
		req, err := c.DecodeRequest(body)
		if err != nil {
			t.Fatalf("DecodeRequest error: %v", err)
		}
		if req.Format != engine.FormatCodexResponses {
			t.Errorf("Format = %v", req.Format)
		}
		if req.Model != "gpt-4" {
			t.Errorf("Model = %q", req.Model)
		}
	})

	t.Run("empty body", func(t *testing.T) {
		_, err := c.DecodeRequest(nil)
		if err == nil {
			t.Fatal("expected error for empty body")
		}
	})

	t.Run("missing model", func(t *testing.T) {
		body := json.RawMessage(`{"input":"hello"}`)
		_, err := c.DecodeRequest(body)
		if err == nil {
			t.Fatal("expected error for missing model")
		}
	})
}

func TestResponsesCodec_TerminalEvent(t *testing.T) {
	c := responses.NewResponsesCodec()
	expected := []byte("event: done\ndata: [DONE]\n\n")
	if got := c.TerminalEvent(); string(got) != string(expected) {
		t.Errorf("TerminalEvent() = %q, want %q", string(got), string(expected))
	}
}

func TestResponsesCodec_StreamChunks(t *testing.T) {
	c := responses.NewResponsesCodec()

	t.Run("response.completed chunk", func(t *testing.T) {
		input := json.RawMessage(`{"type":"response.completed","data":{"id":"resp_123"}}`)
		chunk, err := c.DecodeStreamChunk("", input)
		if err != nil {
			t.Fatalf("DecodeStreamChunk error: %v", err)
		}
		if !chunk.IsFinal {
			t.Error("response.completed should be final")
		}
		if chunk.Event != "response.completed" {
			t.Errorf("Event = %q", chunk.Event)
		}
	})

	t.Run("response.failed chunk", func(t *testing.T) {
		input := json.RawMessage(`{"type":"response.failed","error":{"message":"bad request","code":"invalid"}}`)
		chunk, err := c.DecodeStreamChunk("", input)
		if err != nil {
			t.Fatalf("DecodeStreamChunk error: %v", err)
		}
		if !chunk.IsFinal {
			t.Error("response.failed should be final")
		}
		if chunk.Error == nil {
			t.Fatal("expected error for failed chunk")
		}
	})

	t.Run("done chunk", func(t *testing.T) {
		input := json.RawMessage(`{"type":"done","data":"[DONE]"}`)
		chunk, err := c.DecodeStreamChunk("", input)
		if err != nil {
			t.Fatalf("DecodeStreamChunk error: %v", err)
		}
		if !chunk.IsFinal {
			t.Error("done should be final")
		}
	})

	t.Run("intermediate output_item.added chunk", func(t *testing.T) {
		input := json.RawMessage(`{"type":"response.output_item.added","data":{"id":"item_1"}}`)
		chunk, err := c.DecodeStreamChunk("", input)
		if err != nil {
			t.Fatalf("DecodeStreamChunk error: %v", err)
		}
		if chunk.IsFinal {
			t.Error("intermediate chunk should not be final")
		}
	})

	t.Run("output_item.done is NOT final (A3 gate: fixed)", func(t *testing.T) {
		input := json.RawMessage(`{"type":"response.output_item.done","data":{"id":"item_1"}}`)
		chunk, err := c.DecodeStreamChunk("", input)
		if err != nil {
			t.Fatalf("DecodeStreamChunk error: %v", err)
		}
		if chunk.IsFinal {
			t.Error("response.output_item.done should NOT be final — it is an intermediate event")
		}
	})

	t.Run("error chunk", func(t *testing.T) {
		input := json.RawMessage(`{"type":"error","error":{"message":"rate limited","code":"rate_limit"}}`)
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

	t.Run("non-JSON data", func(t *testing.T) {
		_, err := c.DecodeStreamChunk("", []byte("not json"))
		if err == nil {
			t.Fatal("expected error for non-JSON data")
		}
	})
}

func TestResponsesCodec_EncodeStreamChunk(t *testing.T) {
	c := responses.NewResponsesCodec()

	t.Run("done event", func(t *testing.T) {
		encoded, err := c.EncodeStreamChunk(&formats.Chunk{
			Event: "done",
			Data:  json.RawMessage(`"[DONE]"`),
		})
		if err != nil {
			t.Fatalf("EncodeStreamChunk error: %v", err)
		}
		if string(encoded) != string(c.TerminalEvent()) {
			t.Errorf("got %q, want %q", string(encoded), string(c.TerminalEvent()))
		}
	})

	t.Run("response.completed event", func(t *testing.T) {
		data := json.RawMessage(`{"id":"resp_1"}`)
		encoded, err := c.EncodeStreamChunk(&formats.Chunk{
			Event: "response.completed",
			Data:  data,
		})
		if err != nil {
			t.Fatalf("EncodeStreamChunk error: %v", err)
		}
		expected := "event: response.completed\ndata: " + string(data) + "\n\n"
		if string(encoded) != expected {
			t.Errorf("got %q, want %q", string(encoded), expected)
		}
	})
}

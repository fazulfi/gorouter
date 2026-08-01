package translators

import (
	"context"
	"encoding/json"
	"testing"

	"gorouter/internal/domain/engine"
	"gorouter/internal/engine/formats"
	_ "gorouter/internal/engine/formats/claude"
	_ "gorouter/internal/engine/formats/gemini"
	_ "gorouter/internal/engine/formats/native"
	_ "gorouter/internal/engine/formats/openai"
	_ "gorouter/internal/engine/formats/responses"
)

// allFormats lists every format constant the registry must support.
func TestRegistryCoverage(t *testing.T) {
	reg := NewRegistry()

	// Verify that every source→target pair resolves to a translator.
	for _, src := range allFormats {
		for _, dst := range allFormats {
			t.Run(string(src)+"_to_"+string(dst), func(t *testing.T) {
				tr, err := reg.GetTranslator(src, dst)
				if err != nil {
					t.Fatalf("GetTranslator(%q, %q): %v", src, dst, err)
				}
				if tr == nil {
					t.Fatal("GetTranslator returned nil translator")
				}
				if tr.Source() != src {
					t.Errorf("translator.Source() = %q, want %q", tr.Source(), src)
				}
				if tr.Target() != dst {
					t.Errorf("translator.Target() = %q, want %q", tr.Target(), dst)
				}
			})
		}
	}
}

func TestSupportsPair(t *testing.T) {
	reg := NewRegistry()

	for _, src := range allFormats {
		for _, dst := range allFormats {
			if !reg.SupportsPair(src, dst) {
				t.Errorf("SupportsPair(%q, %q) = false, want true", src, dst)
			}
		}
	}
}

func TestNoopPassthrough(t *testing.T) {
	reg := NewRegistry()
	ctx := context.Background()

	for _, fmt := range allFormats {
		t.Run(string(fmt), func(t *testing.T) {
			tr, err := reg.GetTranslator(fmt, fmt)
			if err != nil {
				t.Fatalf("GetTranslator(%q, %q): %v", fmt, fmt, err)
			}

			// Request passthrough.
			req := &engine.Request{
				ID:         mustParseUUID(t, "00000000-0000-0000-0000-000000000001"),
				Format:     fmt,
				Model:      "test-model",
				RawBody:    json.RawMessage(`{"test":true}`),
				MappedBody: json.RawMessage(`{"test":true}`),
			}
			outReq, err := tr.TranslateRequest(ctx, req)
			if err != nil {
				t.Fatalf("TranslateRequest: %v", err)
			}
			if outReq != req {
				t.Error("expected pointer equality for noop request")
			}

			// Response passthrough.
			resp := &engine.Response{
				RequestID: mustParseUUID(t, "00000000-0000-0000-0000-000000000001"),
				Body:      json.RawMessage(`{"result":"ok"}`),
			}
			outResp, err := tr.TranslateResponse(ctx, resp)
			if err != nil {
				t.Fatalf("TranslateResponse: %v", err)
			}
			if outResp != resp {
				t.Error("expected pointer equality for noop response")
			}

			// Stream chunk passthrough.
			chunk := &formats.Chunk{
				Event:   "chunk",
				Data:    json.RawMessage(`{"content":"hello"}`),
				IsFinal: false,
			}
			outChunk, err := tr.TranslateStreamChunk(ctx, chunk)
			if err != nil {
				t.Fatalf("TranslateStreamChunk: %v", err)
			}
			if outChunk != chunk {
				t.Error("expected pointer equality for noop stream chunk")
			}
		})
	}
}

func TestNilRequestError(t *testing.T) {
	reg := NewRegistry()
	ctx := context.Background()

	tr, err := reg.GetTranslator(engine.FormatOpenAIChat, engine.FormatAnthropic)
	if err != nil {
		t.Fatalf("GetTranslator: %v", err)
	}

	_, err = tr.TranslateRequest(ctx, nil)
	if err == nil {
		t.Fatal("expected error for nil request, got nil")
	}

	_, err = tr.TranslateResponse(ctx, nil)
	if err == nil {
		t.Fatal("expected error for nil response, got nil")
	}

	_, err = tr.TranslateStreamChunk(ctx, nil)
	if err == nil {
		t.Fatal("expected error for nil chunk, got nil")
	}
}

func TestRegistryRegister(t *testing.T) {
	reg := NewRegistry()

	// Register a custom translator.
	ct := &countingTranslator{
		src: engine.FormatOpenAIChat,
		dst: engine.FormatGemini,
	}
	reg.Register(ct)

	// It should now be findable.
	tr, err := reg.GetTranslator(engine.FormatOpenAIChat, engine.FormatGemini)
	if err != nil {
		t.Fatalf("GetTranslator: %v", err)
	}
	if tr != ct {
		t.Error("expected custom translator to be returned")
	}
}

// countingTranslator is a test double that tracks calls.
type countingTranslator struct {
	src, dst   engine.RequestFormat
	reqCount   int
	respCount  int
	chunkCount int
}

func (c *countingTranslator) Source() engine.RequestFormat { return c.src }
func (c *countingTranslator) Target() engine.RequestFormat { return c.dst }
func (c *countingTranslator) TranslateRequest(_ context.Context, req *engine.Request) (*engine.Request, error) {
	c.reqCount++
	return req, nil
}
func (c *countingTranslator) TranslateResponse(_ context.Context, resp *engine.Response) (*engine.Response, error) {
	c.respCount++
	return resp, nil
}
func (c *countingTranslator) TranslateStreamChunk(_ context.Context, chunk *formats.Chunk) (*formats.Chunk, error) {
	c.chunkCount++
	return chunk, nil
}

func mustParseUUID(t *testing.T, s string) [16]byte {
	t.Helper()
	var id [16]byte
	copy(id[:], []byte(s))
	return id
}

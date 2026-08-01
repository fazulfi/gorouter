package translators

import (
	"testing"

	"gorouter/internal/domain/engine"
	"gorouter/internal/engine/formats"
)

func FuzzDirectOpenAIToClaudeRequest(f *testing.F) {
	reg := NewRegistry()
	tr, err := reg.GetTranslator(engine.FormatOpenAIChat, engine.FormatAnthropic)
	if err != nil {
		f.Fatalf("GetTranslator: %v", err)
	}
	f.Add([]byte(`{"model":"gpt-4","messages":[{"role":"user","content":"hi"}]}`))
	f.Add([]byte(`{}`))
	f.Add([]byte(`invalid`))
	f.Fuzz(func(t *testing.T, body []byte) {
		req := &engine.Request{
			Format:     engine.FormatOpenAIChat,
			RawBody:    body,
			MappedBody: body,
		}
		_, _ = tr.TranslateRequest(nil, req)
	})
}

func FuzzDirectClaudeToOpenAIRequest(f *testing.F) {
	reg := NewRegistry()
	tr, err := reg.GetTranslator(engine.FormatAnthropic, engine.FormatOpenAIChat)
	if err != nil {
		f.Fatalf("GetTranslator: %v", err)
	}
	f.Add([]byte(`{"model":"claude","messages":[{"role":"user","content":"hi"}]}`))
	f.Add([]byte(`{}`))
	f.Fuzz(func(t *testing.T, body []byte) {
		req := &engine.Request{
			Format:     engine.FormatAnthropic,
			RawBody:    body,
			MappedBody: body,
		}
		_, _ = tr.TranslateRequest(nil, req)
	})
}

func FuzzDirectOpenAIToGeminiRequest(f *testing.F) {
	reg := NewRegistry()
	tr, err := reg.GetTranslator(engine.FormatOpenAIChat, engine.FormatGemini)
	if err != nil {
		f.Fatalf("GetTranslator: %v", err)
	}
	f.Add([]byte(`{"model":"gpt-4","contents":[],"messages":[{"role":"user","content":"hi"}]}`))
	f.Add([]byte(`{}`))
	f.Fuzz(func(t *testing.T, body []byte) {
		req := &engine.Request{
			Format:     engine.FormatOpenAIChat,
			RawBody:    body,
			MappedBody: body,
		}
		_, _ = tr.TranslateRequest(nil, req)
	})
}

func FuzzDirectGeminiToOpenAIRequest(f *testing.F) {
	reg := NewRegistry()
	tr, err := reg.GetTranslator(engine.FormatGemini, engine.FormatOpenAIChat)
	if err != nil {
		f.Fatalf("GetTranslator: %v", err)
	}
	f.Add([]byte(`{"contents":[{"role":"user","parts":[{"text":"hi"}]}]}`))
	f.Add([]byte(`{}`))
	f.Fuzz(func(t *testing.T, body []byte) {
		req := &engine.Request{
			Format:     engine.FormatGemini,
			RawBody:    body,
			MappedBody: body,
		}
		_, _ = tr.TranslateRequest(nil, req)
	})
}

func FuzzDirectCodexToOpenAIRequest(f *testing.F) {
	reg := NewRegistry()
	tr, err := reg.GetTranslator(engine.FormatCodexResponses, engine.FormatOpenAIChat)
	if err != nil {
		f.Fatalf("GetTranslator: %v", err)
	}
	f.Add([]byte(`{"model":"gpt-4o","input":"hello"}`))
	f.Add([]byte(`{}`))
	f.Fuzz(func(t *testing.T, body []byte) {
		req := &engine.Request{
			Format:     engine.FormatCodexResponses,
			RawBody:    body,
			MappedBody: body,
		}
		_, _ = tr.TranslateRequest(nil, req)
	})
}

func FuzzDirectOpenAIToClaudeResponse(f *testing.F) {
	reg := NewRegistry()
	tr, _ := reg.GetTranslator(engine.FormatOpenAIChat, engine.FormatAnthropic)
	f.Add([]byte(`{"id":"msg_1","type":"message","role":"assistant","content":[{"type":"text","text":"hi"}],"stop_reason":"end_turn"}`))
	f.Fuzz(func(t *testing.T, body []byte) {
		resp := &engine.Response{Body: body}
		_, _ = tr.TranslateResponse(nil, resp)
	})
}

func FuzzDirectClaudeToOpenAIResponse(f *testing.F) {
	reg := NewRegistry()
	tr, _ := reg.GetTranslator(engine.FormatAnthropic, engine.FormatOpenAIChat)
	f.Add([]byte(`{"id":"chatcmpl-1","object":"chat.completion","choices":[{"index":0,"message":{"role":"assistant","content":"hi"},"finish_reason":"stop"}]}`))
	f.Fuzz(func(t *testing.T, body []byte) {
		resp := &engine.Response{Body: body}
		_, _ = tr.TranslateResponse(nil, resp)
	})
}

func FuzzDirectOpenAIToGeminiResponse(f *testing.F) {
	reg := NewRegistry()
	tr, _ := reg.GetTranslator(engine.FormatOpenAIChat, engine.FormatGemini)
	f.Add([]byte(`{"candidates":[{"content":{"parts":[{"text":"hi"}],"role":"model"},"finishReason":"STOP"}]}`))
	f.Fuzz(func(t *testing.T, body []byte) {
		resp := &engine.Response{Body: body}
		_, _ = tr.TranslateResponse(nil, resp)
	})
}

func FuzzDirectGeminiToOpenAIResponse(f *testing.F) {
	reg := NewRegistry()
	tr, _ := reg.GetTranslator(engine.FormatGemini, engine.FormatOpenAIChat)
	f.Add([]byte(`{"id":"chatcmpl-1","object":"chat.completion","choices":[{"index":0,"message":{"role":"assistant","content":"hi"},"finish_reason":"stop"}]}`))
	f.Fuzz(func(t *testing.T, body []byte) {
		resp := &engine.Response{Body: body}
		_, _ = tr.TranslateResponse(nil, resp)
	})
}

// ---- Stream chunk fuzz targets ----

func FuzzDirectOpenAIToClaudeStream(f *testing.F) {
	reg := NewRegistry()
	tr, _ := reg.GetTranslator(engine.FormatOpenAIChat, engine.FormatAnthropic)
	f.Add([]byte(`{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Hello"}}`))
	f.Add([]byte(`{"type":"message_delta","delta":{"stop_reason":"end_turn"}}`))
	f.Add([]byte(`invalid`))
	f.Add([]byte(`{}`))
	f.Fuzz(func(t *testing.T, data []byte) {
		chunk := &formats.Chunk{Event: "chunk", Data: data, IsFinal: false}
		_, _ = tr.TranslateStreamChunk(nil, chunk)
	})
}

func FuzzDirectClaudeToOpenAIStream(f *testing.F) {
	reg := NewRegistry()
	tr, _ := reg.GetTranslator(engine.FormatAnthropic, engine.FormatOpenAIChat)
	f.Add([]byte(`{"choices":[{"index":0,"delta":{"content":"Hello"}}]}`))
	f.Add([]byte(`{"choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}`))
	f.Add([]byte(`invalid`))
	f.Add([]byte(`{}`))
	f.Fuzz(func(t *testing.T, data []byte) {
		chunk := &formats.Chunk{Event: "chunk", Data: data, IsFinal: false}
		_, _ = tr.TranslateStreamChunk(nil, chunk)
	})
}

func FuzzDirectOpenAIToGeminiStream(f *testing.F) {
	reg := NewRegistry()
	tr, _ := reg.GetTranslator(engine.FormatOpenAIChat, engine.FormatGemini)
	f.Add([]byte(`{"candidates":[{"content":{"parts":[{"text":"hi"}],"role":"model"},"finishReason":"STOP"}]}`))
	f.Add([]byte(`{"candidates":[{"content":{"parts":[{"text":"hi"}],"role":"model"}}]}`))
	f.Add([]byte(`invalid`))
	f.Add([]byte(`{}`))
	f.Fuzz(func(t *testing.T, data []byte) {
		chunk := &formats.Chunk{Event: "chunk", Data: data, IsFinal: false}
		_, _ = tr.TranslateStreamChunk(nil, chunk)
	})
}

func FuzzDirectGeminiToOpenAIStream(f *testing.F) {
	reg := NewRegistry()
	tr, _ := reg.GetTranslator(engine.FormatGemini, engine.FormatOpenAIChat)
	f.Add([]byte(`{"choices":[{"index":0,"delta":{"content":"Hello"}}]}`))
	f.Add([]byte(`[DONE]`))
	f.Add([]byte(`invalid`))
	f.Add([]byte(`{}`))
	f.Fuzz(func(t *testing.T, data []byte) {
		chunk := &formats.Chunk{Event: "chunk", Data: data, IsFinal: false}
		_, _ = tr.TranslateStreamChunk(nil, chunk)
	})
}

func FuzzDirectCodexToOpenAIStream(f *testing.F) {
	reg := NewRegistry()
	tr, _ := reg.GetTranslator(engine.FormatCodexResponses, engine.FormatOpenAIChat)
	f.Add([]byte(`{"choices":[{"index":0,"delta":{"content":"Hello"}}]}`))
	f.Add([]byte(`[DONE]`))
	f.Add([]byte(`invalid`))
	f.Add([]byte(`{}`))
	f.Fuzz(func(t *testing.T, data []byte) {
		chunk := &formats.Chunk{Event: "chunk", Data: data, IsFinal: false}
		_, _ = tr.TranslateStreamChunk(nil, chunk)
	})
}

// ---- Codex response fuzz target ----

func FuzzDirectCodexToOpenAIResponse(f *testing.F) {
	reg := NewRegistry()
	tr, _ := reg.GetTranslator(engine.FormatCodexResponses, engine.FormatOpenAIChat)
	f.Add([]byte(`{"id":"resp_1","object":"response","model":"gpt-4o","output":[{"role":"assistant","content":"Hello"}]}`))
	f.Add([]byte(`{"choices":[{"index":0,"message":{"role":"assistant","content":"hi"}}]}`))
	f.Add([]byte(`invalid`))
	f.Add([]byte(`{}`))
	f.Fuzz(func(t *testing.T, body []byte) {
		resp := &engine.Response{Body: body}
		_, _ = tr.TranslateResponse(nil, resp)
	})
}

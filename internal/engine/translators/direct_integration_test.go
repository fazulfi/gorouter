package translators

import (
	"context"
	"encoding/json"
	"testing"

	"gorouter/internal/domain/engine"
	"gorouter/internal/engine/formats"
)

func uuidBytes(s string) [16]byte {
	var id [16]byte
	copy(id[:], []byte(s))
	return id
}

func openAIChatRequest(body json.RawMessage) *engine.Request {
	return &engine.Request{
		ID:         uuidBytes("openai-chat-req-1"),
		Format:     engine.FormatOpenAIChat,
		Model:      "gpt-4",
		RawBody:    body,
		MappedBody: body,
	}
}

func claudeRequest(body json.RawMessage) *engine.Request {
	return &engine.Request{
		ID:         uuidBytes("claude-req-001"),
		Format:     engine.FormatAnthropic,
		Model:      "claude-3-opus-20240229",
		RawBody:    body,
		MappedBody: body,
	}
}

func geminiRequest(body json.RawMessage) *engine.Request {
	return &engine.Request{
		ID:         uuidBytes("gemini-req-001"),
		Format:     engine.FormatGemini,
		Model:      "gemini-1.5-pro",
		RawBody:    body,
		MappedBody: body,
	}
}

func codexRequest(body json.RawMessage) *engine.Request {
	return &engine.Request{
		ID:         uuidBytes("codex-req-001"),
		Format:     engine.FormatCodexResponses,
		Model:      "gpt-4o",
		RawBody:    body,
		MappedBody: body,
	}
}

func TestOpenAIClaudeRoundTrip(t *testing.T) {
	ctx := context.Background()
	reg := NewRegistry()

	o2c, err := reg.GetTranslator(engine.FormatOpenAIChat, engine.FormatAnthropic)
	if err != nil {
		t.Fatalf("GetTranslator(openai->claude): %v", err)
	}
	c2o, err := reg.GetTranslator(engine.FormatAnthropic, engine.FormatOpenAIChat)
	if err != nil {
		t.Fatalf("GetTranslator(claude->openai): %v", err)
	}

	original := openAIChatRequest(json.RawMessage(`{
		"model": "gpt-4",
		"messages": [
			{"role": "system", "content": "You are helpful."},
			{"role": "user", "content": "Hello!"},
			{"role": "assistant", "content": "Hi there!"}
		],
		"max_tokens": 1024,
		"temperature": 0.7
	}`))

	claude, err := o2c.TranslateRequest(ctx, original)
	if err != nil {
		t.Fatalf("openai->claude TranslateRequest: %v", err)
	}
	if claude.Format != engine.FormatAnthropic {
		t.Errorf("expected FormatAnthropic, got %q", claude.Format)
	}

	back, err := c2o.TranslateRequest(ctx, claude)
	if err != nil {
		t.Fatalf("claude->openai TranslateRequest: %v", err)
	}
	if back.Format != engine.FormatOpenAIChat {
		t.Errorf("expected FormatOpenAIChat, got %q", back.Format)
	}

	var origObj, backObj map[string]json.RawMessage
	if err := json.Unmarshal(original.RawBody, &origObj); err != nil {
		t.Fatalf("unmarshal original: %v", err)
	}
	if err := json.Unmarshal(back.RawBody, &backObj); err != nil {
		t.Fatalf("unmarshal back: %v", err)
	}

	var origMsgs, backMsgs []json.RawMessage
	json.Unmarshal(origObj["messages"], &origMsgs)
	json.Unmarshal(backObj["messages"], &backMsgs)
	if len(origMsgs) != len(backMsgs) {
		t.Errorf("message count mismatch: %d vs %d", len(origMsgs), len(backMsgs))
	}
}

func TestClaudeOpenAIRoundTrip(t *testing.T) {
	ctx := context.Background()
	reg := NewRegistry()

	c2o, err := reg.GetTranslator(engine.FormatAnthropic, engine.FormatOpenAIChat)
	if err != nil {
		t.Fatalf("GetTranslator(claude->openai): %v", err)
	}

	original := claudeRequest(json.RawMessage(`{
		"model": "claude-3-opus-20240229",
		"max_tokens": 1024,
		"messages": [
			{"role": "user", "content": "Tell me about AI."}
		]
	}`))

	openai, err := c2o.TranslateRequest(ctx, original)
	if err != nil {
		t.Fatalf("claude->openai TranslateRequest: %v", err)
	}
	if openai.Format != engine.FormatOpenAIChat {
		t.Errorf("expected FormatOpenAIChat, got %q", openai.Format)
	}

	var obj map[string]json.RawMessage
	json.Unmarshal(openai.RawBody, &obj)
	var msgs []json.RawMessage
	json.Unmarshal(obj["messages"], &msgs)
	if len(msgs) == 0 {
		t.Error("no messages in translated request")
	}
}

func TestOpenAIGeminiRoundTrip(t *testing.T) {
	ctx := context.Background()
	reg := NewRegistry()

	o2g, err := reg.GetTranslator(engine.FormatOpenAIChat, engine.FormatGemini)
	if err != nil {
		t.Fatalf("GetTranslator(openai->gemini): %v", err)
	}
	g2o, err := reg.GetTranslator(engine.FormatGemini, engine.FormatOpenAIChat)
	if err != nil {
		t.Fatalf("GetTranslator(gemini->openai): %v", err)
	}

	original := openAIChatRequest(json.RawMessage(`{
		"model": "gpt-4",
		"messages": [
			{"role": "system", "content": "You are helpful."},
			{"role": "user", "content": "What is the weather?"}
		],
		"temperature": 0.5
	}`))

	gemini, err := o2g.TranslateRequest(ctx, original)
	if err != nil {
		t.Fatalf("openai->gemini TranslateRequest: %v", err)
	}
	if gemini.Format != engine.FormatGemini {
		t.Errorf("expected FormatGemini, got %q", gemini.Format)
	}

	back, err := g2o.TranslateRequest(ctx, gemini)
	if err != nil {
		t.Fatalf("gemini->openai TranslateRequest: %v", err)
	}
	if back.Format != engine.FormatOpenAIChat {
		t.Errorf("expected FormatOpenAIChat, got %q", back.Format)
	}

	var backObj map[string]json.RawMessage
	json.Unmarshal(back.RawBody, &backObj)
	var msgs []json.RawMessage
	json.Unmarshal(backObj["messages"], &msgs)
	if len(msgs) == 0 {
		t.Error("no messages in round-tripped request")
	}
}

func TestCodexToOpenAI(t *testing.T) {
	ctx := context.Background()
	reg := NewRegistry()

	tr, err := reg.GetTranslator(engine.FormatCodexResponses, engine.FormatOpenAIChat)
	if err != nil {
		t.Fatalf("GetTranslator(codex->openai): %v", err)
	}

	original := codexRequest(json.RawMessage(`{
		"model": "gpt-4o",
		"input": "Hello, world!",
		"instructions": "Be concise"
	}`))

	result, err := tr.TranslateRequest(ctx, original)
	if err != nil {
		t.Fatalf("codex->openai TranslateRequest: %v", err)
	}
	if result.Format != engine.FormatOpenAIChat {
		t.Errorf("expected FormatOpenAIChat, got %q", result.Format)
	}

	var obj map[string]json.RawMessage
	json.Unmarshal(result.RawBody, &obj)
	var msgs []json.RawMessage
	json.Unmarshal(obj["messages"], &msgs)
	if len(msgs) == 0 {
		t.Error("no messages in translated request")
	}
}

func TestStreamChunkTranslation(t *testing.T) {
	ctx := context.Background()
	reg := NewRegistry()

	o2c, err := reg.GetTranslator(engine.FormatOpenAIChat, engine.FormatAnthropic)
	if err != nil {
		t.Fatalf("GetTranslator(openai->claude): %v", err)
	}

	chunk := &formats.Chunk{
		Event:   "chunk",
		Data:    json.RawMessage(`{"choices":[{"delta":{"content":"Hello"}}]}`),
		IsFinal: false,
	}

	result, err := o2c.TranslateStreamChunk(ctx, chunk)
	if err != nil {
		t.Fatalf("TranslateStreamChunk: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil chunk")
	}

	c2o, err := reg.GetTranslator(engine.FormatAnthropic, engine.FormatOpenAIChat)
	if err != nil {
		t.Fatalf("GetTranslator(claude->openai): %v", err)
	}

	claudeChunk := &formats.Chunk{
		Event:   "content_block_delta",
		Data:    json.RawMessage(`{"type":"content_block_delta","delta":{"type":"text_delta","text":"Hello"}}`),
		IsFinal: false,
	}

	openaiChunk, err := c2o.TranslateStreamChunk(ctx, claudeChunk)
	if err != nil {
		t.Fatalf("claude->openai TranslateStreamChunk: %v", err)
	}
	if openaiChunk == nil {
		t.Fatal("expected non-nil chunk")
	}
}

func TestUnknownFormatPair(t *testing.T) {
	reg := NewRegistry()

	_, err := reg.GetTranslator(
		engine.RequestFormat("nonexistent_a"),
		engine.RequestFormat("nonexistent_b"),
	)
	if err == nil {
		t.Fatal("expected error for unknown format pair, got nil")
	}
}

func TestResponseTranslation(t *testing.T) {
	ctx := context.Background()
	reg := NewRegistry()

	c2o, err := reg.GetTranslator(engine.FormatAnthropic, engine.FormatOpenAIChat)
	if err != nil {
		t.Fatalf("GetTranslator(claude->openai): %v", err)
	}

	claudeResp := &engine.Response{
		RequestID: uuidBytes("resp-0001"),
		Body: json.RawMessage(`{
			"id": "msg_123",
			"type": "message",
			"role": "assistant",
			"content": [{"type":"text","text":"Hello!"}],
			"model": "claude-3-opus-20240229",
			"stop_reason": "end_turn"
		}`),
		Model: "claude-3-opus-20240229",
	}

	openaiResp, err := c2o.TranslateResponse(ctx, claudeResp)
	if err != nil {
		t.Fatalf("claude->openai TranslateResponse: %v", err)
	}
	if openaiResp == nil {
		t.Fatal("expected non-nil response")
	}
}

package safeir

import (
	"encoding/json"
	"errors"
	"testing"

	"gorouter/internal/domain/engine"
	"gorouter/internal/engine/formats"
)

func hasLossy(err error) bool {
	if err == nil {
		return false
	}
	return errors.Is(err, ErrLossyTranslation)
}

func TestToSafeIROpenAI(t *testing.T) {
	body := json.RawMessage(`{
		"model": "gpt-4",
		"messages": [
			{"role": "system", "content": "You are helpful."},
			{"role": "user", "content": "Hello!"}
		],
		"temperature": 0.7,
		"max_tokens": 1024,
		"top_p": 0.9,
		"stop": ["\n", "."]
	}`)

	ir, err := ToSafeIR(body, engine.FormatOpenAIChat)
	if err != nil {
		t.Fatalf("ToSafeIR(openai): %v", err)
	}
	if ir == nil {
		t.Fatal("expected non-nil SafeIR")
	}
	if len(ir.Messages) != 2 {
		t.Errorf("expected 2 messages, got %d", len(ir.Messages))
	}
	if ir.Messages[0].Role != "system" {
		t.Errorf("expected role system, got %q", ir.Messages[0].Role)
	}
	if ir.Temperature == nil || *ir.Temperature != 0.7 {
		t.Errorf("expected temperature 0.7, got %v", ir.Temperature)
	}
}

func TestFromSafeIRToOpenAI(t *testing.T) {
	temperature := 0.5
	ir := &SafeIR{
		Messages: []Message{
			{Role: "user", Text: "Hello!"},
			{Role: "assistant", Text: "Hi there!"},
		},
		Temperature: &temperature,
		MaxTokens:   512,
	}

	body, err := FromSafeIR(ir, engine.FormatOpenAIChat)
	if err != nil {
		t.Fatalf("FromSafeIR(openai): %v", err)
	}
	if len(body) == 0 {
		t.Fatal("expected non-empty body")
	}

	var obj map[string]json.RawMessage
	if err := json.Unmarshal(body, &obj); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	var msgs []json.RawMessage
	if err := json.Unmarshal(obj["messages"], &msgs); err != nil {
		t.Fatalf("unmarshal messages: %v", err)
	}
	if len(msgs) != 2 {
		t.Errorf("expected 2 messages, got %d", len(msgs))
	}
}

func TestToSafeIRClaude(t *testing.T) {
	body := json.RawMessage(`{
		"model": "claude-3-opus-20240229",
		"max_tokens": 1024,
		"messages": [
			{"role": "user", "content": "Hello!"}
		]
	}`)

	ir, err := ToSafeIR(body, engine.FormatAnthropic)
	if err != nil {
		t.Fatalf("ToSafeIR(claude): %v", err)
	}
	if len(ir.Messages) != 1 {
		t.Errorf("expected 1 message, got %d", len(ir.Messages))
	}
	if ir.Messages[0].Text != "Hello!" {
		t.Errorf("expected text 'Hello!', got %q", ir.Messages[0].Text)
	}
}

func TestToSafeIRGemini(t *testing.T) {
	body := json.RawMessage(`{
		"contents": [
			{"role": "user", "parts": [{"text": "Hello Gemini"}]}
		]
	}`)

	ir, err := ToSafeIR(body, engine.FormatGemini)
	if err != nil {
		t.Fatalf("ToSafeIR(gemini): %v", err)
	}
	if len(ir.Messages) != 1 {
		t.Errorf("expected 1 message, got %d", len(ir.Messages))
	}
}

func TestRoundTripOpenAI(t *testing.T) {
	original := json.RawMessage(`{
		"model": "gpt-4",
		"messages": [
			{"role": "user", "content": "Hello!"}
		]
	}`)

	ir, err := ToSafeIR(original, engine.FormatOpenAIChat)
	if err != nil {
		t.Fatalf("ToSafeIR: %v", err)
	}

	result, err := FromSafeIR(ir, engine.FormatOpenAIChat)
	if err != nil {
		t.Fatalf("FromSafeIR: %v", err)
	}

	var origObj, resObj map[string]json.RawMessage
	json.Unmarshal(original, &origObj)
	json.Unmarshal(result, &resObj)

	var origMsgs, resMsgs []json.RawMessage
	json.Unmarshal(origObj["messages"], &origMsgs)
	json.Unmarshal(resObj["messages"], &resMsgs)

	if len(origMsgs) != len(resMsgs) {
		t.Errorf("message count: %d vs %d", len(origMsgs), len(resMsgs))
	}
}

func TestCrossFormatRoundTrip(t *testing.T) {
	// OpenAI → SafeIR → Claude
	openaiBody := json.RawMessage(`{
		"model": "gpt-4",
		"messages": [
			{"role": "user", "content": "Hello cross-format!"}
		]
	}`)

	ir, err := ToSafeIR(openaiBody, engine.FormatOpenAIChat)
	if err != nil {
		t.Fatalf("ToSafeIR: %v", err)
	}

	claudeBody, err := FromSafeIR(ir, engine.FormatAnthropic)
	if err != nil {
		t.Fatalf("FromSafeIR(claude): %v", err)
	}

	var claudeObj map[string]json.RawMessage
	if err := json.Unmarshal(claudeBody, &claudeObj); err != nil {
		t.Fatalf("unmarshal claude: %v", err)
	}
	var msgs []json.RawMessage
	if err := json.Unmarshal(claudeObj["messages"], &msgs); err != nil {
		t.Fatalf("unmarshal messages: %v", err)
	}
	if len(msgs) != 1 {
		t.Errorf("expected 1 claude message, got %d", len(msgs))
	}
}

func TestLossyTranslation(t *testing.T) {
	// Create a message with content that would be lossy to strip
	// (tool_calls, images, etc. are lossy through SafeIR).
	ir := &SafeIR{
		Messages: []Message{
			{Role: "user", Text: "Hello"},
		},
	}

	// Try to encode a message with tool calls marked as lossy.
	ir.Messages[0].ToolCalls = []ToolCall{
		{ID: "call_1", Name: "test_func", Arguments: json.RawMessage(`{}`)},
	}

	// FromSafeIR should handle this.
	body, err := FromSafeIR(ir, engine.FormatOpenAIChat)
	if err != nil {
		t.Fatalf("FromSafeIR with tool_calls: %v", err)
	}
	if len(body) == 0 {
		t.Fatal("expected non-empty body")
	}
}

func TestErrLossyTranslation(t *testing.T) {
	if ErrLossyTranslation == nil {
		t.Fatal("ErrLossyTranslation should be defined")
	}
	if !errors.Is(ErrLossyTranslation, ErrLossyTranslation) {
		t.Fatal("ErrLossyTranslation should match itself")
	}
}

func TestNilSafeIR(t *testing.T) {
	_, err := FromSafeIR(nil, engine.FormatOpenAIChat)
	if err == nil {
		t.Fatal("expected error for nil SafeIR")
	}
}

func TestNilBodyToSafeIR(t *testing.T) {
	_, err := ToSafeIR(nil, engine.FormatOpenAIChat)
	if err == nil {
		t.Fatal("expected error for nil body")
	}

	_, err = ToSafeIR(json.RawMessage{}, engine.FormatOpenAIChat)
	if err == nil {
		t.Fatal("expected error for empty body")
	}
}

func TestLossRejectionThinkingOpenAI(t *testing.T) {
	ir := &SafeIR{
		Messages: []Message{
			{Role: "assistant", Thinking: "I need to reason...", Text: "The answer."},
		},
	}
	_, err := FromSafeIR(ir, engine.FormatOpenAIChat)
	if err == nil {
		t.Fatal("expected ErrLossyTranslation for thinking content to OpenAI")
	}
	if !hasLossy(err) {
		t.Errorf("expected ErrLossyTranslation, got %v", err)
	}
}

func TestLossRejectionThinkingGemini(t *testing.T) {
	ir := &SafeIR{
		Messages: []Message{
			{Role: "assistant", Thinking: "thinking..."},
		},
	}
	_, err := FromSafeIR(ir, engine.FormatGemini)
	if err == nil {
		t.Fatal("expected ErrLossyTranslation for thinking content to Gemini")
	}
}

func TestLossRejectionToolCallsGemini(t *testing.T) {
	ir := &SafeIR{
		Messages: []Message{
			{Role: "assistant", ToolCalls: []ToolCall{{ID: "call_1", Name: "test", Type: "function"}}},
		},
	}
	_, err := FromSafeIR(ir, engine.FormatGemini)
	if err == nil {
		t.Fatal("expected ErrLossyTranslation for tool calls to Gemini via SafeIR")
	}
}

func TestLossRejectionUnknownContentType(t *testing.T) {
	// OpenAI message with unknown content type should trigger loss error.
	body := json.RawMessage(`{
		"model": "gpt-4",
		"messages": [{"role": "user", "content": [{"type": "refusal", "refusal": "I cannot answer"}]}]
	}`)
	_, err := ToSafeIR(body, engine.FormatOpenAIChat)
	if err == nil {
		t.Fatal("expected ErrLossyTranslation for unsupported content type")
	}
}

func TestLossRejectionClaudeUnknownBlock(t *testing.T) {
	body := json.RawMessage(`{
		"model": "claude-3",
		"messages": [{"role": "assistant", "content": [{"type": "unknown_block_type", "data": "test"}]}]
	}`)
	_, err := ToSafeIR(body, engine.FormatAnthropic)
	if err == nil {
		t.Fatal("expected ErrLossyTranslation for unknown Claude block type")
	}
}

func TestLossRejectionGeminiFunctionCall(t *testing.T) {
	body := json.RawMessage(`{
		"contents": [{"role": "model", "parts": [{"functionCall": {"name": "test", "args": {}}}]}]
	}`)
	_, err := ToSafeIR(body, engine.FormatGemini)
	if err == nil {
		t.Fatal("expected ErrLossyTranslation for functionCall in Gemini SafeIR")
	}
}

func TestLossRejectionCodexTools(t *testing.T) {
	ir := &SafeIR{
		Tools: []Tool{{Name: "test_fn"}},
	}
	_, err := FromSafeIR(ir, engine.FormatCodexResponses)
	if err == nil {
		t.Fatal("expected ErrLossyTranslation for tools to Codex via SafeIR")
	}
}

func TestSafeIRPreservesToolsForOpenAI(t *testing.T) {
	body := json.RawMessage(`{
		"model": "gpt-4",
		"messages": [{"role": "user", "content": "use a tool"}],
		"tools": [{"type":"function","function":{"name":"my_tool","description":"does stuff","parameters":{"type":"object"}}}],
		"tool_choice": "auto"
	}`)
	ir, err := ToSafeIR(body, engine.FormatOpenAIChat)
	if err != nil {
		t.Fatalf("ToSafeIR: %v", err)
	}
	if len(ir.Tools) != 1 {
		t.Fatalf("expected 1 tool, got %d", len(ir.Tools))
	}
	if ir.Tools[0].Name != "my_tool" {
		t.Errorf("expected tool name my_tool, got %q", ir.Tools[0].Name)
	}
	result, err := FromSafeIR(ir, engine.FormatOpenAIChat)
	if err != nil {
		t.Fatalf("FromSafeIR: %v", err)
	}
	var obj map[string]json.RawMessage
	json.Unmarshal(result, &obj)
	if _, ok := obj["tools"]; !ok {
		t.Error("tools field was lost in round-trip")
	}
}

func TestSafeIRPreservesToolsForClaude(t *testing.T) {
	body := json.RawMessage(`{
		"model": "claude-3",
		"max_tokens": 100,
		"messages": [{"role": "user", "content": "use a tool"}],
		"tools": [{"name":"my_tool","description":"does stuff","input_schema":{"type":"object"}}]
	}`)
	ir, err := ToSafeIR(body, engine.FormatAnthropic)
	if err != nil {
		t.Fatalf("ToSafeIR: %v", err)
	}
	if len(ir.Tools) != 1 {
		t.Fatalf("expected 1 tool, got %d", len(ir.Tools))
	}
	result, err := FromSafeIR(ir, engine.FormatAnthropic)
	if err != nil {
		t.Fatalf("FromSafeIR: %v", err)
	}
	var obj map[string]json.RawMessage
	json.Unmarshal(result, &obj)
	if _, ok := obj["tools"]; !ok {
		t.Error("tools field was lost in round-trip")
	}
}

func TestSafeIRRejectsToolsForGemini(t *testing.T) {
	ir := &SafeIR{
		Tools: []Tool{{Name: "test_fn"}},
	}
	_, err := FromSafeIR(ir, engine.FormatGemini)
	if err == nil {
		t.Fatal("expected ErrLossyTranslation for tools to Gemini via SafeIR")
	}
}

func TestSafeIRResponseCrossFormat(t *testing.T) {
	tr := NewSafeIRTranslator(engine.FormatOpenAIChat, engine.FormatAnthropic)
	_, err := tr.TranslateResponse(nil, &engine.Response{Body: []byte(`{}`)})
	if err == nil {
		t.Fatal("expected ErrLossyTranslation for cross-format SafeIR response")
	}
}

func TestSafeIRStreamChunkCrossFormat(t *testing.T) {
	tr := NewSafeIRTranslator(engine.FormatOpenAIChat, engine.FormatAnthropic)
	_, err := tr.TranslateStreamChunk(nil, &formats.Chunk{Data: []byte(`{}`)})
	if err == nil {
		t.Fatal("expected ErrLossyTranslation for cross-format SafeIR stream chunk")
	}
}

func TestSafeIRResponseSameFormat(t *testing.T) {
	tr := NewSafeIRTranslator(engine.FormatOpenAIChat, engine.FormatOpenAIChat)
	resp := &engine.Response{Body: []byte(`{"ok":true}`)}
	out, err := tr.TranslateResponse(nil, resp)
	if err != nil {
		t.Fatalf("TranslateResponse same-format: %v", err)
	}
	if out != resp {
		t.Error("expected pointer equality for same-format response")
	}
}

func TestLossRejectionCodexRichContent(t *testing.T) {
	ir := &SafeIR{
		Messages: []Message{
			{Role: "user", ToolCalls: []ToolCall{{ID: "c1", Name: "fn", Type: "function"}}},
		},
	}
	_, err := FromSafeIR(ir, engine.FormatCodexResponses)
	if err == nil {
		t.Fatal("expected ErrLossyTranslation for tool calls to Codex via SafeIR")
	}
}

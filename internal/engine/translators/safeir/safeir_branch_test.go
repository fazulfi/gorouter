package safeir

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"

	"gorouter/internal/domain/engine"
	"gorouter/internal/engine/formats"
)

// isLossy reports whether err wraps ErrLossyTranslation.
func isLossy(err error) bool {
	return err != nil && errors.Is(err, ErrLossyTranslation)
}

// mustJSON parses raw JSON into a generic value, failing the test on error.
func mustJSON(t *testing.T, raw string) interface{} {
	t.Helper()
	var v interface{}
	if err := json.Unmarshal([]byte(raw), &v); err != nil {
		t.Fatalf("invalid test JSON: %v", err)
	}
	return v
}

// bodyField extracts a top-level field from a marshaled body, failing the test
// when the field is absent.
func bodyField(t *testing.T, body json.RawMessage, field string) json.RawMessage {
	t.Helper()
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(body, &obj); err != nil {
		t.Fatalf("unmarshal body: %v", err)
	}
	raw, ok := obj[field]
	if !ok {
		t.Fatalf("missing field %q in body", field)
	}
	return raw
}

// contentBlocks extracts the content block array from a marshaled message,
// failing cleanly when the content was encoded as a base64 string instead.
func contentBlocks(t *testing.T, msg map[string]interface{}) []map[string]interface{} {
	t.Helper()
	c, ok := msg["content"].([]interface{})
	if !ok {
		t.Fatalf("content has type %T ([]byte content is base64-encoded): %v", msg["content"], msg["content"])
	}
	blocks := make([]map[string]interface{}, 0, len(c))
	for _, b := range c {
		blocks = append(blocks, b.(map[string]interface{}))
	}
	return blocks
}

func TestToSafeIR_InvalidJSON(t *testing.T) {
	_, err := ToSafeIR(json.RawMessage(`{not json`), engine.FormatOpenAIChat)
	if err == nil || !strings.Contains(err.Error(), "unmarshal") {
		t.Fatalf("expected unmarshal error, got %v", err)
	}
}

func TestSafeIR_OpenAI_StopAsString(t *testing.T) {
	body := json.RawMessage(`{"model":"gpt-4","messages":[{"role":"user","content":"hi"}],"stop":"END"}`)
	ir, err := ToSafeIR(body, engine.FormatOpenAIChat)
	if err != nil {
		t.Fatalf("ToSafeIR: %v", err)
	}
	if len(ir.Stop) != 1 || ir.Stop[0] != "END" {
		t.Errorf("Stop = %v, want [END]", ir.Stop)
	}
}

func TestSafeIR_OpenAI_ContentPartInvalidType(t *testing.T) {
	body := json.RawMessage(`{
		"model": "gpt-4",
		"messages": [{"role": "user", "content": [42, {"type": "text", "text": "valid part"}]}]
	}`)
	ir, err := ToSafeIR(body, engine.FormatOpenAIChat)
	if err != nil {
		t.Fatalf("ToSafeIR: %v", err)
	}
	if ir.Messages[0].Text != "valid part" {
		t.Errorf("Text = %q, want 'valid part' (invalid part skipped)", ir.Messages[0].Text)
	}
}

func TestSafeIR_ClaudeMessage_ContentNotStringOrArray(t *testing.T) {
	body := json.RawMessage(`{"model":"claude-3","messages":[{"role":"user","content":42}]}`)
	ir, err := ToSafeIR(body, engine.FormatAnthropic)
	if err != nil {
		t.Fatalf("ToSafeIR: %v", err)
	}
	m := ir.Messages[0]
	if m.Text != "" || len(m.RawParts) == 0 {
		t.Errorf("message = %+v, want raw parts passthrough with no text", m)
	}
}

func TestSafeIR_ClaudeMessage_BlockWithoutType(t *testing.T) {
	body := json.RawMessage(`{
		"model": "claude-3",
		"messages": [{"role": "user", "content": [{"text": "no type"}, {"type": "text", "text": "typed"}]}]
	}`)
	ir, err := ToSafeIR(body, engine.FormatAnthropic)
	if err != nil {
		t.Fatalf("ToSafeIR: %v", err)
	}
	if ir.Messages[0].Text != "typed" {
		t.Errorf("Text = %q, want 'typed' (untyped block skipped)", ir.Messages[0].Text)
	}
}

func TestSafeIR_Gemini_SystemInstructionNotObject(t *testing.T) {
	body := json.RawMessage(`{
		"systemInstruction": "plain string",
		"contents": [{"role": "user", "parts": [{"text": "hi"}]}]
	}`)
	ir, err := ToSafeIR(body, engine.FormatGemini)
	if err != nil {
		t.Fatalf("ToSafeIR: %v", err)
	}
	if ir.System != "" {
		t.Errorf("System = %q, want empty (non-object systemInstruction skipped)", ir.System)
	}
}

func TestSafeIR_Gemini_InvalidContentElement(t *testing.T) {
	body := json.RawMessage(`{"contents": [42]}`)
	if _, err := ToSafeIR(body, engine.FormatGemini); err == nil {
		t.Fatal("expected error for non-object content element")
	}
}

func TestSafeIR_Gemini_InvalidPart(t *testing.T) {
	body := json.RawMessage(`{
		"contents": [{"role": "user", "parts": [42, {"text": "ok"}]}]
	}`)
	ir, err := ToSafeIR(body, engine.FormatGemini)
	if err != nil {
		t.Fatalf("ToSafeIR: %v", err)
	}
	if ir.Messages[0].Text != "ok" {
		t.Errorf("Text = %q, want 'ok' (invalid part skipped)", ir.Messages[0].Text)
	}
}

func TestSafeIR_BuildOpenAI_ImageMIMEFallback(t *testing.T) {
	ir := &SafeIR{
		Messages: []Message{{Role: "user", ImageURLs: []string{"https://cdn.example.com/pic.png"}}},
	}
	out, err := FromSafeIR(ir, engine.FormatOpenAIChat)
	if err != nil {
		t.Fatalf("FromSafeIR(openai): %v", err)
	}
	if !json.Valid(out) {
		t.Fatalf("invalid output: %s", out)
	}
}

func TestSafeIR_BuildClaude_ImageMIMEFallback(t *testing.T) {
	ir := &SafeIR{
		Messages: []Message{{Role: "user", ImageURLs: []string{"https://cdn.example.com/pic.webp"}}},
	}
	out, err := FromSafeIR(ir, engine.FormatAnthropic)
	if err != nil {
		t.Fatalf("FromSafeIR(claude): %v", err)
	}
	if !json.Valid(out) {
		t.Fatalf("invalid output: %s", out)
	}
}

func TestSafeIR_BuildClaude_TopP(t *testing.T) {
	topP := 0.6
	ir := &SafeIR{
		Messages: []Message{{Role: "user", Text: "hi"}},
		TopP:     &topP,
	}
	out, err := FromSafeIR(ir, engine.FormatAnthropic)
	if err != nil {
		t.Fatalf("FromSafeIR(claude): %v", err)
	}
	if got := string(bodyField(t, out, "top_p")); got != "0.6" {
		t.Errorf("top_p = %s, want 0.6", got)
	}
}

func TestSafeIR_BuildClaude_ToolChoiceString(t *testing.T) {
	ir := &SafeIR{
		Messages:   []Message{{Role: "user", Text: "hi"}},
		ToolChoice: "none",
	}
	out, err := FromSafeIR(ir, engine.FormatAnthropic)
	if err != nil {
		t.Fatalf("FromSafeIR(claude): %v", err)
	}
	if got := string(bodyField(t, out, "tool_choice")); got != `{"type":"none"}` {
		t.Errorf("tool_choice = %s, want none mapping", got)
	}
}

func TestSafeIR_BuildGeminiContents_ImageMIMEFallback(t *testing.T) {
	ir := &SafeIR{
		Messages: []Message{{Role: "user", ImageURLs: []string{"https://cdn.example.com/pic.gif"}}},
	}
	out, err := FromSafeIR(ir, engine.FormatGemini)
	if err != nil {
		t.Fatalf("FromSafeIR(gemini): %v", err)
	}
	if !json.Valid(out) {
		t.Fatalf("invalid output: %s", out)
	}
}

func TestSafeIR_ClaudeMessage_InvalidMessageElement(t *testing.T) {
	body := json.RawMessage(`{"model":"claude-3","messages":[42]}`)
	if _, err := ToSafeIR(body, engine.FormatAnthropic); err == nil {
		t.Fatal("expected error for non-object message element")
	}
}

// --- SafeIRTranslator accessors and TranslateRequest -------------------------

func TestSafeIRTranslator_SourceTarget(t *testing.T) {
	tr := NewSafeIRTranslator(engine.FormatOpenAIChat, engine.FormatAnthropic)
	if tr.Source() != engine.FormatOpenAIChat {
		t.Errorf("Source() = %s, want %s", tr.Source(), engine.FormatOpenAIChat)
	}
	if tr.Target() != engine.FormatAnthropic {
		t.Errorf("Target() = %s, want %s", tr.Target(), engine.FormatAnthropic)
	}
}

func TestSafeIRTranslator_TranslateRequest_NilRequest(t *testing.T) {
	tr := NewSafeIRTranslator(engine.FormatOpenAIChat, engine.FormatAnthropic)
	_, err := tr.TranslateRequest(context.Background(), nil)
	if err == nil {
		t.Fatal("expected error for nil request")
	}
	if !strings.Contains(err.Error(), "nil request") {
		t.Errorf("expected nil request error, got: %v", err)
	}
}

func TestSafeIRTranslator_TranslateRequest_ToSafeIRError(t *testing.T) {
	tr := NewSafeIRTranslator(engine.FormatOpenAIChat, engine.FormatAnthropic)
	req := &engine.Request{RawBody: json.RawMessage(``)}
	_, err := tr.TranslateRequest(context.Background(), req)
	if err == nil {
		t.Fatal("expected error for empty raw body")
	}
	if !strings.Contains(err.Error(), "toSafeIR") {
		t.Errorf("expected toSafeIR error wrap, got: %v", err)
	}
}

func TestSafeIRTranslator_TranslateRequest_FromSafeIRError(t *testing.T) {
	// Tools are representable in Claude but NOT in Gemini via SafeIR, so the
	// openai→gemini pivot must be rejected as lossy.
	body := json.RawMessage(`{
		"model": "gpt-4",
		"messages": [{"role": "user", "content": "use a tool"}],
		"tools": [{"type": "function", "function": {"name": "my_tool"}}]
	}`)
	tr := NewSafeIRTranslator(engine.FormatOpenAIChat, engine.FormatGemini)
	req := &engine.Request{RawBody: body, Model: "gpt-4", Stream: false}
	_, err := tr.TranslateRequest(context.Background(), req)
	if err == nil {
		t.Fatal("expected lossy translation error for openai→gemini with tools")
	}
	if !strings.Contains(err.Error(), "fromSafeIR") {
		t.Errorf("expected fromSafeIR error wrap, got: %v", err)
	}
	if !isLossy(err) {
		t.Errorf("expected ErrLossyTranslation in chain, got: %v", err)
	}
}

func TestSafeIRTranslator_TranslateRequest_Success(t *testing.T) {
	tr := NewSafeIRTranslator(engine.FormatOpenAIChat, engine.FormatAnthropic)
	temp := 0.3
	req := &engine.Request{
		Model:       "gpt-4",
		RawBody:     json.RawMessage(`{"model":"gpt-4","messages":[{"role":"user","content":"hi"}],"max_tokens":42,"temperature":0.3}`),
		Stream:      true,
		MaxTokens:   99,
		Temperature: &temp,
		Headers:     map[string]string{"x-test": "1"},
	}
	out, err := tr.TranslateRequest(context.Background(), req)
	if err != nil {
		t.Fatalf("TranslateRequest: %v", err)
	}
	if out.Format != engine.FormatAnthropic {
		t.Errorf("Format = %s, want anthropic", out.Format)
	}
	if out.Model != "gpt-4" {
		t.Errorf("Model = %q, want gpt-4", out.Model)
	}
	if !out.Stream {
		t.Error("Stream not preserved")
	}
	if out.MaxTokens != 42 {
		t.Errorf("MaxTokens = %d, want 42 (from IR body)", out.MaxTokens)
	}
	if out.Temperature == nil || *out.Temperature != 0.3 {
		t.Errorf("Temperature = %v, want 0.3", out.Temperature)
	}
	if out.Headers["x-test"] != "1" {
		t.Error("Headers not preserved")
	}
	// The emitted body must be valid Anthropic wire format.
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(out.RawBody, &obj); err != nil {
		t.Fatalf("output is not valid JSON: %v", err)
	}
	if _, ok := obj["messages"]; !ok {
		t.Error("output body missing messages")
	}
	if string(obj["max_tokens"]) != "42" {
		t.Errorf("output max_tokens = %s, want 42", obj["max_tokens"])
	}
}

func TestSafeIRTranslator_TranslateRequest_CompatSource(t *testing.T) {
	tr := NewSafeIRTranslator(engine.FormatOpenAICompat, engine.FormatOpenAIChat)
	req := &engine.Request{RawBody: json.RawMessage(`{"model":"m","messages":[{"role":"user","content":"x"}]}`)}
	out, err := tr.TranslateRequest(context.Background(), req)
	if err != nil {
		t.Fatalf("TranslateRequest compat→chat: %v", err)
	}
	if out.Format != engine.FormatOpenAIChat {
		t.Errorf("Format = %s, want openai_chat", out.Format)
	}
}

// --- Exact preservation: images --------------------------------------------------

func TestSafeIR_Images_ExactOpenAIRoundTrip(t *testing.T) {
	const url = "data:image/png;base64,iVBORw0KGgoAAAANSUhEUg=="
	input := `{"model":"gpt-4o","messages":[{"role":"user","content":[` +
		`{"type":"text","text":"describe this"},` +
		`{"type":"image_url","image_url":{"url":"` + url + `","detail":"auto"}}]}],` +
		`"tool_choice":"auto"}`

	ir, err := ToSafeIR(json.RawMessage(input), engine.FormatOpenAIChat)
	if err != nil {
		t.Fatalf("ToSafeIR: %v", err)
	}
	if len(ir.Messages) != 1 {
		t.Fatalf("expected 1 message, got %d", len(ir.Messages))
	}
	m := ir.Messages[0]
	if len(m.ImageURLs) != 1 || m.ImageURLs[0] != url {
		t.Errorf("ImageURLs = %v, want [%s]", m.ImageURLs, url)
	}
	if m.ImageMIMEs[url] != "image/png" {
		t.Errorf("ImageMIMEs[url] = %q, want image/png", m.ImageMIMEs[url])
	}
	if m.Text != "describe this" {
		t.Errorf("Text = %q, want 'describe this'", m.Text)
	}

	out, err := FromSafeIR(ir, engine.FormatOpenAIChat)
	if err != nil {
		t.Fatalf("FromSafeIR: %v", err)
	}
	// Semantic equality: image URL, text and mime detail must survive exactly.
	if !reflect.DeepEqual(mustJSON(t, input), mustJSON(t, string(out))) {
		t.Errorf("openai image round trip not exact:\ninput:  %s\noutput: %s", input, out)
	}
}

func TestSafeIR_Images_OpenAIToClaude(t *testing.T) {
	const url = "data:image/jpeg;base64,/9j/4AAQSkZJRg=="
	input := `{"model":"gpt-4o","messages":[{"role":"user","content":[` +
		`{"type":"image_url","image_url":{"url":"` + url + `","detail":"auto"}}]}]}`

	ir, err := ToSafeIR(json.RawMessage(input), engine.FormatOpenAIChat)
	if err != nil {
		t.Fatalf("ToSafeIR: %v", err)
	}
	claudeBody, err := FromSafeIR(ir, engine.FormatAnthropic)
	if err != nil {
		t.Fatalf("FromSafeIR(claude): %v", err)
	}

	msgs := bodyField(t, claudeBody, "messages")
	var msgArr []map[string]interface{}
	if err := json.Unmarshal(msgs, &msgArr); err != nil {
		t.Fatalf("unmarshal messages: %v", err)
	}
	blocks := contentBlocks(t, msgArr[0])
	if len(blocks) != 1 {
		t.Fatalf("expected 1 block, got %d", len(blocks))
	}
	img := blocks[0]
	if img["type"] != "image" {
		t.Fatalf("expected image block, got %v", img["type"])
	}
	src := img["source"].(map[string]interface{})
	if src["type"] != "base64" || src["data"] != url || src["media_type"] != "image/jpeg" {
		t.Errorf("claude image source = %v, want base64/%s with media_type image/jpeg", src, url)
	}
}

func TestSafeIR_Images_ClaudeImageRoundTrip(t *testing.T) {
	const data = "aGVsbG8gaW1hZ2U="
	input := `{"model":"claude-3","max_tokens":64,"messages":[` +
		`{"role":"user","content":[` +
		`{"type":"text","text":"what is this"},` +
		`{"type":"image","source":{"type":"base64","media_type":"image/webp","data":"` + data + `"}}]}]}`

	ir, err := ToSafeIR(json.RawMessage(input), engine.FormatAnthropic)
	if err != nil {
		t.Fatalf("ToSafeIR: %v", err)
	}
	m := ir.Messages[0]
	if len(m.ImageURLs) != 1 || m.ImageURLs[0] != data {
		t.Errorf("ImageURLs = %v, want [%s]", m.ImageURLs, data)
	}
	if m.ImageMIMEs[data] != "image/webp" {
		t.Errorf("ImageMIMEs = %v, want image/webp", m.ImageMIMEs)
	}

	out, err := FromSafeIR(ir, engine.FormatAnthropic)
	if err != nil {
		t.Fatalf("FromSafeIR(claude): %v", err)
	}
	if !reflect.DeepEqual(mustJSON(t, input), mustJSON(t, string(out))) {
		t.Errorf("claude image round trip not exact:\ninput:  %s\noutput: %s", input, out)
	}
}

// --- Exact preservation: tools ----------------------------------------------------

func TestSafeIR_Tools_ExactOpenAICloseLoop(t *testing.T) {
	input := `{
		"model": "gpt-4",
		"messages": [{"role": "user", "content": "call the tool"}],
		"tools": [{
			"type": "function",
			"function": {
				"name": "get_weather",
				"description": "current weather",
				"parameters": {"type": "object", "properties": {"city": {"type": "string"}}, "required": ["city"]}
			}
		}],
		"tool_choice": "auto"
	}`

	ir, err := ToSafeIR(json.RawMessage(input), engine.FormatOpenAIChat)
	if err != nil {
		t.Fatalf("ToSafeIR: %v", err)
	}
	if len(ir.Tools) != 1 {
		t.Fatalf("expected 1 tool, got %d", len(ir.Tools))
	}
	tool := ir.Tools[0]
	if tool.Name != "get_weather" || tool.Description != "current weather" {
		t.Errorf("tool = %+v, want get_weather/current weather", tool)
	}
	if string(tool.Parameters) != `{"type": "object", "properties": {"city": {"type": "string"}}, "required": ["city"]}` {
		t.Errorf("parameters not preserved exactly: %s", tool.Parameters)
	}

	out, err := FromSafeIR(ir, engine.FormatOpenAIChat)
	if err != nil {
		t.Fatalf("FromSafeIR: %v", err)
	}
	if !reflect.DeepEqual(mustJSON(t, input), mustJSON(t, string(out))) {
		t.Errorf("openai tools round trip not exact:\ninput:  %s\noutput: %s", input, out)
	}
}

func TestSafeIR_Tools_OpenAIToClaudeInputSchema(t *testing.T) {
	input := `{
		"model": "gpt-4",
		"messages": [{"role": "user", "content": "go"}],
		"tools": [{
			"type": "function",
			"function": {
				"name": "lookup",
				"description": "look things up",
				"parameters": {"type": "object", "properties": {"key": {"type": "string"}}}
			}
		}]
	}`

	ir, err := ToSafeIR(json.RawMessage(input), engine.FormatOpenAIChat)
	if err != nil {
		t.Fatalf("ToSafeIR: %v", err)
	}
	claudeBody, err := FromSafeIR(ir, engine.FormatAnthropic)
	if err != nil {
		t.Fatalf("FromSafeIR(claude): %v", err)
	}

	rawTools := bodyField(t, claudeBody, "tools")
	var tools []map[string]interface{}
	if err := json.Unmarshal(rawTools, &tools); err != nil {
		t.Fatalf("unmarshal claude tools: %v", err)
	}
	if len(tools) != 1 {
		t.Fatalf("expected 1 claude tool, got %d", len(tools))
	}
	ct := tools[0]
	if ct["name"] != "lookup" || ct["description"] != "look things up" {
		t.Errorf("claude tool = %v, want lookup/look things up", ct)
	}
	schema, ok := ct["input_schema"].(map[string]interface{})
	if !ok || schema["type"] != "object" {
		t.Errorf("input_schema = %v, want object schema", ct["input_schema"])
	}
}

// --- Exact preservation: thinking -------------------------------------------------

func TestSafeIR_Thinking_ClaudeRoundTrip(t *testing.T) {
	input := `{
		"model": "claude-3",
		"max_tokens": 256,
		"messages": [{"role": "assistant", "content": [
			{"type": "thinking", "thinking": "Let me reason step by step"},
			{"type": "text", "text": "The final answer"}
		]}]
	}`

	ir, err := ToSafeIR(json.RawMessage(input), engine.FormatAnthropic)
	if err != nil {
		t.Fatalf("ToSafeIR: %v", err)
	}
	if ir.Messages[0].Thinking != "Let me reason step by step" {
		t.Errorf("Thinking = %q, want reasoning text", ir.Messages[0].Thinking)
	}
	if ir.Messages[0].Text != "The final answer" {
		t.Errorf("Text = %q, want final answer", ir.Messages[0].Text)
	}

	out, err := FromSafeIR(ir, engine.FormatAnthropic)
	if err != nil {
		t.Fatalf("FromSafeIR(claude): %v", err)
	}
	if !reflect.DeepEqual(mustJSON(t, input), mustJSON(t, string(out))) {
		t.Errorf("claude thinking round trip not exact:\ninput:  %s\noutput: %s", input, out)
	}
}

func TestSafeIR_Thinking_ExtractOnlyThinkingBlock(t *testing.T) {
	body := json.RawMessage(`{
		"model": "claude-3",
		"messages": [{"role": "assistant", "content": [{"type": "thinking", "thinking": "pure reasoning"}]}]
	}`)
	ir, err := ToSafeIR(body, engine.FormatAnthropic)
	if err != nil {
		t.Fatalf("ToSafeIR: %v", err)
	}
	m := ir.Messages[0]
	if m.Thinking != "pure reasoning" || m.Text != "" {
		t.Errorf("message = %+v, want thinking only", m)
	}
}

// --- Exact preservation: tool_choice ----------------------------------------------

func TestSafeIR_ToolChoice_ExactOpenAIRoundTrip(t *testing.T) {
	input := `{
		"model": "gpt-4",
		"messages": [{"role": "user", "content": "hi"}],
		"tool_choice": {"type": "function", "function": {"name": "get_weather"}}
	}`

	ir, err := ToSafeIR(json.RawMessage(input), engine.FormatOpenAIChat)
	if err != nil {
		t.Fatalf("ToSafeIR: %v", err)
	}
	if string(ir.ToolChoice.(json.RawMessage)) != `{"type": "function", "function": {"name": "get_weather"}}` {
		t.Errorf("ToolChoice not preserved exactly: %s", ir.ToolChoice)
	}

	out, err := FromSafeIR(ir, engine.FormatOpenAIChat)
	if err != nil {
		t.Fatalf("FromSafeIR: %v", err)
	}
	if !reflect.DeepEqual(mustJSON(t, input), mustJSON(t, string(out))) {
		t.Errorf("tool_choice round trip not exact:\ninput:  %s\noutput: %s", input, out)
	}
}

func TestSafeIR_ToolChoice_ClaudeMappings(t *testing.T) {
	cases := []struct {
		name     string
		choice   string
		expected string
	}{
		{"auto", `"auto"`, `{"type":"auto"}`},
		{"required", `"required"`, `{"type":"any"}`},
		{"any", `"any"`, `{"type":"any"}`},
		{"none", `"none"`, `{"type":"none"}`},
		{"unknown-defaults-auto", `"weird"`, `{"type":"auto"}`},
		{"object-defaults-auto", `{"type":"function","function":{"name":"f"}}`, `{"type":"auto"}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ir := &SafeIR{
				ToolChoice: json.RawMessage(tc.choice),
				Messages:   []Message{{Role: "user", Text: "hi"}},
			}
			out, err := FromSafeIR(ir, engine.FormatAnthropic)
			if err != nil {
				t.Fatalf("FromSafeIR(claude): %v", err)
			}
			got := string(bodyField(t, out, "tool_choice"))
			if got != tc.expected {
				t.Errorf("tool_choice = %s, want %s", got, tc.expected)
			}
		})
	}
}

// --- Exact preservation: metadata ------------------------------------------------

func TestSafeIR_Metadata_ExactOpenAIRoundTrip(t *testing.T) {
	input := `{
		"model": "gpt-4",
		"messages": [{"role": "user", "content": "hi"}],
		"metadata": {"session_id": "s-123", "user_tags": ["beta", "trial"], "n": 7}
	}`

	ir, err := ToSafeIR(json.RawMessage(input), engine.FormatOpenAIChat)
	if err != nil {
		t.Fatalf("ToSafeIR: %v", err)
	}
	if string(ir.Metadata) != `{"session_id": "s-123", "user_tags": ["beta", "trial"], "n": 7}` {
		t.Errorf("Metadata not preserved exactly: %s", ir.Metadata)
	}

	out, err := FromSafeIR(ir, engine.FormatOpenAIChat)
	if err != nil {
		t.Fatalf("FromSafeIR: %v", err)
	}
	got := bodyField(t, out, "metadata")
	if !reflect.DeepEqual(mustJSON(t, string(ir.Metadata)), mustJSON(t, string(got))) {
		t.Errorf("metadata round trip not exact:\ninput:  %s\noutput: %s", ir.Metadata, got)
	}
}

// --- Message extraction: tool_calls / tool_call_id / system ------------------------

func TestSafeIR_OpenAIMessage_ToolCallsAndToolResult(t *testing.T) {
	body := json.RawMessage(`{
		"model": "gpt-4",
		"messages": [
			{"role": "assistant", "content": null, "tool_calls": [
				{"id": "call_abc", "type": "function", "function": {"name": "get_weather", "arguments": "{\"city\":\"NYC\"}"}}
			]},
			{"role": "tool", "tool_call_id": "call_abc", "content": "sunny"}
		]
	}`)

	ir, err := ToSafeIR(body, engine.FormatOpenAIChat)
	if err != nil {
		t.Fatalf("ToSafeIR: %v", err)
	}
	if len(ir.Messages) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(ir.Messages))
	}
	assistant := ir.Messages[0]
	if len(assistant.ToolCalls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(assistant.ToolCalls))
	}
	tc := assistant.ToolCalls[0]
	if tc.ID != "call_abc" || tc.Type != "function" || tc.Name != "get_weather" {
		t.Errorf("tool call = %+v, want call_abc/function/get_weather", tc)
	}
	// The OpenAI wire format carries arguments as a JSON string, and the IR
	// preserves the raw field token exactly.
	if string(tc.Arguments) != `"{\"city\":\"NYC\"}"` {
		t.Errorf("arguments = %s, want exact JSON string", tc.Arguments)
	}
	toolMsg := ir.Messages[1]
	if toolMsg.ToolCallID != "call_abc" || toolMsg.Text != "sunny" {
		t.Errorf("tool message = %+v, want call_abc/sunny", toolMsg)
	}

	// Per-field round trip: the tool call and tool result must survive exactly.
	out, err := FromSafeIR(ir, engine.FormatOpenAIChat)
	if err != nil {
		t.Fatalf("FromSafeIR: %v", err)
	}
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(out, &obj); err != nil {
		t.Fatalf("unmarshal round trip: %v", err)
	}
	var msgs []map[string]interface{}
	if err := json.Unmarshal(obj["messages"], &msgs); err != nil {
		t.Fatalf("unmarshal messages: %v", err)
	}
	if len(msgs) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(msgs))
	}
	tcs := msgs[0]["tool_calls"].([]interface{})
	tcObj := tcs[0].(map[string]interface{})
	fn := tcObj["function"].(map[string]interface{})
	if tcObj["id"] != "call_abc" || fn["name"] != "get_weather" || fn["arguments"] != `"{\"city\":\"NYC\"}"` {
		t.Errorf("round trip tool call = %v", tcObj)
	}
	if msgs[1]["role"] != "tool" || msgs[1]["tool_call_id"] != "call_abc" || msgs[1]["content"] != "sunny" {
		t.Errorf("round trip tool message = %v", msgs[1])
	}
}

func TestSafeIR_OpenAI_RoundTripNoToolChoiceEmitsNull(t *testing.T) {
	// An openai body WITHOUT tool_choice must round trip without injecting a
	// tool_choice key. The extractor assigns the nil RawMessage for the absent
	// key into the interface field, which then marshals as null.
	input := `{"model":"gpt-4","messages":[{"role":"user","content":"hi"}]}`
	ir, err := ToSafeIR(json.RawMessage(input), engine.FormatOpenAIChat)
	if err != nil {
		t.Fatalf("ToSafeIR: %v", err)
	}
	out, err := FromSafeIR(ir, engine.FormatOpenAIChat)
	if err != nil {
		t.Fatalf("FromSafeIR: %v", err)
	}
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(out, &obj); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if _, present := obj["tool_choice"]; present {
		t.Errorf("absent tool_choice was emitted as %s in output", obj["tool_choice"])
	}
	if !reflect.DeepEqual(mustJSON(t, input), mustJSON(t, string(out))) {
		t.Errorf("round trip not exact:\ninput:  %s\noutput: %s", input, out)
	}
}

func TestSafeIR_OpenAIMessage_SystemRole(t *testing.T) {
	body := json.RawMessage(`{"model":"gpt-4","messages":[{"role":"system","content":"You are concise."}]}`)
	ir, err := ToSafeIR(body, engine.FormatOpenAIChat)
	if err != nil {
		t.Fatalf("ToSafeIR: %v", err)
	}
	if len(ir.Messages) != 1 || ir.Messages[0].Text != "You are concise." {
		t.Errorf("system message = %+v, want concise text", ir.Messages)
	}
}

func TestSafeIR_OpenAIMessage_InvalidMessageElement(t *testing.T) {
	body := json.RawMessage(`{"model":"gpt-4","messages":[42]}`)
	if _, err := ToSafeIR(body, engine.FormatOpenAIChat); err == nil {
		t.Fatal("expected error for non-object message element")
	}
}

// --- Unsupported-loss branches ------------------------------------------------------

func TestSafeIR_Loss_OpenAIAudioContent(t *testing.T) {
	body := json.RawMessage(`{
		"model": "gpt-4o-audio",
		"messages": [{"role": "user", "content": [{"type": "input_audio", "input_audio": {"data": "AA==", "format": "wav"}}]}]
	}`)
	_, err := ToSafeIR(body, engine.FormatOpenAIChat)
	if !isLossy(err) {
		t.Fatalf("expected ErrLossyTranslation for input_audio, got %v", err)
	}
}

func TestSafeIR_Loss_ClaudeComputerUseBlock(t *testing.T) {
	body := json.RawMessage(`{
		"model": "claude-3",
		"messages": [{"role": "user", "content": [{"type": "computer_use", "action": "click"}]}]
	}`)
	_, err := ToSafeIR(body, engine.FormatAnthropic)
	if !isLossy(err) {
		t.Fatalf("expected ErrLossyTranslation for computer_use block, got %v", err)
	}
}

func TestSafeIR_Loss_GeminiFunctionResponse(t *testing.T) {
	body := json.RawMessage(`{
		"contents": [{"role": "user", "parts": [{"functionResponse": {"name": "f", "response": {"ok": true}}}]}]
	}`)
	_, err := ToSafeIR(body, engine.FormatGemini)
	if !isLossy(err) {
		t.Fatalf("expected ErrLossyTranslation for functionResponse, got %v", err)
	}
}

func TestSafeIR_Loss_GeminiUnknownPart(t *testing.T) {
	body := json.RawMessage(`{
		"contents": [{"role": "user", "parts": [{"thought": true}]}]
	}`)
	_, err := ToSafeIR(body, engine.FormatGemini)
	if !isLossy(err) {
		t.Fatalf("expected ErrLossyTranslation for unknown part, got %v", err)
	}
}

func TestSafeIR_Loss_CodexToolChoice(t *testing.T) {
	ir := &SafeIR{
		ToolChoice: json.RawMessage(`"auto"`),
		Messages:   []Message{{Role: "user", Text: "hi"}},
	}
	_, err := FromSafeIR(ir, engine.FormatCodexResponses)
	if !isLossy(err) {
		t.Fatalf("expected ErrLossyTranslation for tool_choice→codex, got %v", err)
	}
}

func TestSafeIR_Loss_CodexThinking(t *testing.T) {
	ir := &SafeIR{
		Messages: []Message{{Role: "assistant", Thinking: "private reasoning"}},
	}
	_, err := FromSafeIR(ir, engine.FormatCodexResponses)
	if !isLossy(err) {
		t.Fatalf("expected ErrLossyTranslation for thinking→codex, got %v", err)
	}
}

func TestSafeIR_Loss_CodexImages(t *testing.T) {
	ir := &SafeIR{
		Messages: []Message{{Role: "user", ImageURLs: []string{"data:image/png;base64,AA=="}}},
	}
	_, err := FromSafeIR(ir, engine.FormatCodexResponses)
	if !isLossy(err) {
		t.Fatalf("expected ErrLossyTranslation for images→codex, got %v", err)
	}
}

func TestSafeIR_NoLoss_PlainCodex(t *testing.T) {
	ir := &SafeIR{
		Model:    "gpt-5",
		Messages: []Message{{Role: "user", Text: "plain text"}},
	}
	out, err := FromSafeIR(ir, engine.FormatCodexResponses)
	if err != nil {
		t.Fatalf("plain codex should not be lossy: %v", err)
	}
	if !json.Valid(out) {
		t.Fatalf("invalid codex JSON: %s", out)
	}
}

// --- Unsupported source/target formats ---------------------------------------------

func TestToSafeIR_UnsupportedSource(t *testing.T) {
	_, err := ToSafeIR(json.RawMessage(`{"a":1}`), engine.RequestFormat("bogus"))
	if err == nil || !strings.Contains(err.Error(), "unsupported source format") {
		t.Fatalf("expected unsupported source format error, got %v", err)
	}
}

func TestFromSafeIR_UnsupportedTarget(t *testing.T) {
	_, err := FromSafeIR(&SafeIR{}, engine.RequestFormat("bogus"))
	if err == nil || !strings.Contains(err.Error(), "unsupported target format") {
		t.Fatalf("expected unsupported target format error, got %v", err)
	}
}

// --- Claude extraction branches ------------------------------------------------------

func TestSafeIR_Claude_SystemStopToolsToolChoice(t *testing.T) {
	body := json.RawMessage(`{
		"model": "claude-3",
		"system": "You are terse.",
		"max_tokens": 128,
		"temperature": 0.2,
		"stop_sequences": ["END", "STOP"],
		"tool_choice": {"type": "auto"},
		"tools": [{"name": "f", "description": "d", "input_schema": {"type": "object"}}],
		"messages": [{"role": "user", "content": "hi"}]
	}`)

	ir, err := ToSafeIR(body, engine.FormatAnthropic)
	if err != nil {
		t.Fatalf("ToSafeIR: %v", err)
	}
	if ir.System != "You are terse." {
		t.Errorf("System = %q", ir.System)
	}
	if ir.MaxTokens != 128 {
		t.Errorf("MaxTokens = %d", ir.MaxTokens)
	}
	if ir.Temperature == nil || *ir.Temperature != 0.2 {
		t.Errorf("Temperature = %v", ir.Temperature)
	}
	if len(ir.Stop) != 2 || ir.Stop[0] != "END" || ir.Stop[1] != "STOP" {
		t.Errorf("Stop = %v", ir.Stop)
	}
	if string(ir.ToolChoice.(json.RawMessage)) != `{"type": "auto"}` {
		t.Errorf("ToolChoice = %s", ir.ToolChoice)
	}
	if len(ir.Tools) != 1 || ir.Tools[0].Name != "f" || ir.Tools[0].Description != "d" {
		t.Errorf("Tools = %+v", ir.Tools)
	}
	if string(ir.Tools[0].Parameters) != `{"type": "object"}` {
		t.Errorf("input_schema = %s", ir.Tools[0].Parameters)
	}

	out, err := FromSafeIR(ir, engine.FormatAnthropic)
	if err != nil {
		t.Fatalf("FromSafeIR(claude): %v", err)
	}
	if !reflect.DeepEqual(mustJSON(t, string(body)), mustJSON(t, string(out))) {
		t.Errorf("claude extraction/build round trip not exact:\ninput:  %s\noutput: %s", body, out)
	}
}

func TestSafeIR_ClaudeMessage_ToolUseAndToolResult(t *testing.T) {
	body := json.RawMessage(`{
		"model": "claude-3",
		"messages": [
			{"role": "assistant", "content": [{"type": "tool_use", "id": "toolu_1", "name": "get_weather", "input": {"city": "NYC"}}]},
			{"role": "user", "content": [{"type": "tool_result", "tool_use_id": "toolu_1", "content": "sunny"}]}
		]
	}`)

	ir, err := ToSafeIR(body, engine.FormatAnthropic)
	if err != nil {
		t.Fatalf("ToSafeIR: %v", err)
	}
	assistant := ir.Messages[0]
	if len(assistant.ToolCalls) != 1 || assistant.ToolCalls[0].ID != "toolu_1" || assistant.ToolCalls[0].Name != "get_weather" {
		t.Errorf("assistant tool_use = %+v", assistant.ToolCalls)
	}
	toolResult := ir.Messages[1]
	if toolResult.ToolCallID != "toolu_1" || toolResult.Text != "sunny" {
		t.Errorf("tool_result = %+v", toolResult)
	}
}

func TestSafeIR_Claude_ToolUseArgumentsPreserved(t *testing.T) {
	// tool_use input arguments are part of the tool semantics; the pivot must
	// carry them into the IR or reject the conversion as lossy.
	body := json.RawMessage(`{
		"model": "claude-3",
		"messages": [{"role": "assistant", "content": [
			{"type": "tool_use", "id": "toolu_1", "name": "get_weather", "input": {"city": "NYC"}}
		]}]
	}`)
	ir, err := ToSafeIR(body, engine.FormatAnthropic)
	if err != nil {
		t.Fatalf("ToSafeIR: %v", err)
	}
	tc := ir.Messages[0].ToolCalls[0]
	if string(tc.Arguments) != `{"city":"NYC"}` {
		t.Errorf("tool_use input arguments = %s, want {\"city\":\"NYC\"}", tc.Arguments)
	}
}

func TestSafeIR_ClaudeMessage_TextContentString(t *testing.T) {
	body := json.RawMessage(`{"model":"claude-3","messages":[{"role":"user","content":"plain"}]}`)
	ir, err := ToSafeIR(body, engine.FormatAnthropic)
	if err != nil {
		t.Fatalf("ToSafeIR: %v", err)
	}
	if ir.Messages[0].Text != "plain" {
		t.Errorf("Text = %q, want plain", ir.Messages[0].Text)
	}
}

// --- Gemini extraction branches ------------------------------------------------------

func TestSafeIR_Gemini_FullExtraction(t *testing.T) {
	body := json.RawMessage(`{
		"systemInstruction": {"parts": [{"text": "Be"}, {"text": " terse"}]},
		"contents": [{"role": "model", "parts": [{"text": "done"}]}],
		"generationConfig": {"temperature": 0.5, "topP": 0.9, "maxOutputTokens": 64, "stopSequences": ["END"]},
		"tools": [{"functionDeclarations": [
			{"name": "a", "description": "da", "parameters": {"type": "object"}},
			{"name": "b", "description": "db"}
		]}]
	}`)

	ir, err := ToSafeIR(body, engine.FormatGemini)
	if err != nil {
		t.Fatalf("ToSafeIR: %v", err)
	}
	if ir.System != "Be terse" {
		t.Errorf("System = %q, want 'Be terse'", ir.System)
	}
	if len(ir.Messages) != 1 || ir.Messages[0].Role != "assistant" || ir.Messages[0].Text != "done" {
		t.Errorf("messages = %+v", ir.Messages)
	}
	if ir.Temperature == nil || *ir.Temperature != 0.5 {
		t.Errorf("Temperature = %v", ir.Temperature)
	}
	if ir.TopP == nil || *ir.TopP != 0.9 {
		t.Errorf("TopP = %v", ir.TopP)
	}
	if ir.MaxTokens != 64 {
		t.Errorf("MaxTokens = %d", ir.MaxTokens)
	}
	if len(ir.Stop) != 1 || ir.Stop[0] != "END" {
		t.Errorf("Stop = %v", ir.Stop)
	}
	if len(ir.Tools) != 2 || ir.Tools[0].Name != "a" || ir.Tools[1].Name != "b" {
		t.Errorf("Tools = %+v", ir.Tools)
	}
}

func TestSafeIR_GeminiContent_FunctionRole(t *testing.T) {
	body := json.RawMessage(`{
		"contents": [{"role": "function", "parts": [{"text": "tool output"}]}]
	}`)
	ir, err := ToSafeIR(body, engine.FormatGemini)
	if err != nil {
		t.Fatalf("ToSafeIR: %v", err)
	}
	if ir.Messages[0].Role != "tool" {
		t.Errorf("Role = %q, want tool", ir.Messages[0].Role)
	}
}

func TestSafeIR_GeminiContent_InlineImage(t *testing.T) {
	body := json.RawMessage(`{
		"contents": [{"role": "user", "parts": [{"inlineData": {"mimeType": "image/png", "data": "iVBORw0KGgo="}}]}]
	}`)
	ir, err := ToSafeIR(body, engine.FormatGemini)
	if err != nil {
		t.Fatalf("ToSafeIR: %v", err)
	}
	m := ir.Messages[0]
	if len(m.ImageURLs) != 1 || m.ImageURLs[0] != "iVBORw0KGgo=" {
		t.Errorf("ImageURLs = %v", m.ImageURLs)
	}
	if m.ImageMIMEs["iVBORw0KGgo="] != "image/png" {
		t.Errorf("ImageMIMEs = %v", m.ImageMIMEs)
	}

	// And back to gemini wire format.
	out, err := FromSafeIR(ir, engine.FormatGemini)
	if err != nil {
		t.Fatalf("FromSafeIR(gemini): %v", err)
	}
	if !reflect.DeepEqual(mustJSON(t, string(body)), mustJSON(t, string(out))) {
		t.Errorf("gemini image round trip not exact:\ninput:  %s\noutput: %s", body, out)
	}
}

// --- Codex extraction/build branches ------------------------------------------------

func TestSafeIR_Codex_ExtractAllFields(t *testing.T) {
	body := json.RawMessage(`{
		"model": "gpt-5",
		"input": ["first", "second"],
		"instructions": "be brief",
		"max_output_tokens": 200,
		"temperature": 0.1,
		"tools": [{"type": "function", "function": {"name": "f", "description": "d", "parameters": {"type": "object"}}}],
		"tool_choice": "auto"
	}`)

	ir, err := ToSafeIR(body, engine.FormatCodexResponses)
	if err != nil {
		t.Fatalf("ToSafeIR: %v", err)
	}
	if len(ir.Messages) != 2 || ir.Messages[0].Text != "first" || ir.Messages[1].Text != "second" {
		t.Errorf("messages = %+v", ir.Messages)
	}
	if ir.System != "be brief" {
		t.Errorf("System = %q", ir.System)
	}
	if ir.MaxTokens != 200 {
		t.Errorf("MaxTokens = %d", ir.MaxTokens)
	}
	if ir.Temperature == nil || *ir.Temperature != 0.1 {
		t.Errorf("Temperature = %v", ir.Temperature)
	}
	if len(ir.Tools) != 1 || ir.Tools[0].Name != "f" {
		t.Errorf("Tools = %+v", ir.Tools)
	}
	if string(ir.ToolChoice.(json.RawMessage)) != `"auto"` {
		t.Errorf("ToolChoice = %s", ir.ToolChoice)
	}
}

func TestSafeIR_Codex_ExtractStringInput(t *testing.T) {
	body := json.RawMessage(`{"model": "gpt-5", "input": "single"}`)
	ir, err := ToSafeIR(body, engine.FormatCodexResponses)
	if err != nil {
		t.Fatalf("ToSafeIR: %v", err)
	}
	if len(ir.Messages) != 1 || ir.Messages[0].Role != "user" || ir.Messages[0].Text != "single" {
		t.Errorf("messages = %+v", ir.Messages)
	}
}

func TestSafeIR_Codex_ExtractMessageObjects(t *testing.T) {
	body := json.RawMessage(`{
		"model": "gpt-5",
		"input": [{"role": "user", "content": "from object"}]
	}`)
	ir, err := ToSafeIR(body, engine.FormatCodexResponses)
	if err != nil {
		t.Fatalf("ToSafeIR: %v", err)
	}
	if len(ir.Messages) != 1 || ir.Messages[0].Role != "user" || ir.Messages[0].Text != "from object" {
		t.Errorf("messages = %+v", ir.Messages)
	}
}

func TestSafeIR_Codex_BuildSingleUserAsString(t *testing.T) {
	ir := &SafeIR{
		Model:    "gpt-5",
		Messages: []Message{{Role: "user", Text: "just me"}},
	}
	out, err := FromSafeIR(ir, engine.FormatCodexResponses)
	if err != nil {
		t.Fatalf("FromSafeIR(codex): %v", err)
	}
	var obj map[string]interface{}
	if err := json.Unmarshal(out, &obj); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if obj["input"] != "just me" {
		t.Errorf("input = %v (%T), want string 'just me'", obj["input"], obj["input"])
	}
	if obj["model"] != "gpt-5" {
		t.Errorf("model = %v", obj["model"])
	}
}

func TestSafeIR_Codex_BuildMultiMessageItems(t *testing.T) {
	ir := &SafeIR{
		Model:     "gpt-5",
		System:    "be brief",
		MaxTokens: 100,
		Messages: []Message{
			{Role: "user", Text: "first"},
			{Role: "tool", Text: "result", ToolCallID: "call_1"},
		},
	}
	temp := 0.9
	ir.Temperature = &temp

	out, err := FromSafeIR(ir, engine.FormatCodexResponses)
	if err != nil {
		t.Fatalf("FromSafeIR(codex): %v", err)
	}
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(out, &obj); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if string(obj["instructions"]) != `"be brief"` {
		t.Errorf("instructions = %s", obj["instructions"])
	}
	if string(obj["max_output_tokens"]) != "100" {
		t.Errorf("max_output_tokens = %s", obj["max_output_tokens"])
	}
	var items []map[string]interface{}
	if err := json.Unmarshal(obj["input"], &items); err != nil {
		t.Fatalf("input is not an array: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("expected 2 items, got %d", len(items))
	}
	if items[1]["tool_call_id"] != "call_1" {
		t.Errorf("item[1] tool_call_id = %v", items[1]["tool_call_id"])
	}
}

// --- Build branches: OpenAI / Claude / Gemini --------------------------------------

func TestSafeIR_BuildOpenAI_AllParams(t *testing.T) {
	temp, topP := 0.4, 0.8
	ir := &SafeIR{
		Model:       "gpt-4",
		Messages:    []Message{{Role: "user", Text: "hi"}},
		Temperature: &temp,
		TopP:        &topP,
		MaxTokens:   77,
		Stop:        []string{"END"},
		Tools:       []Tool{{Name: "f", Description: "d", Parameters: json.RawMessage(`{"type":"object"}`)}},
		ToolChoice:  json.RawMessage(`"none"`),
		Metadata:    json.RawMessage(`{"k":"v"}`),
	}
	out, err := FromSafeIR(ir, engine.FormatOpenAIChat)
	if err != nil {
		t.Fatalf("FromSafeIR(openai): %v", err)
	}
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(out, &obj); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if string(obj["temperature"]) != "0.4" || string(obj["top_p"]) != "0.8" {
		t.Errorf("temperature/top_p = %s/%s", obj["temperature"], obj["top_p"])
	}
	if string(obj["max_tokens"]) != "77" {
		t.Errorf("max_tokens = %s", obj["max_tokens"])
	}
	if string(obj["stop"]) != `["END"]` {
		t.Errorf("stop = %s", obj["stop"])
	}
	if string(obj["tool_choice"]) != `"none"` {
		t.Errorf("tool_choice = %s", obj["tool_choice"])
	}
	if string(obj["metadata"]) != `{"k":"v"}` {
		t.Errorf("metadata = %s", obj["metadata"])
	}
	var tools []map[string]interface{}
	if err := json.Unmarshal(obj["tools"], &tools); err != nil {
		t.Fatalf("unmarshal tools: %v", err)
	}
	if len(tools) != 1 || tools[0]["type"] != "function" {
		t.Errorf("tools = %v", tools)
	}
	fn := tools[0]["function"].(map[string]interface{})
	if fn["name"] != "f" || fn["description"] != "d" {
		t.Errorf("function = %v", fn)
	}
}

func TestSafeIR_BuildOpenAIMessages_SystemPrepended(t *testing.T) {
	ir := &SafeIR{
		System:   "system prompt",
		Messages: []Message{{Role: "user", Text: "hi"}},
	}
	out, err := FromSafeIR(ir, engine.FormatOpenAIChat)
	if err != nil {
		t.Fatalf("FromSafeIR: %v", err)
	}
	var obj map[string]json.RawMessage
	json.Unmarshal(out, &obj)
	var msgs []map[string]interface{}
	if err := json.Unmarshal(obj["messages"], &msgs); err != nil {
		t.Fatalf("unmarshal messages: %v", err)
	}
	if len(msgs) != 2 {
		t.Fatalf("expected 2 messages (system + user), got %d", len(msgs))
	}
	if msgs[0]["role"] != "system" || msgs[0]["content"] != "system prompt" {
		t.Errorf("first message = %v, want system", msgs[0])
	}
}

func TestSafeIR_BuildClaude_ToolChoiceAndSystem(t *testing.T) {
	ir := &SafeIR{
		Model:      "claude-3",
		System:     "be nice",
		MaxTokens:  64,
		Messages:   []Message{{Role: "user", Text: "hi"}},
		Stop:       []string{"STOP"},
		ToolChoice: json.RawMessage(`"required"`),
	}
	out, err := FromSafeIR(ir, engine.FormatAnthropic)
	if err != nil {
		t.Fatalf("FromSafeIR(claude): %v", err)
	}
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(out, &obj); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if string(obj["system"]) != `"be nice"` {
		t.Errorf("system = %s", obj["system"])
	}
	if string(obj["stop_sequences"]) != `["STOP"]` {
		t.Errorf("stop_sequences = %s", obj["stop_sequences"])
	}
	if string(obj["tool_choice"]) != `{"type":"any"}` {
		t.Errorf("tool_choice = %s, want any for 'required'", obj["tool_choice"])
	}
}

func TestSafeIR_BuildClaudeMessages_ToolCallBlocks(t *testing.T) {
	ir := &SafeIR{
		Messages: []Message{{
			Role: "assistant",
			Text: "let me check",
			ToolCalls: []ToolCall{{
				ID: "toolu_9", Type: "function", Name: "search",
				Arguments: json.RawMessage(`{"q":"weather"}`),
			}},
		}},
	}
	out, err := FromSafeIR(ir, engine.FormatAnthropic)
	if err != nil {
		t.Fatalf("FromSafeIR(claude): %v", err)
	}
	msgs := bodyField(t, out, "messages")
	var arr []map[string]interface{}
	if err := json.Unmarshal(msgs, &arr); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	blocks := contentBlocks(t, arr[0])
	if len(blocks) != 2 {
		t.Fatalf("expected text + tool_use blocks, got %d", len(blocks))
	}
	toolUse := blocks[1]
	if toolUse["type"] != "tool_use" || toolUse["id"] != "toolu_9" || toolUse["name"] != "search" {
		t.Errorf("tool_use block = %v", toolUse)
	}
	if toolUse["input"].(map[string]interface{})["q"] != "weather" {
		t.Errorf("tool_use input = %v", toolUse["input"])
	}
}

func TestSafeIR_BuildClaudeMessages_ToolResultBlock(t *testing.T) {
	ir := &SafeIR{
		Messages: []Message{{Role: "tool", ToolCallID: "toolu_1", Text: "all good"}},
	}
	out, err := FromSafeIR(ir, engine.FormatAnthropic)
	if err != nil {
		t.Fatalf("FromSafeIR(claude): %v", err)
	}
	msgs := bodyField(t, out, "messages")
	var arr []map[string]interface{}
	if err := json.Unmarshal(msgs, &arr); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if arr[0]["role"] != "user" {
		t.Errorf("tool_result message role = %v, want user", arr[0]["role"])
	}
	block := contentBlocks(t, arr[0])[0]
	if block["type"] != "tool_result" || block["tool_use_id"] != "toolu_1" || block["content"] != "all good" {
		t.Errorf("tool_result block = %v", block)
	}
}

func TestSafeIR_BuildClaudeMessages_ThinkingAndText(t *testing.T) {
	ir := &SafeIR{
		Messages: []Message{{Role: "assistant", Thinking: "think", Text: "speak"}},
	}
	out, err := FromSafeIR(ir, engine.FormatAnthropic)
	if err != nil {
		t.Fatalf("FromSafeIR(claude): %v", err)
	}
	msgs := bodyField(t, out, "messages")
	var arr []map[string]interface{}
	if err := json.Unmarshal(msgs, &arr); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	blocks := contentBlocks(t, arr[0])
	if len(blocks) != 2 {
		t.Fatalf("expected thinking + text blocks, got %d", len(blocks))
	}
	thinking := blocks[0]
	if thinking["type"] != "thinking" || thinking["thinking"] != "think" {
		t.Errorf("thinking block = %v", thinking)
	}
	text := blocks[1]
	if text["type"] != "text" || text["text"] != "speak" {
		t.Errorf("text block = %v", text)
	}
}

func TestSafeIR_BuildGemini_SystemAndConfig(t *testing.T) {
	temp, topP := 0.3, 0.7
	ir := &SafeIR{
		System:      "sys",
		Messages:    []Message{{Role: "user", Text: "hi"}},
		Temperature: &temp,
		TopP:        &topP,
		MaxTokens:   50,
		Stop:        []string{"STOP"},
	}
	out, err := FromSafeIR(ir, engine.FormatGemini)
	if err != nil {
		t.Fatalf("FromSafeIR(gemini): %v", err)
	}
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(out, &obj); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if string(obj["systemInstruction"]) != `{"parts":[{"text":"sys"}]}` {
		t.Errorf("systemInstruction = %s", obj["systemInstruction"])
	}
	var gc map[string]json.RawMessage
	if err := json.Unmarshal(obj["generationConfig"], &gc); err != nil {
		t.Fatalf("generationConfig: %v", err)
	}
	if string(gc["temperature"]) != "0.3" || string(gc["topP"]) != "0.7" ||
		string(gc["maxOutputTokens"]) != "50" || string(gc["stopSequences"]) != `["STOP"]` {
		t.Errorf("generationConfig = %s", obj["generationConfig"])
	}
}

func TestSafeIR_BuildGeminiContents_ModelRoleAndImage(t *testing.T) {
	ir := &SafeIR{
		Messages: []Message{
			{Role: "assistant", Text: "answer", ImageURLs: []string{"iVBORw0KGgo="}, ImageMIMEs: map[string]string{"iVBORw0KGgo=": "image/png"}},
		},
	}
	out, err := FromSafeIR(ir, engine.FormatGemini)
	if err != nil {
		t.Fatalf("FromSafeIR(gemini): %v", err)
	}
	contents := bodyField(t, out, "contents")
	var arr []map[string]interface{}
	if err := json.Unmarshal(contents, &arr); err != nil {
		t.Fatalf("unmarshal contents: %v", err)
	}
	if arr[0]["role"] != "model" {
		t.Errorf("role = %v, want model", arr[0]["role"])
	}
	parts := arr[0]["parts"].([]interface{})
	if len(parts) != 2 {
		t.Fatalf("expected text + inlineData parts, got %d", len(parts))
	}
	inline := parts[1].(map[string]interface{})["inlineData"].(map[string]interface{})
	if inline["mimeType"] != "image/png" || inline["data"] != "iVBORw0KGgo=" {
		t.Errorf("inlineData = %v", inline)
	}
}

// --- detectMimeFromURL branches -----------------------------------------------------

func TestDetectMimeFromURL(t *testing.T) {
	cases := []struct {
		url  string
		want string
	}{
		{"data:image/png;base64,AAAA", "image/png"},
		{"data:image/jpeg;base64,AAAA", "image/jpeg"},
		{"data:image/jpg;base64,AAAA", "image/jpeg"},
		{"data:image/webp;base64,AAAA", "image/webp"},
		{"data:image/gif;base64,AAAA", "image/gif"},
		{"data:image/svg+xml;base64,AAAA", "image/svg+xml"},
		{"data:application/pdf;base64,AAAA", "image/jpeg"},
		{"https://cdn.example.com/a.png", "image/png"},
		{"https://cdn.example.com/a.jpg", "image/jpeg"},
		{"https://cdn.example.com/a.jpeg", "image/jpeg"},
		{"https://cdn.example.com/a.webp", "image/webp"},
		{"https://cdn.example.com/a.gif", "image/gif"},
		{"https://cdn.example.com/a.unknown", "image/jpeg"},
	}
	for _, tc := range cases {
		if got := detectMimeFromURL(tc.url); got != tc.want {
			t.Errorf("detectMimeFromURL(%q) = %q, want %q", tc.url, got, tc.want)
		}
	}
}

// --- TranslateResponse / TranslateStreamChunk nil branches ---------------------------

func TestSafeIR_TranslateResponse_Nil(t *testing.T) {
	tr := NewSafeIRTranslator(engine.FormatOpenAIChat, engine.FormatOpenAIChat)
	if _, err := tr.TranslateResponse(context.Background(), nil); err == nil ||
		!strings.Contains(err.Error(), "nil response") {
		t.Fatalf("expected nil response error, got %v", err)
	}
}

func TestSafeIR_TranslateStreamChunk_Nil(t *testing.T) {
	tr := NewSafeIRTranslator(engine.FormatOpenAIChat, engine.FormatOpenAIChat)
	if _, err := tr.TranslateStreamChunk(context.Background(), nil); err == nil ||
		!strings.Contains(err.Error(), "nil chunk") {
		t.Fatalf("expected nil chunk error, got %v", err)
	}
}

func TestSafeIR_TranslateStreamChunk_SameFormat(t *testing.T) {
	tr := NewSafeIRTranslator(engine.FormatOpenAIChat, engine.FormatOpenAIChat)
	chunk := &formats.Chunk{Event: "message", Data: json.RawMessage(`{"x":1}`), IsFinal: true}
	out, err := tr.TranslateStreamChunk(context.Background(), chunk)
	if err != nil {
		t.Fatalf("TranslateStreamChunk: %v", err)
	}
	if out != chunk {
		t.Error("expected pointer equality for same-format chunk")
	}
}

func TestSafeIR_TranslateResponse_CrossFormatLossy(t *testing.T) {
	tr := NewSafeIRTranslator(engine.FormatOpenAIChat, engine.FormatAnthropic)
	_, err := tr.TranslateResponse(context.Background(), &engine.Response{Body: []byte(`{}`)})
	if !isLossy(err) {
		t.Fatalf("expected ErrLossyTranslation for cross-format response, got %v", err)
	}
}

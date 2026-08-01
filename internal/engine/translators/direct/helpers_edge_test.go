package direct

// helpers_edge_test.go — mutation-resistant branch coverage for the shared
// helper conversion functions in internal/engine/translators/direct/helpers.go.
//
// Every assertion locks the exact JSON shape emitted by the helper, not a
// shallow substring. Malformed-input branches are locked to the current
// deterministic behavior.
//
// Known production defects surfaced by these tests (assertions stay RED;
// production is NOT edited by this task):
//
//	D1 codexInputToMessages drops "tool_calls" from Codex input messages
//	   (probe: TestCodexInputToMessagesPreservesToolCalls).
//	D2 openaiUserToClaude / openaiAssistantToClaude / openaiToolToClaude /
//	   claudeUserToOpenAI encode array content as a base64 JSON string
//	   (json.Marshal returns plain []byte, embedded in the message map,
//	   which encoding/json renders as base64) instead of an embedded JSON
//	   array. Affected assertions expect the correct wire format and fail.

import (
	"encoding/json"
	"strings"
	"testing"
)

// ---- shared test helpers (used by translators_branch_test.go too) ----

func mustJSON(t *testing.T, v interface{}) json.RawMessage {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return b
}

func assertJSONEqual(t *testing.T, got, want json.RawMessage) {
	t.Helper()
	var g, w interface{}
	if err := json.Unmarshal(got, &g); err != nil {
		t.Fatalf("unmarshal got: %v (raw=%s)", err, got)
	}
	if err := json.Unmarshal(want, &w); err != nil {
		t.Fatalf("unmarshal want: %v (raw=%s)", err, want)
	}
	if !jsonDeepEqual(g, w) {
		t.Errorf("JSON mismatch:\n got: %s\nwant: %s", got, want)
	}
}

func jsonDeepEqual(a, b interface{}) bool {
	ab, err1 := json.Marshal(a)
	bb, err2 := json.Marshal(b)
	if err1 != nil || err2 != nil {
		return false
	}
	return string(ab) == string(bb)
}

// ---- OpenAI → Claude ----

func TestOpenAIUserToClaude(t *testing.T) {
	t.Run("string content", func(t *testing.T) {
		got, err := openaiUserToClaude(json.RawMessage(`{"role":"user","content":"hello"}`))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		assertJSONEqual(t, got, json.RawMessage(`{"role":"user","content":"hello"}`))
	})

	t.Run("array content with text and image", func(t *testing.T) {
		got, err := openaiUserToClaude(json.RawMessage(`{
			"role":"user",
			"content":[
				{"type":"text","text":"what is this?"},
				{"type":"image_url","image_url":{"url":"https://x.com/a.png"}}
			]
		}`))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		assertJSONEqual(t, got, json.RawMessage(`{
			"role":"user",
			"content":[
				{"type":"text","text":"what is this?"},
				{"type":"image","source":{"type":"base64","media_type":"image/png","data":"https://x.com/a.png"}}
			]
		}`))
	})

	t.Run("unknown and malformed parts are skipped", func(t *testing.T) {
		got, err := openaiUserToClaude(json.RawMessage(`{
			"role":"user",
			"content":[
				{"type":"video","video_url":"x"},
				42,
				{"type":"text","text":"keep me"}
			]
		}`))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		assertJSONEqual(t, got, json.RawMessage(`{
			"role":"user",
			"content":[{"type":"text","text":"keep me"}]
		}`))
	})

	t.Run("content not string or array is an error", func(t *testing.T) {
		_, err := openaiUserToClaude(json.RawMessage(`{"role":"user","content":123}`))
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if !strings.Contains(err.Error(), "content must be string or array") {
			t.Errorf("unexpected error: %v", err)
		}
	})

	t.Run("null content yields empty string branch", func(t *testing.T) {
		got, err := openaiUserToClaude(json.RawMessage(`{"role":"user","content":null}`))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		assertJSONEqual(t, got, json.RawMessage(`{"role":"user","content":""}`))
	})

	t.Run("malformed raw message is an error", func(t *testing.T) {
		if _, err := openaiUserToClaude(json.RawMessage(`123`)); err == nil {
			t.Fatal("expected error, got nil")
		}
	})
}

func TestOpenAIAssistantToClaude(t *testing.T) {
	t.Run("string content becomes text block", func(t *testing.T) {
		got, err := openaiAssistantToClaude(json.RawMessage(`{"role":"assistant","content":"hi"}`))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		assertJSONEqual(t, got, json.RawMessage(`{
			"role":"assistant",
			"content":[{"type":"text","text":"hi"}]
		}`))
	})

	t.Run("empty string content produces empty block list", func(t *testing.T) {
		got, err := openaiAssistantToClaude(json.RawMessage(`{"role":"assistant","content":""}`))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		assertJSONEqual(t, got, json.RawMessage(`{"role":"assistant","content":[]}`))
	})

	t.Run("content absent skips content entirely", func(t *testing.T) {
		got, err := openaiAssistantToClaude(json.RawMessage(`{"role":"assistant"}`))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		assertJSONEqual(t, got, json.RawMessage(`{"role":"assistant","content":[]}`))
	})

	t.Run("array content keeps only text parts", func(t *testing.T) {
		got, err := openaiAssistantToClaude(json.RawMessage(`{
			"role":"assistant",
			"content":[{"type":"text","text":"a"},{"type":"image_url","image_url":{"url":"x"}},{"type":"refusal","refusal":"no"}]
		}`))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		assertJSONEqual(t, got, json.RawMessage(`{"role":"assistant","content":[{"type":"text","text":"a"}]}`))
	})

	t.Run("tool_calls become tool_use blocks", func(t *testing.T) {
		got, err := openaiAssistantToClaude(json.RawMessage(`{
			"role":"assistant",
			"content":"",
			"tool_calls":[{"id":"call_1","type":"function","function":{"name":"get_weather","arguments":"{\"location\":\"NYC\"}"}}]
		}`))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		assertJSONEqual(t, got, json.RawMessage(`{
			"role":"assistant",
			"content":[{"type":"tool_use","id":"call_1","name":"get_weather","input":{"location":"NYC"}}]
		}`))
	})

	t.Run("malformed tool_call is skipped", func(t *testing.T) {
		got, err := openaiAssistantToClaude(json.RawMessage(`{
			"role":"assistant",
			"content":"hi",
			"tool_calls":[42]
		}`))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		assertJSONEqual(t, got, json.RawMessage(`{"role":"assistant","content":[{"type":"text","text":"hi"}]}`))
	})

	t.Run("content absent with tool_calls only", func(t *testing.T) {
		got, err := openaiAssistantToClaude(json.RawMessage(`{
			"role":"assistant",
			"tool_calls":[{"id":"c9","type":"function","function":{"name":"f","arguments":"{}"}}]
		}`))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		assertJSONEqual(t, got, json.RawMessage(`{
			"role":"assistant",
			"content":[{"type":"tool_use","id":"c9","name":"f","input":{}}]
		}`))
	})

	t.Run("malformed raw message is an error", func(t *testing.T) {
		if _, err := openaiAssistantToClaude(json.RawMessage(`[]`)); err == nil {
			t.Fatal("expected error, got nil")
		}
	})
}

func TestOpenAIToolToClaude(t *testing.T) {
	t.Run("basic", func(t *testing.T) {
		got, err := openaiToolToClaude(json.RawMessage(`{"role":"tool","tool_call_id":"call_1","content":"42"}`))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		assertJSONEqual(t, got, json.RawMessage(`{
			"role":"user",
			"content":[{"type":"tool_result","tool_use_id":"call_1","content":"42"}]
		}`))
	})

	t.Run("null content becomes empty string", func(t *testing.T) {
		got, err := openaiToolToClaude(json.RawMessage(`{"role":"tool","tool_call_id":"call_1","content":null}`))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		assertJSONEqual(t, got, json.RawMessage(`{
			"role":"user",
			"content":[{"type":"tool_result","tool_use_id":"call_1","content":""}]
		}`))
	})

	t.Run("malformed raw message is an error", func(t *testing.T) {
		if _, err := openaiToolToClaude(json.RawMessage(`123`)); err == nil {
			t.Fatal("expected error, got nil")
		}
	})
}

func TestOpenAIToolsToClaude(t *testing.T) {
	valid := []json.RawMessage{json.RawMessage(`{
		"type":"function",
		"function":{"name":"get_weather","description":"Weather","parameters":{"type":"object"}}
	}`)}

	t.Run("valid tool with parameters", func(t *testing.T) {
		got := openaiToolsToClaude(valid)
		want := []map[string]interface{}{
			{"name": "get_weather", "description": "Weather", "input_schema": map[string]interface{}{"type": "object"}},
		}
		assertJSONEqual(t, mustJSON(t, got), mustJSON(t, want))
	})

	t.Run("tool without parameters has no input_schema", func(t *testing.T) {
		got := openaiToolsToClaude([]json.RawMessage{json.RawMessage(`{"type":"function","function":{"name":"f"}}`)})
		want := []map[string]interface{}{{"name": "f", "description": ""}}
		assertJSONEqual(t, mustJSON(t, got), mustJSON(t, want))
	})

	t.Run("missing function name is skipped", func(t *testing.T) {
		got := openaiToolsToClaude([]json.RawMessage{json.RawMessage(`{"type":"function","function":{"description":"x"}}`)})
		if len(got) != 0 {
			t.Fatalf("expected empty result, got %v", got)
		}
	})

	t.Run("malformed tool is skipped", func(t *testing.T) {
		got := openaiToolsToClaude([]json.RawMessage{json.RawMessage(`42`)})
		if len(got) != 0 {
			t.Fatalf("expected empty result, got %v", got)
		}
	})

	t.Run("empty input returns empty slice", func(t *testing.T) {
		if got := openaiToolsToClaude(nil); len(got) != 0 {
			t.Fatalf("expected empty result, got %v", got)
		}
	})
}

func TestMapToolChoice(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"auto", `"auto"`, `{"type":"auto"}`},
		{"required", `"required"`, `{"type":"any"}`},
		{"any", `"any"`, `{"type":"any"}`},
		{"none", `"none"`, `{"type":"none"}`},
		{"unknown string defaults to auto", `"weird"`, `{"type":"auto"}`},
		{"specific function maps to tool", `{"type":"function","function":{"name":"f"}}`, `{"type":"tool","name":"f"}`},
		{"function without name defaults to auto", `{"type":"function","function":{}}`, `{"type":"auto"}`},
		{"invalid json defaults to auto", `123`, `{"type":"auto"}`},
		{"null defaults to auto", `null`, `{"type":"auto"}`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := mapToolChoice(json.RawMessage(tc.in))
			assertJSONEqual(t, mustJSON(t, got), json.RawMessage(tc.want))
		})
	}
}

// ---- Claude → OpenAI ----

func TestClaudeUserToOpenAI(t *testing.T) {
	t.Run("string content", func(t *testing.T) {
		got, err := claudeUserToOpenAI(json.RawMessage(`{"role":"user","content":"hi"}`))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		assertJSONEqual(t, got, json.RawMessage(`{"role":"user","content":"hi"}`))
	})

	t.Run("single text block collapses to string", func(t *testing.T) {
		got, err := claudeUserToOpenAI(json.RawMessage(`{"role":"user","content":[{"type":"text","text":"hi"}]}`))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		assertJSONEqual(t, got, json.RawMessage(`{"role":"user","content":"hi"}`))
	})

	t.Run("text and image blocks stay array", func(t *testing.T) {
		got, err := claudeUserToOpenAI(json.RawMessage(`{
			"role":"user",
			"content":[
				{"type":"text","text":"see this"},
				{"type":"image","source":{"type":"base64","media_type":"image/png","data":"iVBOR"}}
			]
		}`))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		assertJSONEqual(t, got, json.RawMessage(`{
			"role":"user",
			"content":[
				{"type":"text","text":"see this"},
				{"type":"image_url","image_url":{"url":"iVBOR"}}
			]
		}`))
	})

	t.Run("image block without source yields empty url", func(t *testing.T) {
		got, err := claudeUserToOpenAI(json.RawMessage(`{
			"role":"user",
			"content":[{"type":"image"}]
		}`))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		assertJSONEqual(t, got, json.RawMessage(`{
			"role":"user",
			"content":[{"type":"image_url","image_url":{"url":""}}]
		}`))
	})

	t.Run("tool_result block becomes tool message", func(t *testing.T) {
		got, err := claudeUserToOpenAI(json.RawMessage(`{
			"role":"user",
			"content":[{"type":"tool_result","tool_use_id":"tu_1","content":"42"}]
		}`))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		assertJSONEqual(t, got, json.RawMessage(`{"role":"tool","tool_call_id":"tu_1","content":"42"}`))
	})

	t.Run("invalid block type is skipped", func(t *testing.T) {
		got, err := claudeUserToOpenAI(json.RawMessage(`{
			"role":"user",
			"content":[{"type":"video","video_url":"x"},{"type":"text","text":"ok"}]
		}`))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		assertJSONEqual(t, got, json.RawMessage(`{"role":"user","content":"ok"}`))
	})

	t.Run("content not string or array is an error", func(t *testing.T) {
		_, err := claudeUserToOpenAI(json.RawMessage(`{"role":"user","content":123}`))
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if !strings.Contains(err.Error(), "content must be string or array") {
			t.Errorf("unexpected error: %v", err)
		}
	})

	t.Run("malformed raw message is an error", func(t *testing.T) {
		if _, err := claudeUserToOpenAI(json.RawMessage(`"x"`)); err == nil {
			t.Fatal("expected error, got nil")
		}
	})
}

func TestClaudeToolResultToOpenAI(t *testing.T) {
	t.Run("extracts id and content", func(t *testing.T) {
		blocks := []map[string]json.RawMessage{
			{"type": json.RawMessage(`"tool_result"`), "tool_use_id": json.RawMessage(`"tu_1"`), "content": json.RawMessage(`"done"`)},
		}
		got, err := claudeToolResultToOpenAI(blocks)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		assertJSONEqual(t, got, json.RawMessage(`{"role":"tool","tool_call_id":"tu_1","content":"done"}`))
	})

	t.Run("missing content becomes empty string", func(t *testing.T) {
		blocks := []map[string]json.RawMessage{
			{"type": json.RawMessage(`"tool_result"`), "tool_use_id": json.RawMessage(`"tu_2"`)},
		}
		got, err := claudeToolResultToOpenAI(blocks)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		assertJSONEqual(t, got, json.RawMessage(`{"role":"tool","tool_call_id":"tu_2","content":""}`))
	})

	t.Run("non-tool blocks are skipped", func(t *testing.T) {
		blocks := []map[string]json.RawMessage{
			{"type": json.RawMessage(`"text"`), "text": json.RawMessage(`"ignored"`)},
			{"type": json.RawMessage(`123`), "tool_use_id": json.RawMessage(`"tu_3"`), "content": json.RawMessage(`"x"`)},
		}
		got, err := claudeToolResultToOpenAI(blocks)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		assertJSONEqual(t, got, json.RawMessage(`{"role":"tool","tool_call_id":"","content":""}`))
	})
}

func TestClaudeAssistantToOpenAI(t *testing.T) {
	t.Run("string content", func(t *testing.T) {
		got, err := claudeAssistantToOpenAI(json.RawMessage(`{"role":"assistant","content":"hi"}`))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		assertJSONEqual(t, got, json.RawMessage(`{"role":"assistant","content":"hi"}`))
	})

	t.Run("text thinking and tool_use blocks", func(t *testing.T) {
		got, err := claudeAssistantToOpenAI(json.RawMessage(`{
			"role":"assistant",
			"content":[
				{"type":"text","text":"answer"},
				{"type":"thinking","thinking":"let me think"},
				{"type":"tool_use","id":"tu_1","name":"get_weather","input":{"location":"NYC"}}
			]
		}`))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		assertJSONEqual(t, got, json.RawMessage(`{
			"role":"assistant",
			"content":"answer[thinking] let me think",
			"tool_calls":[{"id":"tu_1","type":"function","function":{"name":"get_weather","arguments":"{\"location\":\"NYC\"}"}}]
		}`))
	})

	t.Run("tool_use without input yields empty arguments", func(t *testing.T) {
		got, err := claudeAssistantToOpenAI(json.RawMessage(`{
			"role":"assistant",
			"content":[{"type":"tool_use","id":"tu_2","name":"f"}]
		}`))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		assertJSONEqual(t, got, json.RawMessage(`{
			"role":"assistant",
			"content":null,
			"tool_calls":[{"id":"tu_2","type":"function","function":{"name":"f","arguments":""}}]
		}`))
	})

	t.Run("empty content yields content null", func(t *testing.T) {
		got, err := claudeAssistantToOpenAI(json.RawMessage(`{"role":"assistant","content":[]}`))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		assertJSONEqual(t, got, json.RawMessage(`{"role":"assistant","content":null}`))
	})

	t.Run("invalid block type is skipped", func(t *testing.T) {
		got, err := claudeAssistantToOpenAI(json.RawMessage(`{
			"role":"assistant",
			"content":[{"type":"image","source":{"type":"base64"}},{"type":"text","text":"a"}]
		}`))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		assertJSONEqual(t, got, json.RawMessage(`{"role":"assistant","content":"a"}`))
	})

	t.Run("content not string or array is an error", func(t *testing.T) {
		_, err := claudeAssistantToOpenAI(json.RawMessage(`{"role":"assistant","content":123}`))
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if !strings.Contains(err.Error(), "assistant content must be string or array") {
			t.Errorf("unexpected error: %v", err)
		}
	})

	t.Run("malformed raw message is an error", func(t *testing.T) {
		if _, err := claudeAssistantToOpenAI(json.RawMessage(`123`)); err == nil {
			t.Fatal("expected error, got nil")
		}
	})
}

func TestClaudeToolsToOpenAI(t *testing.T) {
	t.Run("valid tool with input_schema", func(t *testing.T) {
		got := claudeToolsToOpenAI([]json.RawMessage{json.RawMessage(`{
			"name":"get_weather","description":"W","input_schema":{"type":"object"}
		}`)})
		want := []map[string]interface{}{
			{"type": "function", "function": map[string]interface{}{"name": "get_weather", "description": "W", "parameters": map[string]interface{}{"type": "object"}}},
		}
		assertJSONEqual(t, mustJSON(t, got), mustJSON(t, want))
	})

	t.Run("tool without input_schema has no parameters", func(t *testing.T) {
		got := claudeToolsToOpenAI([]json.RawMessage{json.RawMessage(`{"name":"f","description":"d"}`)})
		want := []map[string]interface{}{
			{"type": "function", "function": map[string]interface{}{"name": "f", "description": "d"}},
		}
		assertJSONEqual(t, mustJSON(t, got), mustJSON(t, want))
	})

	t.Run("missing name is skipped", func(t *testing.T) {
		got := claudeToolsToOpenAI([]json.RawMessage{json.RawMessage(`{"description":"x"}`)})
		if len(got) != 0 {
			t.Fatalf("expected empty result, got %v", got)
		}
	})

	t.Run("malformed tool is skipped", func(t *testing.T) {
		got := claudeToolsToOpenAI([]json.RawMessage{json.RawMessage(`[]`)})
		if len(got) != 0 {
			t.Fatalf("expected empty result, got %v", got)
		}
	})
}

func TestMapClaudeToolChoice(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"invalid json defaults to auto", `123`, `"auto"`},
		{"auto", `{"type":"auto"}`, `"auto"`},
		{"any maps to required", `{"type":"any"}`, `"required"`},
		{"none", `{"type":"none"}`, `"none"`},
		{"tool with name maps to function", `{"type":"tool","name":"f"}`, `{"type":"function","function":{"name":"f"}}`},
		{"tool without name maps to required", `{"type":"tool"}`, `"required"`},
		{"unknown type defaults to auto", `{"type":"weird"}`, `"auto"`},
		{"null defaults to auto", `null`, `"auto"`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := mapClaudeToolChoice(json.RawMessage(tc.in))
			assertJSONEqual(t, mustJSON(t, got), json.RawMessage(tc.want))
		})
	}
}

// ---- OpenAI → Gemini ----

func TestOpenAIContentToGeminiParts(t *testing.T) {
	t.Run("string content", func(t *testing.T) {
		got, err := openaiContentToGeminiParts(json.RawMessage(`"hi"`))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		want := []map[string]interface{}{{"text": "hi"}}
		assertJSONEqual(t, mustJSON(t, got), mustJSON(t, want))
	})

	t.Run("array content with text and image", func(t *testing.T) {
		got, err := openaiContentToGeminiParts(json.RawMessage(`[
			{"type":"text","text":"a"},
			{"type":"image_url","image_url":{"url":"https://x.com/b.webp"}},
			{"type":"audio","audio_url":"x"},
			42
		]`))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		want := []map[string]interface{}{
			{"text": "a"},
			{"inlineData": map[string]string{"mimeType": "image/webp", "data": "https://x.com/b.webp"}},
		}
		assertJSONEqual(t, mustJSON(t, got), mustJSON(t, want))
	})

	t.Run("content not string or array is an error", func(t *testing.T) {
		_, err := openaiContentToGeminiParts(json.RawMessage(`123`))
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if !strings.Contains(err.Error(), "content is not string or array") {
			t.Errorf("unexpected error: %v", err)
		}
	})

	t.Run("empty array yields empty parts", func(t *testing.T) {
		got, err := openaiContentToGeminiParts(json.RawMessage(`[]`))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		assertJSONEqual(t, mustJSON(t, got), json.RawMessage(`[]`))
	})
}

func TestOpenAIAsstToGeminiParts(t *testing.T) {
	t.Run("content and tool calls", func(t *testing.T) {
		got, err := openaiAsstToGeminiParts(json.RawMessage(`{
			"role":"assistant",
			"content":"hi",
			"tool_calls":[{"id":"c1","type":"function","function":{"name":"get_weather","arguments":"{\"loc\":\"NYC\"}"}}]
		}`))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		want := map[string]interface{}{
			"role":  "model",
			"parts": []map[string]interface{}{{"text": "hi"}, {"functionCall": map[string]interface{}{"name": "get_weather", "args": map[string]interface{}{"loc": "NYC"}}}},
		}
		assertJSONEqual(t, mustJSON(t, got), mustJSON(t, want))
	})

	t.Run("invalid tool call arguments become null args", func(t *testing.T) {
		got, err := openaiAsstToGeminiParts(json.RawMessage(`{
			"role":"assistant",
			"tool_calls":[{"id":"c2","type":"function","function":{"name":"f","arguments":"notjson"}}]
		}`))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		want := map[string]interface{}{
			"role":  "model",
			"parts": []map[string]interface{}{{"functionCall": map[string]interface{}{"name": "f", "args": nil}}},
		}
		assertJSONEqual(t, mustJSON(t, got), mustJSON(t, want))
	})

	t.Run("array content with tool calls", func(t *testing.T) {
		got, err := openaiAsstToGeminiParts(json.RawMessage(`{
			"role":"assistant",
			"content":[{"type":"text","text":"a"},{"type":"text","text":"b"}],
			"tool_calls":[{"id":"c3","type":"function","function":{"name":"f","arguments":"{}"}}]
		}`))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		want := map[string]interface{}{
			"role":  "model",
			"parts": []map[string]interface{}{{"text": "a"}, {"text": "b"}, {"functionCall": map[string]interface{}{"name": "f", "args": map[string]interface{}{}}}},
		}
		assertJSONEqual(t, mustJSON(t, got), mustJSON(t, want))
	})

	t.Run("malformed raw message is an error", func(t *testing.T) {
		if _, err := openaiAsstToGeminiParts(json.RawMessage(`123`)); err == nil {
			t.Fatal("expected error, got nil")
		}
	})
}

func TestOpenAIToolToGeminiParts(t *testing.T) {
	got, err := openaiToolToGeminiParts(json.RawMessage(`{"role":"tool","tool_call_id":"call_1","content":"42"}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := map[string]interface{}{
		"role":  "function",
		"parts": []map[string]interface{}{{"functionResponse": map[string]interface{}{"name": "call_1", "response": map[string]interface{}{"name": "call_1", "content": "42"}}}},
	}
	assertJSONEqual(t, mustJSON(t, got), mustJSON(t, want))

	if _, err := openaiToolToGeminiParts(json.RawMessage(`123`)); err == nil {
		t.Fatal("expected error for malformed raw message, got nil")
	}
}

func TestOpenAIToolsToGemini(t *testing.T) {
	t.Run("valid tool with parameters", func(t *testing.T) {
		got := openaiToolsToGemini([]json.RawMessage{json.RawMessage(`{
			"type":"function",
			"function":{"name":"get_weather","description":"W","parameters":{"type":"object"}}
		}`)})
		want := []map[string]interface{}{
			{"functionDeclarations": []map[string]interface{}{{"name": "get_weather", "description": "W", "parameters": map[string]interface{}{"type": "object"}}}},
		}
		assertJSONEqual(t, mustJSON(t, got), mustJSON(t, want))
	})

	t.Run("non-object parameters are dropped", func(t *testing.T) {
		got := openaiToolsToGemini([]json.RawMessage{json.RawMessage(`{
			"type":"function",
			"function":{"name":"f","parameters":"notanobject"}
		}`)})
		want := []map[string]interface{}{
			{"functionDeclarations": []map[string]interface{}{{"name": "f", "description": ""}}},
		}
		assertJSONEqual(t, mustJSON(t, got), mustJSON(t, want))
	})

	t.Run("missing name is skipped", func(t *testing.T) {
		got := openaiToolsToGemini([]json.RawMessage{json.RawMessage(`{"type":"function","function":{"description":"x"}}`)})
		if len(got) != 0 {
			t.Fatalf("expected empty result, got %v", got)
		}
	})

	t.Run("malformed tool is skipped", func(t *testing.T) {
		got := openaiToolsToGemini([]json.RawMessage{json.RawMessage(`42`)})
		if len(got) != 0 {
			t.Fatalf("expected empty result, got %v", got)
		}
	})
}

// ---- Gemini → OpenAI ----

func TestExtractGeminiSystemText(t *testing.T) {
	t.Run("parts are concatenated", func(t *testing.T) {
		got := extractGeminiSystemText(json.RawMessage(`{"parts":[{"text":"a"},{"text":"b"}]}`))
		if got != "ab" {
			t.Errorf("expected %q, got %q", "ab", got)
		}
	})

	t.Run("text key without parts yields empty", func(t *testing.T) {
		if got := extractGeminiSystemText(json.RawMessage(`{"text":"hi"}`)); got != "" {
			t.Errorf("expected empty, got %q", got)
		}
	})

	t.Run("text key with broken parts falls to text branch", func(t *testing.T) {
		if got := extractGeminiSystemText(json.RawMessage(`{"text":"hi","parts":123}`)); got != "hi" {
			t.Errorf("expected %q, got %q", "hi", got)
		}
	})

	t.Run("invalid input yields empty", func(t *testing.T) {
		if got := extractGeminiSystemText(json.RawMessage(`123`)); got != "" {
			t.Errorf("expected empty, got %q", got)
		}
	})
}

func TestMapGeminiRole(t *testing.T) {
	tests := []struct{ in, want string }{
		{"model", "assistant"},
		{"function", "tool"},
		{"user", "user"},
		{"", "user"},
		{"weird", "user"},
	}
	for _, tc := range tests {
		if got := mapGeminiRole(tc.in); got != tc.want {
			t.Errorf("mapGeminiRole(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestGeminiToolsToOpenAI(t *testing.T) {
	t.Run("valid declaration with parameters", func(t *testing.T) {
		got := geminiToolsToOpenAI([]json.RawMessage{json.RawMessage(`{
			"functionDeclarations":[{"name":"get_weather","description":"W","parameters":{"type":"object"}}]
		}`)})
		want := []map[string]interface{}{
			{"type": "function", "function": map[string]interface{}{"name": "get_weather", "description": "W", "parameters": map[string]interface{}{"type": "object"}}},
		}
		assertJSONEqual(t, mustJSON(t, got), mustJSON(t, want))
	})

	t.Run("declaration without name is skipped", func(t *testing.T) {
		got := geminiToolsToOpenAI([]json.RawMessage{json.RawMessage(`{"functionDeclarations":[{"description":"x"}]}`)})
		if len(got) != 0 {
			t.Fatalf("expected empty result, got %v", got)
		}
	})

	t.Run("no functionDeclarations key yields empty", func(t *testing.T) {
		got := geminiToolsToOpenAI([]json.RawMessage{json.RawMessage(`{"other":"x"}`)})
		if len(got) != 0 {
			t.Fatalf("expected empty result, got %v", got)
		}
	})

	t.Run("malformed tool is skipped", func(t *testing.T) {
		got := geminiToolsToOpenAI([]json.RawMessage{json.RawMessage(`42`)})
		if len(got) != 0 {
			t.Fatalf("expected empty result, got %v", got)
		}
	})
}

// ---- Codex → OpenAI ----

func TestCodexInputToMessages(t *testing.T) {
	t.Run("string input becomes single user message", func(t *testing.T) {
		got, err := codexInputToMessages(json.RawMessage(`"Hello"`))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		assertJSONEqual(t, mustJSON(t, got), json.RawMessage(`[{"role":"user","content":"Hello"}]`))
	})

	t.Run("array of strings becomes user messages", func(t *testing.T) {
		got, err := codexInputToMessages(json.RawMessage(`["a","b"]`))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		assertJSONEqual(t, mustJSON(t, got), json.RawMessage(`[{"role":"user","content":"a"},{"role":"user","content":"b"}]`))
	})

	t.Run("message objects preserve role content and tool_call_id", func(t *testing.T) {
		got, err := codexInputToMessages(json.RawMessage(`[
			{"role":"system","content":"sys"},
			{"role":"tool","content":42,"tool_call_id":"call_1"}
		]`))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		assertJSONEqual(t, mustJSON(t, got), json.RawMessage(`[
			{"role":"system","content":"sys"},
			{"role":"tool","content":42,"tool_call_id":"call_1"}
		]`))
	})

	t.Run("role-less and non-object items are skipped", func(t *testing.T) {
		got, err := codexInputToMessages(json.RawMessage(`[
			{"content":"no role"},
			42,
			{"role":"user","content":"keep"}
		]`))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		assertJSONEqual(t, mustJSON(t, got), json.RawMessage(`[{"role":"user","content":"keep"}]`))
	})

	t.Run("null content is preserved as null", func(t *testing.T) {
		got, err := codexInputToMessages(json.RawMessage(`[{"role":"user","content":null}]`))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		assertJSONEqual(t, mustJSON(t, got), json.RawMessage(`[{"role":"user","content":null}]`))
	})

	t.Run("neither string nor array is an error", func(t *testing.T) {
		_, err := codexInputToMessages(json.RawMessage(`123`))
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if err.Error() != "input must be string or array" {
			t.Errorf("unexpected error: %v", err)
		}
	})

	t.Run("empty raw message is an error", func(t *testing.T) {
		_, err := codexInputToMessages(json.RawMessage(``))
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if err.Error() != "input must be string or array" {
			t.Errorf("unexpected error: %v", err)
		}
	})
}

// TestCodexInputToMessagesPreservesToolCalls is a targeted RED probe.
//
// Defect: codexInputToMessages (helpers.go) parses the "tool_calls" field of a
// Codex Responses input message into a throwaway anonymous struct and discards
// it:
//
//	json.Unmarshal(item, &struct{ ToolCalls []json.RawMessage `json:"tool_calls,omitempty"` }{})
//
// The result is never copied into the OpenAI message, so every assistant
// tool_call in a Codex input is silently DROPPED during translation.
// This test is intentionally RED: it asserts the correct behavior (tool_calls
// preserved) and must fail against the current production code. Production must
// NOT be edited by this task; the failure documents the defect.
func TestCodexInputToMessagesPreservesToolCalls(t *testing.T) {
	input := json.RawMessage(`[
		{"role":"user","content":"weather?"},
		{"role":"assistant","content":null,"tool_calls":[
			{"id":"call_1","type":"function","function":{"name":"get_weather","arguments":"{\"location\":\"NYC\"}"}}
		]}
	]`)

	msgs, err := codexInputToMessages(input)
	if err != nil {
		t.Fatalf("codexInputToMessages: %v", err)
	}
	if len(msgs) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(msgs))
	}

	var asst map[string]interface{}
	if err := json.Unmarshal(msgs[1], &asst); err != nil {
		t.Fatalf("unmarshal assistant message: %v", err)
	}

	// The tool_calls field MUST survive the conversion.
	tc, ok := asst["tool_calls"]
	if !ok {
		t.Fatalf("DEFECT: tool_calls dropped by codexInputToMessages — assistant message = %s", msgs[1])
	}
	tcs, ok := tc.([]interface{})
	if !ok || len(tcs) != 1 {
		t.Fatalf("DEFECT: malformed tool_calls in assistant message: %s", msgs[1])
	}
	first, ok := tcs[0].(map[string]interface{})
	if !ok {
		t.Fatalf("DEFECT: malformed tool_call entry: %s", msgs[1])
	}
	if first["id"] != "call_1" {
		t.Fatalf("DEFECT: tool_call id lost: %s", msgs[1])
	}
	if first["type"] != "function" {
		t.Fatalf("DEFECT: tool_call type lost: %s", msgs[1])
	}
	fn, ok := first["function"].(map[string]interface{})
	if !ok || fn["name"] != "get_weather" {
		t.Fatalf("DEFECT: tool_call function lost: %s", msgs[1])
	}
}

// ---- Shared stop/image helpers ----

func TestParseStopSequences(t *testing.T) {
	t.Run("array", func(t *testing.T) {
		got := parseStopSequences(json.RawMessage(`[".","!"]`))
		if len(got) != 2 || got[0] != "." || got[1] != "!" {
			t.Errorf("unexpected result: %v", got)
		}
	})
	t.Run("single string", func(t *testing.T) {
		got := parseStopSequences(json.RawMessage(`"\n"`))
		if len(got) != 1 || got[0] != "\n" {
			t.Errorf("unexpected result: %v", got)
		}
	})
	t.Run("empty raw message", func(t *testing.T) {
		if got := parseStopSequences(json.RawMessage(``)); got != nil {
			t.Errorf("expected nil, got %v", got)
		}
	})
	t.Run("neither array nor string", func(t *testing.T) {
		if got := parseStopSequences(json.RawMessage(`123`)); got != nil {
			t.Errorf("expected nil, got %v", got)
		}
	})
	t.Run("null is neither array nor string", func(t *testing.T) {
		if got := parseStopSequences(json.RawMessage(`null`)); got != nil {
			t.Errorf("expected nil, got %v", got)
		}
	})
}

func TestDetectImageMIME(t *testing.T) {
	tests := []struct {
		name string
		url  string
		want string
	}{
		{"png extension", "https://x.com/a.png", "image/png"},
		{"png mime", "data:image/png;base64,AAAA", "image/png"},
		{"jpg extension", "https://x.com/a.jpg", "image/jpeg"},
		{"jpeg extension", "https://x.com/a.jpeg", "image/jpeg"},
		{"jpeg mime", "data:image/jpeg;base64,AAAA", "image/jpeg"},
		{"jpg mime", "data:image/jpg;base64,AAAA", "image/jpeg"},
		{"webp extension", "https://x.com/a.webp", "image/webp"},
		{"webp mime", "data:image/webp;base64,AAAA", "image/webp"},
		{"gif extension", "https://x.com/a.gif", "image/gif"},
		{"gif mime", "data:image/gif;base64,AAAA", "image/gif"},
		{"unknown defaults to jpeg", "https://x.com/a.avif", "image/jpeg"},
		{"extension past 100-char truncation is not detected", strings.Repeat("x", 120) + ".png", "image/jpeg"},
		{"extension within first 100 chars is detected", "https://example.com/" + strings.Repeat("a", 75) + ".png", "image/png"},
		{"empty url defaults to jpeg", "", "image/jpeg"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := detectImageMIME(tc.url); got != tc.want {
				t.Errorf("detectImageMIME() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestContainsHelpers(t *testing.T) {
	tests := []struct {
		s, sub string
		want   bool
	}{
		{"abc", "b", true},
		{"abc", "abc", true},
		{"abc", "a", true},
		{"abc", "c", true},
		{"abc", "d", false},
		{"abc", "abcd", false}, // longer than s
		{"", "", true},
		{"abc", "", true},
	}
	for _, tc := range tests {
		if got := contains(tc.s, tc.sub); got != tc.want {
			t.Errorf("contains(%q, %q) = %v, want %v", tc.s, tc.sub, got, tc.want)
		}
	}
}

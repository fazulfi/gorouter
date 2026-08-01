package direct

import (
	"context"
	"encoding/json"
	"testing"
)

func TestG104Helpers_MalformedInputsDegrade(t *testing.T) {
	ctx := context.Background()
	t.Run("claude to openai response tool call without function", func(t *testing.T) {
		tr := NewClaudeToOpenAI()
		resp := mustResp(t, `{"choices":[{"message":{"content":null,"tool_calls":[{"id":"c1","type":"function"}]}}]}`)
		out, err := tr.TranslateResponse(ctx, resp)
		if err != nil {
			t.Fatalf("TranslateResponse: %v", err)
		}
		assertJSONEqual(t, out.Body, json.RawMessage(`{"id":"","type":"message","role":"assistant","content":[{"type":"tool_use","id":"c1","name":"","input":null}],"model":"","stop_reason":"end_turn"}`))
	})
	t.Run("claude to openai response malformed arguments string degrades to null", func(t *testing.T) {
		tr := NewClaudeToOpenAI()
		resp := mustResp(t, `{"choices":[{"message":{"content":null,"tool_calls":[{"id":"c1","type":"function","function":{"name":"f","arguments":"notjson"}}]}}]}`)
		out, err := tr.TranslateResponse(ctx, resp)
		if err != nil {
			t.Fatalf("TranslateResponse: %v", err)
		}
		var obj map[string]interface{}
		if err := json.Unmarshal(out.Body, &obj); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		content := obj["content"].([]interface{})[0].(map[string]interface{})
		if content["input"] != nil {
			t.Errorf("input = %v, want null for malformed arguments", content["input"])
		}
	})
	t.Run("claude to openai stream malformed delta degrades to empty tool block", func(t *testing.T) {
		tr := NewClaudeToOpenAI()
		ch := mustChunk(t, "chunk", `{"choices":[{"index":0,"delta":{"tool_calls":[42]}}]}`, false, nil)
		out, err := tr.TranslateStreamChunk(ctx, ch)
		if err != nil {
			t.Fatalf("TranslateStreamChunk: %v", err)
		}
		assertJSONEqual(t, out.Data, json.RawMessage(`{"type":"content_block_start","index":0,"content_block":{"type":"tool_use","id":"","name":"","input":{}}}`))
	})
	t.Run("gemini to openai response malformed arguments degrades to null", func(t *testing.T) {
		tr := NewGeminiToOpenAI()
		resp := mustResp(t, `{"choices":[{"message":{"content":null,"tool_calls":[{"id":"c1","type":"function","function":{"name":"f","arguments":"notjson"}}]}}]}`)
		out, err := tr.TranslateResponse(ctx, resp)
		if err != nil {
			t.Fatalf("TranslateResponse: %v", err)
		}
		var obj map[string]interface{}
		if err := json.Unmarshal(out.Body, &obj); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		cand := obj["candidates"].([]interface{})[0].(map[string]interface{})
		content := cand["content"].(map[string]interface{})
		part := content["parts"].([]interface{})[0].(map[string]interface{})
		fc := part["functionCall"].(map[string]interface{})
		if fc["args"] != nil {
			t.Errorf("args = %v, want null for malformed arguments", fc["args"])
		}
	})
	t.Run("openai to gemini system content as array degrades to empty", func(t *testing.T) {
		tr := NewOpenAIToGemini()
		req := mustReq(t, `{"model":"gpt-4","messages":[{"role":"system","content":["a","b"]},{"role":"user","content":"hi"}]}`)
		out, err := tr.TranslateRequest(ctx, req)
		if err != nil {
			t.Fatalf("TranslateRequest: %v", err)
		}
		assertJSONEqual(t, out.RawBody, json.RawMessage(`{"contents":[{"role":"user","parts":[{"text":"hi"}]}]}`))
	})
	t.Run("openai to gemini response functionCall malformed args degrades", func(t *testing.T) {
		tr := NewOpenAIToGemini()
		resp := mustResp(t, `{"candidates":[{"content":{"parts":[{"functionCall":{"name":"f","args":"notjson"}}],"role":"model"}}]}`)
		out, err := tr.TranslateResponse(ctx, resp)
		if err != nil {
			t.Fatalf("TranslateResponse: %v", err)
		}
		var obj map[string]interface{}
		if err := json.Unmarshal(out.Body, &obj); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		msg := obj["choices"].([]interface{})[0].(map[string]interface{})["message"].(map[string]interface{})
		tcs := msg["tool_calls"].([]interface{})
		fn := tcs[0].(map[string]interface{})["function"].(map[string]interface{})
		if fn["arguments"] != "null" {
			t.Errorf("arguments = %v, want null for malformed args", fn["arguments"])
		}
	})
	t.Run("openai to gemini response functionCall missing name", func(t *testing.T) {
		tr := NewOpenAIToGemini()
		resp := mustResp(t, `{"candidates":[{"content":{"parts":[{"functionCall":{"args":{}}}],"role":"model"}}]}`)
		out, err := tr.TranslateResponse(ctx, resp)
		if err != nil {
			t.Fatalf("TranslateResponse: %v", err)
		}
		var obj map[string]interface{}
		if err := json.Unmarshal(out.Body, &obj); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		msg := obj["choices"].([]interface{})[0].(map[string]interface{})["message"].(map[string]interface{})
		tcs := msg["tool_calls"].([]interface{})
		if len(tcs) != 1 {
			t.Fatalf("expected 1 tool_call, got %d", len(tcs))
		}
	})
	t.Run("helpers openaiUserToClaude image source malformed is skipped", func(t *testing.T) {
		got, err := openaiUserToClaude(json.RawMessage(`{
			"role":"user",
			"content":[{"type":"image","source":42}]
		}`))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		assertJSONEqual(t, got, json.RawMessage(`{"role":"user","content":[]}`))
	})
	t.Run("helpers claudeUserToOpenAI image source malformed", func(t *testing.T) {
		got, err := claudeUserToOpenAI(json.RawMessage(`{
			"role":"user",
			"content":[{"type":"image","source":42}]
		}`))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		assertJSONEqual(t, got, json.RawMessage(`{"role":"user","content":[{"type":"image_url","image_url":{"url":""}}]}`))
	})
	t.Run("helpers claudeToolResultToOpenAI content as number degrades", func(t *testing.T) {
		blocks := []map[string]json.RawMessage{
			{"type": json.RawMessage(`"tool_result"`), "tool_use_id": json.RawMessage(`"tu_1"`), "content": json.RawMessage(`42`)},
		}
		got, err := claudeToolResultToOpenAI(blocks)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		assertJSONEqual(t, got, json.RawMessage(`{"role":"tool","tool_call_id":"tu_1","content":""}`))
	})
	t.Run("helpers claudeAssistantToOpenAI tool_use id/name malformed", func(t *testing.T) {
		got, err := claudeAssistantToOpenAI(json.RawMessage(`{
			"role":"assistant",
			"content":[{"type":"tool_use","id":5,"name":7,"input":{"a":1}}]
		}`))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		assertJSONEqual(t, got, json.RawMessage(`{
			"role":"assistant","content":null,
			"tool_calls":[{"id":"","type":"function","function":{"name":"","arguments":"{\"a\":1}"}}]
		}`))
	})
	t.Run("helpers openaiAsstToGeminiParts malformed arguments degrades to null", func(t *testing.T) {
		got, err := openaiAsstToGeminiParts(json.RawMessage(`{
			"role":"assistant",
			"tool_calls":[{"id":"c1","type":"function","function":{"name":"f","arguments":"notjson"}}]
		}`))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		assertJSONEqual(t, mustJSON(t, got), json.RawMessage(`{
			"role":"model",
			"parts":[{"functionCall":{"name":"f","args":null}}]
		}`))
	})
	t.Run("helpers openaiToolToGeminiParts content as number degrades", func(t *testing.T) {
		got, err := openaiToolToGeminiParts(json.RawMessage(`{"role":"tool","tool_call_id":"call_1","content":42}`))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		assertJSONEqual(t, mustJSON(t, got), json.RawMessage(`{
			"role":"function",
			"parts":[{"functionResponse":{"name":"call_1","response":{"name":"call_1","content":""}}}]
		}`))
	})
	t.Run("helpers openaiToolToClaude content as number degrades to empty", func(t *testing.T) {
		got, err := openaiToolToClaude(json.RawMessage(`{"role":"tool","tool_call_id":"call_1","content":42}`))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		assertJSONEqual(t, got, json.RawMessage(`{
			"role":"user",
			"content":[{"type":"tool_result","tool_use_id":"call_1","content":""}]
		}`))
	})
}

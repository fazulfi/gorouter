package direct

// translators_branch_test.go — mutation-resistant branch coverage for the
// request/response/stream branches of every direct translator:
// CodexToOpenAI, ClaudeToOpenAI, GeminiToOpenAI, OpenAIToClaude,
// OpenAIToGemini, OpenAIToCompat, CompatToOpenAI.
//
// Every assertion checks the exact emitted JSON plus the engine.Request /
// engine.Response / formats.Chunk fields that the caller depends on.
//
// Known production defects surfaced by these tests (assertions stay RED;
// production is NOT edited by this task):
//
//	D1 codexInputToMessages drops "tool_calls" (helpers_edge_test.go probe).
//	D2 openaiUserToClaude / openaiAssistantToClaude / openaiToolToClaude /
//	   claudeUserToOpenAI emit base64-encoded content instead of embedded
//	   JSON arrays (helpers_edge_test.go probes; request_full_body below is
//	   blocked on the same defect).
//	D3 OpenAIToClaude.TranslateStreamChunk emits delta.tool_calls as a
//	   base64 string instead of a JSON array (stream chunk tool_use start).
//	D4 OpenAIToClaude.TranslateStreamChunk passes raw Claude stop_reason
//	   values through, emitting invalid OpenAI finish_reason "tool_use" and
//	   "max_tokens" instead of "tool_calls" and "length".

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"gorouter/internal/domain/engine"
	"gorouter/internal/engine/formats"
)

func mustReq(t *testing.T, body string) *engine.Request {
	t.Helper()
	return &engine.Request{
		Model:     "gpt-4",
		RawBody:   json.RawMessage(body),
		Headers:   map[string]string{"x-test": "1"},
		Stream:    true,
		MaxTokens: 128,
		UserID:    nil,
	}
}

func mustResp(t *testing.T, body string) *engine.Response {
	t.Helper()
	return &engine.Response{
		Body:       json.RawMessage(body),
		Model:      "gpt-4",
		StatusCode: 200,
		Headers:    map[string]string{"x-test": "1"},
	}
}

func mustChunk(t *testing.T, event, data string, isFinal bool, err error) *formats.Chunk {
	t.Helper()
	return &formats.Chunk{Event: event, Data: json.RawMessage(data), IsFinal: isFinal, Error: err}
}

func assertChunk(t *testing.T, got *formats.Chunk, event string, data string, isFinal bool, wantErrMsg string) {
	t.Helper()
	if got == nil {
		t.Fatal("nil chunk")
	}
	if got.Event != event {
		t.Errorf("Event = %q, want %q", got.Event, event)
	}
	if data != "" {
		var w interface{}
		if err := json.Unmarshal([]byte(data), &w); err != nil {
			// Expected data is not JSON (e.g. the literal "[DONE]"); compare bytes.
			if string(got.Data) != data {
				t.Errorf("Data = %q, want %q", got.Data, data)
			}
		} else {
			assertJSONEqual(t, got.Data, json.RawMessage(data))
		}
	}
	if got.IsFinal != isFinal {
		t.Errorf("IsFinal = %v, want %v", got.IsFinal, isFinal)
	}
	if wantErrMsg != "" {
		if got.Error == nil {
			t.Errorf("expected error containing %q, got nil", wantErrMsg)
		} else if !strings.Contains(got.Error.Error(), wantErrMsg) {
			t.Errorf("Error = %v, want containing %q", got.Error, wantErrMsg)
		}
	}
}

// ---- CodexToOpenAI ----

func TestCodexToOpenAI(t *testing.T) {
	ctx := context.Background()
	tr := NewCodexToOpenAI()

	t.Run("source and target", func(t *testing.T) {
		if tr.Source() != engine.FormatCodexResponses || tr.Target() != engine.FormatOpenAIChat {
			t.Errorf("unexpected formats: %v -> %v", tr.Source(), tr.Target())
		}
	})

	t.Run("request nil", func(t *testing.T) {
		if _, err := tr.TranslateRequest(ctx, nil); !errors.Is(err, errNilRequest) {
			t.Errorf("expected errNilRequest, got %v", err)
		}
	})

	t.Run("request invalid json", func(t *testing.T) {
		req := mustReq(t, `{`)
		if _, err := tr.TranslateRequest(ctx, req); err == nil || !strings.Contains(err.Error(), "unmarshal") {
			t.Errorf("expected unmarshal error, got %v", err)
		}
	})

	t.Run("request empty input", func(t *testing.T) {
		req := mustReq(t, `{"model":"gpt-4"}`)
		if _, err := tr.TranslateRequest(ctx, req); err == nil || !strings.Contains(err.Error(), "empty input") {
			t.Errorf("expected empty input error, got %v", err)
		}
	})

	t.Run("request full body", func(t *testing.T) {
		req := mustReq(t, `{
			"model":"gpt-4",
			"input":"Hello",
			"instructions":"Be concise",
			"max_output_tokens":500,
			"temperature":0.5,
			"top_p":0.9,
			"tools":[{"type":"function","function":{"name":"get_weather"}}],
			"tool_choice":"auto"
		}`)
		out, err := tr.TranslateRequest(ctx, req)
		if err != nil {
			t.Fatalf("TranslateRequest: %v", err)
		}
		if out.Format != engine.FormatOpenAIChat || out.Model != "gpt-4" {
			t.Errorf("format/model mismatch: %v %q", out.Format, out.Model)
		}
		if !out.Stream {
			t.Error("expected Stream preserved = true")
		}
		if out.MaxTokens != 500 {
			t.Errorf("MaxTokens = %d, want 500", out.MaxTokens)
		}
		if out.Temperature == nil || *out.Temperature != 0.5 {
			t.Errorf("Temperature = %v, want 0.5", out.Temperature)
		}
		if out.Headers["x-test"] != "1" {
			t.Errorf("Headers not preserved: %v", out.Headers)
		}
		assertJSONEqual(t, out.RawBody, json.RawMessage(`{
			"model":"gpt-4",
			"messages":[
				{"role":"system","content":"Be concise"},
				{"role":"user","content":"Hello"}
			],
			"max_tokens":500,
			"temperature":0.5,
			"top_p":0.9,
			"tools":[{"type":"function","function":{"name":"get_weather"}}],
			"tool_choice":"auto"
		}`))
		assertJSONEqual(t, out.MappedBody, out.RawBody)
	})

	t.Run("request malformed input yields null messages", func(t *testing.T) {
		req := mustReq(t, `{"model":"gpt-4","input":123}`)
		out, err := tr.TranslateRequest(ctx, req)
		if err != nil {
			t.Fatalf("TranslateRequest: %v", err)
		}
		assertJSONEqual(t, out.RawBody, json.RawMessage(`{"model":"gpt-4","messages":null}`))
	})

	t.Run("request preserves id and user id", func(t *testing.T) {
		req := mustReq(t, `{"model":"gpt-4","input":"hi"}`)
		out, err := tr.TranslateRequest(ctx, req)
		if err != nil {
			t.Fatalf("TranslateRequest: %v", err)
		}
		if out.ID != req.ID {
			t.Error("ID not preserved")
		}
		if out.UserID != nil {
			t.Error("expected nil UserID")
		}
	})

	t.Run("response nil", func(t *testing.T) {
		if _, err := tr.TranslateResponse(ctx, nil); !errors.Is(err, errNilResponse) {
			t.Errorf("expected errNilResponse, got %v", err)
		}
	})

	t.Run("response invalid json", func(t *testing.T) {
		if _, err := tr.TranslateResponse(ctx, mustResp(t, `{`)); err == nil || !strings.Contains(err.Error(), "unmarshal response") {
			t.Errorf("expected unmarshal response error, got %v", err)
		}
	})

	t.Run("response with content tool_calls and usage", func(t *testing.T) {
		resp := mustResp(t, `{
			"id":"cmpl-1",
			"model":"gpt-4",
			"choices":[{"message":{"role":"assistant","content":"hi","tool_calls":[{"id":"c1","type":"function","function":{"name":"get_weather","arguments":"{\"loc\":\"NYC\"}"}}]}}],
			"usage":{"prompt_tokens":10,"completion_tokens":5}
		}`)
		out, err := tr.TranslateResponse(ctx, resp)
		if err != nil {
			t.Fatalf("TranslateResponse: %v", err)
		}
		assertJSONEqual(t, out.Body, json.RawMessage(`{
			"id":"cmpl-1","object":"response","model":"gpt-4",
			"output":[{"role":"assistant","content":"hi","tool_calls":[{"id":"c1","type":"function","function":{"name":"get_weather","arguments":"{\"loc\":\"NYC\"}"}}]}],
			"usage":{"input_tokens":10,"output_tokens":5}
		}`))
		if out.StatusCode != 200 || out.Headers["x-test"] != "1" {
			t.Errorf("status/headers not preserved: %d %v", out.StatusCode, out.Headers)
		}
	})

	t.Run("response tool call without function yields null name", func(t *testing.T) {
		resp := mustResp(t, `{"choices":[{"message":{"content":null,"tool_calls":[{"id":"c1"}]}}]}`)
		out, err := tr.TranslateResponse(ctx, resp)
		if err != nil {
			t.Fatalf("TranslateResponse: %v", err)
		}
		assertJSONEqual(t, out.Body, json.RawMessage(`{
			"id":"","object":"response","model":"",
			"output":[{"role":"assistant","content":null,"tool_calls":[{"id":"c1","type":"function","function":{"name":null,"arguments":null}}]}]
		}`))
	})

	t.Run("response without choices yields empty output", func(t *testing.T) {
		resp := mustResp(t, `{"id":"x","model":"gpt-4"}`)
		out, err := tr.TranslateResponse(ctx, resp)
		if err != nil {
			t.Fatalf("TranslateResponse: %v", err)
		}
		assertJSONEqual(t, out.Body, json.RawMessage(`{"id":"x","object":"response","model":"gpt-4","output":[]}`))
	})

	t.Run("stream chunk nil", func(t *testing.T) {
		if _, err := tr.TranslateStreamChunk(ctx, nil); !errors.Is(err, errNilChunk) {
			t.Errorf("expected errNilChunk, got %v", err)
		}
	})

	t.Run("stream chunk done", func(t *testing.T) {
		out, err := tr.TranslateStreamChunk(ctx, mustChunk(t, "", "[DONE]", false, nil))
		if err != nil {
			t.Fatalf("TranslateStreamChunk: %v", err)
		}
		assertChunk(t, out, "response.completed", `{}`, true, "")
	})

	t.Run("stream chunk invalid json", func(t *testing.T) {
		if _, err := tr.TranslateStreamChunk(ctx, mustChunk(t, "", `{`, false, nil)); err == nil || !strings.Contains(err.Error(), "unmarshal chunk") {
			t.Errorf("expected unmarshal chunk error, got %v", err)
		}
	})

	t.Run("stream chunk no choices passes through", func(t *testing.T) {
		ch := mustChunk(t, "chunk", `{"choices":[]}`, false, nil)
		out, err := tr.TranslateStreamChunk(ctx, ch)
		if err != nil {
			t.Fatalf("TranslateStreamChunk: %v", err)
		}
		if out != ch {
			t.Error("expected passthrough of same chunk")
		}
	})

	t.Run("stream chunk content delta", func(t *testing.T) {
		out, err := tr.TranslateStreamChunk(ctx, mustChunk(t, "chunk", `{"choices":[{"index":0,"delta":{"content":"hi"}}]}`, false, nil))
		if err != nil {
			t.Fatalf("TranslateStreamChunk: %v", err)
		}
		assertChunk(t, out, "chunk", `{"type":"response.output_text.delta","delta":"hi"}`, false, "")
	})

	t.Run("stream chunk finish reason", func(t *testing.T) {
		out, err := tr.TranslateStreamChunk(ctx, mustChunk(t, "chunk", `{"choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}`, false, nil))
		if err != nil {
			t.Fatalf("TranslateStreamChunk: %v", err)
		}
		assertChunk(t, out, "chunk", `{"type":"response.output_text.done","delta":""}`, true, "")
	})
}

// ---- ClaudeToOpenAI ----

func TestClaudeToOpenAI(t *testing.T) {
	ctx := context.Background()
	tr := NewClaudeToOpenAI()

	t.Run("source and target", func(t *testing.T) {
		if tr.Source() != engine.FormatAnthropic || tr.Target() != engine.FormatOpenAIChat {
			t.Errorf("unexpected formats: %v -> %v", tr.Source(), tr.Target())
		}
	})

	t.Run("request nil", func(t *testing.T) {
		if _, err := tr.TranslateRequest(ctx, nil); !errors.Is(err, errNilRequest) {
			t.Errorf("expected errNilRequest, got %v", err)
		}
	})

	t.Run("request invalid json", func(t *testing.T) {
		if _, err := tr.TranslateRequest(ctx, mustReq(t, `{`)); err == nil || !strings.Contains(err.Error(), "unmarshal request") {
			t.Errorf("expected unmarshal request error, got %v", err)
		}
	})

	t.Run("request full body", func(t *testing.T) {
		req := mustReq(t, `{
			"model":"claude-3-5-sonnet",
			"system":"You are helpful.",
			"messages":[
				{"role":"user","content":"hi"},
				{"role":"assistant","content":"hello"}
			],
			"max_tokens":1024,
			"temperature":0.7,
			"top_p":0.9,
			"stream":true,
			"stop_sequences":["\n\nHuman:"],
			"tools":[{"name":"get_weather","description":"W","input_schema":{"type":"object","properties":{"loc":{"type":"string"}}}}],
			"tool_choice":{"type":"tool","name":"get_weather"}
		}`)
		out, err := tr.TranslateRequest(ctx, req)
		if err != nil {
			t.Fatalf("TranslateRequest: %v", err)
		}
		if out.Format != engine.FormatOpenAIChat || out.Model != "claude-3-5-sonnet" {
			t.Errorf("format/model mismatch: %v %q", out.Format, out.Model)
		}
		if !out.Stream || out.MaxTokens != 1024 {
			t.Errorf("stream/maxTokens mismatch: %v %d", out.Stream, out.MaxTokens)
		}
		assertJSONEqual(t, out.RawBody, json.RawMessage(`{
			"model":"claude-3-5-sonnet",
			"messages":[
				{"role":"system","content":"You are helpful."},
				{"role":"user","content":"hi"},
				{"role":"assistant","content":"hello"}
			],
			"max_tokens":1024,
			"temperature":0.7,
			"top_p":0.9,
			"stream":true,
			"stop":["\n\nHuman:"],
			"tools":[{"type":"function","function":{"name":"get_weather","description":"W","parameters":{"type":"object","properties":{"loc":{"type":"string"}}}}}],
			"tool_choice":{"type":"function","function":{"name":"get_weather"}}
		}`))
	})

	t.Run("request unknown role skipped and empty system omitted", func(t *testing.T) {
		req := mustReq(t, `{"model":"m","messages":[{"role":"weird","content":"x"}]}`)
		out, err := tr.TranslateRequest(ctx, req)
		if err != nil {
			t.Fatalf("TranslateRequest: %v", err)
		}
		assertJSONEqual(t, out.RawBody, json.RawMessage(`{"model":"m","messages":[]}`))
	})

	t.Run("request user conversion error", func(t *testing.T) {
		req := mustReq(t, `{"model":"m","messages":[{"role":"user","content":123}]}`)
		if _, err := tr.TranslateRequest(ctx, req); err == nil || !strings.Contains(err.Error(), "user") {
			t.Errorf("expected user conversion error, got %v", err)
		}
	})

	t.Run("request assistant conversion error", func(t *testing.T) {
		req := mustReq(t, `{"model":"m","messages":[{"role":"assistant","content":123}]}`)
		if _, err := tr.TranslateRequest(ctx, req); err == nil || !strings.Contains(err.Error(), "assistant") {
			t.Errorf("expected assistant conversion error, got %v", err)
		}
	})

	t.Run("request message unmarshal error", func(t *testing.T) {
		req := mustReq(t, `{"model":"m","messages":[42]}`)
		if _, err := tr.TranslateRequest(ctx, req); err == nil || !strings.Contains(err.Error(), "unmarshal message") {
			t.Errorf("expected unmarshal message error, got %v", err)
		}
	})

	t.Run("response nil", func(t *testing.T) {
		if _, err := tr.TranslateResponse(ctx, nil); !errors.Is(err, errNilResponse) {
			t.Errorf("expected errNilResponse, got %v", err)
		}
	})

	t.Run("response invalid json", func(t *testing.T) {
		if _, err := tr.TranslateResponse(ctx, mustResp(t, `{`)); err == nil || !strings.Contains(err.Error(), "unmarshal response") {
			t.Errorf("expected unmarshal response error, got %v", err)
		}
	})

	t.Run("response full with stop mapping", func(t *testing.T) {
		resp := mustResp(t, `{
			"id":"msg_1","object":"chat.completion","model":"claude-3-5-sonnet",
			"choices":[{"index":0,"message":{"role":"assistant","content":"hi","tool_calls":[{"id":"call_1","type":"function","function":{"name":"get_weather","arguments":"{\"loc\":\"NYC\"}"}}]},"finish_reason":"tool_calls"}],
			"usage":{"prompt_tokens":10,"completion_tokens":5}
		}`)
		out, err := tr.TranslateResponse(ctx, resp)
		if err != nil {
			t.Fatalf("TranslateResponse: %v", err)
		}
		assertJSONEqual(t, out.Body, json.RawMessage(`{
			"id":"msg_1","type":"message","role":"assistant",
			"content":[
				{"type":"text","text":"hi"},
				{"type":"tool_use","id":"call_1","name":"get_weather","input":{"loc":"NYC"}}
			],
			"model":"claude-3-5-sonnet",
			"stop_reason":"tool_use",
			"usage":{"input_tokens":10,"output_tokens":5}
		}`))
	})

	t.Run("response tool call id falls back to name", func(t *testing.T) {
		resp := mustResp(t, `{"choices":[{"message":{"content":null,"tool_calls":[{"type":"function","function":{"name":"get_weather","arguments":"{}"}}]},"finish_reason":"stop"}]}`)
		out, err := tr.TranslateResponse(ctx, resp)
		if err != nil {
			t.Fatalf("TranslateResponse: %v", err)
		}
		assertJSONEqual(t, out.Body, json.RawMessage(`{
			"id":"","type":"message","role":"assistant",
			"content":[{"type":"tool_use","id":"get_weather","name":"get_weather","input":{}}],
			"model":"","stop_reason":"end_turn"
		}`))
	})

	t.Run("response without choices yields null content", func(t *testing.T) {
		resp := mustResp(t, `{"id":"x","model":"m"}`)
		out, err := tr.TranslateResponse(ctx, resp)
		if err != nil {
			t.Fatalf("TranslateResponse: %v", err)
		}
		assertJSONEqual(t, out.Body, json.RawMessage(`{
			"id":"x","type":"message","role":"assistant","content":null,"model":"m"
		}`))
	})

	t.Run("stream chunk nil", func(t *testing.T) {
		if _, err := tr.TranslateStreamChunk(ctx, nil); !errors.Is(err, errNilChunk) {
			t.Errorf("expected errNilChunk, got %v", err)
		}
	})

	t.Run("stream chunk done via DONE marker", func(t *testing.T) {
		out, err := tr.TranslateStreamChunk(ctx, mustChunk(t, "", "[DONE]", false, nil))
		if err != nil {
			t.Fatalf("TranslateStreamChunk: %v", err)
		}
		assertChunk(t, out, "message_stop", `{}`, true, "")
	})

	t.Run("stream chunk done via IsFinal", func(t *testing.T) {
		out, err := tr.TranslateStreamChunk(ctx, mustChunk(t, "chunk", `{"choices":[]}`, true, nil))
		if err != nil {
			t.Fatalf("TranslateStreamChunk: %v", err)
		}
		assertChunk(t, out, "message_stop", `{}`, true, "")
	})

	t.Run("stream chunk invalid json", func(t *testing.T) {
		if _, err := tr.TranslateStreamChunk(ctx, mustChunk(t, "", `{`, false, nil)); err == nil || !strings.Contains(err.Error(), "unmarshal chunk") {
			t.Errorf("expected unmarshal chunk error, got %v", err)
		}
	})

	t.Run("stream chunk no choices passes through", func(t *testing.T) {
		ch := mustChunk(t, "chunk", `{"choices":[]}`, false, nil)
		out, err := tr.TranslateStreamChunk(ctx, ch)
		if err != nil {
			t.Fatalf("TranslateStreamChunk: %v", err)
		}
		if out != ch {
			t.Error("expected passthrough of same chunk")
		}
	})

	t.Run("stream chunk finish reason stop", func(t *testing.T) {
		out, err := tr.TranslateStreamChunk(ctx, mustChunk(t, "chunk", `{"choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}`, false, nil))
		if err != nil {
			t.Fatalf("TranslateStreamChunk: %v", err)
		}
		assertChunk(t, out, "message_delta", `{"stop_reason":"end_turn"}`, true, "")
	})

	t.Run("stream chunk finish reason tool_calls", func(t *testing.T) {
		out, err := tr.TranslateStreamChunk(ctx, mustChunk(t, "chunk", `{"choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}]}`, false, nil))
		if err != nil {
			t.Fatalf("TranslateStreamChunk: %v", err)
		}
		assertChunk(t, out, "message_delta", `{"stop_reason":"tool_use"}`, true, "")
	})

	t.Run("stream chunk finish reason length", func(t *testing.T) {
		out, err := tr.TranslateStreamChunk(ctx, mustChunk(t, "chunk", `{"choices":[{"index":0,"delta":{},"finish_reason":"length"}]}`, false, nil))
		if err != nil {
			t.Fatalf("TranslateStreamChunk: %v", err)
		}
		assertChunk(t, out, "message_delta", `{"stop_reason":"max_tokens"}`, true, "")
	})

	t.Run("stream chunk finish reason default passthrough", func(t *testing.T) {
		out, err := tr.TranslateStreamChunk(ctx, mustChunk(t, "chunk", `{"choices":[{"index":0,"delta":{},"finish_reason":"content_filter"}]}`, false, nil))
		if err != nil {
			t.Fatalf("TranslateStreamChunk: %v", err)
		}
		assertChunk(t, out, "message_delta", `{"stop_reason":"content_filter"}`, true, "")
	})

	t.Run("stream chunk content delta", func(t *testing.T) {
		out, err := tr.TranslateStreamChunk(ctx, mustChunk(t, "chunk", `{"choices":[{"index":0,"delta":{"content":"hi"}}]}`, false, nil))
		if err != nil {
			t.Fatalf("TranslateStreamChunk: %v", err)
		}
		assertChunk(t, out, "content_block_delta", `{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"hi"}}`, false, "")
	})

	t.Run("stream chunk tool call delta", func(t *testing.T) {
		out, err := tr.TranslateStreamChunk(ctx, mustChunk(t, "chunk", `{"choices":[{"index":0,"delta":{"tool_calls":[{"id":"c1","type":"function","function":{"name":"f","arguments":""}}]}}]}`, false, nil))
		if err != nil {
			t.Fatalf("TranslateStreamChunk: %v", err)
		}
		assertChunk(t, out, "content_block_start", `{"type":"content_block_start","index":0,"content_block":{"type":"tool_use","id":"c1","name":"f","input":{}}}`, false, "")
	})

	t.Run("stream chunk role-only delta passes through", func(t *testing.T) {
		ch := mustChunk(t, "chunk", `{"choices":[{"index":0,"delta":{"role":"assistant"}}]}`, false, nil)
		out, err := tr.TranslateStreamChunk(ctx, ch)
		if err != nil {
			t.Fatalf("TranslateStreamChunk: %v", err)
		}
		if out != ch {
			t.Error("expected passthrough of same chunk")
		}
	})

	t.Run("stream chunk empty delta passes through", func(t *testing.T) {
		ch := mustChunk(t, "chunk", `{"choices":[{"index":0,"delta":{}}]}`, false, nil)
		out, err := tr.TranslateStreamChunk(ctx, ch)
		if err != nil {
			t.Fatalf("TranslateStreamChunk: %v", err)
		}
		if out != ch {
			t.Error("expected passthrough of same chunk")
		}
	})

	t.Run("response choice without finish reason maps to end_turn", func(t *testing.T) {
		resp := mustResp(t, `{"id":"x","model":"m","choices":[{"index":0,"message":{"role":"assistant","content":"hi"}}]}`)
		out, err := tr.TranslateResponse(ctx, resp)
		if err != nil {
			t.Fatalf("TranslateResponse: %v", err)
		}
		assertJSONEqual(t, out.Body, json.RawMessage(`{
			"id":"x","type":"message","role":"assistant",
			"content":[{"type":"text","text":"hi"}],
			"model":"m","stop_reason":"end_turn"
		}`))
	})
}

func TestMapOpenAIStop(t *testing.T) {
	tests := []struct {
		name string
		in   *string
		want string
	}{
		{"nil maps to end_turn", nil, "end_turn"},
		{"stop maps to end_turn", strPtr("stop"), "end_turn"},
		{"tool_calls maps to tool_use", strPtr("tool_calls"), "tool_use"},
		{"length maps to max_tokens", strPtr("length"), "max_tokens"},
		{"unknown passes through", strPtr("content_filter"), "content_filter"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := mapOpenAIStop(tc.in); got != tc.want {
				t.Errorf("mapOpenAIStop(%v) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func strPtr(s string) *string { return &s }

// ---- GeminiToOpenAI ----

func TestGeminiToOpenAI(t *testing.T) {
	ctx := context.Background()
	tr := NewGeminiToOpenAI()

	t.Run("source and target", func(t *testing.T) {
		if tr.Source() != engine.FormatGemini || tr.Target() != engine.FormatOpenAIChat {
			t.Errorf("unexpected formats: %v -> %v", tr.Source(), tr.Target())
		}
	})

	t.Run("request nil", func(t *testing.T) {
		if _, err := tr.TranslateRequest(ctx, nil); !errors.Is(err, errNilRequest) {
			t.Errorf("expected errNilRequest, got %v", err)
		}
	})

	t.Run("request invalid json", func(t *testing.T) {
		if _, err := tr.TranslateRequest(ctx, mustReq(t, `{`)); err == nil || !strings.Contains(err.Error(), "unmarshal") {
			t.Errorf("expected unmarshal error, got %v", err)
		}
	})

	t.Run("request full body", func(t *testing.T) {
		req := mustReq(t, `{
			"contents":[
				{"role":"user","parts":[{"text":"hi"}]},
				{"role":"model","parts":[{"text":"hello"},{"text":"world"}]},
				{"role":"function","parts":[{"text":"42"}]}
			],
			"systemInstruction":{"parts":[{"text":"sys"},{"text":"stem"}]},
			"generationConfig":{"temperature":0.5,"topP":0.9,"maxOutputTokens":256},
			"tools":[{"functionDeclarations":[{"name":"get_weather","description":"W","parameters":{"type":"object"}}]}]
		}`)
		out, err := tr.TranslateRequest(ctx, req)
		if err != nil {
			t.Fatalf("TranslateRequest: %v", err)
		}
		if out.Format != engine.FormatOpenAIChat || out.Model != "gpt-4" {
			t.Errorf("format/model mismatch: %v %q", out.Format, out.Model)
		}
		if !out.Stream {
			t.Error("expected Stream preserved")
		}
		assertJSONEqual(t, out.RawBody, json.RawMessage(`{
			"model":"gpt-4",
			"messages":[
				{"role":"system","content":"sysstem"},
				{"role":"user","content":"hi"},
				{"role":"assistant","content":"helloworld"},
				{"role":"tool","content":"42"}
			],
			"temperature":0.5,
			"top_p":0.9,
			"max_tokens":256,
			"tools":[{"type":"function","function":{"name":"get_weather","description":"W","parameters":{"type":"object"}}}]
		}`))
	})

	t.Run("request empty system text omits system message", func(t *testing.T) {
		req := mustReq(t, `{"contents":[{"role":"user","parts":[{"text":"hi"}]}],"systemInstruction":{"text":"hi"}}`)
		out, err := tr.TranslateRequest(ctx, req)
		if err != nil {
			t.Fatalf("TranslateRequest: %v", err)
		}
		assertJSONEqual(t, out.RawBody, json.RawMessage(`{"model":"gpt-4","messages":[{"role":"user","content":"hi"}]}`))
	})

	t.Run("request malformed content skipped and empty text skipped", func(t *testing.T) {
		req := mustReq(t, `{"contents":[42,{"role":"user","parts":[{"text":""},{"text":"keep"}]},{"role":"user","parts":[{"functionCall":{"name":"f"}}]}]}`)
		out, err := tr.TranslateRequest(ctx, req)
		if err != nil {
			t.Fatalf("TranslateRequest: %v", err)
		}
		assertJSONEqual(t, out.RawBody, json.RawMessage(`{"model":"gpt-4","messages":[{"role":"user","content":"keep"}]}`))
	})

	t.Run("request generation config partial", func(t *testing.T) {
		req := mustReq(t, `{"contents":[{"role":"user","parts":[{"text":"hi"}]}],"generationConfig":{"temperature":0.1}}`)
		out, err := tr.TranslateRequest(ctx, req)
		if err != nil {
			t.Fatalf("TranslateRequest: %v", err)
		}
		assertJSONEqual(t, out.RawBody, json.RawMessage(`{"model":"gpt-4","messages":[{"role":"user","content":"hi"}],"temperature":0.1}`))
	})

	t.Run("response nil", func(t *testing.T) {
		if _, err := tr.TranslateResponse(ctx, nil); !errors.Is(err, errNilResponse) {
			t.Errorf("expected errNilResponse, got %v", err)
		}
	})

	t.Run("response invalid json", func(t *testing.T) {
		if _, err := tr.TranslateResponse(ctx, mustResp(t, `{`)); err == nil || !strings.Contains(err.Error(), "unmarshal response") {
			t.Errorf("expected unmarshal response error, got %v", err)
		}
	})

	t.Run("response full with tool calls", func(t *testing.T) {
		resp := mustResp(t, `{
			"choices":[{"message":{"role":"assistant","content":"hi","tool_calls":[{"id":"c1","type":"function","function":{"name":"get_weather","arguments":"{\"loc\":\"NYC\"}"}}]},"finish_reason":"tool_calls"}],
			"usage":{"prompt_tokens":10,"completion_tokens":5,"total_tokens":15}
		}`)
		out, err := tr.TranslateResponse(ctx, resp)
		if err != nil {
			t.Fatalf("TranslateResponse: %v", err)
		}
		assertJSONEqual(t, out.Body, json.RawMessage(`{
			"candidates":[{
				"content":{"parts":[{"text":"hi"},{"functionCall":{"name":"get_weather","args":{"loc":"NYC"}}}],"role":"model"},
				"finishReason":"TOOL_CALLS"
			}],
			"usageMetadata":{"promptTokenCount":10,"candidatesTokenCount":5,"totalTokenCount":15}
		}`))
	})

	t.Run("response without choices defaults to STOP", func(t *testing.T) {
		resp := mustResp(t, `{"id":"x"}`)
		out, err := tr.TranslateResponse(ctx, resp)
		if err != nil {
			t.Fatalf("TranslateResponse: %v", err)
		}
		assertJSONEqual(t, out.Body, json.RawMessage(`{
			"candidates":[{"content":{"parts":[],"role":"model"},"finishReason":"STOP"}]
		}`))
	})

	t.Run("response finish reason content_filter", func(t *testing.T) {
		resp := mustResp(t, `{"choices":[{"message":{"content":null},"finish_reason":"content_filter"}]}`)
		out, err := tr.TranslateResponse(ctx, resp)
		if err != nil {
			t.Fatalf("TranslateResponse: %v", err)
		}
		assertJSONEqual(t, out.Body, json.RawMessage(`{
			"candidates":[{"content":{"parts":[],"role":"model"},"finishReason":"SAFETY"}]
		}`))
	})

	t.Run("stream chunk nil", func(t *testing.T) {
		if _, err := tr.TranslateStreamChunk(ctx, nil); !errors.Is(err, errNilChunk) {
			t.Errorf("expected errNilChunk, got %v", err)
		}
	})

	t.Run("stream chunk done", func(t *testing.T) {
		out, err := tr.TranslateStreamChunk(ctx, mustChunk(t, "", "[DONE]", false, nil))
		if err != nil {
			t.Fatalf("TranslateStreamChunk: %v", err)
		}
		assertChunk(t, out, "candidate.finished", ``, true, "")
	})

	t.Run("stream chunk invalid json", func(t *testing.T) {
		if _, err := tr.TranslateStreamChunk(ctx, mustChunk(t, "", `{`, false, nil)); err == nil || !strings.Contains(err.Error(), "unmarshal chunk") {
			t.Errorf("expected unmarshal chunk error, got %v", err)
		}
	})

	t.Run("stream chunk no choices passes through", func(t *testing.T) {
		ch := mustChunk(t, "chunk", `{"choices":[]}`, false, nil)
		out, err := tr.TranslateStreamChunk(ctx, ch)
		if err != nil {
			t.Fatalf("TranslateStreamChunk: %v", err)
		}
		if out != ch {
			t.Error("expected passthrough of same chunk")
		}
	})

	t.Run("stream chunk content delta", func(t *testing.T) {
		out, err := tr.TranslateStreamChunk(ctx, mustChunk(t, "chunk", `{"choices":[{"index":0,"delta":{"content":"hi"}}]}`, false, nil))
		if err != nil {
			t.Fatalf("TranslateStreamChunk: %v", err)
		}
		assertChunk(t, out, "chunk", `{"candidates":[{"content":{"parts":[{"text":"hi"}],"role":"model"}}]}`, false, "")
	})

	t.Run("stream chunk finish variants", func(t *testing.T) {
		cases := []struct{ reason, want string }{
			{"stop", "STOP"},
			{"length", "MAX_TOKENS"},
			{"tool_calls", "TOOL_CALLS"},
			{"content_filter", "SAFETY"},
			{"weird", "STOP"},
		}
		for _, c := range cases {
			t.Run(c.reason, func(t *testing.T) {
				out, err := tr.TranslateStreamChunk(ctx, mustChunk(t, "chunk", `{"choices":[{"index":0,"delta":{},"finish_reason":"`+c.reason+`"}]}`, false, nil))
				if err != nil {
					t.Fatalf("TranslateStreamChunk: %v", err)
				}
				assertChunk(t, out, "chunk", `{"candidates":[{"content":{"parts":[{"text":""}],"role":"model"},"finishReason":"`+c.want+`"}]}`, true, "")
			})
		}
	})
}

// ---- OpenAIToClaude ----

func TestOpenAIToClaude(t *testing.T) {
	ctx := context.Background()
	tr := NewOpenAIToClaude()

	t.Run("source and target", func(t *testing.T) {
		if tr.Source() != engine.FormatOpenAIChat || tr.Target() != engine.FormatAnthropic {
			t.Errorf("unexpected formats: %v -> %v", tr.Source(), tr.Target())
		}
	})

	t.Run("request nil", func(t *testing.T) {
		if _, err := tr.TranslateRequest(ctx, nil); !errors.Is(err, errNilRequest) {
			t.Errorf("expected errNilRequest, got %v", err)
		}
	})

	t.Run("request invalid json", func(t *testing.T) {
		if _, err := tr.TranslateRequest(ctx, mustReq(t, `{`)); err == nil || !strings.Contains(err.Error(), "unmarshal request") {
			t.Errorf("expected unmarshal request error, got %v", err)
		}
	})

	// RED probe (D2): assistant and tool messages are routed through
	// openaiAssistantToClaude / openaiToolToClaude, which base64-encode their
	// block arrays, so this full-body translation fails until D2 is fixed.
	t.Run("request full body", func(t *testing.T) {
		req := mustReq(t, `{
			"model":"gpt-4",
			"max_tokens":512,
			"temperature":0.3,
			"top_p":0.8,
			"stream":true,
			"messages":[
				{"role":"system","content":"You are helpful."},
				{"role":"system","content":"Be concise."},
				{"role":"user","content":"hi"},
				{"role":"assistant","content":"hello"},
				{"role":"tool","tool_call_id":"call_1","content":"42"}
			],
			"stop":["\n\nHuman:"],
			"tools":[{"type":"function","function":{"name":"get_weather","description":"W","parameters":{"type":"object"}}}],
			"tool_choice":"auto"
		}`)
		out, err := tr.TranslateRequest(ctx, req)
		if err != nil {
			t.Fatalf("TranslateRequest: %v", err)
		}
		if out.Format != engine.FormatAnthropic || out.Model != "gpt-4" {
			t.Errorf("format/model mismatch: %v %q", out.Format, out.Model)
		}
		if !out.Stream || out.MaxTokens != 512 {
			t.Errorf("stream/maxTokens mismatch: %v %d", out.Stream, out.MaxTokens)
		}
		assertJSONEqual(t, out.RawBody, json.RawMessage(`{
			"model":"gpt-4",
			"max_tokens":512,
			"messages":[
				{"role":"user","content":"hi"},
				{"role":"assistant","content":[{"type":"text","text":"hello"}]},
				{"role":"user","content":[{"type":"tool_result","tool_use_id":"call_1","content":"42"}]}
			],
			"system":"You are helpful.\nBe concise.",
			"temperature":0.3,
			"top_p":0.8,
			"stream":true,
			"stop_sequences":["\n\nHuman:"],
			"tools":[{"name":"get_weather","description":"W","input_schema":{"type":"object"}}],
			"tool_choice":{"type":"auto"}
		}`))
	})

	t.Run("request unknown role skipped and non-string system ignored", func(t *testing.T) {
		req := mustReq(t, `{
			"model":"gpt-4",
			"max_tokens":1,
			"messages":[
				{"role":"system","content":["array"]},
				{"role":"weird","content":"x"},
				{"role":"user","content":"hi"}
			]
		}`)
		out, err := tr.TranslateRequest(ctx, req)
		if err != nil {
			t.Fatalf("TranslateRequest: %v", err)
		}
		assertJSONEqual(t, out.RawBody, json.RawMessage(`{
			"model":"gpt-4","max_tokens":1,
			"messages":[{"role":"user","content":"hi"}]
		}`))
	})

	t.Run("request user conversion error", func(t *testing.T) {
		req := mustReq(t, `{"model":"gpt-4","messages":[{"role":"user","content":123}]}`)
		if _, err := tr.TranslateRequest(ctx, req); err == nil || !strings.Contains(err.Error(), "user") {
			t.Errorf("expected user conversion error, got %v", err)
		}
	})

	t.Run("request message unmarshal error", func(t *testing.T) {
		req := mustReq(t, `{"model":"gpt-4","messages":[42]}`)
		if _, err := tr.TranslateRequest(ctx, req); err == nil || !strings.Contains(err.Error(), "unmarshal message") {
			t.Errorf("expected unmarshal message error, got %v", err)
		}
	})

	t.Run("request stop null yields null stop_sequences", func(t *testing.T) {
		req := mustReq(t, `{"model":"gpt-4","messages":[{"role":"user","content":"hi"}],"stop":null}`)
		out, err := tr.TranslateRequest(ctx, req)
		if err != nil {
			t.Fatalf("TranslateRequest: %v", err)
		}
		assertJSONEqual(t, out.RawBody, json.RawMessage(`{
			"model":"gpt-4","max_tokens":0,
			"messages":[{"role":"user","content":"hi"}],
			"stop_sequences":null
		}`))
	})

	t.Run("response nil", func(t *testing.T) {
		if _, err := tr.TranslateResponse(ctx, nil); !errors.Is(err, errNilResponse) {
			t.Errorf("expected errNilResponse, got %v", err)
		}
	})

	t.Run("response invalid json", func(t *testing.T) {
		if _, err := tr.TranslateResponse(ctx, mustResp(t, `{`)); err == nil || !strings.Contains(err.Error(), "unmarshal response") {
			t.Errorf("expected unmarshal response error, got %v", err)
		}
	})

	t.Run("response full with thinking and tool_use", func(t *testing.T) {
		resp := mustResp(t, `{
			"id":"msg_1","type":"message","role":"assistant","model":"claude-3-5-sonnet","stop_reason":"tool_use",
			"content":[
				{"type":"text","text":"Let me check"},
				{"type":"thinking","thinking":"hmm"},
				{"type":"tool_use","id":"tu_1","name":"get_weather","input":{"loc":"NYC"}}
			],
			"usage":{"input_tokens":10,"output_tokens":5}
		}`)
		out, err := tr.TranslateResponse(ctx, resp)
		if err != nil {
			t.Fatalf("TranslateResponse: %v", err)
		}
		assertJSONEqual(t, out.Body, json.RawMessage(`{
			"id":"chatcmpl-msg_1","object":"chat.completion","model":"claude-3-5-sonnet",
			"choices":[{
				"index":0,
				"message":{
					"role":"assistant",
					"content":"Let me check[thinking] hmm",
					"tool_calls":[{"id":"tu_1","type":"function","function":{"name":"get_weather","arguments":"{\"loc\":\"NYC\"}"}}]
				},
				"finish_reason":"tool_calls"
			}],
			"usage":{"prompt_tokens":10,"completion_tokens":5,"total_tokens":15}
		}`))
	})

	t.Run("response content with invalid block is skipped", func(t *testing.T) {
		resp := mustResp(t, `{"id":"m","type":"message","role":"assistant","model":"m","stop_reason":"stop","content":[42,{"type":"text","text":"ok"}]}`)
		out, err := tr.TranslateResponse(ctx, resp)
		if err != nil {
			t.Fatalf("TranslateResponse: %v", err)
		}
		assertJSONEqual(t, out.Body, json.RawMessage(`{
			"id":"chatcmpl-m","object":"chat.completion","model":"m",
			"choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}]
		}`))
	})

	t.Run("response stop reason variants", func(t *testing.T) {
		cases := []struct{ in, want string }{
			{"end_turn", "stop"},
			{"max_tokens", "length"},
			{"weird", "stop"},
		}
		for _, c := range cases {
			t.Run(c.in, func(t *testing.T) {
				resp := mustResp(t, `{"id":"m","type":"message","role":"assistant","model":"m","stop_reason":"`+c.in+`","content":[{"type":"text","text":"hi"}]}`)
				out, err := tr.TranslateResponse(ctx, resp)
				if err != nil {
					t.Fatalf("TranslateResponse: %v", err)
				}
				assertJSONEqual(t, out.Body, json.RawMessage(`{
					"id":"chatcmpl-m","object":"chat.completion","model":"m",
					"choices":[{"index":0,"message":{"role":"assistant","content":"hi"},"finish_reason":"`+c.want+`"}]
				}`))
			})
		}
	})

	t.Run("stream chunk nil", func(t *testing.T) {
		if _, err := tr.TranslateStreamChunk(ctx, nil); !errors.Is(err, errNilChunk) {
			t.Errorf("expected errNilChunk, got %v", err)
		}
	})

	t.Run("stream chunk error with message_stop becomes done", func(t *testing.T) {
		ch := mustChunk(t, "message_stop", `{}`, false, errors.New("boom"))
		out, err := tr.TranslateStreamChunk(ctx, ch)
		if err != nil {
			t.Fatalf("TranslateStreamChunk: %v", err)
		}
		assertChunk(t, out, "done", `[DONE]`, true, "")
	})

	t.Run("stream chunk error passthrough", func(t *testing.T) {
		ch := mustChunk(t, "error", `{"error":"x"}`, false, errors.New("boom"))
		out, err := tr.TranslateStreamChunk(ctx, ch)
		if err != nil {
			t.Fatalf("TranslateStreamChunk: %v", err)
		}
		assertChunk(t, out, "error", `{"error":"x"}`, false, "boom")
	})

	t.Run("stream chunk IsFinal becomes done", func(t *testing.T) {
		out, err := tr.TranslateStreamChunk(ctx, mustChunk(t, "chunk", `{"x":1}`, true, nil))
		if err != nil {
			t.Fatalf("TranslateStreamChunk: %v", err)
		}
		assertChunk(t, out, "done", `[DONE]`, true, "")
	})

	t.Run("stream chunk invalid json", func(t *testing.T) {
		if _, err := tr.TranslateStreamChunk(ctx, mustChunk(t, "", `{`, false, nil)); err == nil || !strings.Contains(err.Error(), "unmarshal chunk") {
			t.Errorf("expected unmarshal chunk error, got %v", err)
		}
	})

	t.Run("stream chunk text delta", func(t *testing.T) {
		out, err := tr.TranslateStreamChunk(ctx, mustChunk(t, "content_block_delta", `{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"hi"}}`, false, nil))
		if err != nil {
			t.Fatalf("TranslateStreamChunk: %v", err)
		}
		assertChunk(t, out, "chunk", `{"choices":[{"index":0,"delta":{"content":"hi"}}]}`, false, "")
	})

	t.Run("stream chunk non-text delta passes through", func(t *testing.T) {
		ch := mustChunk(t, "content_block_delta", `{"type":"content_block_delta","index":0,"delta":{"type":"thinking_delta","thinking":"x"}}`, false, nil)
		out, err := tr.TranslateStreamChunk(ctx, ch)
		if err != nil {
			t.Fatalf("TranslateStreamChunk: %v", err)
		}
		if out != ch {
			t.Error("expected passthrough of same chunk")
		}
	})

	// RED probe (D3): production embeds the marshaled tool_calls slice as a
	// plain []byte inside the delta map, so encoding/json renders it as a
	// base64 STRING. OpenAI stream deltas require tool_calls to be a JSON
	// array; assertion stays failing until fixed.
	t.Run("stream chunk tool_use start", func(t *testing.T) {
		out, err := tr.TranslateStreamChunk(ctx, mustChunk(t, "content_block_start", `{"type":"content_block_start","index":0,"content_block":{"type":"tool_use","id":"tu_1","name":"get_weather"}}`, false, nil))
		if err != nil {
			t.Fatalf("TranslateStreamChunk: %v", err)
		}
		assertChunk(t, out, "chunk", `{"choices":[{"index":0,"delta":{"role":"assistant","content":null,"tool_calls":[{"id":"tu_1","type":"function","function":{"name":"get_weather","arguments":""}}]}}]}`, false, "")
	})

	t.Run("stream chunk non-tool start passes through", func(t *testing.T) {
		ch := mustChunk(t, "content_block_start", `{"type":"content_block_start","index":0,"content_block":{"type":"text","text":"x"}}`, false, nil)
		out, err := tr.TranslateStreamChunk(ctx, ch)
		if err != nil {
			t.Fatalf("TranslateStreamChunk: %v", err)
		}
		if out != ch {
			t.Error("expected passthrough of same chunk")
		}
	})

	t.Run("stream chunk message_delta stop", func(t *testing.T) {
		out, err := tr.TranslateStreamChunk(ctx, mustChunk(t, "message_delta", `{"type":"message_delta","delta":{"stop_reason":"end_turn"}}`, false, nil))
		if err != nil {
			t.Fatalf("TranslateStreamChunk: %v", err)
		}
		assertChunk(t, out, "chunk", `{"choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}`, true, "")
	})

	// RED probe (D4): production passes the raw Claude stop_reason through,
	// emitting the invalid OpenAI finish_reason "tool_use". The OpenAI wire
	// format requires "tool_calls"; assertion stays failing until fixed.
	t.Run("stream chunk message_delta tool_use maps to tool_calls", func(t *testing.T) {
		out, err := tr.TranslateStreamChunk(ctx, mustChunk(t, "message_delta", `{"type":"message_delta","delta":{"stop_reason":"tool_use"}}`, false, nil))
		if err != nil {
			t.Fatalf("TranslateStreamChunk: %v", err)
		}
		assertChunk(t, out, "chunk", `{"choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}]}`, true, "")
	})

	// RED probe (D4): production passes "max_tokens" through; OpenAI requires
	// "length".
	t.Run("stream chunk message_delta max_tokens maps to length", func(t *testing.T) {
		out, err := tr.TranslateStreamChunk(ctx, mustChunk(t, "message_delta", `{"type":"message_delta","delta":{"stop_reason":"max_tokens"}}`, false, nil))
		if err != nil {
			t.Fatalf("TranslateStreamChunk: %v", err)
		}
		assertChunk(t, out, "chunk", `{"choices":[{"index":0,"delta":{},"finish_reason":"length"}]}`, true, "")
	})

	t.Run("stream chunk message_delta without stop_reason passes through", func(t *testing.T) {
		ch := mustChunk(t, "message_delta", `{"type":"message_delta","delta":{}}`, false, nil)
		out, err := tr.TranslateStreamChunk(ctx, ch)
		if err != nil {
			t.Fatalf("TranslateStreamChunk: %v", err)
		}
		if out != ch {
			t.Error("expected passthrough of same chunk")
		}
	})

	t.Run("stream chunk error event", func(t *testing.T) {
		out, err := tr.TranslateStreamChunk(ctx, mustChunk(t, "error", `{"type":"error","error":{"message":"boom"}}`, false, nil))
		if err != nil {
			t.Fatalf("TranslateStreamChunk: %v", err)
		}
		assertChunk(t, out, "error", `{"type":"error","error":{"message":"boom"}}`, true, "boom")
	})

	t.Run("stream chunk error event without message yields empty error", func(t *testing.T) {
		out, err := tr.TranslateStreamChunk(ctx, mustChunk(t, "error", `{"type":"error"}`, false, nil))
		if err != nil {
			t.Fatalf("TranslateStreamChunk: %v", err)
		}
		if out.Event != "error" || !out.IsFinal || out.Error == nil || out.Error.Error() != "" {
			t.Errorf("unexpected chunk: %+v", out)
		}
	})

	t.Run("stream chunk error event with non-object error uses default message", func(t *testing.T) {
		out, err := tr.TranslateStreamChunk(ctx, mustChunk(t, "error", `{"type":"error","error":123}`, false, nil))
		if err != nil {
			t.Fatalf("TranslateStreamChunk: %v", err)
		}
		assertChunk(t, out, "error", ``, true, "claude stream error")
	})

	t.Run("stream chunk unknown event passes through", func(t *testing.T) {
		ch := mustChunk(t, "ping", `{"type":"ping"}`, false, nil)
		out, err := tr.TranslateStreamChunk(ctx, ch)
		if err != nil {
			t.Fatalf("TranslateStreamChunk: %v", err)
		}
		if out != ch {
			t.Error("expected passthrough of same chunk")
		}
	})
}

// ---- OpenAIToGemini ----

func TestOpenAIToGemini(t *testing.T) {
	ctx := context.Background()
	tr := NewOpenAIToGemini()

	t.Run("source and target", func(t *testing.T) {
		if tr.Source() != engine.FormatOpenAIChat || tr.Target() != engine.FormatGemini {
			t.Errorf("unexpected formats: %v -> %v", tr.Source(), tr.Target())
		}
	})

	t.Run("request nil", func(t *testing.T) {
		if _, err := tr.TranslateRequest(ctx, nil); !errors.Is(err, errNilRequest) {
			t.Errorf("expected errNilRequest, got %v", err)
		}
	})

	t.Run("request invalid json", func(t *testing.T) {
		if _, err := tr.TranslateRequest(ctx, mustReq(t, `{`)); err == nil || !strings.Contains(err.Error(), "unmarshal") {
			t.Errorf("expected unmarshal error, got %v", err)
		}
	})

	t.Run("request full body", func(t *testing.T) {
		req := mustReq(t, `{
			"model":"gpt-4",
			"max_tokens":256,
			"temperature":0.5,
			"top_p":0.9,
			"messages":[
				{"role":"system","content":"You are helpful."},
				{"role":"user","content":"hi"},
				{"role":"assistant","content":"hello"},
				{"role":"tool","tool_call_id":"call_1","content":"42"}
			],
			"stop":["\n\nHuman:"],
			"tools":[{"type":"function","function":{"name":"get_weather","description":"W","parameters":{"type":"object"}}}]
		}`)
		out, err := tr.TranslateRequest(ctx, req)
		if err != nil {
			t.Fatalf("TranslateRequest: %v", err)
		}
		if out.Format != engine.FormatGemini || out.Model != "gpt-4" {
			t.Errorf("format/model mismatch: %v %q", out.Format, out.Model)
		}
		if !out.Stream || out.MaxTokens != 256 {
			t.Errorf("stream/maxTokens mismatch: %v %d", out.Stream, out.MaxTokens)
		}
		assertJSONEqual(t, out.RawBody, json.RawMessage(`{
			"contents":[
				{"role":"user","parts":[{"text":"hi"}]},
				{"role":"model","parts":[{"text":"hello"}]},
				{"role":"function","parts":[{"functionResponse":{"name":"call_1","response":{"name":"call_1","content":"42"}}}]}
			],
			"systemInstruction":{"parts":[{"text":"You are helpful."}]},
			"generationConfig":{"temperature":0.5,"topP":0.9,"maxOutputTokens":256,"stopSequences":["\n\nHuman:"]},
			"tools":[{"functionDeclarations":[{"name":"get_weather","description":"W","parameters":{"type":"object"}}]}]
		}`))
	})

	t.Run("request malformed user content yields null parts", func(t *testing.T) {
		req := mustReq(t, `{"model":"gpt-4","messages":[{"role":"user","content":123}]}`)
		out, err := tr.TranslateRequest(ctx, req)
		if err != nil {
			t.Fatalf("TranslateRequest: %v", err)
		}
		assertJSONEqual(t, out.RawBody, json.RawMessage(`{
			"contents":[{"role":"user","parts":null}]
		}`))
	})

	t.Run("request message unmarshal error", func(t *testing.T) {
		req := mustReq(t, `{"model":"gpt-4","messages":[42]}`)
		if _, err := tr.TranslateRequest(ctx, req); err == nil || !strings.Contains(err.Error(), "message") {
			t.Errorf("expected message unmarshal error, got %v", err)
		}
	})

	t.Run("response nil", func(t *testing.T) {
		if _, err := tr.TranslateResponse(ctx, nil); !errors.Is(err, errNilResponse) {
			t.Errorf("expected errNilResponse, got %v", err)
		}
	})

	t.Run("response invalid json", func(t *testing.T) {
		if _, err := tr.TranslateResponse(ctx, mustResp(t, `{`)); err == nil || !strings.Contains(err.Error(), "unmarshal response") {
			t.Errorf("expected unmarshal response error, got %v", err)
		}
	})

	t.Run("response full with function call", func(t *testing.T) {
		resp := mustResp(t, `{
			"candidates":[{
				"content":{
					"parts":[{"text":"hello"},{"functionCall":{"name":"get_weather","args":{"loc":"NYC"}}}],
					"role":"model"
				},
				"finishReason":"TOOL_CALLS"
			}],
			"usageMetadata":{"promptTokenCount":10,"candidatesTokenCount":5,"totalTokenCount":15}
		}`)
		out, err := tr.TranslateResponse(ctx, resp)
		if err != nil {
			t.Fatalf("TranslateResponse: %v", err)
		}
		assertJSONEqual(t, out.Body, json.RawMessage(`{
			"id":"chatcmpl-gemini","object":"chat.completion","model":"gpt-4",
			"choices":[{
				"index":0,
				"message":{
					"role":"assistant",
					"content":"hello",
					"tool_calls":[{"id":"get_weather","type":"function","function":{"name":"get_weather","arguments":"{\"loc\":\"NYC\"}"}}]
				},
				"finish_reason":"tool_calls"
			}],
			"usage":{"prompt_tokens":10,"completion_tokens":5,"total_tokens":15}
		}`))
	})

	t.Run("response without candidates defaults to stop", func(t *testing.T) {
		resp := mustResp(t, `{"id":"x"}`)
		out, err := tr.TranslateResponse(ctx, resp)
		if err != nil {
			t.Fatalf("TranslateResponse: %v", err)
		}
		assertJSONEqual(t, out.Body, json.RawMessage(`{
			"id":"chatcmpl-gemini","object":"chat.completion","model":"gpt-4",
			"choices":[{"index":0,"message":{"role":"assistant","content":null},"finish_reason":"stop"}]
		}`))
	})

	t.Run("response finish reason variants", func(t *testing.T) {
		cases := []struct{ in, want string }{
			{"MAX_TOKENS", "length"},
			{"SAFETY", "content_filter"},
			{"RECITATION", "content_filter"},
			{"FUNCTION_CALL", "tool_calls"},
			{"WEIRD", "stop"},
		}
		for _, c := range cases {
			t.Run(c.in, func(t *testing.T) {
				resp := mustResp(t, `{"candidates":[{"content":{"parts":[{"text":"t"}],"role":"model"},"finishReason":"`+c.in+`"}]}`)
				out, err := tr.TranslateResponse(ctx, resp)
				if err != nil {
					t.Fatalf("TranslateResponse: %v", err)
				}
				assertJSONEqual(t, out.Body, json.RawMessage(`{
					"id":"chatcmpl-gemini","object":"chat.completion","model":"gpt-4",
					"choices":[{"index":0,"message":{"role":"assistant","content":"t"},"finish_reason":"`+c.want+`"}]
				}`))
			})
		}
	})

	t.Run("stream chunk nil", func(t *testing.T) {
		if _, err := tr.TranslateStreamChunk(ctx, nil); !errors.Is(err, errNilChunk) {
			t.Errorf("expected errNilChunk, got %v", err)
		}
	})

	t.Run("stream chunk error becomes error chunk", func(t *testing.T) {
		ch := mustChunk(t, "chunk", `{"x":1}`, false, errors.New("boom"))
		out, err := tr.TranslateStreamChunk(ctx, ch)
		if err != nil {
			t.Fatalf("TranslateStreamChunk: %v", err)
		}
		assertChunk(t, out, "error", `{"x":1}`, true, "boom")
	})

	t.Run("stream chunk invalid json", func(t *testing.T) {
		if _, err := tr.TranslateStreamChunk(ctx, mustChunk(t, "", `{`, false, nil)); err == nil || !strings.Contains(err.Error(), "unmarshal chunk") {
			t.Errorf("expected unmarshal chunk error, got %v", err)
		}
	})

	t.Run("stream chunk no candidates passes through", func(t *testing.T) {
		ch := mustChunk(t, "chunk", `{"candidates":[]}`, false, nil)
		out, err := tr.TranslateStreamChunk(ctx, ch)
		if err != nil {
			t.Fatalf("TranslateStreamChunk: %v", err)
		}
		if out != ch {
			t.Error("expected passthrough of same chunk")
		}
	})

	t.Run("stream chunk text without finish", func(t *testing.T) {
		out, err := tr.TranslateStreamChunk(ctx, mustChunk(t, "chunk", `{"candidates":[{"content":{"parts":[{"text":"hi"}]}}]}`, false, nil))
		if err != nil {
			t.Fatalf("TranslateStreamChunk: %v", err)
		}
		assertChunk(t, out, "chunk", `{"choices":[{"index":0,"delta":{"content":"hi"},"finish_reason":""}]}`, false, "")
	})

	t.Run("stream chunk finish stop", func(t *testing.T) {
		out, err := tr.TranslateStreamChunk(ctx, mustChunk(t, "chunk", `{"candidates":[{"content":{"parts":[{"text":"hi"}]},"finishReason":"STOP"}]}`, false, nil))
		if err != nil {
			t.Fatalf("TranslateStreamChunk: %v", err)
		}
		assertChunk(t, out, "chunk", `{"choices":[{"index":0,"delta":{"content":"hi"},"finish_reason":"stop"}]}`, true, "")
	})

	t.Run("stream chunk finish max tokens", func(t *testing.T) {
		out, err := tr.TranslateStreamChunk(ctx, mustChunk(t, "chunk", `{"candidates":[{"content":{"parts":[{"text":"hi"}]},"finishReason":"MAX_TOKENS"}]}`, false, nil))
		if err != nil {
			t.Fatalf("TranslateStreamChunk: %v", err)
		}
		assertChunk(t, out, "chunk", `{"choices":[{"index":0,"delta":{"content":"hi"},"finish_reason":"length"}]}`, true, "")
	})

	t.Run("stream chunk without parts yields empty text", func(t *testing.T) {
		out, err := tr.TranslateStreamChunk(ctx, mustChunk(t, "chunk", `{"candidates":[{"content":{"parts":[]}}]}`, false, nil))
		if err != nil {
			t.Fatalf("TranslateStreamChunk: %v", err)
		}
		assertChunk(t, out, "chunk", `{"choices":[{"index":0,"delta":{"content":""},"finish_reason":""}]}`, false, "")
	})
}

// ---- OpenAIToCompat / CompatToOpenAI passthrough ----

func TestCompatTranslators(t *testing.T) {
	ctx := context.Background()

	t.Run("openai to compat source and target", func(t *testing.T) {
		tr := NewOpenAIToCompat()
		if tr.Source() != engine.FormatOpenAIChat || tr.Target() != engine.FormatOpenAICompat {
			t.Errorf("unexpected formats: %v -> %v", tr.Source(), tr.Target())
		}
	})

	t.Run("compat to openai source and target", func(t *testing.T) {
		tr := NewCompatToOpenAI()
		if tr.Source() != engine.FormatOpenAICompat || tr.Target() != engine.FormatOpenAIChat {
			t.Errorf("unexpected formats: %v -> %v", tr.Source(), tr.Target())
		}
	})

	t.Run("request passthrough changes format only", func(t *testing.T) {
		body := `{"model":"gpt-4","messages":[{"role":"user","content":"hi"}]}`
		req := mustReq(t, body)
		req.Format = engine.FormatOpenAIChat

		tr := NewOpenAIToCompat()
		out, err := tr.TranslateRequest(ctx, req)
		if err != nil {
			t.Fatalf("TranslateRequest: %v", err)
		}
		if out.Format != engine.FormatOpenAICompat {
			t.Errorf("Format = %v, want openai_compat", out.Format)
		}
		if string(out.RawBody) != body || string(out.MappedBody) != body {
			t.Errorf("body not preserved: %q %q", out.RawBody, out.MappedBody)
		}
		if out.Model != req.Model || out.Stream != req.Stream {
			t.Error("request fields not preserved")
		}

		back, err := NewCompatToOpenAI().TranslateRequest(ctx, req)
		if err != nil {
			t.Fatalf("TranslateRequest reverse: %v", err)
		}
		if back.Format != engine.FormatOpenAIChat {
			t.Errorf("Format = %v, want openai_chat", back.Format)
		}
	})

	t.Run("response passthrough returns same pointer", func(t *testing.T) {
		resp := mustResp(t, `{"id":"x"}`)
		out, err := NewOpenAIToCompat().TranslateResponse(ctx, resp)
		if err != nil {
			t.Fatalf("TranslateResponse: %v", err)
		}
		if out != resp {
			t.Error("expected identical response pointer")
		}
		out2, err := NewCompatToOpenAI().TranslateResponse(ctx, resp)
		if err != nil {
			t.Fatalf("TranslateResponse reverse: %v", err)
		}
		if out2 != resp {
			t.Error("expected identical response pointer")
		}
	})

	t.Run("chunk passthrough returns same pointer", func(t *testing.T) {
		ch := mustChunk(t, "chunk", `{"x":1}`, false, nil)
		out, err := NewOpenAIToCompat().TranslateStreamChunk(ctx, ch)
		if err != nil {
			t.Fatalf("TranslateStreamChunk: %v", err)
		}
		if out != ch {
			t.Error("expected identical chunk pointer")
		}
		out2, err := NewCompatToOpenAI().TranslateStreamChunk(ctx, ch)
		if err != nil {
			t.Fatalf("TranslateStreamChunk reverse: %v", err)
		}
		if out2 != ch {
			t.Error("expected identical chunk pointer")
		}
	})

	t.Run("nil inputs", func(t *testing.T) {
		for _, m := range []struct {
			request  func(context.Context, *engine.Request) (*engine.Request, error)
			response func(context.Context, *engine.Response) (*engine.Response, error)
			chunk    func(context.Context, *formats.Chunk) (*formats.Chunk, error)
		}{
			{NewOpenAIToCompat().TranslateRequest, NewOpenAIToCompat().TranslateResponse, NewOpenAIToCompat().TranslateStreamChunk},
			{NewCompatToOpenAI().TranslateRequest, NewCompatToOpenAI().TranslateResponse, NewCompatToOpenAI().TranslateStreamChunk},
		} {
			if _, err := m.request(ctx, nil); !errors.Is(err, errNilRequest) {
				t.Errorf("expected errNilRequest, got %v", err)
			}
			if _, err := m.response(ctx, nil); !errors.Is(err, errNilResponse) {
				t.Errorf("expected errNilResponse, got %v", err)
			}
			if _, err := m.chunk(ctx, nil); !errors.Is(err, errNilChunk) {
				t.Errorf("expected errNilChunk, got %v", err)
			}
		}
	})
}

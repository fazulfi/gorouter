package translators_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"gorouter/internal/engine/translators"
)

func TestParseChatToCanonical(t *testing.T) {
	ctx := context.Background()
	body := json.RawMessage(`{
		"model": "gpt-4",
		"messages": [{"role": "system", "content": "sys"}, {"role": "user", "content": "hi"}],
		"stream": true,
		"max_tokens": 100,
		"temperature": 0.5,
		"top_p": 0.9,
		"stop": ["END"],
		"tools": [{"type": "function", "function": {"name": "lookup", "description": "d", "parameters": {}}}],
		"tool_choice": "auto",
		"response_format": {"type": "json_object"}
	}`)

	req, err := translators.ParseChatToCanonical(ctx, body)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if req.Model != "gpt-4" {
		t.Errorf("model = %q", req.Model)
	}
	if !req.Stream {
		t.Error("stream not preserved")
	}
	if req.MaxTokens != 100 {
		t.Errorf("max_tokens = %d", req.MaxTokens)
	}
	if req.Temperature == nil || *req.Temperature != 0.5 {
		t.Errorf("temperature = %v", req.Temperature)
	}
	if string(req.RawBody) != string(body) {
		t.Error("raw body not preserved")
	}
	var canon translators.CanonicalBody
	if err := json.Unmarshal(req.MappedBody, &canon); err != nil {
		t.Fatalf("mapped body not canonical: %v", err)
	}
	if len(canon.Messages) != 2 {
		t.Errorf("messages = %d", len(canon.Messages))
	}
	if canon.Messages[0].Role != "system" || canon.Messages[1].Role != "user" {
		t.Errorf("roles = %q, %q", canon.Messages[0].Role, canon.Messages[1].Role)
	}
	if len(canon.Tools) != 1 || canon.Tools[0].Function.Name != "lookup" {
		t.Errorf("tools = %+v", canon.Tools)
	}
	if len(canon.Stop) != 1 || canon.Stop[0] != "END" {
		t.Errorf("stop = %v", canon.Stop)
	}
}

func TestParseChatToCanonical_Errors(t *testing.T) {
	ctx := context.Background()
	for _, tc := range []struct {
		name string
		body json.RawMessage
	}{
		{"empty", json.RawMessage(``)},
		{"missing model", json.RawMessage(`{"messages":[{"role":"user","content":"hi"}]}`)},
		{"missing messages", json.RawMessage(`{"model":"gpt-4"}`)},
		{"invalid json", json.RawMessage(`{`)},
	} {
		if _, err := translators.ParseChatToCanonical(ctx, tc.body); err == nil {
			t.Errorf("%s: expected error", tc.name)
		}
	}
}

func TestParseCodexToCanonical_StringInput(t *testing.T) {
	ctx := context.Background()
	body := json.RawMessage(`{"model":"gpt-5-codex","input":"hello world","instructions":"be terse","store":true,"max_output_tokens":256}`)

	req, err := translators.ParseCodexToCanonical(ctx, body)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if req.Model != "gpt-5-codex" {
		t.Errorf("model = %q", req.Model)
	}
	if req.MaxTokens != 256 {
		t.Errorf("max_tokens = %d", req.MaxTokens)
	}
	var canon translators.CanonicalBody
	if err := json.Unmarshal(req.MappedBody, &canon); err != nil {
		t.Fatalf("mapped body: %v", err)
	}
	if len(canon.Messages) != 1 || canon.Messages[0].Role != "user" {
		t.Fatalf("messages = %+v", canon.Messages)
	}
	var content string
	if err := json.Unmarshal(canon.Messages[0].Content, &content); err != nil || content != "hello world" {
		t.Errorf("content = %q (err %v)", content, err)
	}
	if canon.Instructions != "be terse" {
		t.Errorf("instructions = %q", canon.Instructions)
	}
	if canon.Store == nil || !*canon.Store {
		t.Error("store not preserved")
	}
}

func TestParseCodexToCanonical_ArrayInput(t *testing.T) {
	ctx := context.Background()
	body := json.RawMessage(`{"model":"gpt-5-codex","input":[{"role":"user","content":"hi"},{"role":"assistant","content":"yo","tool_calls":[{"id":"call_1","type":"function","function":{"name":"f","arguments":"{}"}}]}]}`)

	req, err := translators.ParseCodexToCanonical(ctx, body)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	var canon translators.CanonicalBody
	if err := json.Unmarshal(req.MappedBody, &canon); err != nil {
		t.Fatalf("mapped body: %v", err)
	}
	if len(canon.Messages) != 2 {
		t.Fatalf("messages = %d", len(canon.Messages))
	}
	if len(canon.Messages[1].ToolCalls) != 1 || canon.Messages[1].ToolCalls[0].ID != "call_1" {
		t.Errorf("tool_calls = %+v", canon.Messages[1].ToolCalls)
	}
}

func TestParseCodexToCanonical_Errors(t *testing.T) {
	ctx := context.Background()
	for _, tc := range []struct {
		name string
		body json.RawMessage
	}{
		{"empty", json.RawMessage(``)},
		{"missing model", json.RawMessage(`{"input":"hi"}`)},
		{"missing input", json.RawMessage(`{"model":"gpt-5-codex"}`)},
		{"bad input", json.RawMessage(`{"model":"gpt-5-codex","input":42}`)},
	} {
		if _, err := translators.ParseCodexToCanonical(ctx, tc.body); err == nil {
			t.Errorf("%s: expected error", tc.name)
		}
	}
}

func TestParseErrorsWrapSentinels(t *testing.T) {
	ctx := context.Background()
	_, err := translators.ParseChatToCanonical(ctx, json.RawMessage(``))
	if err == nil || !errors.Is(err, translators.ErrNilRequest) {
		t.Fatalf("chat empty body err = %v", err)
	}
	_, err = translators.ParseCodexToCanonical(ctx, json.RawMessage(``))
	if err == nil || !errors.Is(err, translators.ErrNilRequest) {
		t.Fatalf("codex empty body err = %v", err)
	}
}

func TestParseRequestFormatNotSet(t *testing.T) {
	ctx := context.Background()
	req, err := translators.ParseChatToCanonical(ctx, json.RawMessage(`{"model":"m","messages":[{"role":"user","content":"hi"}]}`))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if req.Format != "" {
		t.Errorf("format should be set by the orchestrator, got %q", req.Format)
	}
}

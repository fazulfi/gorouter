package translate

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"gorouter/internal/domain/engine"
)

func TestDetectFormat(t *testing.T) {
	svc := NewService()
	ctx := context.Background()

	tests := []struct {
		name    string
		body    json.RawMessage
		want    string
		wantErr bool
	}{
		{
			name: "OpenAI Chat format",
			body: json.RawMessage(`{"model":"gpt-4","messages":[{"role":"user","content":"hi"}]}`),
			want: string(RequestFormatOpenAIChat),
		},
		{
			name: "Codex Responses format with string input",
			body: json.RawMessage(`{"model":"gpt-4","input":"hello world"}`),
			want: string(RequestFormatCodexResponses),
		},
		{
			name: "Codex Responses format with array input",
			body: json.RawMessage(`{"model":"gpt-4","input":[{"role":"user","content":"hi"}]}`),
			want: string(RequestFormatCodexResponses),
		},
		{
			name:    "empty body",
			body:    json.RawMessage(``),
			want:    string(RequestFormatUnknown),
			wantErr: true,
		},
		{
			name:    "invalid JSON",
			body:    json.RawMessage(`{invalid`),
			want:    string(RequestFormatUnknown),
			wantErr: true,
		},
		{
			name:    "no recognized fields",
			body:    json.RawMessage(`{"foo":"bar"}`),
			want:    string(RequestFormatUnknown),
			wantErr: true,
		},
		{
			name:    "messages without model",
			body:    json.RawMessage(`{"messages":[{"role":"user","content":"hi"}]}`),
			want:    string(RequestFormatUnknown),
			wantErr: true,
		},
		{
			name:    "input without model",
			body:    json.RawMessage(`{"input":"hello"}`),
			want:    string(RequestFormatUnknown),
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			format, err := svc.DetectFormat(ctx, tc.body)
			if tc.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				if !errors.Is(err, ErrUnsupportedFormat) {
					t.Errorf("expected ErrUnsupportedFormat, got %v", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if string(format) != tc.want {
				t.Errorf("expected format %q, got %q", tc.want, format)
			}
		})
	}
}

func TestTranslateChatToRequest(t *testing.T) {
	svc := NewService()
	ctx := context.Background()

	t.Run("valid minimal body", func(t *testing.T) {
		body := json.RawMessage(`{
			"model": "gpt-4",
			"messages": [
				{"role": "user", "content": "Hello"},
				{"role": "assistant", "content": "Hi there"}
			]
		}`)

		req, err := svc.TranslateChatToRequest(ctx, body)
		if err != nil {
			t.Fatalf("TranslateChatToRequest failed: %v", err)
		}
		if req.Model != "gpt-4" {
			t.Errorf("expected model gpt-4, got %q", req.Model)
		}
		if req.Stream {
			t.Error("expected stream=false")
		}
		if req.Temperature != nil {
			t.Error("expected Temperature=nil")
		}
		if req.MaxTokens != 0 {
			t.Errorf("expected MaxTokens=0, got %d", req.MaxTokens)
		}

		var canon CanonicalBody
		if err := json.Unmarshal(req.MappedBody, &canon); err != nil {
			t.Fatalf("unmarshal MappedBody: %v", err)
		}
		if len(canon.Messages) != 2 {
			t.Fatalf("expected 2 messages, got %d", len(canon.Messages))
		}
		if canon.Messages[0].Role != "user" {
			t.Errorf("expected role=user, got %q", canon.Messages[0].Role)
		}
		if canon.Messages[1].Role != "assistant" {
			t.Errorf("expected role=assistant, got %q", canon.Messages[1].Role)
		}
	})

	t.Run("all fields", func(t *testing.T) {
		body := json.RawMessage(`{
			"model": "gpt-4-turbo",
			"messages": [{"role": "user", "content": "Hi"}],
			"stream": true,
			"max_tokens": 2048,
			"temperature": 0.7,
			"top_p": 0.9,
			"stop": [".", "!"],
			"presence_penalty": 0.1,
			"frequency_penalty": 0.2
		}`)

		req, err := svc.TranslateChatToRequest(ctx, body)
		if err != nil {
			t.Fatalf("TranslateChatToRequest failed: %v", err)
		}
		if req.Model != "gpt-4-turbo" {
			t.Errorf("expected model gpt-4-turbo, got %q", req.Model)
		}
		if !req.Stream {
			t.Error("expected stream=true")
		}
		if req.MaxTokens != 2048 {
			t.Errorf("expected MaxTokens=2048, got %d", req.MaxTokens)
		}
		if req.Temperature == nil || *req.Temperature != 0.7 {
			t.Errorf("expected Temperature=0.7, got %v", req.Temperature)
		}

		var canon CanonicalBody
		if err := json.Unmarshal(req.MappedBody, &canon); err != nil {
			t.Fatalf("unmarshal MappedBody: %v", err)
		}
		if canon.TopP == nil || *canon.TopP != 0.9 {
			t.Errorf("expected TopP=0.9, got %v", canon.TopP)
		}
		if len(canon.Stop) != 2 || canon.Stop[0] != "." {
			t.Errorf("expected Stop=[., !], got %v", canon.Stop)
		}
		if canon.PresencePenalty == nil || *canon.PresencePenalty != 0.1 {
			t.Errorf("expected PresencePenalty=0.1, got %v", canon.PresencePenalty)
		}
		if canon.FrequencyPenalty == nil || *canon.FrequencyPenalty != 0.2 {
			t.Errorf("expected FrequencyPenalty=0.2, got %v", canon.FrequencyPenalty)
		}
	})

	t.Run("missing model", func(t *testing.T) {
		body := json.RawMessage(`{"messages":[{"role":"user","content":"hi"}]}`)
		_, err := svc.TranslateChatToRequest(ctx, body)
		if err == nil {
			t.Fatal("expected error for missing model, got nil")
		}
		if !errors.Is(err, ErrInvalidRequest) {
			t.Errorf("expected ErrInvalidRequest, got %v", err)
		}
	})

	t.Run("missing messages", func(t *testing.T) {
		body := json.RawMessage(`{"model":"gpt-4"}`)
		_, err := svc.TranslateChatToRequest(ctx, body)
		if err == nil {
			t.Fatal("expected error for missing messages, got nil")
		}
		if !errors.Is(err, ErrInvalidRequest) {
			t.Errorf("expected ErrInvalidRequest, got %v", err)
		}
	})

	t.Run("empty body", func(t *testing.T) {
		_, err := svc.TranslateChatToRequest(ctx, json.RawMessage(``))
		if err == nil {
			t.Fatal("expected error for empty body, got nil")
		}
	})

	t.Run("with tools and tool_choice", func(t *testing.T) {
		body := json.RawMessage(`{
			"model": "gpt-4",
			"messages": [{"role": "user", "content": "What's the weather?"}],
			"tools": [{
				"type": "function",
				"function": {
					"name": "get_weather",
					"description": "Get weather for a city",
					"parameters": {
						"type": "object",
						"properties": {
							"location": {"type": "string"}
						}
					}
				}
			}],
			"tool_choice": "auto"
		}`)

		req, err := svc.TranslateChatToRequest(ctx, body)
		if err != nil {
			t.Fatalf("TranslateChatToRequest failed: %v", err)
		}

		var canon CanonicalBody
		if err := json.Unmarshal(req.MappedBody, &canon); err != nil {
			t.Fatalf("unmarshal MappedBody: %v", err)
		}
		if len(canon.Tools) != 1 {
			t.Fatalf("expected 1 tool, got %d", len(canon.Tools))
		}
		if canon.Tools[0].Type != "function" {
			t.Errorf("expected tool type function, got %q", canon.Tools[0].Type)
		}
		if canon.Tools[0].Function.Name != "get_weather" {
			t.Errorf("expected function name get_weather, got %q", canon.Tools[0].Function.Name)
		}
		if len(canon.ToolChoice) == 0 {
			t.Error("expected non-empty tool_choice")
		}
	})

	t.Run("stop as single string", func(t *testing.T) {
		body := json.RawMessage(`{
			"model": "gpt-4",
			"messages": [{"role": "user", "content": "hi"}],
			"stop": "\n"
		}`)

		req, err := svc.TranslateChatToRequest(ctx, body)
		if err != nil {
			t.Fatalf("TranslateChatToRequest failed: %v", err)
		}

		var canon CanonicalBody
		if err := json.Unmarshal(req.MappedBody, &canon); err != nil {
			t.Fatalf("unmarshal MappedBody: %v", err)
		}
		if len(canon.Stop) != 1 || canon.Stop[0] != "\n" {
			t.Errorf("expected Stop=[\\n], got %v", canon.Stop)
		}
	})

	t.Run("array content (multimodal)", func(t *testing.T) {
		body := json.RawMessage(`{
			"model": "gpt-4-vision",
			"messages": [{
				"role": "user",
				"content": [
					{"type": "text", "text": "Describe this image"},
					{"type": "image_url", "image_url": {"url": "https://example.com/img.jpg"}}
				]
			}]
		}`)

		req, err := svc.TranslateChatToRequest(ctx, body)
		if err != nil {
			t.Fatalf("TranslateChatToRequest failed: %v", err)
		}

		var canon CanonicalBody
		if err := json.Unmarshal(req.MappedBody, &canon); err != nil {
			t.Fatalf("unmarshal MappedBody: %v", err)
		}
		if len(canon.Messages) != 1 {
			t.Fatalf("expected 1 message, got %d", len(canon.Messages))
		}

		var contentParts []json.RawMessage
		if err := json.Unmarshal(canon.Messages[0].Content, &contentParts); err != nil {
			t.Fatalf("content should decode as JSON array: %v", err)
		}
		if len(contentParts) != 2 {
			t.Errorf("expected 2 content parts, got %d", len(contentParts))
		}
	})

	t.Run("tool_calls in assistant message", func(t *testing.T) {
		body := json.RawMessage(`{
			"model": "gpt-4",
			"messages": [
				{"role": "user", "content": "Weather?"},
				{
					"role": "assistant",
					"content": null,
					"tool_calls": [{
						"id": "call_1",
						"type": "function",
						"function": {
							"name": "get_weather",
							"arguments": "{\"location\":\"NYC\"}"
						}
					}]
				}
			]
		}`)

		req, err := svc.TranslateChatToRequest(ctx, body)
		if err != nil {
			t.Fatalf("TranslateChatToRequest failed: %v", err)
		}

		var canon CanonicalBody
		if err := json.Unmarshal(req.MappedBody, &canon); err != nil {
			t.Fatalf("unmarshal MappedBody: %v", err)
		}
		if len(canon.Messages) != 2 {
			t.Fatalf("expected 2 messages, got %d", len(canon.Messages))
		}
		asst := canon.Messages[1]
		if len(asst.ToolCalls) != 1 {
			t.Fatalf("expected 1 tool_call, got %d", len(asst.ToolCalls))
		}
		if asst.ToolCalls[0].ID != "call_1" {
			t.Errorf("expected tool_call id call_1, got %q", asst.ToolCalls[0].ID)
		}
		if asst.ToolCalls[0].Function.Name != "get_weather" {
			t.Errorf("expected function name get_weather, got %q", asst.ToolCalls[0].Function.Name)
		}
	})
}

func TestTranslateCodexToRequest(t *testing.T) {
	svc := NewService()
	ctx := context.Background()

	t.Run("string input -> single user message", func(t *testing.T) {
		body := json.RawMessage(`{
			"model": "gpt-4",
			"input": "Hello, world!"
		}`)

		req, err := svc.TranslateCodexToRequest(ctx, body)
		if err != nil {
			t.Fatalf("TranslateCodexToRequest failed: %v", err)
		}
		if req.Model != "gpt-4" {
			t.Errorf("expected model gpt-4, got %q", req.Model)
		}

		var canon CanonicalBody
		if err := json.Unmarshal(req.MappedBody, &canon); err != nil {
			t.Fatalf("unmarshal MappedBody: %v", err)
		}
		if len(canon.Messages) != 1 {
			t.Fatalf("expected 1 message, got %d", len(canon.Messages))
		}
		if canon.Messages[0].Role != "user" {
			t.Errorf("expected role user, got %q", canon.Messages[0].Role)
		}
		var content string
		if err := json.Unmarshal(canon.Messages[0].Content, &content); err != nil {
			t.Fatalf("content should be a string: %v", err)
		}
		if content != "Hello, world!" {
			t.Errorf("expected content %q, got %q", "Hello, world!", content)
		}
	})

	t.Run("array input -> multiple messages", func(t *testing.T) {
		body := json.RawMessage(`{
			"model": "gpt-4",
			"input": [
				{"role": "system", "content": "You are a helpful assistant."},
				{"role": "user", "content": "Tell me a joke"},
				{"role": "assistant", "content": "Why did the chicken cross the road?"}
			]
		}`)

		req, err := svc.TranslateCodexToRequest(ctx, body)
		if err != nil {
			t.Fatalf("TranslateCodexToRequest failed: %v", err)
		}
		if req.Model != "gpt-4" {
			t.Errorf("expected model gpt-4, got %q", req.Model)
		}

		var canon CanonicalBody
		if err := json.Unmarshal(req.MappedBody, &canon); err != nil {
			t.Fatalf("unmarshal MappedBody: %v", err)
		}
		if len(canon.Messages) != 3 {
			t.Fatalf("expected 3 messages, got %d", len(canon.Messages))
		}
		if canon.Messages[0].Role != "system" {
			t.Errorf("expected role system, got %q", canon.Messages[0].Role)
		}
		if canon.Messages[1].Role != "user" {
			t.Errorf("expected role user, got %q", canon.Messages[1].Role)
		}
		if canon.Messages[2].Role != "assistant" {
			t.Errorf("expected role assistant, got %q", canon.Messages[2].Role)
		}
	})

	t.Run("string array input elements -> user messages", func(t *testing.T) {
		body := json.RawMessage(`{
			"model": "gpt-4",
			"input": ["first message", "second message"]
		}`)

		req, err := svc.TranslateCodexToRequest(ctx, body)
		if err != nil {
			t.Fatalf("TranslateCodexToRequest failed: %v", err)
		}

		var canon CanonicalBody
		if err := json.Unmarshal(req.MappedBody, &canon); err != nil {
			t.Fatalf("unmarshal MappedBody: %v", err)
		}
		if len(canon.Messages) != 2 {
			t.Fatalf("expected 2 messages, got %d", len(canon.Messages))
		}
		for i, msg := range canon.Messages {
			if msg.Role != "user" {
				t.Errorf("message[%d]: expected role user, got %q", i, msg.Role)
			}
		}
	})

	t.Run("with optional fields", func(t *testing.T) {
		body := json.RawMessage(`{
			"model": "gpt-4",
			"input": "Hello",
			"instructions": "Be concise",
			"max_output_tokens": 500,
			"temperature": 0.5,
			"top_p": 0.95,
			"store": true,
			"metadata": {"session_id": "abc123"},
			"reasoning": {"effort": "medium"}
		}`)

		req, err := svc.TranslateCodexToRequest(ctx, body)
		if err != nil {
			t.Fatalf("TranslateCodexToRequest failed: %v", err)
		}
		if req.MaxTokens != 500 {
			t.Errorf("expected MaxTokens=500, got %d", req.MaxTokens)
		}
		if req.Temperature == nil || *req.Temperature != 0.5 {
			t.Errorf("expected Temperature=0.5, got %v", req.Temperature)
		}

		var canon CanonicalBody
		if err := json.Unmarshal(req.MappedBody, &canon); err != nil {
			t.Fatalf("unmarshal MappedBody: %v", err)
		}
		if canon.Instructions != "Be concise" {
			t.Errorf("expected instructions 'Be concise', got %q", canon.Instructions)
		}
		if canon.Store == nil || !*canon.Store {
			t.Error("expected store=true")
		}
		if canon.TopP == nil || *canon.TopP != 0.95 {
			t.Errorf("expected TopP=0.95, got %v", canon.TopP)
		}
		if len(canon.Metadata) == 0 {
			t.Error("expected non-empty metadata")
		}
		if len(canon.Reasoning) == 0 {
			t.Error("expected non-empty reasoning")
		}
	})

	t.Run("missing model", func(t *testing.T) {
		body := json.RawMessage(`{"input":"hi"}`)
		_, err := svc.TranslateCodexToRequest(ctx, body)
		if err == nil {
			t.Fatal("expected error for missing model, got nil")
		}
		if !errors.Is(err, ErrInvalidRequest) {
			t.Errorf("expected ErrInvalidRequest, got %v", err)
		}
	})

	t.Run("missing input", func(t *testing.T) {
		body := json.RawMessage(`{"model":"gpt-4"}`)
		_, err := svc.TranslateCodexToRequest(ctx, body)
		if err == nil {
			t.Fatal("expected error for missing input, got nil")
		}
	})

	t.Run("empty body", func(t *testing.T) {
		_, err := svc.TranslateCodexToRequest(ctx, json.RawMessage(``))
		if err == nil {
			t.Fatal("expected error for empty body, got nil")
		}
	})
}

func TestTranslateRequestToCodex(t *testing.T) {
	svc := NewService()
	ctx := context.Background()

	t.Run("round-trip: string input returns raw body", func(t *testing.T) {
		original := json.RawMessage(`{"model":"gpt-4","input":"Hello, world!"}`)

		req, err := svc.TranslateCodexToRequest(ctx, original)
		if err != nil {
			t.Fatalf("TranslateCodexToRequest failed: %v", err)
		}

		result, err := svc.TranslateRequestToCodex(ctx, req)
		if err != nil {
			t.Fatalf("TranslateRequestToCodex failed: %v", err)
		}

		var origObj, resObj map[string]json.RawMessage
		if err := json.Unmarshal(original, &origObj); err != nil {
			t.Fatalf("unmarshal original: %v", err)
		}
		if err := json.Unmarshal(result, &resObj); err != nil {
			t.Fatalf("unmarshal result: %v", err)
		}
		if string(resObj["model"]) != string(origObj["model"]) {
			t.Errorf("model mismatch: %s vs %s", resObj["model"], origObj["model"])
		}
		if string(resObj["input"]) != string(origObj["input"]) {
			t.Errorf("input mismatch: %s vs %s", resObj["input"], origObj["input"])
		}
	})

	t.Run("round-trip: array input with params", func(t *testing.T) {
		original := json.RawMessage(`{
			"model": "gpt-4",
			"input": [
				{"role": "system", "content": "You are helpful."},
				{"role": "user", "content": "Hi"}
			],
			"max_output_tokens": 1024,
			"temperature": 0.8
		}`)

		req, err := svc.TranslateCodexToRequest(ctx, original)
		if err != nil {
			t.Fatalf("TranslateCodexToRequest failed: %v", err)
		}

		result, err := svc.TranslateRequestToCodex(ctx, req)
		if err != nil {
			t.Fatalf("TranslateRequestToCodex failed: %v", err)
		}

		var resObj map[string]json.RawMessage
		if err := json.Unmarshal(result, &resObj); err != nil {
			t.Fatalf("unmarshal result: %v", err)
		}
		if string(resObj["model"]) != `"gpt-4"` {
			t.Errorf("expected model gpt-4, got %s", string(resObj["model"]))
		}

		var maxTokens int
		if err := json.Unmarshal(resObj["max_output_tokens"], &maxTokens); err != nil {
			t.Fatalf("unmarshal max_output_tokens: %v", err)
		}
		if maxTokens != 1024 {
			t.Errorf("expected max_output_tokens=1024, got %d", maxTokens)
		}

		var temp float64
		if err := json.Unmarshal(resObj["temperature"], &temp); err != nil {
			t.Fatalf("unmarshal temperature: %v", err)
		}
		if temp != 0.8 {
			t.Errorf("expected temperature=0.8, got %f", temp)
		}
	})

	t.Run("nil request", func(t *testing.T) {
		_, err := svc.TranslateRequestToCodex(ctx, nil)
		if err == nil {
			t.Fatal("expected error for nil request, got nil")
		}
		if !errors.Is(err, ErrInvalidRequest) {
			t.Errorf("expected ErrInvalidRequest, got %v", err)
		}
	})

	t.Run("rebuild from canonical body (no raw body)", func(t *testing.T) {
		canon := CanonicalBody{
			Messages: []Message{
				{Role: "user", Content: mustRaw(t, "Hello")},
				{Role: "assistant", Content: mustRaw(t, "World")},
			},
		}
		mapped, _ := json.Marshal(canon)
		req := &engine.Request{
			Model:      "gpt-4",
			MappedBody: mapped,
		}

		result, err := svc.TranslateRequestToCodex(ctx, req)
		if err != nil {
			t.Fatalf("TranslateRequestToCodex failed: %v", err)
		}

		var resObj map[string]json.RawMessage
		if err := json.Unmarshal(result, &resObj); err != nil {
			t.Fatalf("unmarshal result: %v", err)
		}

		var inputArr []json.RawMessage
		if err := json.Unmarshal(resObj["input"], &inputArr); err != nil {
			t.Fatalf("expected input to be array: %v", err)
		}
		if len(inputArr) != 2 {
			t.Fatalf("expected 2 input elements, got %d", len(inputArr))
		}
	})
}

func mustRaw(t *testing.T, v interface{}) json.RawMessage {
	t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return data
}

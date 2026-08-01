package safeir

// g104_remediation_test.go — locks the required-vs-optional JSON decode
// contract for every G104 (errcheck) site remediated in safeir/roundtrip.go.
//
// Required boundaries (malformed JSON at translation entry points) MUST return
// an error. Intentionally optional field decodes — the
// `_ = json.Unmarshal(...)` lines gosec v2.28.0 flagged as G104 — MUST degrade
// gracefully to zero values WITHOUT error, preserving exact wire semantics.

import (
	"encoding/json"
	"testing"

	"gorouter/internal/domain/engine"
)

func TestG104SafeIR_RequiredBoundaries(t *testing.T) {
	cases := []struct {
		name   string
		body   string
		format engine.RequestFormat
	}{
		{"invalid JSON body", `{`, engine.FormatOpenAIChat},
		{"non-object body", `[1,2]`, engine.FormatOpenAIChat},
		{"openai malformed message element", `{"model":"m","messages":[42]}`, engine.FormatOpenAIChat},
		{"claude malformed message element", `{"model":"m","messages":[42]}`, engine.FormatAnthropic},
		{"gemini malformed content element", `{"contents":[42]}`, engine.FormatGemini},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := ToSafeIR(json.RawMessage(tc.body), tc.format); err == nil {
				t.Fatalf("expected error for %q", tc.body)
			}
		})
	}
}

func TestG104SafeIR_OptionalDecode_DegradesGracefully(t *testing.T) {
	t.Run("openai model as number degrades to empty", func(t *testing.T) {
		ir, err := ToSafeIR(json.RawMessage(`{"model":123,"messages":[{"role":"user","content":"hi"}]}`), engine.FormatOpenAIChat)
		if err != nil {
			t.Fatalf("ToSafeIR: %v", err)
		}
		if ir.Model != "" {
			t.Errorf("Model = %q, want empty", ir.Model)
		}
	})
	t.Run("openai max_tokens as string degrades to 0", func(t *testing.T) {
		ir, err := ToSafeIR(json.RawMessage(`{"model":"m","messages":[{"role":"user","content":"hi"}],"max_tokens":"1024"}`), engine.FormatOpenAIChat)
		if err != nil {
			t.Fatalf("ToSafeIR: %v", err)
		}
		if ir.MaxTokens != 0 {
			t.Errorf("MaxTokens = %d, want 0", ir.MaxTokens)
		}
	})
	t.Run("openai system content as number degrades to empty text", func(t *testing.T) {
		ir, err := ToSafeIR(json.RawMessage(`{"model":"m","messages":[{"role":"system","content":42}]}`), engine.FormatOpenAIChat)
		if err != nil {
			t.Fatalf("ToSafeIR: %v", err)
		}
		if len(ir.Messages) != 1 || ir.Messages[0].Text != "" {
			t.Errorf("Messages = %+v, want single message with empty text", ir.Messages)
		}
	})
	t.Run("claude system as array degrades to empty", func(t *testing.T) {
		ir, err := ToSafeIR(json.RawMessage(`{"model":"m","system":["a","b"],"messages":[{"role":"user","content":"hi"}]}`), engine.FormatAnthropic)
		if err != nil {
			t.Fatalf("ToSafeIR: %v", err)
		}
		if ir.System != "" {
			t.Errorf("System = %q, want empty", ir.System)
		}
	})
	t.Run("claude max_tokens as string degrades to 0", func(t *testing.T) {
		ir, err := ToSafeIR(json.RawMessage(`{"model":"m","max_tokens":"99","messages":[{"role":"user","content":"hi"}]}`), engine.FormatAnthropic)
		if err != nil {
			t.Fatalf("ToSafeIR: %v", err)
		}
		if ir.MaxTokens != 0 {
			t.Errorf("MaxTokens = %d, want 0", ir.MaxTokens)
		}
	})
	t.Run("claude stop_sequences as object degrades to nil", func(t *testing.T) {
		ir, err := ToSafeIR(json.RawMessage(`{"model":"m","stop_sequences":{"a":1},"messages":[{"role":"user","content":"hi"}]}`), engine.FormatAnthropic)
		if err != nil {
			t.Fatalf("ToSafeIR: %v", err)
		}
		if ir.Stop != nil {
			t.Errorf("Stop = %v, want nil", ir.Stop)
		}
	})
	t.Run("claude text block text as number degrades to empty", func(t *testing.T) {
		ir, err := ToSafeIR(json.RawMessage(`{"model":"m","messages":[{"role":"user","content":[{"type":"text","text":5}]}]}`), engine.FormatAnthropic)
		if err != nil {
			t.Fatalf("ToSafeIR: %v", err)
		}
		if ir.Messages[0].Text != "" {
			t.Errorf("Text = %q, want empty", ir.Messages[0].Text)
		}
	})
	t.Run("claude thinking as number degrades to empty", func(t *testing.T) {
		ir, err := ToSafeIR(json.RawMessage(`{"model":"m","messages":[{"role":"assistant","content":[{"type":"thinking","thinking":9}]}]}`), engine.FormatAnthropic)
		if err != nil {
			t.Fatalf("ToSafeIR: %v", err)
		}
		if ir.Messages[0].Thinking != "" {
			t.Errorf("Thinking = %q, want empty", ir.Messages[0].Thinking)
		}
	})
	t.Run("claude tool_use id and name as numbers degrade", func(t *testing.T) {
		ir, err := ToSafeIR(json.RawMessage(`{"model":"m","messages":[{"role":"assistant","content":[{"type":"tool_use","id":5,"name":7,"input":{}}]}]}`), engine.FormatAnthropic)
		if err != nil {
			t.Fatalf("ToSafeIR: %v", err)
		}
		tc := ir.Messages[0].ToolCalls[0]
		if tc.ID != "" || tc.Name != "" {
			t.Errorf("ToolCall = %+v, want empty id/name", tc)
		}
	})
	t.Run("claude tool_result id and content as numbers degrade", func(t *testing.T) {
		ir, err := ToSafeIR(json.RawMessage(`{"model":"m","messages":[{"role":"user","content":[{"type":"tool_result","tool_use_id":5,"content":7}]}]}`), engine.FormatAnthropic)
		if err != nil {
			t.Fatalf("ToSafeIR: %v", err)
		}
		if ir.Messages[0].ToolCallID != "" || ir.Messages[0].Text != "" {
			t.Errorf("Message = %+v, want empty id/text", ir.Messages[0])
		}
	})
	t.Run("codex model as number degrades to empty", func(t *testing.T) {
		ir, err := ToSafeIR(json.RawMessage(`{"model":123,"input":"hi"}`), engine.FormatCodexResponses)
		if err != nil {
			t.Fatalf("ToSafeIR: %v", err)
		}
		if ir.Model != "" {
			t.Errorf("Model = %q, want empty", ir.Model)
		}
	})
	t.Run("codex malformed input element is skipped", func(t *testing.T) {
		ir, err := ToSafeIR(json.RawMessage(`{"model":"m","input":[42]}`), engine.FormatCodexResponses)
		if err != nil {
			t.Fatalf("ToSafeIR: %v", err)
		}
		if len(ir.Messages) != 0 {
			t.Errorf("Messages = %+v, want empty", ir.Messages)
		}
	})
	t.Run("codex input object content as number degrades to empty text", func(t *testing.T) {
		ir, err := ToSafeIR(json.RawMessage(`{"model":"m","input":[{"role":"user","content":42}]}`), engine.FormatCodexResponses)
		if err != nil {
			t.Fatalf("ToSafeIR: %v", err)
		}
		if len(ir.Messages) != 1 || ir.Messages[0].Text != "" {
			t.Errorf("Messages = %+v, want single message with empty text", ir.Messages)
		}
	})
	t.Run("codex instructions as array degrades to empty", func(t *testing.T) {
		ir, err := ToSafeIR(json.RawMessage(`{"model":"m","input":"hi","instructions":["a"]}`), engine.FormatCodexResponses)
		if err != nil {
			t.Fatalf("ToSafeIR: %v", err)
		}
		if ir.System != "" {
			t.Errorf("System = %q, want empty", ir.System)
		}
	})
	t.Run("codex max_output_tokens as string degrades to 0", func(t *testing.T) {
		ir, err := ToSafeIR(json.RawMessage(`{"model":"m","input":"hi","max_output_tokens":"77"}`), engine.FormatCodexResponses)
		if err != nil {
			t.Fatalf("ToSafeIR: %v", err)
		}
		if ir.MaxTokens != 0 {
			t.Errorf("MaxTokens = %d, want 0", ir.MaxTokens)
		}
	})
}

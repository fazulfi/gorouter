package claude_test

import (
	"encoding/json"
	"testing"

	"gorouter/internal/engine/formats/claude"
)

func FuzzClaudeCodecDecode(f *testing.F) {
	seeds := []string{
		`{"model":"claude-3","messages":[{"role":"user","content":"hi"}],"max_tokens":100}`,
		`{"messages":[{"role":"user","content":"hi"}]}`,
		`{}`,
		`invalid`,
	}
	for _, s := range seeds {
		f.Add(s)
	}

	c := claude.NewMessagesCodec()
	f.Fuzz(func(t *testing.T, data string) {
		_, err := c.DecodeRequest(json.RawMessage(data))
		if err != nil {
			_ = err.Error()
		}
	})
}

func FuzzClaudeCodecStream(f *testing.F) {
	seeds := []string{
		`{"type":"message_stop"}`,
		`{"type":"message_delta","delta":{"stop_reason":"end_turn","text":""}}`,
		`{"type":"content_block_delta","index":0,"delta":{"text":"hello"}}`,
		`{"type":"error","error":{"type":"err","message":"fail"}}`,
		`invalid`,
	}
	for _, s := range seeds {
		f.Add(s)
	}

	c := claude.NewMessagesCodec()
	f.Fuzz(func(t *testing.T, data string) {
		_, err := c.DecodeStreamChunk("", []byte(data))
		if err != nil {
			_ = err.Error()
		}
	})
}

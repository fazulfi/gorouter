package openai_test

import (
	"encoding/json"
	"testing"

	"gorouter/internal/engine/formats/openai"
)

func FuzzChatCodecDecode(f *testing.F) {
	seeds := []string{
		`{"model":"gpt-4","messages":[{"role":"user","content":"hi"}],"stream":true}`,
		`{"model":"gpt-4","messages":[]}`,
		`{"messages":[{"role":"user","content":"hi"}]}`,
		`{}`,
		`invalid`,
	}
	for _, s := range seeds {
		f.Add(s)
	}

	c := openai.NewChatCodec()
	f.Fuzz(func(t *testing.T, data string) {
		_, err := c.DecodeRequest(json.RawMessage(data))
		if err != nil {
			_ = err.Error()
		}
	})
}

func FuzzChatCodecStream(f *testing.F) {
	seeds := []string{
		`{"choices":[{"delta":{"content":"hi"},"finish_reason":null}]}`,
		`{"choices":[{"delta":{},"finish_reason":"stop"}]}`,
		`{"error":{"message":"err","type":"test"}}`,
		`{}`,
		`invalid`,
	}
	for _, s := range seeds {
		f.Add(s)
	}

	c := openai.NewChatCodec()
	f.Fuzz(func(t *testing.T, data string) {
		chunk, err := c.DecodeStreamChunk("", []byte(data))
		if err != nil {
			_ = err.Error()
			return
		}
		_, encErr := c.EncodeStreamChunk(chunk)
		if encErr != nil {
			_ = encErr.Error()
		}
	})
}

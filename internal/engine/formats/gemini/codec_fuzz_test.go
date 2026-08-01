package gemini_test

import (
	"encoding/json"
	"testing"

	"gorouter/internal/engine/formats/gemini"
)

func FuzzGeminiCodecDecode(f *testing.F) {
	seeds := []string{
		`{"contents":[{"role":"user","parts":[{"text":"hi"}]}],"temperature":0.7}`,
		`{"contents":[]}`,
		`{}`,
		`invalid`,
	}
	for _, s := range seeds {
		f.Add(s)
	}

	c := gemini.NewGenerateCodec()
	f.Fuzz(func(t *testing.T, data string) {
		_, err := c.DecodeRequest(json.RawMessage(data))
		if err != nil {
			_ = err.Error()
		}
	})
}

func FuzzGeminiCodecStream(f *testing.F) {
	seeds := []string{
		`{"candidates":[{"index":0,"content":{"parts":[{"text":"hi"}],"role":"model"}}]}`,
		`{"candidates":[{"finishReason":"STOP"}]}`,
		`{"error":{"code":400,"message":"bad","status":"INVALID_ARGUMENT"}}`,
		`invalid`,
	}
	for _, s := range seeds {
		f.Add(s)
	}

	c := gemini.NewGenerateCodec()
	f.Fuzz(func(t *testing.T, data string) {
		_, err := c.DecodeStreamChunk("", []byte(data))
		if err != nil {
			_ = err.Error()
		}
	})
}

package responses_test

import (
	"encoding/json"
	"testing"

	"gorouter/internal/engine/formats/responses"
)

func FuzzResponsesCodecDecode(f *testing.F) {
	seeds := []string{
		`{"model":"gpt-4","input":"hello","instructions":"help"}`,
		`{"model":"gpt-4","input":[{"role":"user","content":"hi"}]}`,
		`{}`,
		`invalid`,
	}
	for _, s := range seeds {
		f.Add(s)
	}

	c := responses.NewResponsesCodec()
	f.Fuzz(func(t *testing.T, data string) {
		_, err := c.DecodeRequest(json.RawMessage(data))
		if err != nil {
			_ = err.Error()
		}
	})
}

func FuzzResponsesCodecStream(f *testing.F) {
	seeds := []string{
		`{"type":"response.completed","data":{"id":"r_1"}}`,
		`{"type":"response.failed","error":{"message":"err","code":"bad"}}`,
		`{"type":"done","data":"[DONE]"}`,
		`{"type":"error","error":{"message":"err","code":"e"}}`,
		`invalid`,
	}
	for _, s := range seeds {
		f.Add(s)
	}

	c := responses.NewResponsesCodec()
	f.Fuzz(func(t *testing.T, data string) {
		_, err := c.DecodeStreamChunk("", []byte(data))
		if err != nil {
			_ = err.Error()
		}
	})
}

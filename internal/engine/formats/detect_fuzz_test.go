package formats_test

import (
	"context"
	"encoding/json"
	"testing"

	"gorouter/internal/engine/formats"
)

func FuzzDetect(f *testing.F) {
	seeds := []struct {
		endpoint string
		body     string
	}{
		{"/v1/chat/completions", `{"model":"gpt-4","messages":[{"role":"user","content":"hi"}]}`},
		{"/v1/responses", `{"model":"gpt-4","input":"hello"}`},
		{"/v1/messages", `{"model":"claude-3","messages":[{"role":"user","content":"hi"}]}`},
		{"/v1beta/models/gemini-pro:generateContent", `{"contents":[{"parts":[{"text":"hi"}]}]}`},
		{"/api/generate", `{"model":"gpt-4","messages":[]}`},
		{"", `{}`},
		{"/v1/unknown", ``},
	}
	for _, s := range seeds {
		f.Add(s.endpoint, s.body)
	}

	d := formats.NewDetector()
	ctx := context.Background()

	f.Fuzz(func(t *testing.T, endpoint string, body string) {
		_, err := d.Detect(ctx, endpoint, json.RawMessage(body))
		// Must never panic; errors are acceptable.
		if err != nil {
			_ = err.Error() // must be safe to call
		}
	})
}

package safeir

import (
	"encoding/json"
	"testing"

	"gorouter/internal/domain/engine"
)

func FuzzSafeIRToOpenAI(f *testing.F) {
	f.Add([]byte(`{"model":"gpt-4","messages":[{"role":"user","content":"hi"}]}`))
	f.Add([]byte(`{"model":"claude","messages":[{"role":"user","content":"hi"}]}`))
	f.Add([]byte(`{"contents":[{"role":"user","parts":[{"text":"hi"}]}]}`))
	f.Fuzz(func(t *testing.T, data []byte) {
		// Ensure data is valid JSON before proceeding.
		if !json.Valid(data) {
			return
		}
		// Try each format as source.
		for _, fmt := range []engine.RequestFormat{
			engine.FormatOpenAIChat,
			engine.FormatAnthropic,
			engine.FormatGemini,
			engine.FormatCodexResponses,
		} {
			ir, err := ToSafeIR(data, fmt)
			if err != nil {
				continue
			}
			_, err = FromSafeIR(ir, engine.FormatOpenAIChat)
			if err != nil {
				if err == ErrLossyTranslation {
					continue // expected for lossy content
				}
			}
		}
	})
}

func FuzzSafeIRRoundTrip(f *testing.F) {
	f.Add([]byte(`{"model":"gpt-4","messages":[{"role":"user","content":"hi"}]}`))
	f.Add([]byte(`{}`))
	f.Fuzz(func(t *testing.T, data []byte) {
		if !json.Valid(data) {
			return
		}
		ir, err := ToSafeIR(data, engine.FormatOpenAIChat)
		if err != nil {
			return
		}
		_, err = FromSafeIR(ir, engine.FormatOpenAIChat)
		if err != nil {
			if err == ErrLossyTranslation {
				return
			}
			t.Errorf("round-trip failed: %v", err)
		}
	})
}

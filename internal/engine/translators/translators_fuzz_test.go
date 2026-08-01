package translators

import (
	"encoding/json"
	"testing"

	"gorouter/internal/domain/engine"
)

func FuzzRegistryGetTranslator(f *testing.F) {
	formats := []engine.RequestFormat{
		engine.FormatOpenAIChat,
		engine.FormatOpenAICompat,
		engine.FormatCodexResponses,
		engine.FormatAnthropic,
		engine.FormatGemini,
		engine.RequestFormat("unknown"),
		engine.RequestFormat(""),
	}
	for _, src := range formats {
		for _, dst := range formats {
			body, _ := json.Marshal(map[string]string{"src": string(src), "dst": string(dst)})
			f.Add(body)
		}
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		var m map[string]string
		if err := json.Unmarshal(data, &m); err != nil {
			return
		}
		reg := NewRegistry()
		tr, err := reg.GetTranslator(engine.RequestFormat(m["src"]), engine.RequestFormat(m["dst"]))
		if err == nil && tr == nil {
			t.Error("GetTranslator returned nil with nil error")
		}
	})
}

func FuzzNoopTranslator(f *testing.F) {
	reg := NewRegistry()
	f.Add([]byte(`{"model":"test","messages":[{"role":"user","content":"hi"}]}`))
	f.Fuzz(func(t *testing.T, body []byte) {
		tr, err := reg.GetTranslator(engine.FormatOpenAIChat, engine.FormatOpenAIChat)
		if err != nil {
			return
		}
		req := &engine.Request{
			Format:     engine.FormatOpenAIChat,
			RawBody:    body,
			MappedBody: body,
		}
		out, err := tr.TranslateRequest(nil, req)
		if err != nil {
			return
		}
		if out != req {
			t.Error("noop should return same pointer")
		}
	})
}

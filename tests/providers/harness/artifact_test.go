package harness_test

import (
	"strings"
	"testing"

	"gorouter/internal/domain/engine"
	"gorouter/tests/providers/harness"
)

func TestCompareArtifactsSemanticEquality(t *testing.T) {
	want := []byte(`{"a":1,"b":{"c":[1,2,3]}}`)
	got := []byte(`{
	  "b": {"c": [1, 2, 3]},
	  "a": 1
	}`)
	if err := harness.CompareArtifacts(got, want); err != nil {
		t.Errorf("semantically equal artifacts must compare clean, got %v", err)
	}
}

func TestCompareArtifactsMismatch(t *testing.T) {
	want := []byte(`{"a":1,"b":"pong"}`)
	got := []byte(`{"a":1,"b":"other"}`)
	err := harness.CompareArtifacts(got, want)
	if err == nil {
		t.Fatal("mismatched artifacts must fail comparison")
	}
	if !strings.Contains(err.Error(), "b") {
		t.Errorf("mismatch error must pinpoint the differing path, got %v", err)
	}
}

func TestCompareArtifactsInvalidJSON(t *testing.T) {
	if err := harness.CompareArtifacts([]byte(`{"a":`), []byte(`{"a":1}`)); err == nil {
		t.Error("invalid artifact JSON must fail comparison")
	}
	if err := harness.CompareArtifacts([]byte(`{"a":1}`), []byte(`{`)); err == nil {
		t.Error("invalid golden fixture JSON must fail comparison")
	}
}

func TestValidateArtifactShape(t *testing.T) {
	valid := map[engine.RequestFormat]string{
		engine.FormatOpenAIChat:     `{"choices":[{"message":{"content":"pong"}}]}`,
		engine.FormatOpenAICompat:   `{"choices":[{"text":"pong"}]}`,
		engine.FormatCodexResponses: `{"output":[{"type":"message"}]}`,
		engine.FormatAnthropic:      `{"content":[{"type":"text","text":"pong"}]}`,
		engine.FormatGemini:         `{"candidates":[{"content":{"parts":[{"text":"pong"}]}}]}`,
	}
	for format, body := range valid {
		if err := harness.ValidateArtifactShape(format, []byte(body)); err != nil {
			t.Errorf("shape %s must validate: %v", format, err)
		}
	}
	errorBody := `{"error":{"type":"authentication_error","message":"unauthorized"}}`
	for format := range valid {
		if err := harness.ValidateArtifactShape(format, []byte(errorBody)); err != nil {
			t.Errorf("structured error shape must validate for %s: %v", format, err)
		}
	}
	invalid := map[engine.RequestFormat]string{
		engine.FormatOpenAIChat:     `{"foo":1}`,
		engine.FormatOpenAICompat:   `{"choices":[]}`,
		engine.FormatCodexResponses: `{"output":[]}`,
		engine.FormatAnthropic:      `{"content":[]}`,
		engine.FormatGemini:         `{"candidates":[]}`,
	}
	for format, body := range invalid {
		if err := harness.ValidateArtifactShape(format, []byte(body)); err == nil {
			t.Errorf("shape %s must reject %s", format, body)
		}
	}
	if err := harness.ValidateArtifactShape(engine.FormatOpenAIChat, []byte("not json")); err == nil {
		t.Error("non-JSON artifact must fail shape validation")
	}
	if err := harness.ValidateArtifactShape(engine.FormatOpenAIChat, []byte(`[1,2,3]`)); err == nil {
		t.Error("non-object JSON artifact must fail shape validation")
	}
}

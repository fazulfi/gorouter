package engine

import (
	"testing"

	"github.com/google/uuid"
)

func TestRequestFormatConstants(t *testing.T) {
	if FormatOpenAIChat != "openai_chat" {
		t.Errorf("FormatOpenAIChat = %q, want %q", FormatOpenAIChat, "openai_chat")
	}
	if FormatOpenAICompat != "openai_compat" {
		t.Errorf("FormatOpenAICompat = %q, want %q", FormatOpenAICompat, "openai_compat")
	}
	if FormatCodexResponses != "codex_responses" {
		t.Errorf("FormatCodexResponses = %q, want %q", FormatCodexResponses, "codex_responses")
	}
	if FormatAnthropic != "anthropic" {
		t.Errorf("FormatAnthropic = %q, want %q", FormatAnthropic, "anthropic")
	}
	if FormatGemini != "gemini" {
		t.Errorf("FormatGemini = %q, want %q", FormatGemini, "gemini")
	}
}

func TestTerminalStateConstants(t *testing.T) {
	if TerminalSuccess != "success" {
		t.Errorf("TerminalSuccess = %q, want %q", TerminalSuccess, "success")
	}
	if TerminalError != "error" {
		t.Errorf("TerminalError = %q, want %q", TerminalError, "error")
	}
	if TerminalCancelled != "cancelled" {
		t.Errorf("TerminalCancelled = %q, want %q", TerminalCancelled, "cancelled")
	}
	if TerminalTimeout != "timeout" {
		t.Errorf("TerminalTimeout = %q, want %q", TerminalTimeout, "timeout")
	}
	if TerminalStreamAbort != "stream_abort" {
		t.Errorf("TerminalStreamAbort = %q, want %q", TerminalStreamAbort, "stream_abort")
	}
}

func TestUsage(t *testing.T) {
	u := Usage{PromptTokens: 10, CompletionTokens: 20, TotalTokens: 30}
	if u.PromptTokens != 10 {
		t.Errorf("PromptTokens = %d, want %d", u.PromptTokens, 10)
	}
	if u.CompletionTokens != 20 {
		t.Errorf("CompletionTokens = %d, want %d", u.CompletionTokens, 20)
	}
	if u.TotalTokens != 30 {
		t.Errorf("TotalTokens = %d, want %d", u.TotalTokens, 30)
	}
}

func TestRequest(t *testing.T) {
	id := uuid.New()
	temp := 0.7
	uid := uuid.New()

	r := Request{
		ID:          id,
		Format:      FormatOpenAIChat,
		Model:       "gpt-4",
		RawBody:     []byte(`{"model":"gpt-4"}`),
		MappedBody:  []byte(`{"model":"gpt-4"}`),
		Headers:     map[string]string{"Authorization": "Bearer test"},
		Stream:      true,
		MaxTokens:   4096,
		Temperature: &temp,
		UserID:      &uid,
	}

	if r.ID != id {
		t.Errorf("ID mismatch")
	}
	if r.Format != FormatOpenAIChat {
		t.Errorf("Format mismatch")
	}
	if r.Model != "gpt-4" {
		t.Errorf("Model mismatch")
	}
	if r.Stream != true {
		t.Errorf("Stream mismatch")
	}
	if r.MaxTokens != 4096 {
		t.Errorf("MaxTokens mismatch")
	}
	if *r.Temperature != 0.7 {
		t.Errorf("Temperature mismatch")
	}
	if *r.UserID != uid {
		t.Errorf("UserID mismatch")
	}
}

func TestResponse(t *testing.T) {
	id := uuid.New()
	u := &Usage{PromptTokens: 10, CompletionTokens: 20, TotalTokens: 30}

	resp := Response{
		RequestID:  id,
		Body:       []byte(`{"choices":[]}`),
		Stream:     nil,
		Model:      "gpt-4",
		Usage:      u,
		StatusCode: 200,
		Headers:    map[string]string{"Content-Type": "application/json"},
	}

	if resp.RequestID != id {
		t.Errorf("RequestID mismatch")
	}
	if resp.Model != "gpt-4" {
		t.Errorf("Model mismatch")
	}
	if resp.StatusCode != 200 {
		t.Errorf("StatusCode = %d, want %d", resp.StatusCode, 200)
	}
	if resp.Usage.TotalTokens != 30 {
		t.Errorf("Usage.TotalTokens = %d, want %d", resp.Usage.TotalTokens, 30)
	}
}

func TestStreamRef(t *testing.T) {
	// Verify StreamRef is an interface
	var _ StreamRef = &mockStreamRef{}
}

type mockStreamRef struct{}

func (m *mockStreamRef) StreamID() uuid.UUID {
	return uuid.Nil
}

func TestRequestHasID(t *testing.T) {
	r := Request{}
	if r.ID != uuid.Nil {
		t.Error("zero-value Request should have nil UUID ID")
	}
}

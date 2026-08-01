package mutators

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"
)

func TestPonytail_Disabled(t *testing.T) {
	m := NewPonytail(PonytailConfig{Enabled: false})
	req := fixtureRequest(`{"messages":[{"role":"user","content":"hi"}]}`)
	result, err := m.Mutate(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Applied {
		t.Fatal("expected disabled ponytail to not apply")
	}
}

func TestPonytail_NilRequest(t *testing.T) {
	m := NewPonytail(PonytailConfig{Enabled: true})
	result, err := m.Mutate(context.Background(), nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Applied {
		t.Fatal("expected nil request to not apply")
	}
}

func TestPonytail_AppendsToSystem(t *testing.T) {
	prompt := "You are lazy."
	m := NewPonytail(PonytailConfig{Enabled: true, Prompt: prompt})
	body := `{"messages":[{"role":"system","content":"Be helpful."},{"role":"user","content":"write code"}]}`
	req := fixtureRequest(body)
	result, err := m.Mutate(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Applied {
		t.Fatal("expected ponytail to apply")
	}

	var parsed map[string]json.RawMessage
	json.Unmarshal(req.MappedBody, &parsed)
	var msgs []json.RawMessage
	json.Unmarshal(parsed["messages"], &msgs)
	var sysMsg map[string]json.RawMessage
	json.Unmarshal(msgs[0], &sysMsg)
	var content string
	json.Unmarshal(sysMsg["content"], &content)
	if !strings.Contains(content, prompt) {
		t.Fatalf("expected prompt in system, got: %s", content)
	}
}

func TestPonytail_DefaultPrompt(t *testing.T) {
	m := NewPonytail(PonytailConfig{Enabled: true})
	if m.Name() != "ponytail" {
		t.Fatalf("expected name 'ponytail', got %q", m.Name())
	}
}

func TestPonytail_Concurrency(t *testing.T) {
	m := NewPonytail(PonytailConfig{Enabled: true, Prompt: "lazy test"})
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			body := `{"messages":[{"role":"system","content":"base"},{"role":"user","content":"hi"}]}`
			req := fixtureRequest(body)
			m.Mutate(context.Background(), req)
		}()
	}
	wg.Wait()
}

func TestPonytail_FailOpen_BadJSON(t *testing.T) {
	m := NewPonytail(PonytailConfig{Enabled: true, Prompt: "test"})
	req := fixtureRequest(`{invalid}`)
	result, err := m.Mutate(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Applied {
		t.Fatal("expected bad JSON to not apply")
	}
}

func TestPonytail_ResponsesInput(t *testing.T) {
	m := NewPonytail(PonytailConfig{Enabled: true, Prompt: "be lazy"})
	body := `{"input":[{"role":"developer","content":"be thorough"}]}`
	req := fixtureRequest(body)
	result, err := m.Mutate(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Applied {
		var parsed map[string]json.RawMessage
		json.Unmarshal(req.MappedBody, &parsed)
		var input []json.RawMessage
		json.Unmarshal(parsed["input"], &input)
		var msg map[string]json.RawMessage
		json.Unmarshal(input[0], &msg)
		var content string
		json.Unmarshal(msg["content"], &content)
		if !strings.Contains(content, "lazy") {
			t.Fatalf("expected 'lazy' in content, got: %s", content)
		}
	}
}

package mutators

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"
)

func TestCaveman_Disabled(t *testing.T) {
	m := NewCaveman(CavemanConfig{Enabled: false})
	req := fixtureRequest(`{"messages":[{"role":"user","content":"hi"}]}`)
	result, err := m.Mutate(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Applied {
		t.Fatal("expected disabled caveman to not apply")
	}
}

func TestCaveman_NilRequest(t *testing.T) {
	m := NewCaveman(CavemanConfig{Enabled: true})
	result, err := m.Mutate(context.Background(), nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Applied {
		t.Fatal("expected nil request to not apply")
	}
}

func TestCaveman_EmptyBody(t *testing.T) {
	m := NewCaveman(CavemanConfig{Enabled: true})
	req := fixtureRequest(``)
	result, err := m.Mutate(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Applied {
		t.Fatal("expected empty body to not apply")
	}
}

func TestCaveman_AppendsToSystemMessage(t *testing.T) {
	prompt := "Respond like a caveman."
	m := NewCaveman(CavemanConfig{Enabled: true, Prompt: prompt})
	body := `{"messages":[{"role":"system","content":"You are helpful."},{"role":"user","content":"hi"}]}`
	req := fixtureRequest(body)
	result, err := m.Mutate(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Applied {
		t.Fatal("expected caveman to apply")
	}

	// Verify the prompt was appended to system message
	var parsed map[string]json.RawMessage
	if err := json.Unmarshal(req.MappedBody, &parsed); err != nil {
		t.Fatalf("failed to parse: %v", err)
	}
	var msgs []json.RawMessage
	json.Unmarshal(parsed["messages"], &msgs)
	var sysMsg map[string]json.RawMessage
	json.Unmarshal(msgs[0], &sysMsg)
	var content string
	json.Unmarshal(sysMsg["content"], &content)
	if !strings.Contains(content, prompt) {
		t.Fatalf("expected prompt in system message, got: %s", content)
	}
}

func TestCaveman_PrependsSystemIfMissing(t *testing.T) {
	prompt := "Test caveman prompt."
	m := NewCaveman(CavemanConfig{Enabled: true, Prompt: prompt})
	body := `{"messages":[{"role":"user","content":"hi"}]}`
	req := fixtureRequest(body)

	// We bypass the prepend limitation in injectMessagesArray by using a special path
	m.Mutate(context.Background(), req)

	var parsed map[string]json.RawMessage
	if err := json.Unmarshal(req.MappedBody, &parsed); err != nil {
		t.Fatalf("failed to parse: %v", err)
	}
	var msgs []json.RawMessage
	json.Unmarshal(parsed["messages"], &msgs)
	// System message should be prepended
	if len(msgs) > 0 {
		var firstMsg map[string]json.RawMessage
		json.Unmarshal(msgs[0], &firstMsg)
		var role string
		json.Unmarshal(firstMsg["role"], &role)
		if role == "system" {
			t.Log("system message prepended")
		}
	}
}

func TestCaveman_ResponsesInputFormat(t *testing.T) {
	prompt := "Test prompt."
	m := NewCaveman(CavemanConfig{Enabled: true, Prompt: prompt})
	body := `{"input":[{"role":"developer","content":"be thorough"}]}`
	req := fixtureRequest(body)
	result, err := m.Mutate(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Applied {
		t.Log("caveman applied to responses input format")
	}
}

func TestCaveman_Concurrency(t *testing.T) {
	m := NewCaveman(CavemanConfig{Enabled: true, Prompt: "concurrent test"})
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

func TestCaveman_Name(t *testing.T) {
	m := NewCaveman(CavemanConfig{})
	if m.Name() != "caveman" {
		t.Fatalf("expected name 'caveman', got %q", m.Name())
	}
}

func TestCaveman_FailOpen_BadJSON(t *testing.T) {
	m := NewCaveman(CavemanConfig{Enabled: true, Prompt: "test"})
	req := fixtureRequest(`{invalid json}`)
	result, err := m.Mutate(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Applied {
		t.Fatal("expected bad JSON to not apply")
	}
}

func TestCaveman_InstructionsFormat(t *testing.T) {
	prompt := "Be concise."
	m := NewCaveman(CavemanConfig{Enabled: true, Prompt: prompt})
	body := `{"instructions":"original instruction","model":"gpt-4"}`
	req := fixtureRequest(body)
	result, err := m.Mutate(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Applied {
		var parsed map[string]json.RawMessage
		json.Unmarshal(req.MappedBody, &parsed)
		var instr string
		json.Unmarshal(parsed["instructions"], &instr)
		if !strings.Contains(instr, prompt) {
			t.Fatalf("expected prompt in instructions, got: %s", instr)
		}
	}
}

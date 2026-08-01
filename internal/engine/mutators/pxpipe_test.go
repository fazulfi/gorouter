package mutators

import (
	"context"
	"encoding/json"
	"sync"
	"testing"

	"gorouter/internal/domain/engine"
)

func TestPxpipe_Disabled(t *testing.T) {
	m := NewPxpipe(PxpipeConfig{Enabled: false})
	req := fixtureRequest(`{"messages":[{"role":"user","content":"hi"}]}`)
	result, err := m.Mutate(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Applied {
		t.Fatal("expected disabled pxpipe to not apply")
	}
}

func TestPxpipe_NilRequest(t *testing.T) {
	m := NewPxpipe(PxpipeConfig{Enabled: true})
	result, err := m.Mutate(context.Background(), nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Applied {
		t.Fatal("expected nil request to not apply")
	}
}

func TestPxpipe_EmptyBody(t *testing.T) {
	m := NewPxpipe(PxpipeConfig{Enabled: true})
	req := fixtureRequest(``)
	result, err := m.Mutate(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Applied {
		t.Fatal("expected empty body to not apply")
	}
}

func TestPxpipe_NoTransformFn(t *testing.T) {
	m := NewPxpipe(PxpipeConfig{Enabled: true})
	req := fixtureRequest(`{"messages":[{"role":"user","content":"hi"}]}`)
	result, err := m.Mutate(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Applied {
		t.Fatal("expected no transform fn to not apply")
	}
}

func TestPxpipe_TransformSuccess(t *testing.T) {
	m := NewPxpipe(PxpipeConfig{Enabled: true, MinChars: 1})
	pxm, ok := m.(*pxpipeMutator)
	if !ok {
		t.Fatal("expected *pxpipeMutator")
	}
	pxm.SetTransform(func(body []byte) ([]byte, error) {
		// Return modified body (swap model name to show it worked)
		return []byte(`{"model":"transformed","messages":[{"role":"user","content":"done"}]}`), nil
	})

	req := fixtureRequest(`{"model":"original","messages":[{"role":"user","content":"hello"}]}`)
	result, err := m.Mutate(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Applied {
		t.Fatal("expected pxpipe to apply")
	}
	// Verify body was transformed
	var body map[string]interface{}
	if err := bodyAsMap(req, &body); err != nil {
		t.Fatalf("failed to parse: %v", err)
	}
	if body["model"] != "transformed" {
		t.Fatalf("expected 'transformed' model, got %v", body["model"])
	}
}

func TestPxpipe_BelowMinChars(t *testing.T) {
	m := NewPxpipe(PxpipeConfig{Enabled: true, MinChars: 999999})
	pxm, ok := m.(*pxpipeMutator)
	if !ok {
		t.Fatal("expected *pxpipeMutator")
	}
	pxm.SetTransform(func(body []byte) ([]byte, error) {
		return []byte(`{"transformed":true}`), nil
	})
	req := fixtureRequest(`{"messages":[{"role":"user","content":"hi"}]}`)
	result, err := m.Mutate(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Applied {
		t.Fatal("expected below min chars to not apply")
	}
}

func TestPxpipe_Concurrency(t *testing.T) {
	m := NewPxpipe(PxpipeConfig{Enabled: true, MinChars: 1})
	pxm, ok := m.(*pxpipeMutator)
	if !ok {
		t.Fatal("expected *pxpipeMutator")
	}
	pxm.SetTransform(func(body []byte) ([]byte, error) {
		return []byte(`{"t":true}`), nil
	})

	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			req := fixtureRequest(`{"test":"data"}`)
			m.Mutate(context.Background(), req)
		}()
	}
	wg.Wait()
}

func TestPxpipe_TransformReturnsNil(t *testing.T) {
	m := NewPxpipe(PxpipeConfig{Enabled: true, MinChars: 1})
	pxm, ok := m.(*pxpipeMutator)
	if !ok {
		t.Fatal("expected *pxpipeMutator")
	}
	pxm.SetTransform(func(body []byte) ([]byte, error) {
		return nil, nil
	})
	req := fixtureRequest(`{"test":"data"}`)
	result, err := m.Mutate(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Applied {
		t.Fatal("expected nil transform result to not apply")
	}
}

func TestPxpipe_TransformReturnsInvalidJSON(t *testing.T) {
	m := NewPxpipe(PxpipeConfig{Enabled: true, MinChars: 1})
	pxm, ok := m.(*pxpipeMutator)
	if !ok {
		t.Fatal("expected *pxpipeMutator")
	}
	pxm.SetTransform(func(body []byte) ([]byte, error) {
		return []byte(`{invalid json}`), nil
	})
	req := fixtureRequest(`{"test":"data"}`)
	result, err := m.Mutate(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Applied {
		t.Fatal("expected invalid JSON result to not apply")
	}
}

func TestPxpipe_Name(t *testing.T) {
	m := NewPxpipe(PxpipeConfig{})
	if m.Name() != "pxpipe" {
		t.Fatalf("expected name 'pxpipe', got %q", m.Name())
	}
}

func bodyAsMap(req *engine.Request, out *map[string]interface{}) error {
	return json.Unmarshal(req.MappedBody, out)
}

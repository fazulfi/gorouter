package mutators

import (
	"context"
	"sync"
	"testing"
)

func TestHeadroom_Disabled(t *testing.T) {
	m := NewHeadroom(HeadroomConfig{Enabled: false})
	req := fixtureRequest(`{"messages":[{"role":"user","content":"hi"}]}`)
	result, err := m.Mutate(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Applied {
		t.Fatal("expected disabled headroom to not apply")
	}
}

func TestHeadroom_NilRequest(t *testing.T) {
	m := NewHeadroom(HeadroomConfig{Enabled: true})
	result, err := m.Mutate(context.Background(), nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Applied {
		t.Fatal("expected nil request to not apply")
	}
}

func TestHeadroom_NoURL(t *testing.T) {
	m := NewHeadroom(HeadroomConfig{Enabled: true, URL: ""})
	req := fixtureRequest(`{"messages":[{"role":"user","content":"hi"}]}`)
	result, err := m.Mutate(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Applied {
		t.Fatal("expected no URL to not apply")
	}
}

func TestHeadroom_EmptyBody(t *testing.T) {
	m := NewHeadroom(HeadroomConfig{Enabled: true, URL: "http://localhost:8080"})
	req := fixtureRequest(``)
	result, err := m.Mutate(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Applied {
		t.Fatal("expected empty body to not apply")
	}
}

func TestHeadroom_NoMessages(t *testing.T) {
	m := NewHeadroom(HeadroomConfig{Enabled: true, URL: "http://localhost:8080"})
	req := fixtureRequest(`{"model":"gpt-4"}`)
	result, err := m.Mutate(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Applied {
		t.Fatal("expected no messages to not apply")
	}
}

func TestHeadroom_FailOpenOnNetworkError(t *testing.T) {
	m := NewHeadroom(HeadroomConfig{Enabled: true, URL: "http://127.0.0.1:1", TimeoutMs: 1})
	req := fixtureRequest(`{"messages":[{"role":"user","content":"hi"}]}`)
	result, err := m.Mutate(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error (should fail-open): %v", err)
	}
	if result.Applied {
		t.Log("headroom applied despite connection failure (unlikely)")
	}
}

func TestHeadroom_Concurrency(t *testing.T) {
	m := NewHeadroom(HeadroomConfig{Enabled: true, URL: "http://localhost:1", TimeoutMs: 1})
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			req := fixtureRequest(`{"messages":[{"role":"user","content":"test"}]}`)
			m.Mutate(context.Background(), req)
		}()
	}
	wg.Wait()
}

func TestHeadroom_Name(t *testing.T) {
	m := NewHeadroom(HeadroomConfig{})
	if m.Name() != "headroom" {
		t.Fatalf("expected name 'headroom', got %q", m.Name())
	}
}

func TestBuildCompressEndpoint(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"http://localhost:8080", "http://localhost:8080/v1/compress"},
		{"http://localhost:8080/", "http://localhost:8080/v1/compress"},
		{"http://localhost:8080/v1/compress", "http://localhost:8080/v1/compress"},
		{"http://localhost:8080/api", "http://localhost:8080/api/v1/compress"},
	}
	for _, tt := range tests {
		got := buildCompressEndpoint(tt.input)
		if got != tt.want {
			t.Errorf("buildCompressEndpoint(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

package transport

import (
	"context"
	"net/http"
	"testing"

	"github.com/google/uuid"
	"gorouter/internal/domain/provider"
)

func TestExecContext(t *testing.T) {
	acct := &provider.Account{
		ID:            uuid.New(),
		Label:         "test-account",
		AuthType:      "api_key",
		CredentialRef: "sk-test",
		Priority:      0,
		IsEnabled:     true,
	}

	ec := ExecContext{
		Account:  acct,
		BaseURL:  "https://api.openai.com",
		Endpoint: "/v1/chat/completions",
		Method:   "POST",
		Body:     []byte(`{"model":"gpt-4"}`),
		Headers:  map[string]string{"Authorization": "Bearer sk-test"},
		Stream:   false,
		ProxyURL: "",
	}

	if ec.Account.Label != "test-account" {
		t.Errorf("Account.Label = %q, want %q", ec.Account.Label, "test-account")
	}
	if ec.BaseURL != "https://api.openai.com" {
		t.Errorf("BaseURL = %q, want %q", ec.BaseURL, "https://api.openai.com")
	}
	if ec.Endpoint != "/v1/chat/completions" {
		t.Errorf("Endpoint = %q, want %q", ec.Endpoint, "/v1/chat/completions")
	}
	if ec.Method != "POST" {
		t.Errorf("Method = %q, want %q", ec.Method, "POST")
	}
	if ec.Stream != false {
		t.Errorf("Stream = %v, want %v", ec.Stream, false)
	}
}

func TestRoundTripperInterface(t *testing.T) {
	// Compile-time check: RoundTripper is an interface
	var _ RoundTripper = &mockRoundTripper{}
}

type mockRoundTripper struct{}

func (m *mockRoundTripper) RoundTrip(ctx context.Context, ec *ExecContext) (*http.Response, error) {
	return nil, nil
}

func TestExecContext_ProxyURL(t *testing.T) {
	ec := ExecContext{
		Account:  &provider.Account{ID: uuid.New()},
		BaseURL:  "https://api.openai.com",
		Endpoint: "/v1/chat/completions",
		Method:   "POST",
		ProxyURL: "http://proxy:8080",
	}
	if ec.ProxyURL != "http://proxy:8080" {
		t.Errorf("ProxyURL = %q, want %q", ec.ProxyURL, "http://proxy:8080")
	}
}

func TestExecContext_EmptyBody(t *testing.T) {
	ec := ExecContext{
		Account:  &provider.Account{ID: uuid.New()},
		BaseURL:  "https://api.openai.com",
		Endpoint: "/v1/models",
		Method:   "GET",
	}
	if ec.Body != nil {
		t.Errorf("expected nil Body for GET request")
	}
}

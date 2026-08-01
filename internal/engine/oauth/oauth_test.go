package oauth

import (
	"testing"

	"gorouter/internal/domain/oauth"
)

func TestGeneratePKCE(t *testing.T) {
	p, err := GeneratePKCE()
	if err != nil {
		t.Fatalf("GeneratePKCE: %v", err)
	}
	if p.CodeVerifier == "" {
		t.Error("CodeVerifier should not be empty")
	}
	if p.CodeChallenge == "" {
		t.Error("CodeChallenge should not be empty")
	}
	if p.Method != "S256" {
		t.Errorf("Method = %s, want S256", p.Method)
	}
}

func TestGenerateState(t *testing.T) {
	s1, err := GenerateState()
	if err != nil {
		t.Fatalf("GenerateState: %v", err)
	}
	s2, err := GenerateState()
	if err != nil {
		t.Fatalf("GenerateState: %v", err)
	}
	if s1 == s2 {
		t.Error("state values should be unique")
	}
}

func TestHashToken(t *testing.T) {
	token := "my-secret-token-12345"
	hash := HashToken(token)
	if hash == "" {
		t.Error("hash should not be empty")
	}
	if len(hash) != 64 {
		t.Errorf("expected SHA-256 hex (64 chars), got %d", len(hash))
	}
	if HashToken(token) != HashToken(token) {
		t.Error("hash should be deterministic")
	}
}

func TestSessionTTL(t *testing.T) {
	tests := []struct {
		flowID oauth.FlowID
		want   int64
	}{
		{oauth.FlowCodex, 5},
		{oauth.FlowXAI, 5},
		{oauth.FlowKimchi, 2},
		{oauth.FlowCursor, 2},
		{oauth.FlowOpenAI, 10},
		{oauth.FlowClaude, 10},
	}
	for _, tt := range tests {
		got := SessionTTL(tt.flowID)
		if int64(got.Minutes()) != tt.want {
			t.Errorf("SessionTTL(%s) = %v, want %d min", tt.flowID, got, tt.want)
		}
	}
}

func TestVerificationURIFromDevice(t *testing.T) {
	uri := VerificationURIFromDevice("https://example.com/activate", "ABC-123")
	if uri != "https://example.com/activate?user_code=ABC-123" {
		t.Errorf("unexpected URI: %s", uri)
	}
}

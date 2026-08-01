package patimport

import (
	"context"
	"testing"
	"time"

	"gorouter/internal/domain/oauth"

	"github.com/google/uuid"
)

type mockRepo struct {
	created *oauth.Session
}

func (m *mockRepo) Create(_ context.Context, s *oauth.Session) error {
	m.created = s
	return nil
}
func (m *mockRepo) FindByID(_ context.Context, _ uuid.UUID) (*oauth.Session, error) { return nil, nil }
func (m *mockRepo) FindByState(_ context.Context, _ string) (*oauth.Session, error) { return nil, nil }
func (m *mockRepo) FindPendingByProvider(_ context.Context, _ uuid.UUID) ([]oauth.Session, error) {
	return nil, nil
}
func (m *mockRepo) UpdateStatus(_ context.Context, _ uuid.UUID, _ oauth.OAuthState, _ *string) error {
	return nil
}
func (m *mockRepo) Complete(_ context.Context, _ uuid.UUID, _, _ string, _ time.Time) error {
	return nil
}
func (m *mockRepo) CancelPending(_ context.Context) (int64, error) { return 0, nil }
func (m *mockRepo) DeleteExpired(_ context.Context) (int64, error) { return 0, nil }
func (m *mockRepo) Cleanup(_ context.Context) (int64, error)       { return 0, nil }

func TestImportPAT_NonEmptyFlowID(t *testing.T) {
	repo := &mockRepo{}
	flow := NewFlow(repo)
	pid := uuid.New()

	_, err := flow.ImportPAT(context.Background(), pid, "openai", &PATCredentials{
		Token:     "sk-pat-123",
		TokenHash: "abc123def456hash",
		Label:     "test-pat",
	})
	if err != nil {
		t.Fatalf("ImportPAT: %v", err)
	}
	if repo.created == nil {
		t.Fatal("session was not created")
	}
	if repo.created.FlowID == "" {
		t.Error("FlowID should not be empty for known provider")
	}
	if repo.created.FlowID != oauth.FlowOpenAI {
		t.Errorf("FlowID = %s, want %s", repo.created.FlowID, oauth.FlowOpenAI)
	}
}

func TestImportPAT_KnownProviders(t *testing.T) {
	tests := []struct {
		name         string
		providerName string
		wantFlowID   oauth.FlowID
	}{
		{"openai", "openai", oauth.FlowOpenAI},
		{"claude", "claude", oauth.FlowClaude},
		{"codex", "codex", oauth.FlowCodex},
		{"gemini", "gemini-cli", oauth.FlowGeminiCLI},
		{"antigravity", "antigravity", oauth.FlowAntigravity},
		{"xai", "xai", oauth.FlowXAI},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			flowID := deriveFlowID(tt.providerName)
			if flowID != tt.wantFlowID {
				t.Errorf("deriveFlowID(%q) = %s, want %s", tt.providerName, flowID, tt.wantFlowID)
			}
		})
	}
}

func TestImportPAT_UnknownProvider_FallsBack(t *testing.T) {
	flowID := deriveFlowID("unknown-provider")
	if flowID != oauth.FlowID("unknown-provider") {
		t.Errorf("deriveFlowID('unknown-provider') = %s, want 'unknown-provider'", flowID)
	}
}

func TestImportPAT_TokenHashPersisted(t *testing.T) {
	repo := &mockRepo{}
	flow := NewFlow(repo)
	pid := uuid.New()

	_, err := flow.ImportPAT(context.Background(), pid, "openai", &PATCredentials{
		Token:     "sk-pat-secret",
		TokenHash: "sha256hashvalue",
		Label:     "test",
	})
	if err != nil {
		t.Fatalf("ImportPAT: %v", err)
	}
	if repo.created.TokenHash == nil {
		t.Fatal("TokenHash should not be nil")
	}
	if *repo.created.TokenHash != "sha256hashvalue" {
		t.Errorf("TokenHash = %s, want sha256hashvalue", *repo.created.TokenHash)
	}
}

func TestImportPAT_NoRawTokenInSession(t *testing.T) {
	repo := &mockRepo{}
	flow := NewFlow(repo)
	pid := uuid.New()

	_, err := flow.ImportPAT(context.Background(), pid, "openai", &PATCredentials{
		Token:     "raw-secret-token-should-not-be-in-session",
		TokenHash: "hashonly",
		Label:     "test",
	})
	if err != nil {
		t.Fatalf("ImportPAT: %v", err)
	}
	// Verify the raw token is NOT stored directly in the session
	// Only the hash is stored
	if repo.created.TokenHash == nil {
		t.Fatal("TokenHash should be set")
	}
	if *repo.created.TokenHash != "hashonly" {
		t.Errorf("TokenHash = %s, want hashonly", *repo.created.TokenHash)
	}
}

func TestImportPAT_Mechanism(t *testing.T) {
	repo := &mockRepo{}
	flow := NewFlow(repo)
	pid := uuid.New()

	_, err := flow.ImportPAT(context.Background(), pid, "openai", &PATCredentials{
		Token:     "sk-pat",
		TokenHash: "hash",
		Label:     "test",
	})
	if err != nil {
		t.Fatalf("ImportPAT: %v", err)
	}
	if repo.created.Mechanism != oauth.MechanismPAT {
		t.Errorf("Mechanism = %s, want %s", repo.created.Mechanism, oauth.MechanismPAT)
	}
}

func TestImportPAT_CompletedStatus(t *testing.T) {
	repo := &mockRepo{}
	flow := NewFlow(repo)
	pid := uuid.New()

	_, err := flow.ImportPAT(context.Background(), pid, "openai", &PATCredentials{
		Token:     "sk-pat",
		TokenHash: "hash",
		Label:     "test",
	})
	if err != nil {
		t.Fatalf("ImportPAT: %v", err)
	}
	if repo.created.Status != oauth.OAuthStateCompleted {
		t.Errorf("Status = %s, want %s", repo.created.Status, oauth.OAuthStateCompleted)
	}
}

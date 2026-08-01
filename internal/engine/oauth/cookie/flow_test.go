package cookie

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

func TestImportCredentials_TokenHashPersisted(t *testing.T) {
	repo := &mockRepo{}
	flow := NewFlow(repo)
	pid := uuid.New()

	_, err := flow.ImportCredentials(context.Background(), pid, "kimchi", &CookieCredentials{
		CookieName:  "session",
		CookieValue: "raw-cookie-value",
		Domain:      "example.com",
		TokenHash:   "sha256hashofcookie",
	})
	if err != nil {
		t.Fatalf("ImportCredentials: %v", err)
	}
	if repo.created == nil {
		t.Fatal("session was not created")
	}
	if repo.created.TokenHash == nil {
		t.Fatal("TokenHash should not be nil when credentials provide it")
	}
	if *repo.created.TokenHash != "sha256hashofcookie" {
		t.Errorf("TokenHash = %s, want sha256hashofcookie", *repo.created.TokenHash)
	}
}

func TestImportCredentials_EmptyTokenHash(t *testing.T) {
	repo := &mockRepo{}
	flow := NewFlow(repo)
	pid := uuid.New()

	_, err := flow.ImportCredentials(context.Background(), pid, "kimchi", &CookieCredentials{
		CookieName:  "session",
		CookieValue: "raw-value",
		Domain:      "example.com",
		TokenHash:   "",
	})
	if err != nil {
		t.Fatalf("ImportCredentials: %v", err)
	}
	if repo.created.TokenHash != nil && *repo.created.TokenHash != "" {
		t.Error("TokenHash should be nil or empty when credentials provide empty hash")
	}
}

func TestImportCredentials_FlowID(t *testing.T) {
	repo := &mockRepo{}
	flow := NewFlow(repo)
	pid := uuid.New()

	_, err := flow.ImportCredentials(context.Background(), pid, "kimchi", &CookieCredentials{
		CookieName:  "session",
		CookieValue: "val",
		Domain:      "example.com",
		TokenHash:   "hash",
	})
	if err != nil {
		t.Fatalf("ImportCredentials: %v", err)
	}
	if repo.created.FlowID != oauth.FlowKimchi {
		t.Errorf("FlowID = %s, want %s", repo.created.FlowID, oauth.FlowKimchi)
	}
}

func TestImportCredentials_Mechanism(t *testing.T) {
	repo := &mockRepo{}
	flow := NewFlow(repo)
	pid := uuid.New()

	_, err := flow.ImportCredentials(context.Background(), pid, "kimchi", &CookieCredentials{
		CookieName:  "session",
		CookieValue: "val",
		Domain:      "example.com",
		TokenHash:   "hash",
	})
	if err != nil {
		t.Fatalf("ImportCredentials: %v", err)
	}
	if repo.created.Mechanism != oauth.MechanismCookieImport {
		t.Errorf("Mechanism = %s, want %s", repo.created.Mechanism, oauth.MechanismCookieImport)
	}
}

func TestImportCredentials_Status(t *testing.T) {
	repo := &mockRepo{}
	flow := NewFlow(repo)
	pid := uuid.New()

	_, err := flow.ImportCredentials(context.Background(), pid, "kimchi", &CookieCredentials{
		CookieName:  "session",
		CookieValue: "val",
		Domain:      "example.com",
		TokenHash:   "hash",
	})
	if err != nil {
		t.Fatalf("ImportCredentials: %v", err)
	}
	if repo.created.Status != oauth.OAuthStateCompleted {
		t.Errorf("Status = %s, want %s", repo.created.Status, oauth.OAuthStateCompleted)
	}
}

func TestImportCredentials_NoRawCookieInTokenHash(t *testing.T) {
	repo := &mockRepo{}
	flow := NewFlow(repo)
	pid := uuid.New()

	rawCookie := "super-secret-session-cookie-value"
	_, err := flow.ImportCredentials(context.Background(), pid, "kimchi", &CookieCredentials{
		CookieName:  "session",
		CookieValue: rawCookie,
		Domain:      "example.com",
		TokenHash:   "hash-of-cookie-not-raw",
	})
	if err != nil {
		t.Fatalf("ImportCredentials: %v", err)
	}
	if repo.created.TokenHash != nil && *repo.created.TokenHash == rawCookie {
		t.Error("TokenHash must not contain the raw cookie value")
	}
}

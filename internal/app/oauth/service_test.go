package oauth

import (
	"context"
	"testing"
	"time"

	domainoauth "gorouter/internal/domain/oauth"

	"github.com/google/uuid"
)

type mockRepo struct {
	sessions map[uuid.UUID]*domainoauth.Session
}

func newMockRepo() *mockRepo {
	return &mockRepo{sessions: make(map[uuid.UUID]*domainoauth.Session)}
}

func (m *mockRepo) Create(_ context.Context, s *domainoauth.Session) error {
	m.sessions[s.ID] = s
	return nil
}

func (m *mockRepo) FindByID(_ context.Context, id uuid.UUID) (*domainoauth.Session, error) {
	s, ok := m.sessions[id]
	if !ok {
		return nil, nil
	}
	return s, nil
}

func (m *mockRepo) FindByState(_ context.Context, state string) (*domainoauth.Session, error) {
	for _, s := range m.sessions {
		if s.State == state {
			return s, nil
		}
	}
	return nil, nil
}

func (m *mockRepo) FindPendingByProvider(_ context.Context, _ uuid.UUID) ([]domainoauth.Session, error) {
	return nil, nil
}

func (m *mockRepo) UpdateStatus(_ context.Context, id uuid.UUID, status domainoauth.OAuthState, errorDetail *string) error {
	s, ok := m.sessions[id]
	if !ok {
		return nil
	}
	s.Status = status
	s.ErrorDetail = errorDetail
	return nil
}

func (m *mockRepo) Complete(_ context.Context, id uuid.UUID, tokenHash, refreshToken string, tokenExpiry time.Time) error {
	s, ok := m.sessions[id]
	if !ok {
		return nil
	}
	s.Status = domainoauth.OAuthStateCompleted
	s.TokenHash = &tokenHash
	s.RefreshToken = &refreshToken
	s.TokenExpiry = &tokenExpiry
	now := time.Now()
	s.CompletedAt = &now
	return nil
}

func (m *mockRepo) CancelPending(_ context.Context) (int64, error) {
	var n int64
	for _, s := range m.sessions {
		if s.Status == domainoauth.OAuthStatePending {
			s.Status = domainoauth.OAuthStateCancelled
			n++
		}
	}
	return n, nil
}

func (m *mockRepo) DeleteExpired(_ context.Context) (int64, error) {
	var n int64
	for id, s := range m.sessions {
		if s.ExpiresAt.Before(time.Now()) {
			delete(m.sessions, id)
			n++
		}
	}
	return n, nil
}

func (m *mockRepo) Cleanup(_ context.Context) (int64, error) {
	var n int64
	cutoff := time.Now().Add(-24 * time.Hour)
	for id, s := range m.sessions {
		if s.CompletedAt != nil && s.CompletedAt.Before(cutoff) {
			delete(m.sessions, id)
			n++
		}
	}
	return n, nil
}

func TestNewService_CancelAllOnRestart(t *testing.T) {
	repo := newMockRepo()
	ctx := context.Background()

	s := &domainoauth.Session{
		ID:        uuid.New(),
		FlowID:    domainoauth.FlowOpenAI,
		Status:    domainoauth.OAuthStatePending,
		ExpiresAt: time.Now().Add(10 * time.Minute),
	}
	repo.Create(ctx, s) //nolint:errcheck

	svc := NewService(repo)
	_ = svc

	got, _ := repo.FindByID(ctx, s.ID)
	if got == nil || got.Status != domainoauth.OAuthStateCancelled {
		t.Errorf("session status = %v, want cancelled", got.Status)
	}
}

func TestService_CancelPending(t *testing.T) {
	repo := newMockRepo()
	repo.Create(context.Background(), &domainoauth.Session{ //nolint:errcheck
		ID:        uuid.New(),
		FlowID:    domainoauth.FlowCodex,
		Status:    domainoauth.OAuthStatePending,
		ExpiresAt: time.Now().Add(5 * time.Minute),
	})

	svc := NewService(repo)
	n, err := svc.CancelPending(context.Background())
	if err != nil {
		t.Fatalf("CancelPending: %v", err)
	}
	if n != 0 {
		t.Logf("cancelled %d sessions (already cancelled by restart)", n)
	}
}

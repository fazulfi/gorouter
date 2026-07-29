package auth

import (
	"context"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
)

// inMemorySessionRepo is a thread-safe in-memory session repository for testing.
type inMemorySessionRepo struct {
	mu       sync.RWMutex
	sessions map[uuid.UUID]*Session
	byHash   map[string]*Session
}

func newInMemorySessionRepo() *inMemorySessionRepo {
	return &inMemorySessionRepo{
		sessions: make(map[uuid.UUID]*Session),
		byHash:   make(map[string]*Session),
	}
}

func (r *inMemorySessionRepo) FindByID(_ context.Context, id uuid.UUID) (*Session, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	s, ok := r.sessions[id]
	if !ok {
		return nil, nil
	}
	return s, nil
}

func (r *inMemorySessionRepo) FindByTokenHash(_ context.Context, hash string) (*Session, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	s, ok := r.byHash[hash]
	if !ok {
		return nil, nil
	}
	return s, nil
}

func (r *inMemorySessionRepo) Create(_ context.Context, s *Session) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.sessions[s.ID] = s
	r.byHash[s.TokenHash] = s
	return nil
}

func (r *inMemorySessionRepo) Revoke(_ context.Context, id uuid.UUID) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	s, ok := r.sessions[id]
	if !ok {
		return nil
	}
	now := time.Now()
	s.RevokedAt = &now
	return nil
}

func (r *inMemorySessionRepo) DeleteExpired(_ context.Context) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	now := time.Now()
	for id, s := range r.sessions {
		if s.ExpiresAt.Before(now) {
			delete(r.sessions, id)
			delete(r.byHash, s.TokenHash)
		}
	}
	return nil
}

func TestSessionService_Create(t *testing.T) {
	repo := newInMemorySessionRepo()
	svc := NewSessionService(repo, 24*time.Hour)

	userID := uuid.New()
	ip := net.ParseIP("192.168.1.1")
	userAgent := "test-agent"

	session, rawToken, err := svc.Create(context.Background(), userID, ip, userAgent, 0)
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	if session.UserID != userID {
		t.Errorf("expected UserID %v, got %v", userID, session.UserID)
	}
	if session.TokenHash == "" {
		t.Error("expected non-empty TokenHash")
	}
	if rawToken == "" {
		t.Error("expected non-empty rawToken")
	}
	if !session.IPAddress.Equal(ip) {
		t.Errorf("expected IP %v, got %v", ip, session.IPAddress)
	}
	if session.UserAgent != userAgent {
		t.Errorf("expected UserAgent %q, got %q", userAgent, session.UserAgent)
	}
	if session.ExpiresAt.Before(time.Now()) {
		t.Error("session already expired")
	}
	if session.RevokedAt != nil {
		t.Error("new session should not be revoked")
	}
}

func TestSessionService_Create_CustomDuration(t *testing.T) {
	repo := newInMemorySessionRepo()
	svc := NewSessionService(repo, 24*time.Hour)

	userID := uuid.New()
	session, _, err := svc.Create(context.Background(), userID, nil, "", 1*time.Hour)
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	expectedExpiry := time.Now().Add(1 * time.Hour)
	if session.ExpiresAt.After(expectedExpiry.Add(2*time.Second)) || session.ExpiresAt.Before(expectedExpiry.Add(-2*time.Second)) {
		t.Errorf("expected expiry near %v, got %v", expectedExpiry, session.ExpiresAt)
	}
}

func TestSessionService_Validate_Valid(t *testing.T) {
	repo := newInMemorySessionRepo()
	svc := NewSessionService(repo, 24*time.Hour)

	userID := uuid.New()
	_, rawToken, err := svc.Create(context.Background(), userID, nil, "", 1*time.Hour)
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	session, err := svc.Validate(context.Background(), rawToken)
	if err != nil {
		t.Fatalf("Validate failed: %v", err)
	}
	if session.UserID != userID {
		t.Errorf("expected UserID %v, got %v", userID, session.UserID)
	}
}

func TestSessionService_Validate_Expired(t *testing.T) {
	repo := newInMemorySessionRepo()
	svc := NewSessionService(repo, 24*time.Hour)

	userID := uuid.New()
	// Create with 1ns duration - effectively already expired by the time we validate
	_, rawToken, err := svc.Create(context.Background(), userID, nil, "", 1*time.Nanosecond)
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	// Brief sleep ensures the 1ns window has passed
	time.Sleep(5 * time.Millisecond)

	_, err = svc.Validate(context.Background(), rawToken)
	if err == nil {
		t.Fatal("expected error for expired session, got nil")
	}
}

func TestSessionService_Validate_Revoked(t *testing.T) {
	repo := newInMemorySessionRepo()
	svc := NewSessionService(repo, 24*time.Hour)

	userID := uuid.New()
	session, rawToken, err := svc.Create(context.Background(), userID, nil, "", 1*time.Hour)
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	if err := svc.Revoke(context.Background(), session.ID); err != nil {
		t.Fatalf("Revoke failed: %v", err)
	}

	_, err = svc.Validate(context.Background(), rawToken)
	if err == nil {
		t.Fatal("expected error for revoked session, got nil")
	}
}

func TestSessionService_Validate_InvalidToken(t *testing.T) {
	repo := newInMemorySessionRepo()
	svc := NewSessionService(repo, 24*time.Hour)

	_, err := svc.Validate(context.Background(), "this-token-does-not-exist")
	if err == nil {
		t.Fatal("expected error for invalid token, got nil")
	}
}

func TestSessionService_Revoke(t *testing.T) {
	repo := newInMemorySessionRepo()
	svc := NewSessionService(repo, 24*time.Hour)

	userID := uuid.New()
	session, _, err := svc.Create(context.Background(), userID, nil, "", 1*time.Hour)
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	if err := svc.Revoke(context.Background(), session.ID); err != nil {
		t.Fatalf("Revoke failed: %v", err)
	}

	// Verify revoked in repo
	stored, err := repo.FindByID(context.Background(), session.ID)
	if err != nil {
		t.Fatalf("FindByID failed: %v", err)
	}
	if stored.RevokedAt == nil {
		t.Fatal("expected RevokedAt to be set after revoke")
	}
}

func TestSessionService_Create_RawTokenNotStored(t *testing.T) {
	repo := newInMemorySessionRepo()
	svc := NewSessionService(repo, 24*time.Hour)

	_, rawToken, err := svc.Create(context.Background(), uuid.New(), nil, "", 1*time.Hour)
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	// The raw token should NOT be stored - only its hash
	for _, s := range repo.sessions {
		if s.TokenHash == rawToken {
			t.Fatal("raw token must not be stored directly; only hash should be stored")
		}
	}
}

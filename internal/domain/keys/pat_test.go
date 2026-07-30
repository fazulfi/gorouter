package keys

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
)

type inMemoryPATRepo struct {
	mu     sync.RWMutex
	pats   map[uuid.UUID]*PAT
	byHash map[string]*PAT
}

func newInMemoryPATRepo() *inMemoryPATRepo {
	return &inMemoryPATRepo{
		pats:   make(map[uuid.UUID]*PAT),
		byHash: make(map[string]*PAT),
	}
}

func (r *inMemoryPATRepo) FindByID(_ context.Context, id uuid.UUID) (*PAT, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	p, ok := r.pats[id]
	if !ok {
		return nil, nil
	}
	return p, nil
}

func (r *inMemoryPATRepo) FindByHash(_ context.Context, hash string) (*PAT, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	p, ok := r.byHash[hash]
	if !ok {
		return nil, nil
	}
	return p, nil
}

func (r *inMemoryPATRepo) FindByUserID(_ context.Context, userID uuid.UUID) ([]PAT, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var out []PAT
	for _, p := range r.pats {
		if p.UserID == userID {
			out = append(out, *p)
		}
	}
	return out, nil
}

func (r *inMemoryPATRepo) Create(_ context.Context, pat *PAT) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.pats[pat.ID] = pat
	r.byHash[pat.TokenHash] = pat
	return nil
}

func (r *inMemoryPATRepo) Revoke(_ context.Context, id uuid.UUID) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	p, ok := r.pats[id]
	if !ok {
		return nil
	}
	now := time.Now()
	p.RevokedAt = &now
	return nil
}

func TestPATService_Create(t *testing.T) {
	repo := newInMemoryPATRepo()
	svc := NewPATService(repo)

	userID := uuid.New()
	desc := "my-pat"
	expiresAt := time.Now().Add(30 * 24 * time.Hour)

	pat, rawToken, err := svc.Create(context.Background(), userID, &desc, &expiresAt)
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	if pat.UserID != userID {
		t.Errorf("expected UserID %v, got %v", userID, pat.UserID)
	}
	if pat.Description == nil || *pat.Description != desc {
		t.Errorf("expected Description %q, got %v", desc, pat.Description)
	}
	if rawToken == "" {
		t.Fatal("expected non-empty rawToken")
	}
	if pat.TokenHash == "" {
		t.Fatal("expected non-empty TokenHash")
	}
}

func TestPATService_Create_DefaultsPrefix(t *testing.T) {
	repo := newInMemoryPATRepo()
	svc := NewPATService(repo)

	_, rawToken, err := svc.Create(context.Background(), uuid.New(), nil, nil)
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	expectedPrefix := "gorouter_pat_"
	if len(rawToken) < len(expectedPrefix) || rawToken[:len(expectedPrefix)] != expectedPrefix {
		t.Errorf("expected token to start with %q, got %q", expectedPrefix, rawToken)
	}
}

func TestPATService_Create_RawTokenNotStored(t *testing.T) {
	repo := newInMemoryPATRepo()
	svc := NewPATService(repo)

	_, rawToken, err := svc.Create(context.Background(), uuid.New(), nil, nil)
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	for _, p := range repo.pats {
		if p.TokenHash == rawToken {
			t.Fatal("raw token must not be stored directly; only hash should be stored")
		}
	}
}

func TestPATService_Validate_Valid(t *testing.T) {
	repo := newInMemoryPATRepo()
	svc := NewPATService(repo)

	userID := uuid.New()
	future := time.Now().Add(30 * 24 * time.Hour)
	_, rawToken, err := svc.Create(context.Background(), userID, nil, &future)
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	validated, err := svc.Validate(context.Background(), rawToken)
	if err != nil {
		t.Fatalf("Validate failed: %v", err)
	}
	if validated.UserID != userID {
		t.Errorf("expected UserID %v, got %v", userID, validated.UserID)
	}
}

func TestPATService_Validate_WrongToken(t *testing.T) {
	repo := newInMemoryPATRepo()
	svc := NewPATService(repo)

	_, err := svc.Validate(context.Background(), "gorouter_pat_invalidtoken")
	if err == nil {
		t.Fatal("expected error for invalid token, got nil")
	}
}

func TestPATService_Validate_Expired(t *testing.T) {
	repo := newInMemoryPATRepo()
	svc := NewPATService(repo)

	past := time.Now().Add(-1 * time.Hour)
	_, rawToken, err := svc.Create(context.Background(), uuid.New(), nil, &past)
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	_, err = svc.Validate(context.Background(), rawToken)
	if err == nil {
		t.Fatal("expected error for expired PAT, got nil")
	}
}

func TestPATService_Validate_Revoked(t *testing.T) {
	repo := newInMemoryPATRepo()
	svc := NewPATService(repo)

	future := time.Now().Add(30 * 24 * time.Hour)
	pat, rawToken, err := svc.Create(context.Background(), uuid.New(), nil, &future)
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	if err := svc.Revoke(context.Background(), pat.ID); err != nil {
		t.Fatalf("Revoke failed: %v", err)
	}

	_, err = svc.Validate(context.Background(), rawToken)
	if err == nil {
		t.Fatal("expected error for revoked PAT, got nil")
	}
}

func TestPATService_Validate_NoExpiry(t *testing.T) {
	repo := newInMemoryPATRepo()
	svc := NewPATService(repo)

	_, rawToken, err := svc.Create(context.Background(), uuid.New(), nil, nil)
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	_, err = svc.Validate(context.Background(), rawToken)
	if err != nil {
		t.Fatalf("Validate failed for token with no expiry: %v", err)
	}
}

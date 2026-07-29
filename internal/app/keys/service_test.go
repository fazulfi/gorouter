package keys

import (
	"context"
	"sync"
	"testing"
	"time"

	domain "gorouter/internal/domain/keys"

	"github.com/google/uuid"
)

type mockAPIKeyRepo struct {
	mu     sync.RWMutex
	keys   map[uuid.UUID]*domain.APIKey
	byHash map[string]*domain.APIKey
}

func newMockAPIKeyRepo() *mockAPIKeyRepo {
	return &mockAPIKeyRepo{
		keys:   make(map[uuid.UUID]*domain.APIKey),
		byHash: make(map[string]*domain.APIKey),
	}
}

func (r *mockAPIKeyRepo) FindByID(_ context.Context, id uuid.UUID) (*domain.APIKey, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	k, ok := r.keys[id]
	if !ok {
		return nil, nil
	}
	return k, nil
}

func (r *mockAPIKeyRepo) FindByHash(_ context.Context, hash string) (*domain.APIKey, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	k, ok := r.byHash[hash]
	if !ok {
		return nil, nil
	}
	return k, nil
}

func (r *mockAPIKeyRepo) FindByUserID(_ context.Context, userID uuid.UUID) ([]domain.APIKey, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var out []domain.APIKey
	for _, k := range r.keys {
		if k.UserID == userID {
			out = append(out, *k)
		}
	}
	return out, nil
}

func (r *mockAPIKeyRepo) Create(_ context.Context, key *domain.APIKey) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.keys[key.ID] = key
	r.byHash[key.KeyHash] = key
	return nil
}

func (r *mockAPIKeyRepo) Revoke(_ context.Context, id uuid.UUID) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	k, ok := r.keys[id]
	if !ok {
		return nil
	}
	now := time.Now()
	k.RevokedAt = &now
	return nil
}

type mockPATRepo struct {
	mu     sync.RWMutex
	pats   map[uuid.UUID]*domain.PAT
	byHash map[string]*domain.PAT
}

func newMockPATRepo() *mockPATRepo {
	return &mockPATRepo{
		pats:   make(map[uuid.UUID]*domain.PAT),
		byHash: make(map[string]*domain.PAT),
	}
}

func (r *mockPATRepo) FindByID(_ context.Context, id uuid.UUID) (*domain.PAT, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	p, ok := r.pats[id]
	if !ok {
		return nil, nil
	}
	return p, nil
}

func (r *mockPATRepo) FindByHash(_ context.Context, hash string) (*domain.PAT, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	p, ok := r.byHash[hash]
	if !ok {
		return nil, nil
	}
	return p, nil
}

func (r *mockPATRepo) FindByUserID(_ context.Context, userID uuid.UUID) ([]domain.PAT, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var out []domain.PAT
	for _, p := range r.pats {
		if p.UserID == userID {
			out = append(out, *p)
		}
	}
	return out, nil
}

func (r *mockPATRepo) Create(_ context.Context, pat *domain.PAT) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.pats[pat.ID] = pat
	r.byHash[pat.TokenHash] = pat
	return nil
}

func (r *mockPATRepo) Revoke(_ context.Context, id uuid.UUID) error {
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

func TestKeyService_CreateAPIKey(t *testing.T) {
	apiKeyRepo := newMockAPIKeyRepo()
	patRepo := newMockPATRepo()
	modelSvc := domain.NewModelKeyService(apiKeyRepo)
	patSvc := domain.NewPATService(patRepo)
	svc := NewKeyService(apiKeyRepo, patRepo, modelSvc, patSvc)

	userID := uuid.New()
	future := time.Now().Add(30 * 24 * time.Hour)

	apiKey, rawKey, err := svc.CreateAPIKey(context.Background(), userID, "my-key", &future)
	if err != nil {
		t.Fatalf("CreateAPIKey failed: %v", err)
	}
	if apiKey == nil {
		t.Fatal("expected non-nil APIKey")
	}
	if rawKey == "" {
		t.Fatal("expected non-empty rawKey")
	}
	if apiKey.Name != "my-key" {
		t.Errorf("expected Name %q, got %q", "my-key", apiKey.Name)
	}
}

func TestKeyService_ValidateAPIKey(t *testing.T) {
	apiKeyRepo := newMockAPIKeyRepo()
	patRepo := newMockPATRepo()
	modelSvc := domain.NewModelKeyService(apiKeyRepo)
	patSvc := domain.NewPATService(patRepo)
	svc := NewKeyService(apiKeyRepo, patRepo, modelSvc, patSvc)

	userID := uuid.New()
	future := time.Now().Add(30 * 24 * time.Hour)
	_, rawKey, err := svc.CreateAPIKey(context.Background(), userID, "valid-key", &future)
	if err != nil {
		t.Fatalf("CreateAPIKey failed: %v", err)
	}

	validated, err := svc.ValidateAPIKey(context.Background(), rawKey)
	if err != nil {
		t.Fatalf("ValidateAPIKey failed: %v", err)
	}
	if validated.UserID != userID {
		t.Errorf("expected UserID %v, got %v", userID, validated.UserID)
	}
}

func TestKeyService_ValidateAPIKey_Invalid(t *testing.T) {
	apiKeyRepo := newMockAPIKeyRepo()
	patRepo := newMockPATRepo()
	modelSvc := domain.NewModelKeyService(apiKeyRepo)
	patSvc := domain.NewPATService(patRepo)
	svc := NewKeyService(apiKeyRepo, patRepo, modelSvc, patSvc)

	_, err := svc.ValidateAPIKey(context.Background(), "nonexistent-key")
	if err == nil {
		t.Fatal("expected error for invalid API key, got nil")
	}
}

func TestKeyService_RevokeAPIKey(t *testing.T) {
	apiKeyRepo := newMockAPIKeyRepo()
	patRepo := newMockPATRepo()
	modelSvc := domain.NewModelKeyService(apiKeyRepo)
	patSvc := domain.NewPATService(patRepo)
	svc := NewKeyService(apiKeyRepo, patRepo, modelSvc, patSvc)

	future := time.Now().Add(30 * 24 * time.Hour)
	apiKey, rawKey, err := svc.CreateAPIKey(context.Background(), uuid.New(), "revokable", &future)
	if err != nil {
		t.Fatalf("CreateAPIKey failed: %v", err)
	}

	if err := svc.RevokeAPIKey(context.Background(), apiKey.ID); err != nil {
		t.Fatalf("RevokeAPIKey failed: %v", err)
	}

	_, err = svc.ValidateAPIKey(context.Background(), rawKey)
	if err == nil {
		t.Fatal("expected error for revoked API key, got nil")
	}
}

func TestKeyService_CreatePAT(t *testing.T) {
	apiKeyRepo := newMockAPIKeyRepo()
	patRepo := newMockPATRepo()
	modelSvc := domain.NewModelKeyService(apiKeyRepo)
	patSvc := domain.NewPATService(patRepo)
	svc := NewKeyService(apiKeyRepo, patRepo, modelSvc, patSvc)

	userID := uuid.New()
	desc := "my-pat"
	future := time.Now().Add(30 * 24 * time.Hour)

	pat, rawToken, err := svc.CreatePAT(context.Background(), userID, &desc, &future)
	if err != nil {
		t.Fatalf("CreatePAT failed: %v", err)
	}
	if pat == nil {
		t.Fatal("expected non-nil PAT")
	}
	if rawToken == "" {
		t.Fatal("expected non-empty rawToken")
	}
	if pat.Description == nil || *pat.Description != desc {
		t.Errorf("expected Description %q, got %v", desc, pat.Description)
	}
}

func TestKeyService_ValidatePAT(t *testing.T) {
	apiKeyRepo := newMockAPIKeyRepo()
	patRepo := newMockPATRepo()
	modelSvc := domain.NewModelKeyService(apiKeyRepo)
	patSvc := domain.NewPATService(patRepo)
	svc := NewKeyService(apiKeyRepo, patRepo, modelSvc, patSvc)

	userID := uuid.New()
	future := time.Now().Add(30 * 24 * time.Hour)
	_, rawToken, err := svc.CreatePAT(context.Background(), userID, nil, &future)
	if err != nil {
		t.Fatalf("CreatePAT failed: %v", err)
	}

	validated, err := svc.ValidatePAT(context.Background(), rawToken)
	if err != nil {
		t.Fatalf("ValidatePAT failed: %v", err)
	}
	if validated.UserID != userID {
		t.Errorf("expected UserID %v, got %v", userID, validated.UserID)
	}
}

func TestKeyService_ValidatePAT_Invalid(t *testing.T) {
	apiKeyRepo := newMockAPIKeyRepo()
	patRepo := newMockPATRepo()
	modelSvc := domain.NewModelKeyService(apiKeyRepo)
	patSvc := domain.NewPATService(patRepo)
	svc := NewKeyService(apiKeyRepo, patRepo, modelSvc, patSvc)

	_, err := svc.ValidatePAT(context.Background(), "gorouter_pat_invalid")
	if err == nil {
		t.Fatal("expected error for invalid PAT, got nil")
	}
}

func TestKeyService_RevokePAT(t *testing.T) {
	apiKeyRepo := newMockAPIKeyRepo()
	patRepo := newMockPATRepo()
	modelSvc := domain.NewModelKeyService(apiKeyRepo)
	patSvc := domain.NewPATService(patRepo)
	svc := NewKeyService(apiKeyRepo, patRepo, modelSvc, patSvc)

	future := time.Now().Add(30 * 24 * time.Hour)
	pat, rawToken, err := svc.CreatePAT(context.Background(), uuid.New(), nil, &future)
	if err != nil {
		t.Fatalf("CreatePAT failed: %v", err)
	}

	if err := svc.RevokePAT(context.Background(), pat.ID); err != nil {
		t.Fatalf("RevokePAT failed: %v", err)
	}

	_, err = svc.ValidatePAT(context.Background(), rawToken)
	if err == nil {
		t.Fatal("expected error for revoked PAT, got nil")
	}
}

package keys

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
)

type inMemoryAPIKeyRepo struct {
	mu   sync.RWMutex
	keys map[uuid.UUID]*APIKey
	byHash map[string]*APIKey
}

func newInMemoryAPIKeyRepo() *inMemoryAPIKeyRepo {
	return &inMemoryAPIKeyRepo{
		keys:   make(map[uuid.UUID]*APIKey),
		byHash: make(map[string]*APIKey),
	}
}

func (r *inMemoryAPIKeyRepo) FindByID(_ context.Context, id uuid.UUID) (*APIKey, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	k, ok := r.keys[id]
	if !ok {
		return nil, nil
	}
	return k, nil
}

func (r *inMemoryAPIKeyRepo) FindByHash(_ context.Context, hash string) (*APIKey, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	k, ok := r.byHash[hash]
	if !ok {
		return nil, nil
	}
	return k, nil
}

func (r *inMemoryAPIKeyRepo) FindByUserID(_ context.Context, userID uuid.UUID) ([]APIKey, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var out []APIKey
	for _, k := range r.keys {
		if k.UserID == userID {
			out = append(out, *k)
		}
	}
	return out, nil
}

func (r *inMemoryAPIKeyRepo) Create(_ context.Context, key *APIKey) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.keys[key.ID] = key
	r.byHash[key.KeyHash] = key
	return nil
}

func (r *inMemoryAPIKeyRepo) Revoke(_ context.Context, id uuid.UUID) error {
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

func TestModelKeyService_Generate(t *testing.T) {
	repo := newInMemoryAPIKeyRepo()
	svc := NewModelKeyService(repo)

	userID := uuid.New()
	expiresAt := time.Now().Add(30 * 24 * time.Hour)

	apiKey, rawKey, err := svc.Generate(context.Background(), userID, "test-key", &expiresAt)
	if err != nil {
		t.Fatalf("Generate failed: %v", err)
	}

	if apiKey.UserID != userID {
		t.Errorf("expected UserID %v, got %v", userID, apiKey.UserID)
	}
	if apiKey.Name != "test-key" {
		t.Errorf("expected Name %q, got %q", "test-key", apiKey.Name)
	}
	if rawKey == "" {
		t.Fatal("expected non-empty rawKey")
	}
	if len(apiKey.KeyPrefix) != 8 {
		t.Errorf("expected KeyPrefix length 8, got %d", len(apiKey.KeyPrefix))
	}
	if apiKey.KeyHash == "" {
		t.Fatal("expected non-empty KeyHash")
	}
	if apiKey.ExpiresAt == nil || !apiKey.ExpiresAt.Equal(expiresAt) {
		t.Errorf("expected ExpiresAt %v, got %v", expiresAt, apiKey.ExpiresAt)
	}
}

func TestModelKeyService_Generate_NoExpiry(t *testing.T) {
	repo := newInMemoryAPIKeyRepo()
	svc := NewModelKeyService(repo)

	apiKey, rawKey, err := svc.Generate(context.Background(), uuid.New(), "no-expiry", nil)
	if err != nil {
		t.Fatalf("Generate failed: %v", err)
	}

	if rawKey == "" {
		t.Fatal("expected non-empty rawKey")
	}
	if apiKey.ExpiresAt != nil {
		t.Errorf("expected nil ExpiresAt, got %v", apiKey.ExpiresAt)
	}
}

func TestModelKeyService_Generate_RawKeyNotStored(t *testing.T) {
	repo := newInMemoryAPIKeyRepo()
	svc := NewModelKeyService(repo)

	_, rawKey, err := svc.Generate(context.Background(), uuid.New(), "test", nil)
	if err != nil {
		t.Fatalf("Generate failed: %v", err)
	}

	for _, k := range repo.keys {
		if k.KeyHash == rawKey {
			t.Fatal("raw key must not be stored directly; only hash should be stored")
		}
	}
}

func TestModelKeyService_Validate_Valid(t *testing.T) {
	repo := newInMemoryAPIKeyRepo()
	svc := NewModelKeyService(repo)

	userID := uuid.New()
	expiresAt := time.Now().Add(30 * 24 * time.Hour)
	_, rawKey, err := svc.Generate(context.Background(), userID, "valid-key", &expiresAt)
	if err != nil {
		t.Fatalf("Generate failed: %v", err)
	}

	validated, err := svc.Validate(context.Background(), rawKey)
	if err != nil {
		t.Fatalf("Validate failed: %v", err)
	}
	if validated.UserID != userID {
		t.Errorf("expected UserID %v, got %v", userID, validated.UserID)
	}
}

func TestModelKeyService_Validate_WrongKey(t *testing.T) {
	repo := newInMemoryAPIKeyRepo()
	svc := NewModelKeyService(repo)

	_, err := svc.Validate(context.Background(), "this-key-does-not-exist")
	if err == nil {
		t.Fatal("expected error for invalid key, got nil")
	}
}

func TestModelKeyService_Validate_Expired(t *testing.T) {
	repo := newInMemoryAPIKeyRepo()
	svc := NewModelKeyService(repo)

	past := time.Now().Add(-1 * time.Hour)
	_, rawKey, err := svc.Generate(context.Background(), uuid.New(), "expired-key", &past)
	if err != nil {
		t.Fatalf("Generate failed: %v", err)
	}

	_, err = svc.Validate(context.Background(), rawKey)
	if err == nil {
		t.Fatal("expected error for expired key, got nil")
	}
}

func TestModelKeyService_Validate_Revoked(t *testing.T) {
	repo := newInMemoryAPIKeyRepo()
	svc := NewModelKeyService(repo)

	future := time.Now().Add(30 * 24 * time.Hour)
	apiKey, rawKey, err := svc.Generate(context.Background(), uuid.New(), "revokable-key", &future)
	if err != nil {
		t.Fatalf("Generate failed: %v", err)
	}

	if err := svc.Revoke(context.Background(), apiKey.ID); err != nil {
		t.Fatalf("Revoke failed: %v", err)
	}

	_, err = svc.Validate(context.Background(), rawKey)
	if err == nil {
		t.Fatal("expected error for revoked key, got nil")
	}
}

func TestModelKeyService_Generate_DefaultsScopes(t *testing.T) {
	repo := newInMemoryAPIKeyRepo()
	svc := NewModelKeyService(repo)

	apiKey, _, err := svc.Generate(context.Background(), uuid.New(), "scoped", nil)
	if err != nil {
		t.Fatalf("Generate failed: %v", err)
	}
	if apiKey.Scopes == nil {
		t.Fatal("expected non-nil Scopes")
	}
}

package keys

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
)

var (
	ErrKeyExpired  = errors.New("API key has expired")
	ErrKeyRevoked  = errors.New("API key has been revoked")
	ErrKeyNotFound = errors.New("API key not found")
)

const (
	modelKeyBytes     = 48
	modelKeyPrefixLen = 8
)

// ModelKeyService manages model-scoped API key generation, validation, and revocation.
type ModelKeyService struct {
	repo APIKeyRepository
}

// NewModelKeyService creates a ModelKeyService backed by the given repository.
func NewModelKeyService(repo APIKeyRepository) *ModelKeyService {
	return &ModelKeyService{repo: repo}
}

// Generate creates a new API key. It produces a cryptographically random key,
// stores only its SHA-256 hash, and returns the raw key exactly once.
func (s *ModelKeyService) Generate(ctx context.Context, userID uuid.UUID, name string, expiresAt *time.Time) (*APIKey, string, error) {
	rawKey, err := generateBase64URLKey(modelKeyBytes)
	if err != nil {
		return nil, "", fmt.Errorf("generate key: %w", err)
	}

	prefix := rawKey[:modelKeyPrefixLen]
	keyHash := hashSHA256(rawKey)

	now := time.Now()
	apiKey := &APIKey{
		ID:        uuid.New(),
		UserID:    userID,
		KeyPrefix: prefix,
		KeyHash:   keyHash,
		Name:      name,
		Scopes:    []string{},
		ExpiresAt: expiresAt,
		CreatedAt: now,
	}

	if err := s.repo.Create(ctx, apiKey); err != nil {
		return nil, "", fmt.Errorf("persist key: %w", err)
	}

	return apiKey, rawKey, nil
}

// Validate checks that the raw key corresponds to a valid (non-expired, non-revoked) API key.
func (s *ModelKeyService) Validate(ctx context.Context, rawKey string) (*APIKey, error) {
	keyHash := hashSHA256(rawKey)

	apiKey, err := s.repo.FindByHash(ctx, keyHash)
	if err != nil {
		return nil, fmt.Errorf("lookup key: %w", err)
	}
	if apiKey == nil {
		return nil, ErrKeyNotFound
	}

	if apiKey.ExpiresAt != nil && time.Now().After(*apiKey.ExpiresAt) {
		return nil, ErrKeyExpired
	}

	if apiKey.RevokedAt != nil {
		return nil, ErrKeyRevoked
	}

	return apiKey, nil
}

// Revoke marks an API key as revoked.
func (s *ModelKeyService) Revoke(ctx context.Context, id uuid.UUID) error {
	return s.repo.Revoke(ctx, id)
}

func generateBase64URLKey(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func hashSHA256(input string) string {
	h := sha256.Sum256([]byte(input))
	return hex.EncodeToString(h[:])
}

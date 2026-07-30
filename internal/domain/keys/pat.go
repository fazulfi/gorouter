package keys

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
)

var (
	ErrPATExpired  = errors.New("personal access token has expired")
	ErrPATRevoked  = errors.New("personal access token has been revoked")
	ErrPATNotFound = errors.New("personal access token not found")
)

const (
	patPrefix      = "gorouter_pat_"
	patRandomBytes = 32
)

// PATService manages personal access token creation, validation, and revocation.
type PATService struct {
	repo PATRepository
}

// NewPATService creates a PATService backed by the given repository.
func NewPATService(repo PATRepository) *PATService {
	return &PATService{repo: repo}
}

// Create generates a new personal access token. It stores only the SHA-256 hash
// and returns the raw token exactly once. Tokens are prefixed with "gorouter_pat_".
func (s *PATService) Create(ctx context.Context, userID uuid.UUID, description *string, expiresAt *time.Time) (*PAT, string, error) {
	randomHex, err := generateRandomHex(patRandomBytes)
	if err != nil {
		return nil, "", fmt.Errorf("generate PAT: %w", err)
	}

	rawToken := patPrefix + randomHex
	tokenHash := hashToken(rawToken)

	now := time.Now()
	pat := &PAT{
		ID:          uuid.New(),
		UserID:      userID,
		TokenHash:   tokenHash,
		Description: description,
		ExpiresAt:   expiresAt,
		CreatedAt:   now,
	}

	if err := s.repo.Create(ctx, pat); err != nil {
		return nil, "", fmt.Errorf("persist PAT: %w", err)
	}

	return pat, rawToken, nil
}

// Validate checks that the raw token corresponds to a valid (non-expired, non-revoked) PAT.
func (s *PATService) Validate(ctx context.Context, rawToken string) (*PAT, error) {
	tokenHash := hashToken(rawToken)

	pat, err := s.repo.FindByHash(ctx, tokenHash)
	if err != nil {
		return nil, fmt.Errorf("lookup PAT: %w", err)
	}
	if pat == nil {
		return nil, ErrPATNotFound
	}

	if pat.ExpiresAt != nil && time.Now().After(*pat.ExpiresAt) {
		return nil, ErrPATExpired
	}

	if pat.RevokedAt != nil {
		return nil, ErrPATRevoked
	}

	return pat, nil
}

// Revoke marks a PAT as revoked.
func (s *PATService) Revoke(ctx context.Context, id uuid.UUID) error {
	return s.repo.Revoke(ctx, id)
}

func generateRandomHex(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func hashToken(token string) string {
	h := sha256.Sum256([]byte(token))
	return hex.EncodeToString(h[:])
}

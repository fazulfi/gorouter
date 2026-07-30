// Package keys provides the application-layer key management service that
// orchestrates API key and PAT creation, validation, and revocation.
package keys

import (
	"context"
	"time"

	domain "gorouter/internal/domain/keys"

	"github.com/google/uuid"
)

// Service is the application-layer key management service.
type Service struct {
	apiKeyRepo domain.APIKeyRepository
	patRepo    domain.PATRepository
	modelSvc   *domain.ModelKeyService
	patSvc     *domain.PATService
}

// NewKeyService creates a KeyService with the given dependencies.
func NewKeyService(
	apiKeyRepo domain.APIKeyRepository,
	patRepo domain.PATRepository,
	modelSvc *domain.ModelKeyService,
	patSvc *domain.PATService,
) *Service {
	return &Service{
		apiKeyRepo: apiKeyRepo,
		patRepo:    patRepo,
		modelSvc:   modelSvc,
		patSvc:     patSvc,
	}
}

// CreateAPIKey creates a new API key for the given user.
func (s *Service) CreateAPIKey(ctx context.Context, userID uuid.UUID, name string, expiresAt *time.Time) (*domain.APIKey, string, error) {
	return s.modelSvc.Generate(ctx, userID, name, expiresAt)
}

// ValidateAPIKey validates a raw API key string.
func (s *Service) ValidateAPIKey(ctx context.Context, rawKey string) (*domain.APIKey, error) {
	return s.modelSvc.Validate(ctx, rawKey)
}

// RevokeAPIKey revokes an API key by ID.
func (s *Service) RevokeAPIKey(ctx context.Context, id uuid.UUID) error {
	return s.modelSvc.Revoke(ctx, id)
}

// CreatePAT creates a new personal access token for the given user.
func (s *Service) CreatePAT(ctx context.Context, userID uuid.UUID, description *string, expiresAt *time.Time) (*domain.PAT, string, error) {
	return s.patSvc.Create(ctx, userID, description, expiresAt)
}

// ValidatePAT validates a raw PAT string.
func (s *Service) ValidatePAT(ctx context.Context, rawToken string) (*domain.PAT, error) {
	return s.patSvc.Validate(ctx, rawToken)
}

// RevokePAT revokes a PAT by ID.
func (s *Service) RevokePAT(ctx context.Context, id uuid.UUID) error {
	return s.patSvc.Revoke(ctx, id)
}

// Package keys provides domain types for API keys and personal access tokens.
package keys

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// APIKey represents a user-level API key used for programmatic access.
type APIKey struct {
	ID         uuid.UUID
	UserID     uuid.UUID
	KeyPrefix  string
	KeyHash    string
	Name       string
	Scopes     []string
	ExpiresAt  *time.Time
	LastUsedAt *time.Time
	RevokedAt  *time.Time
	CreatedAt  time.Time
}

// PAT represents a personal access token tied to a user account.
type PAT struct {
	ID          uuid.UUID
	UserID      uuid.UUID
	TokenHash   string
	Description *string
	ExpiresAt   *time.Time
	LastUsedAt  *time.Time
	RevokedAt   *time.Time
	CreatedAt   time.Time
}

// ModelAPIKey represents an API key configured for a specific provider model.
type ModelAPIKey struct {
	ID         uuid.UUID
	ProviderID uuid.UUID
	KeyPrefix  string
	KeyHash    string
	Label      string
	ExpiresAt  *time.Time
	LastUsedAt *time.Time
	CreatedAt  time.Time
}

// KeyScope defines a permission scope for API keys.
type KeyScope string

const (
	ScopeReadKeys   KeyScope = "keys:read"
	ScopeWriteKeys  KeyScope = "keys:write"
	ScopeReadProxy  KeyScope = "proxy:read"
	ScopeWriteProxy KeyScope = "proxy:write"
	ScopeAdmin      KeyScope = "admin"
)

// ModelAPIKeyConfig represents configuration for model-scoped API keys.
type ModelAPIKeyConfig struct {
	ID         uuid.UUID
	ModelID    string
	APIKeyID   uuid.UUID
	Priority   int
	MaxTokens  int64
	AllowedIPs []string
	IsActive   bool
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

// APIKeyRepository defines persistence operations for API keys.
type APIKeyRepository interface {
	FindByID(ctx context.Context, id uuid.UUID) (*APIKey, error)
	FindByHash(ctx context.Context, hash string) (*APIKey, error)
	FindByUserID(ctx context.Context, userID uuid.UUID) ([]APIKey, error)
	Create(ctx context.Context, key *APIKey) error
	Revoke(ctx context.Context, id uuid.UUID) error
}

// PATRepository defines persistence operations for personal access tokens.
type PATRepository interface {
	FindByID(ctx context.Context, id uuid.UUID) (*PAT, error)
	FindByHash(ctx context.Context, hash string) (*PAT, error)
	FindByUserID(ctx context.Context, userID uuid.UUID) ([]PAT, error)
	Create(ctx context.Context, pat *PAT) error
	Revoke(ctx context.Context, id uuid.UUID) error
}

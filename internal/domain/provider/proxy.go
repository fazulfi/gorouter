package provider

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// ProxyConfig holds proxy/transport settings for an account.
//
// Password is stored as restricted plaintext per decision #120 — the field
// MUST be redacted in logs and API responses.
type ProxyConfig struct {
	ID        uuid.UUID
	AccountID uuid.UUID
	URL       string
	Username  string
	Password  string
	IsEnabled bool
	Priority  int
	CreatedAt time.Time
	UpdatedAt time.Time
}

// ProxyRepository defines persistence operations for proxy configs.
type ProxyRepository interface {
	FindByAccountID(ctx context.Context, accountID uuid.UUID) ([]ProxyConfig, error)
	Create(ctx context.Context, cfg *ProxyConfig) error
	Update(ctx context.Context, cfg *ProxyConfig) error
	Delete(ctx context.Context, id uuid.UUID) error
}

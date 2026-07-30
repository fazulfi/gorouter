package provider

import (
	"time"

	"github.com/google/uuid"
)

// AccountStatus enumerates possible states for a provider account.
type AccountStatus string

const (
	AccountStatusActive    AccountStatus = "active"
	AccountStatusCooldown  AccountStatus = "cooldown"
	AccountStatusDegraded  AccountStatus = "degraded"
	AccountStatusDisabled  AccountStatus = "disabled"
)

// Account represents an upstream provider account (one API key/credential per provider).
type Account struct {
	ID            uuid.UUID
	ProviderID    uuid.UUID
	Label         string
	AuthType      string   // "api_key", "oauth", "cookie", "free"
	CredentialRef string
	Priority      int      // lower = higher priority (0 = highest)
	IsEnabled     bool
	MaxConcurrent int      // 0 = unlimited
	ModelFilters  []string
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

// AccountCooldown tracks the temporary disablement of an account after repeated failures.
type AccountCooldown struct {
	AccountID  uuid.UUID
	Reason     string
	StartedAt  time.Time
	ExpiresAt  time.Time
	RetryCount int
}

// IsOnCooldown returns true when the cooldown period has not yet expired.
func (c *AccountCooldown) IsOnCooldown() bool {
	return time.Now().Before(c.ExpiresAt)
}
